package lokavm

import (
	"net"
	"time"
)

// Hypervisor manages microVM lifecycle. On macOS it uses Apple Virtualization
// Framework, on Linux it uses a custom Go KVM VMM. lokad imports this package
// directly — no CLI, no subprocess, no IPC.
type Hypervisor interface {
	CreateVM(config VMConfig) (*VM, error)
	StartVM(id string) error
	StopVM(id string) error
	DeleteVM(id string) error
	ListVMs() ([]*VM, error)
	PauseVM(id string) error
	ResumeVM(id string) error
	CreateSnapshot(id string) (Snapshot, error)
	RestoreSnapshot(id string, snap Snapshot) error
}

// HypervisorConfig holds global configuration for the hypervisor.
type HypervisorConfig struct {
	KernelPath  string // Path to custom Linux kernel image.
	InitrdPath  string // Path to initramfs (required for Apple VZ).
	DataDir     string // Working directory for VM state, layers, upper dirs.
}

// VMConfig describes a microVM to create.
type VMConfig struct {
	ID          string
	VCPUsMin    int // Initial vCPUs (always active).
	VCPUsMax    int // Maximum vCPUs (hotplug on demand, KVM only).
	CPUShareMin int // Guaranteed CPU share (% of a core, e.g. 25).
	CPUShareMax int // Maximum CPU share (cap %, e.g. 100).
	MemoryMinMB int // Pre-allocated minimum memory (guaranteed).
	MemoryMaxMB int // Maximum memory (demand-paged beyond min).

	// Filesystem.
	Drives     []Drive     // Extra block devices (ext4 images).
	SharedDirs []SharedDir // Host dirs shared realtime with guest via virtiofs.
	Layers     []string    // Ordered layer directories (bottom to top) for overlay rootfs.
	UpperDir   string      // Per-VM writable upper dir for overlay.

	// Networking.
	Network NetworkConfig

	// Communication.
	Vsock bool // Enable virtio-vsock for host↔guest RPC.

	// Boot.
	BootArgs string // Kernel command line arguments.
}

// Drive describes an extra block device to attach to the VM.
type Drive struct {
	ID       string // Drive identifier (e.g. "data").
	Path     string // Path to ext4 image on host.
	ReadOnly bool
}

// SharedDir describes a host directory to share with the guest via virtiofs.
type SharedDir struct {
	Tag       string // Mount tag (guest uses this to mount: mount -t virtiofs <tag> <path>).
	HostPath  string // Absolute path on host.
	GuestPath string // Mount point inside guest.
	ReadOnly  bool
}

// NetworkConfig describes VM networking.
type NetworkConfig struct {
	Mode string // "nat" (macOS VZ), "tap" (Linux KVM).

	// TAP-specific (Linux).
	TAPDevice string // Host TAP device name.
	GuestIP   string // Guest IP address.
	HostIP    string // Host-side IP for the TAP bridge.
	GuestMAC  string // Guest MAC address.
}

// VM represents a running or created microVM.
type VM struct {
	ID     string
	Config VMConfig
	State  VMState
	PID    int       // Host process/thread ID (0 if not applicable).
	Booted time.Time // When the VM finished booting.

	// DialVsock connects to a vsock port inside the guest.
	// Returns a net.Conn for host↔guest communication.
	DialVsock func(port uint32) (net.Conn, error)
}

// VMState represents the lifecycle state of a VM.
type VMState string

const (
	VMStateCreated  VMState = "created"
	VMStateStarting VMState = "starting"
	VMStateRunning  VMState = "running"
	VMStatePaused   VMState = "paused"
	VMStateStopped  VMState = "stopped"
)

// Snapshot holds paths to a VM snapshot (memory + state).
type Snapshot struct {
	MemPath   string // Path to memory dump file.
	StatePath string // Path to VM state file.
	UpperDir  string // Path to overlay upper dir at snapshot time.
}
