//go:build darwin

package vz

import (
	"fmt"
	"net"
	"os"

	codevz "github.com/Code-Hex/vz/v3"
)

// VM wraps a Code-Hex/vz virtual machine instance.
type VM struct {
	vm     *codevz.VirtualMachine
	vsock  *codevz.VirtioSocketDevice
	config Config
}

// Config holds configuration for creating a VZ virtual machine.
type Config struct {
	CPUs      int
	MemoryMB  int
	Kernel    string
	Cmdline   string
	Initrd    string // Optional initramfs path
	Rootfs    string
	SharedDir string
	VsockPort uint32
}

// NewVM creates a new VZ virtual machine with the given configuration.
func NewVM(cfg Config) (*VM, error) {
	// Boot loader.
	opts := []codevz.LinuxBootLoaderOption{
		codevz.WithCommandLine(cfg.Cmdline),
	}
	if cfg.Initrd != "" {
		opts = append(opts, codevz.WithInitrd(cfg.Initrd))
	}
	bootLoader, err := codevz.NewLinuxBootLoader(cfg.Kernel, opts...)
	if err != nil {
		return nil, fmt.Errorf("boot loader: %w", err)
	}

	// VM configuration.
	vmConfig, err := codevz.NewVirtualMachineConfiguration(bootLoader, uint(cfg.CPUs), uint64(cfg.MemoryMB)*1024*1024)
	if err != nil {
		return nil, fmt.Errorf("vm config: %w", err)
	}

	// Serial console -> /tmp/lokavm-console.log.
	logFile, err := os.Create("/tmp/lokavm-console.log")
	if err != nil {
		return nil, fmt.Errorf("create console log: %w", err)
	}
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		return nil, fmt.Errorf("open /dev/null: %w", err)
	}
	serialAttachment, err := codevz.NewFileHandleSerialPortAttachment(devNull, logFile)
	if err != nil {
		return nil, fmt.Errorf("serial attachment: %w", err)
	}
	serial, err := codevz.NewVirtioConsoleDeviceSerialPortConfiguration(serialAttachment)
	if err != nil {
		return nil, fmt.Errorf("serial config: %w", err)
	}
	vmConfig.SetSerialPortsVirtualMachineConfiguration([]*codevz.VirtioConsoleDeviceSerialPortConfiguration{serial})

	// Rootfs disk (read-write).
	diskAttachment, err := codevz.NewDiskImageStorageDeviceAttachment(cfg.Rootfs, false)
	if err != nil {
		return nil, fmt.Errorf("rootfs attachment: %w", err)
	}
	blockDevice, err := codevz.NewVirtioBlockDeviceConfiguration(diskAttachment)
	if err != nil {
		return nil, fmt.Errorf("block device: %w", err)
	}
	vmConfig.SetStorageDevicesVirtualMachineConfiguration([]codevz.StorageDeviceConfiguration{blockDevice})

	// Network (NAT).
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
	vsockConfig, err := codevz.NewVirtioSocketDeviceConfiguration()
	if err != nil {
		return nil, fmt.Errorf("vsock config: %w", err)
	}
	vmConfig.SetSocketDevicesVirtualMachineConfiguration([]codevz.SocketDeviceConfiguration{vsockConfig})

	// Shared directory (virtiofs).
	if cfg.SharedDir != "" {
		sharedDir, err := codevz.NewSharedDirectory(cfg.SharedDir, false)
		if err != nil {
			return nil, fmt.Errorf("shared directory: %w", err)
		}
		singleDirShare, err := codevz.NewSingleDirectoryShare(sharedDir)
		if err != nil {
			return nil, fmt.Errorf("single dir share: %w", err)
		}
		fsConfig, err := codevz.NewVirtioFileSystemDeviceConfiguration("share")
		if err != nil {
			return nil, fmt.Errorf("virtiofs config: %w", err)
		}
		fsConfig.SetDirectoryShare(singleDirShare)
		vmConfig.SetDirectorySharingDevicesVirtualMachineConfiguration([]codevz.DirectorySharingDeviceConfiguration{fsConfig})
	}

	// Validate.
	validated, err := vmConfig.Validate()
	if err != nil {
		return nil, fmt.Errorf("validate: %w", err)
	}
	if !validated {
		return nil, fmt.Errorf("invalid VM configuration")
	}

	// Create VM.
	vm, err := codevz.NewVirtualMachine(vmConfig)
	if err != nil {
		return nil, fmt.Errorf("create VM: %w", err)
	}

	return &VM{vm: vm, config: cfg}, nil
}

// Start boots the virtual machine.
func (v *VM) Start() error {
	if err := v.vm.Start(); err != nil {
		return fmt.Errorf("start VM: %w", err)
	}
	// Cache vsock device after start.
	socketDevices := v.vm.SocketDevices()
	if len(socketDevices) > 0 {
		v.vsock = socketDevices[0]
	}
	return nil
}

// Stop shuts down the virtual machine.
func (v *VM) Stop() error {
	if v.vm.CanRequestStop() {
		_, err := v.vm.RequestStop()
		return err
	}
	if v.vm.CanStop() {
		return v.vm.Stop()
	}
	return nil
}

// GuestIP returns the expected guest IP address.
// VZ NAT assigns IPs in 192.168.64.0/24 range;
// the first guest typically gets 192.168.64.2.
func (v *VM) GuestIP() string {
	return "192.168.64.2"
}

// State returns the current VM state as an int:
// 0=stopped, 1=running, 2=paused, 3=saving, 4=restoring.
func (v *VM) State() int {
	return int(v.vm.State())
}

// DialVsock connects to a vsock port inside the VM guest.
// Returns a net.Conn wrapping the vsock connection.
func (v *VM) DialVsock(port uint32) (net.Conn, error) {
	if v.vsock == nil {
		return nil, fmt.Errorf("no vsock device")
	}
	conn, err := v.vsock.Connect(port)
	if err != nil {
		return nil, fmt.Errorf("vsock connect port %d: %w", port, err)
	}
	return conn, nil
}
