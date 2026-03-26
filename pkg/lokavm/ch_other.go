//go:build !linux

package lokavm

import (
	"fmt"
	"log/slog"
)

type CHHypervisor struct{}

func NewCHHypervisor(config HypervisorConfig, logger *slog.Logger) (*CHHypervisor, error) {
	return nil, fmt.Errorf("Cloud Hypervisor is only available on Linux")
}

func (h *CHHypervisor) CreateVM(config VMConfig) (*VM, error)          { return nil, fmt.Errorf("CH not available") }
func (h *CHHypervisor) StartVM(id string) error                       { return fmt.Errorf("CH not available") }
func (h *CHHypervisor) StopVM(id string) error                        { return fmt.Errorf("CH not available") }
func (h *CHHypervisor) DeleteVM(id string) error                      { return fmt.Errorf("CH not available") }
func (h *CHHypervisor) ListVMs() ([]*VM, error)                       { return nil, fmt.Errorf("CH not available") }
func (h *CHHypervisor) PauseVM(id string) error                       { return fmt.Errorf("CH not available") }
func (h *CHHypervisor) ResumeVM(id string) error                      { return fmt.Errorf("CH not available") }
func (h *CHHypervisor) CreateSnapshot(id string) (Snapshot, error)    { return Snapshot{}, fmt.Errorf("CH not available") }
func (h *CHHypervisor) RestoreSnapshot(id string, snap Snapshot) error { return fmt.Errorf("CH not available") }
