//go:build linux

package lokavm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// CHHypervisor implements the Hypervisor interface using Cloud Hypervisor.
// Cloud Hypervisor is a Rust-based VMM that supports virtiofs, vsock, and ARM64.
// It's the recommended Linux backend — more features than Firecracker, same security.
//
// Requires: cloud-hypervisor binary in PATH or at /usr/local/bin/cloud-hypervisor
type CHHypervisor struct {
	config  HypervisorConfig
	logger  *slog.Logger
	chBin   string

	mu  sync.RWMutex
	vms map[string]*chVM
}

type chVM struct {
	config  VMConfig
	state   VMState
	booted  time.Time
	process *exec.Cmd
	apiSock string // Cloud Hypervisor API socket path.
	vsockPath string
}

// NewCHHypervisor creates a Cloud Hypervisor-based hypervisor for Linux.
func NewCHHypervisor(config HypervisorConfig, logger *slog.Logger) (*CHHypervisor, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// Find cloud-hypervisor binary.
	chBin := ""
	for _, p := range []string{"cloud-hypervisor", "/usr/local/bin/cloud-hypervisor"} {
		if path, err := exec.LookPath(p); err == nil {
			chBin = path
			break
		}
	}
	if chBin == "" {
		return nil, fmt.Errorf("cloud-hypervisor binary not found")
	}

	return &CHHypervisor{
		config: config,
		logger: logger,
		chBin:  chBin,
		vms:    make(map[string]*chVM),
	}, nil
}

func (h *CHHypervisor) CreateVM(config VMConfig) (*VM, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.vms[config.ID]; exists {
		return nil, fmt.Errorf("VM %s already exists", config.ID)
	}

	vmDir := filepath.Join(h.config.DataDir, "vms", config.ID)
	os.MkdirAll(vmDir, 0o755)

	ch := &chVM{
		config:  config,
		state:   VMStateCreated,
		apiSock: filepath.Join(vmDir, "ch.sock"),
		vsockPath: filepath.Join(vmDir, "vsock.sock"),
	}
	h.vms[config.ID] = ch

	return h.toVM(ch), nil
}

func (h *CHHypervisor) StartVM(id string) error {
	h.mu.Lock()
	ch, ok := h.vms[id]
	h.mu.Unlock()
	if !ok {
		return fmt.Errorf("VM %s not found", id)
	}

	cfg := ch.config

	// Build Cloud Hypervisor command.
	args := []string{
		"--api-socket", ch.apiSock,
		"--kernel", h.config.KernelPath,
		"--cmdline", cfg.BootArgs,
		"--cpus", fmt.Sprintf("boot=%d,max=%d", cfg.VCPUsMin, cfg.VCPUsMax),
		"--memory", fmt.Sprintf("size=%dM", cfg.MemoryMaxMB),
		"--console", "off",
		"--serial", "tty",
	}

	if h.config.InitrdPath != "" {
		args = append(args, "--initramfs", h.config.InitrdPath)
	}

	// Drives.
	for _, d := range cfg.Drives {
		ro := "off"
		if d.ReadOnly {
			ro = "on"
		}
		args = append(args, "--disk", fmt.Sprintf("path=%s,readonly=%s", d.Path, ro))
	}

	// Virtiofs shared dirs (requires virtiofsd).
	if _, err := exec.LookPath("virtiofsd"); err == nil {
		for _, sd := range cfg.SharedDirs {
			sockPath := filepath.Join(filepath.Dir(ch.apiSock), sd.Tag+".virtiofs.sock")
			args = append(args, "--fs", fmt.Sprintf("tag=%s,socket=%s,num_queues=1,queue_size=512", sd.Tag, sockPath))
			go h.startVirtiofsd(ch, sd, sockPath)
			time.Sleep(100 * time.Millisecond) // Let virtiofsd start before CH connects.
		}
	} else {
		h.logger.Warn("virtiofsd not found, virtiofs mounts disabled")
	}

	// Vsock.
	if cfg.Vsock {
		args = append(args, "--vsock", fmt.Sprintf("cid=3,socket=%s", ch.vsockPath))
	}

	// Network.
	if cfg.Network.Mode == "tap" && cfg.Network.TAPDevice != "" {
		args = append(args, "--net", fmt.Sprintf("tap=%s", cfg.Network.TAPDevice))
	}

	cmd := exec.Command(h.chBin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start cloud-hypervisor: %w", err)
	}

	ch.process = cmd
	ch.state = VMStateRunning
	ch.booted = time.Now()
	h.logger.Info("Cloud Hypervisor VM started", "id", id, "pid", cmd.Process.Pid)

	return nil
}

func (h *CHHypervisor) StopVM(id string) error {
	h.mu.Lock()
	ch, ok := h.vms[id]
	h.mu.Unlock()
	if !ok {
		return fmt.Errorf("VM %s not found", id)
	}

	if ch.process != nil && ch.process.Process != nil {
		ch.process.Process.Kill()
		ch.process.Wait()
	}
	ch.state = VMStateStopped
	return nil
}

func (h *CHHypervisor) DeleteVM(id string) error {
	h.StopVM(id)
	h.mu.Lock()
	delete(h.vms, id)
	h.mu.Unlock()
	os.RemoveAll(filepath.Join(h.config.DataDir, "vms", id))
	return nil
}

func (h *CHHypervisor) ListVMs() ([]*VM, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	result := make([]*VM, 0, len(h.vms))
	for _, ch := range h.vms {
		result = append(result, h.toVM(ch))
	}
	return result, nil
}

func (h *CHHypervisor) PauseVM(id string) error {
	return h.apiCall(id, "PUT", "/api/v1/vm.pause", nil)
}

func (h *CHHypervisor) ResumeVM(id string) error {
	return h.apiCall(id, "PUT", "/api/v1/vm.resume", nil)
}

func (h *CHHypervisor) CreateSnapshot(id string) (Snapshot, error) {
	h.PauseVM(id)
	snapDir := filepath.Join(h.config.DataDir, "vms", id, "snapshot")
	os.MkdirAll(snapDir, 0o755)
	body := fmt.Sprintf(`{"destination_url":"file://%s"}`, snapDir)
	if err := h.apiCall(id, "PUT", "/api/v1/vm.snapshot", strings.NewReader(body)); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		StatePath: snapDir,
		UpperDir:  h.vms[id].config.UpperDir,
	}, nil
}

func (h *CHHypervisor) RestoreSnapshot(id string, snap Snapshot) error {
	body := fmt.Sprintf(`{"source_url":"file://%s"}`, snap.StatePath)
	return h.apiCall(id, "PUT", "/api/v1/vm.restore", strings.NewReader(body))
}

func (h *CHHypervisor) apiCall(id, method, path string, body interface{}) error {
	h.mu.RLock()
	ch, ok := h.vms[id]
	h.mu.RUnlock()
	if !ok {
		return fmt.Errorf("VM %s not found", id)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", ch.apiSock)
			},
		},
	}

	var bodyReader *strings.Reader
	if body != nil {
		switch v := body.(type) {
		case *strings.Reader:
			bodyReader = v
		default:
			data, _ := json.Marshal(v)
			bodyReader = strings.NewReader(string(data))
		}
	}

	url := "http://localhost" + path
	req, _ := http.NewRequest(method, url, bodyReader)
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("CH API %s %s: %w", method, path, err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("CH API %s %s: HTTP %d", method, path, resp.StatusCode)
	}
	return nil
}

func (h *CHHypervisor) startVirtiofsd(ch *chVM, sd SharedDir, sockPath string) {
	cmd := exec.Command("virtiofsd",
		"--socket-path="+sockPath,
		"-o", "source="+sd.HostPath,
		"-o", "cache=auto",
	)
	if err := cmd.Run(); err != nil {
		h.logger.Warn("virtiofsd exited", "tag", sd.Tag, "error", err)
	}
}

func (h *CHHypervisor) toVM(ch *chVM) *VM {
	vm := &VM{
		ID:     ch.config.ID,
		Config: ch.config,
		State:  ch.state,
		Booted: ch.booted,
	}
	if ch.vsockPath != "" {
		vsockPath := ch.vsockPath
		vm.DialVsock = func(port uint32) (net.Conn, error) {
			// Cloud Hypervisor vsock UDS uses same protocol as Firecracker:
			// connect to UDS, send "CONNECT <port>\n", receive "OK <port>\n".
			conn, err := net.Dial("unix", vsockPath)
			if err != nil {
				return nil, fmt.Errorf("vsock dial: %w", err)
			}
			if _, err := fmt.Fprintf(conn, "CONNECT %d\n", port); err != nil {
				conn.Close()
				return nil, fmt.Errorf("vsock CONNECT: %w", err)
			}
			buf := make([]byte, 32)
			n, err := conn.Read(buf)
			if err != nil {
				conn.Close()
				return nil, fmt.Errorf("vsock handshake: %w", err)
			}
			if n < 2 || string(buf[:2]) != "OK" {
				conn.Close()
				return nil, fmt.Errorf("vsock handshake failed: %s", string(buf[:n]))
			}
			return conn, nil
		}
	}
	return vm
}
