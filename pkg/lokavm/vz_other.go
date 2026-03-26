//go:build !darwin

package lokavm

import (
	"fmt"
	"log/slog"
)

// VZHypervisor is not available on non-macOS platforms.
type VZHypervisor struct{}

// NewHypervisor returns an error on non-macOS platforms.
// Use NewKVMHypervisor on Linux instead.
func NewHypervisor(config HypervisorConfig, logger *slog.Logger) (*VZHypervisor, error) {
	return nil, fmt.Errorf("Apple Virtualization Framework is only available on macOS")
}

func (h *VZHypervisor) CreateVM(config VMConfig) (*VM, error) {
	return nil, fmt.Errorf("VZ not available")
}
func (h *VZHypervisor) StartVM(id string) error             { return fmt.Errorf("VZ not available") }
func (h *VZHypervisor) StopVM(id string) error              { return fmt.Errorf("VZ not available") }
func (h *VZHypervisor) DeleteVM(id string) error            { return fmt.Errorf("VZ not available") }
func (h *VZHypervisor) ListVMs() ([]*VM, error)             { return nil, fmt.Errorf("VZ not available") }
func (h *VZHypervisor) PauseVM(id string) error             { return fmt.Errorf("VZ not available") }
func (h *VZHypervisor) ResumeVM(id string) error            { return fmt.Errorf("VZ not available") }
func (h *VZHypervisor) CreateSnapshot(id string) (Snapshot, error) {
	return Snapshot{}, fmt.Errorf("VZ not available")
}
func (h *VZHypervisor) RestoreSnapshot(id string, snap Snapshot) error {
	return fmt.Errorf("VZ not available")
}
