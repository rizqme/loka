package worker

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/vyprai/loka/pkg/lokavm"
)

// mockHypervisor implements lokavm.Hypervisor for testing.
type mockHypervisor struct {
	vms     map[string]*lokavm.VM
	created []string
	started []string
	stopped []string
}

func newMockHypervisor() *mockHypervisor {
	return &mockHypervisor{vms: make(map[string]*lokavm.VM)}
}

func (h *mockHypervisor) CreateVM(config lokavm.VMConfig) (*lokavm.VM, error) {
	h.created = append(h.created, config.ID)
	vm := &lokavm.VM{
		ID:     config.ID,
		Config: config,
		State:  lokavm.VMStateCreated,
		DialVsock: func(port uint32) (net.Conn, error) {
			return nil, fmt.Errorf("mock: no vsock")
		},
	}
	h.vms[config.ID] = vm
	return vm, nil
}

func (h *mockHypervisor) StartVM(id string) error {
	h.started = append(h.started, id)
	if vm, ok := h.vms[id]; ok {
		vm.State = lokavm.VMStateRunning
		vm.Booted = time.Now()
	}
	return nil
}

func (h *mockHypervisor) StopVM(id string) error {
	h.stopped = append(h.stopped, id)
	if vm, ok := h.vms[id]; ok {
		vm.State = lokavm.VMStateStopped
	}
	return nil
}

func (h *mockHypervisor) DeleteVM(id string) error {
	delete(h.vms, id)
	return nil
}

func (h *mockHypervisor) ListVMs() ([]*lokavm.VM, error) {
	var result []*lokavm.VM
	for _, vm := range h.vms {
		result = append(result, vm)
	}
	return result, nil
}

func (h *mockHypervisor) PauseVM(id string) error {
	if vm, ok := h.vms[id]; ok {
		vm.State = lokavm.VMStatePaused
	}
	return nil
}

func (h *mockHypervisor) ResumeVM(id string) error {
	if vm, ok := h.vms[id]; ok {
		vm.State = lokavm.VMStateRunning
	}
	return nil
}

func (h *mockHypervisor) CreateSnapshot(id string) (lokavm.Snapshot, error) {
	return lokavm.Snapshot{UpperDir: "/tmp/upper"}, nil
}

func (h *mockHypervisor) RestoreSnapshot(id string, snap lokavm.Snapshot) error {
	return nil
}

func TestAgentHypervisor(t *testing.T) {
	mock := newMockHypervisor()
	agent := &Agent{
		sessions:   make(map[string]*SessionState),
		hypervisor: mock,
	}

	if agent.Hypervisor() == nil {
		t.Error("expected non-nil hypervisor")
	}

	// Without hypervisor.
	agent2 := &Agent{sessions: make(map[string]*SessionState)}
	if agent2.Hypervisor() != nil {
		t.Error("expected nil hypervisor")
	}
}

func TestMockHypervisorLifecycle(t *testing.T) {
	h := newMockHypervisor()

	// Create.
	vm, err := h.CreateVM(lokavm.VMConfig{ID: "test-vm", VCPUsMin: 1, MemoryMinMB: 64})
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	if vm.State != lokavm.VMStateCreated {
		t.Errorf("state=%s, want created", vm.State)
	}

	// Start.
	if err := h.StartVM("test-vm"); err != nil {
		t.Fatalf("StartVM: %v", err)
	}
	if h.vms["test-vm"].State != lokavm.VMStateRunning {
		t.Error("expected running after start")
	}

	// Pause.
	h.PauseVM("test-vm")
	if h.vms["test-vm"].State != lokavm.VMStatePaused {
		t.Error("expected paused")
	}

	// Resume.
	h.ResumeVM("test-vm")
	if h.vms["test-vm"].State != lokavm.VMStateRunning {
		t.Error("expected running after resume")
	}

	// Snapshot.
	snap, err := h.CreateSnapshot("test-vm")
	if err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}
	if snap.UpperDir != "/tmp/upper" {
		t.Errorf("snapshot upper=%q", snap.UpperDir)
	}

	// Stop.
	h.StopVM("test-vm")
	if h.vms["test-vm"].State != lokavm.VMStateStopped {
		t.Error("expected stopped")
	}

	// Delete.
	h.DeleteVM("test-vm")
	if _, ok := h.vms["test-vm"]; ok {
		t.Error("expected VM to be deleted")
	}

	// List.
	vms, _ := h.ListVMs()
	if len(vms) != 0 {
		t.Errorf("expected 0 VMs, got %d", len(vms))
	}
}

// Verify the mock implements the interface.
var _ lokavm.Hypervisor = (*mockHypervisor)(nil)
