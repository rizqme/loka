//go:build !linux

package lokavm

import (
	"fmt"
	"log/slog"
)

// KVMHypervisor is not available on non-Linux platforms.
type KVMHypervisor struct{}

// NewKVMHypervisor returns an error on non-Linux platforms.
// Use NewHypervisor (Apple VZ) on macOS instead.
func NewKVMHypervisor(config HypervisorConfig, logger *slog.Logger) (*KVMHypervisor, error) {
	return nil, fmt.Errorf("KVM hypervisor is only available on Linux")
}

func (h *KVMHypervisor) CreateVM(config VMConfig) (*VM, error) {
	return nil, fmt.Errorf("KVM not available")
}
func (h *KVMHypervisor) StartVM(id string) error             { return fmt.Errorf("KVM not available") }
func (h *KVMHypervisor) StopVM(id string) error              { return fmt.Errorf("KVM not available") }
func (h *KVMHypervisor) DeleteVM(id string) error            { return fmt.Errorf("KVM not available") }
func (h *KVMHypervisor) ListVMs() ([]*VM, error)             { return nil, fmt.Errorf("KVM not available") }
func (h *KVMHypervisor) PauseVM(id string) error             { return fmt.Errorf("KVM not available") }
func (h *KVMHypervisor) ResumeVM(id string) error            { return fmt.Errorf("KVM not available") }
func (h *KVMHypervisor) CreateSnapshot(id string) (Snapshot, error) {
	return Snapshot{}, fmt.Errorf("KVM not available")
}
func (h *KVMHypervisor) RestoreSnapshot(id string, snap Snapshot) error {
	return fmt.Errorf("KVM not available")
}
