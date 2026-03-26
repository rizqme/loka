//go:build darwin

package lokavm

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	codevz "github.com/Code-Hex/vz/v3"
)

// VZHypervisor implements the Hypervisor interface using Apple Virtualization Framework.
type VZHypervisor struct {
	config HypervisorConfig
	logger *slog.Logger

	mu  sync.RWMutex
	vms map[string]*vzVM
}

type vzVM struct {
	vm     *codevz.VirtualMachine
	vsock  *codevz.VirtioSocketDevice
	config VMConfig
	state  VMState
	booted time.Time

	// Console output capture.
	consoleRead  *os.File
	consoleWrite *os.File
}

// NewHypervisor creates a new Apple VZ hypervisor.
func NewHypervisor(config HypervisorConfig, logger *slog.Logger) (*VZHypervisor, error) {
	if logger == nil {
		logger = slog.Default()
	}
	return &VZHypervisor{
		config: config,
		logger: logger,
		vms:    make(map[string]*vzVM),
	}, nil
}

func (h *VZHypervisor) CreateVM(config VMConfig) (*VM, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if _, exists := h.vms[config.ID]; exists {
		return nil, fmt.Errorf("VM %s already exists", config.ID)
	}

	vz, err := h.buildVZ(config)
	if err != nil {
		return nil, fmt.Errorf("build VZ config: %w", err)
	}

	vz.state = VMStateCreated
	h.vms[config.ID] = vz

	return h.toVM(vz), nil
}

func (h *VZHypervisor) StartVM(id string) error {
	h.mu.Lock()
	vz, ok := h.vms[id]
	h.mu.Unlock()
	if !ok {
		return fmt.Errorf("VM %s not found", id)
	}

	vz.state = VMStateStarting
	if err := vz.vm.Start(); err != nil {
		vz.state = VMStateStopped
		return fmt.Errorf("start VM: %w", err)
	}

	// Cache vsock device.
	socketDevices := vz.vm.SocketDevices()
	if len(socketDevices) > 0 {
		vz.vsock = socketDevices[0]
	}

	vz.state = VMStateRunning
	vz.booted = time.Now()

	h.logger.Info("VM started", "id", id, "vcpus", config(vz).VCPUsMin, "memory_mb", config(vz).MemoryMinMB)
	return nil
}

func (h *VZHypervisor) StopVM(id string) error {
	h.mu.Lock()
	vz, ok := h.vms[id]
	h.mu.Unlock()
	if !ok {
		return fmt.Errorf("VM %s not found", id)
	}

	if vz.vm.CanRequestStop() {
		vz.vm.RequestStop()
	} else if vz.vm.CanStop() {
		vz.vm.Stop()
	}

	vz.state = VMStateStopped
	if vz.consoleRead != nil {
		vz.consoleRead.Close()
	}
	if vz.consoleWrite != nil {
		vz.consoleWrite.Close()
	}

	h.logger.Info("VM stopped", "id", id)
	return nil
}

func (h *VZHypervisor) DeleteVM(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	vz, ok := h.vms[id]
	if !ok {
		return fmt.Errorf("VM %s not found", id)
	}

	if vz.state == VMStateRunning || vz.state == VMStatePaused {
		if vz.vm.CanRequestStop() {
			vz.vm.RequestStop()
		} else if vz.vm.CanStop() {
			vz.vm.Stop()
		}
	}

	delete(h.vms, id)
	return nil
}

func (h *VZHypervisor) ListVMs() ([]*VM, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	result := make([]*VM, 0, len(h.vms))
	for _, vz := range h.vms {
		result = append(result, h.toVM(vz))
	}
	return result, nil
}

func (h *VZHypervisor) PauseVM(id string) error {
	h.mu.Lock()
	vz, ok := h.vms[id]
	h.mu.Unlock()
	if !ok {
		return fmt.Errorf("VM %s not found", id)
	}

	if err := vz.vm.Pause(); err != nil {
		return fmt.Errorf("pause VM: %w", err)
	}
	vz.state = VMStatePaused
	return nil
}

func (h *VZHypervisor) ResumeVM(id string) error {
	h.mu.Lock()
	vz, ok := h.vms[id]
	h.mu.Unlock()
	if !ok {
		return fmt.Errorf("VM %s not found", id)
	}

	if err := vz.vm.Resume(); err != nil {
		return fmt.Errorf("resume VM: %w", err)
	}
	vz.state = VMStateRunning
	return nil
}

func (h *VZHypervisor) CreateSnapshot(id string) (Snapshot, error) {
	h.mu.RLock()
	vz, ok := h.vms[id]
	h.mu.RUnlock()
	if !ok {
		return Snapshot{}, fmt.Errorf("VM %s not found", id)
	}

	// Pause VM before saving state.
	if err := vz.vm.Pause(); err != nil {
		return Snapshot{}, fmt.Errorf("pause for snapshot: %w", err)
	}
	vz.state = VMStatePaused

	// Save VM state to disk (memory + CPU registers).
	// macOS Sonoma 14.0+ — saveMachineStateTo / restoreMachineStateFrom.
	stateDir := fmt.Sprintf("%s/snapshots/%s", h.config.DataDir, id)
	os.MkdirAll(stateDir, 0o755)
	statePath := stateDir + "/vm.vzsave"

	if err := vz.vm.SaveMachineStateToPath(statePath); err != nil {
		// Save not supported (older macOS) — fall back to pause-only.
		h.logger.Warn("save VM state failed (pause-only mode)", "id", id, "error", err)
		return Snapshot{
			UpperDir: vz.config.UpperDir,
		}, nil
	}

	h.logger.Info("VM state saved", "id", id, "path", statePath)
	return Snapshot{
		StatePath: statePath,
		UpperDir:  vz.config.UpperDir,
	}, nil
}

func (h *VZHypervisor) RestoreSnapshot(id string, snap Snapshot) error {
	h.mu.RLock()
	vz, ok := h.vms[id]
	h.mu.RUnlock()
	if !ok {
		return fmt.Errorf("VM %s not found", id)
	}

	if snap.StatePath != "" {
		// Restore from saved state file (instant boot — no cold start).
		if err := vz.vm.RestoreMachineStateFromURL(snap.StatePath); err != nil {
			return fmt.Errorf("restore VM state: %w", err)
		}
		vz.state = VMStateRunning
		h.logger.Info("VM restored from snapshot", "id", id)
		return nil
	}

	// No state file — just resume from paused.
	if err := vz.vm.Resume(); err != nil {
		return fmt.Errorf("resume VM: %w", err)
	}
	vz.state = VMStateRunning
	return nil
}

// buildVZ constructs the VZ virtual machine configuration.
func (h *VZHypervisor) buildVZ(cfg VMConfig) (*vzVM, error) {
	// Boot loader.
	opts := []codevz.LinuxBootLoaderOption{}
	if cfg.BootArgs != "" {
		opts = append(opts, codevz.WithCommandLine(cfg.BootArgs))
	}
	if h.config.InitrdPath != "" {
		opts = append(opts, codevz.WithInitrd(h.config.InitrdPath))
	}

	bootLoader, err := codevz.NewLinuxBootLoader(h.config.KernelPath, opts...)
	if err != nil {
		return nil, fmt.Errorf("boot loader: %w", err)
	}

	// VZ doesn't support vCPU hotplug — boot with max.
	cpus := cfg.VCPUsMax
	if cpus < cfg.VCPUsMin {
		cpus = cfg.VCPUsMin
	}
	if cpus < 1 {
		cpus = 1
	}

	// VZ memory: use max (VZ handles demand paging natively).
	memMB := cfg.MemoryMaxMB
	if memMB < cfg.MemoryMinMB {
		memMB = cfg.MemoryMinMB
	}
	if memMB < 64 {
		memMB = 64
	}

	vmConfig, err := codevz.NewVirtualMachineConfiguration(bootLoader, uint(cpus), uint64(memMB)*1024*1024)
	if err != nil {
		return nil, fmt.Errorf("vm config: %w", err)
	}

	// Serial console via pipe — pump kernel output to logger.
	readPipe, writePipe, _ := os.Pipe()
	inputRead, inputWrite, _ := os.Pipe()
	_ = inputWrite
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := readPipe.Read(buf)
			if n > 0 {
				// Log at Info so it's always visible (kernel boot messages).
				h.logger.Info("vm-console", "id", cfg.ID, "output", string(buf[:n]))
			}
			if err != nil {
				return
			}
		}
	}()

	serialAttachment, err := codevz.NewFileHandleSerialPortAttachment(inputRead, writePipe)
	if err != nil {
		return nil, fmt.Errorf("serial attachment: %w", err)
	}
	serial, err := codevz.NewVirtioConsoleDeviceSerialPortConfiguration(serialAttachment)
	if err != nil {
		return nil, fmt.Errorf("serial config: %w", err)
	}
	vmConfig.SetSerialPortsVirtualMachineConfiguration([]*codevz.VirtioConsoleDeviceSerialPortConfiguration{serial})

	// Block devices (drives).
	var storageDevices []codevz.StorageDeviceConfiguration
	for _, drive := range cfg.Drives {
		diskAttachment, err := codevz.NewDiskImageStorageDeviceAttachment(drive.Path, drive.ReadOnly)
		if err != nil {
			return nil, fmt.Errorf("drive %s attachment: %w", drive.ID, err)
		}
		blockDevice, err := codevz.NewVirtioBlockDeviceConfiguration(diskAttachment)
		if err != nil {
			return nil, fmt.Errorf("drive %s block device: %w", drive.ID, err)
		}
		storageDevices = append(storageDevices, blockDevice)
	}
	if len(storageDevices) > 0 {
		vmConfig.SetStorageDevicesVirtualMachineConfiguration(storageDevices)
	}

	// Network (NAT for macOS).
	natAttachment, err := codevz.NewNATNetworkDeviceAttachment()
	if err != nil {
		return nil, fmt.Errorf("NAT attachment: %w", err)
	}
	networkDevice, err := codevz.NewVirtioNetworkDeviceConfiguration(natAttachment)
	if err != nil {
		return nil, fmt.Errorf("network device: %w", err)
	}
	vmConfig.SetNetworkDevicesVirtualMachineConfiguration([]*codevz.VirtioNetworkDeviceConfiguration{networkDevice})

	// Vsock.
	if cfg.Vsock {
		vsockConfig, err := codevz.NewVirtioSocketDeviceConfiguration()
		if err != nil {
			return nil, fmt.Errorf("vsock config: %w", err)
		}
		vmConfig.SetSocketDevicesVirtualMachineConfiguration([]codevz.SocketDeviceConfiguration{vsockConfig})
	}

	// Shared directories via virtiofs.
	// On macOS, each layer + upper dir + shared dirs get their own virtiofs device.
	var fsConfigs []codevz.DirectorySharingDeviceConfiguration

	// Container image layers (read-only).
	for i, layerDir := range cfg.Layers {
		tag := fmt.Sprintf("layer-%d", i)
		fsCfg, err := h.makeVirtioFS(tag, layerDir, true)
		if err != nil {
			return nil, fmt.Errorf("layer %d virtiofs: %w", i, err)
		}
		fsConfigs = append(fsConfigs, fsCfg)
	}

	// Overlay upper dir (read-write, per-VM).
	if cfg.UpperDir != "" {
		if err := os.MkdirAll(cfg.UpperDir, 0o755); err != nil {
			return nil, fmt.Errorf("create upper dir: %w", err)
		}
		fsCfg, err := h.makeVirtioFS("upper", cfg.UpperDir, false)
		if err != nil {
			return nil, fmt.Errorf("upper dir virtiofs: %w", err)
		}
		fsConfigs = append(fsConfigs, fsCfg)
	}

	// User-specified shared directories.
	for _, sd := range cfg.SharedDirs {
		fsCfg, err := h.makeVirtioFS(sd.Tag, sd.HostPath, sd.ReadOnly)
		if err != nil {
			return nil, fmt.Errorf("shared dir %s virtiofs: %w", sd.Tag, err)
		}
		fsConfigs = append(fsConfigs, fsCfg)
	}

	if len(fsConfigs) > 0 {
		vmConfig.SetDirectorySharingDevicesVirtualMachineConfiguration(fsConfigs)
	}

	// Validate.
	validated, err := vmConfig.Validate()
	if err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	if !validated {
		return nil, fmt.Errorf("invalid VM configuration")
	}

	// Create VZ VM.
	vm, err := codevz.NewVirtualMachine(vmConfig)
	if err != nil {
		return nil, fmt.Errorf("create VM: %w", err)
	}

	return &vzVM{
		vm:           vm,
		config:       cfg,
		consoleRead:  readPipe,
		consoleWrite: writePipe,
	}, nil
}

// makeVirtioFS creates a VZVirtioFileSystemDeviceConfiguration for a single directory.
func (h *VZHypervisor) makeVirtioFS(tag, hostPath string, readOnly bool) (codevz.DirectorySharingDeviceConfiguration, error) {
	sharedDir, err := codevz.NewSharedDirectory(hostPath, readOnly)
	if err != nil {
		return nil, fmt.Errorf("shared directory %s: %w", hostPath, err)
	}
	singleDirShare, err := codevz.NewSingleDirectoryShare(sharedDir)
	if err != nil {
		return nil, fmt.Errorf("single dir share %s: %w", hostPath, err)
	}
	fsCfg, err := codevz.NewVirtioFileSystemDeviceConfiguration(tag)
	if err != nil {
		return nil, fmt.Errorf("virtiofs config %s: %w", tag, err)
	}
	fsCfg.SetDirectoryShare(singleDirShare)
	return fsCfg, nil
}

// toVM converts an internal vzVM to the public VM type.
func (h *VZHypervisor) toVM(vz *vzVM) *VM {
	return &VM{
		ID:     vz.config.ID,
		Config: vz.config,
		State:  vz.state,
		Booted: vz.booted,
		DialVsock: func(port uint32) (net.Conn, error) {
			if vz.vsock == nil {
				return nil, fmt.Errorf("no vsock device")
			}
			conn, err := vz.vsock.Connect(port)
			if err != nil {
				return nil, fmt.Errorf("vsock connect port %d: %w", port, err)
			}
			return conn, nil
		},
	}
}

func config(vz *vzVM) VMConfig {
	return vz.config
}
