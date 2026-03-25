//go:build darwin

package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"time"

	"github.com/Code-Hex/vz/v3"
)

// VM wraps a Code-Hex/vz virtual machine with convenience methods for lokavm.
type VM struct {
	vm    *vz.VirtualMachine
	vsock *vz.VirtioSocketDevice

	logger *slog.Logger
}

func bootVM(ctx context.Context, kernelPath, initrdPath, rootfsPath string, cpus, memoryMB int, dataDir string, logger *slog.Logger) (*VM, error) {
	// Boot loader options.
	opts := []vz.LinuxBootLoaderOption{
		vz.WithCommandLine("console=ttyAMA0 console=hvc0 root=/dev/vda rw init=/sbin/init ip=dhcp"),
	}
	if initrdPath != "" {
		opts = append(opts, vz.WithInitrd(initrdPath))
	}

	bootLoader, err := vz.NewLinuxBootLoader(kernelPath, opts...)
	if err != nil {
		return nil, fmt.Errorf("boot loader: %w", err)
	}

	// VM configuration.
	config, err := vz.NewVirtualMachineConfiguration(bootLoader, uint(cpus), uint64(memoryMB)*1024*1024)
	if err != nil {
		return nil, fmt.Errorf("vm config: %w", err)
	}

	// Serial console → stdout (to see kernel boot messages).
	logPath := dataDir + "/console.log"
	_ = logPath
	serialAttachment, err := vz.NewFileHandleSerialPortAttachment(os.Stdin, os.Stdout)
	if err != nil {
		return nil, fmt.Errorf("serial attachment: %w", err)
	}
	serial, err := vz.NewVirtioConsoleDeviceSerialPortConfiguration(serialAttachment)
	if err != nil {
		return nil, fmt.Errorf("serial config: %w", err)
	}
	config.SetSerialPortsVirtualMachineConfiguration([]*vz.VirtioConsoleDeviceSerialPortConfiguration{serial})

	// Rootfs disk (read-write).
	diskAttachment, err := vz.NewDiskImageStorageDeviceAttachment(rootfsPath, false)
	if err != nil {
		return nil, fmt.Errorf("rootfs attachment: %w", err)
	}
	blockDevice, err := vz.NewVirtioBlockDeviceConfiguration(diskAttachment)
	if err != nil {
		return nil, fmt.Errorf("block device: %w", err)
	}
	config.SetStorageDevicesVirtualMachineConfiguration([]vz.StorageDeviceConfiguration{blockDevice})

	// Network (NAT).
	natAttachment, err := vz.NewNATNetworkDeviceAttachment()
	if err != nil {
		return nil, fmt.Errorf("NAT attachment: %w", err)
	}
	networkDevice, err := vz.NewVirtioNetworkDeviceConfiguration(natAttachment)
	if err != nil {
		return nil, fmt.Errorf("network device: %w", err)
	}
	config.SetNetworkDevicesVirtualMachineConfiguration([]*vz.VirtioNetworkDeviceConfiguration{networkDevice})

	// Vsock.
	vsockConfig, err := vz.NewVirtioSocketDeviceConfiguration()
	if err != nil {
		return nil, fmt.Errorf("vsock config: %w", err)
	}
	config.SetSocketDevicesVirtualMachineConfiguration([]vz.SocketDeviceConfiguration{vsockConfig})

	// Shared directory (virtiofs).
	if dataDir != "" {
		sharedDir, err := vz.NewSharedDirectory(dataDir, false)
		if err != nil {
			return nil, fmt.Errorf("shared directory: %w", err)
		}
		singleDirShare, err := vz.NewSingleDirectoryShare(sharedDir)
		if err != nil {
			return nil, fmt.Errorf("single dir share: %w", err)
		}
		fsConfig, err := vz.NewVirtioFileSystemDeviceConfiguration("share")
		if err != nil {
			return nil, fmt.Errorf("virtiofs config: %w", err)
		}
		fsConfig.SetDirectoryShare(singleDirShare)
		config.SetDirectorySharingDevicesVirtualMachineConfiguration([]vz.DirectorySharingDeviceConfiguration{fsConfig})
	}

	// Validate configuration.
	validated, err := config.Validate()
	if err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	if !validated {
		return nil, fmt.Errorf("invalid VM configuration")
	}

	// Create and start VM.
	vm, err := vz.NewVirtualMachine(config)
	if err != nil {
		return nil, fmt.Errorf("create VM: %w", err)
	}

	if err := vm.Start(); err != nil {
		return nil, fmt.Errorf("start VM: %w", err)
	}

	logger.Info("VZ VM started", "cpus", cpus, "memory_mb", memoryMB, "console_log", logPath)

	// Get vsock device from running VM.
	var vsockDev *vz.VirtioSocketDevice
	socketDevices := vm.SocketDevices()
	if len(socketDevices) > 0 {
		vsockDev = socketDevices[0]
	}

	return &VM{vm: vm, vsock: vsockDev, logger: logger}, nil
}

// Stop shuts down the virtual machine.
func (v *VM) Stop() {
	if v.vm.CanRequestStop() {
		v.vm.RequestStop()
	} else if v.vm.CanStop() {
		v.vm.Stop()
	}
}

// DialVsock connects to a vsock port inside the guest.
// The returned connection implements net.Conn.
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

// waitForLokad polls the VM until lokad is reachable on vsock port 6840.
func waitForLokad(vm *VM, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			return fmt.Errorf("lokad did not become ready within %s", timeout)
		case <-ticker.C:
			conn, err := vm.DialVsock(6840)
			if err != nil {
				continue
			}
			conn.Close()
			return nil // lokad is listening
		}
	}
}
