package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/vyprai/loka/pkg/lokavm"
)

// pullBuildImage pulls a container image via crane and returns the extracted rootfs path.
// Used by e2e builds that run inside a VM.
func pullBuildImage(cfg buildConfig) (string, error) {
	ctx := context.Background()
	cacheDir := filepath.Join(cfg.OutputDir, ".build-cache")
	os.MkdirAll(cacheDir, 0o755)

	// Check if already cached.
	rootfsDir := filepath.Join(cacheDir, "rootfs")
	if info, err := os.Stat(filepath.Join(rootfsDir, "bin")); err == nil && info.IsDir() {
		cfg.Logger.Info("build image cached", "path", rootfsDir)
		return rootfsDir, nil
	}

	img, err := crane.Pull(cfg.Image,
		crane.WithContext(ctx),
		crane.WithPlatform(&v1.Platform{OS: "linux", Architecture: cfg.Arch}),
	)
	if err != nil {
		return "", fmt.Errorf("crane pull %s: %w", cfg.Image, err)
	}

	// Extract all layers into rootfsDir.
	os.RemoveAll(rootfsDir)
	os.MkdirAll(rootfsDir, 0o755)

	layers, err := img.Layers()
	if err != nil {
		return "", fmt.Errorf("get layers: %w", err)
	}

	for i, l := range layers {
		rc, err := l.Uncompressed()
		if err != nil {
			return "", fmt.Errorf("uncompress layer %d: %w", i, err)
		}
		if err := extractTar(rc, rootfsDir); err != nil {
			rc.Close()
			return "", fmt.Errorf("extract layer %d: %w", i, err)
		}
		rc.Close()
	}

	cfg.Logger.Info("build image extracted", "layers", len(layers), "path", rootfsDir)
	return rootfsDir, nil
}

// findBootstrapKernel locates the bootstrap kernel used to boot build VMs.
// Checks: build/bootstrap/vmlinux (repo), then build/vmlinux-lokavm (current).
func findBootstrapKernel(outputDir string) (kernelPath, initrdPath string, err error) {
	projectDir, _ := findProjectRoot()

	// Check build/bootstrap/ first (committed to repo).
	bootstrap := filepath.Join(projectDir, "build", "bootstrap")
	if _, err := os.Stat(filepath.Join(bootstrap, "vmlinux")); err == nil {
		k := filepath.Join(bootstrap, "vmlinux")
		i := filepath.Join(bootstrap, "initramfs.cpio.gz")
		if _, err := os.Stat(i); err != nil {
			i = "" // Initramfs optional for CH.
		}
		return k, i, nil
	}

	// Fall back to current kernel (use previous build to build next).
	current := filepath.Join(projectDir, "build", "vmlinux-lokavm")
	if _, err := os.Stat(current); err == nil {
		i := filepath.Join(projectDir, "build", "initramfs.cpio.gz")
		if _, err := os.Stat(i); err != nil {
			i = ""
		}
		return current, i, nil
	}

	return "", "", fmt.Errorf("no bootstrap kernel found — place a kernel at build/bootstrap/vmlinux")
}

// runBuildVM boots a VM with the given rootfs and shared dirs, runs the build script, and waits.
// Used by e2e builds.
func runBuildVM(cfg buildConfig, rootfsDir string, sharedDirs []lokavm.SharedDir, scriptPath string) error {
	logger := cfg.Logger

	// Find bootstrap kernel to boot the build VM.
	kernelPath, initrdPath, err := findBootstrapKernel(cfg.OutputDir)
	if err != nil {
		return err
	}
	logger.Info("using bootstrap kernel", "kernel", kernelPath, "initrd", initrdPath)

	// Initialize hypervisor with the bootstrap kernel.
	hvConfig := lokavm.HypervisorConfig{
		KernelPath: kernelPath,
		InitrdPath: initrdPath,
		DataDir:    filepath.Join(cfg.OutputDir, ".build-cache"),
	}

	var hv lokavm.Hypervisor

	// Try VZ first (macOS), then CH (Linux).
	if hv, err = lokavm.NewHypervisor(hvConfig, logger); err != nil {
		if hv, err = lokavm.NewCHHypervisor(hvConfig, logger); err != nil {
			return fmt.Errorf("no hypervisor available: %w", err)
		}
	}

	vmID := "loka-build-kernel"

	bootArgs := fmt.Sprintf("console=hvc0 ip=dhcp rootfstype=virtiofs root=rootfs loka.exec=%s", scriptPath)
	for _, sd := range sharedDirs {
		bootArgs += fmt.Sprintf(" loka.virtiofs=%s:%s", sd.Tag, sd.GuestPath)
	}

	upperDir := filepath.Join(cfg.OutputDir, ".build-cache", "upper")
	os.MkdirAll(upperDir, 0o755)

	vmCfg := lokavm.VMConfig{
		ID:          vmID,
		VCPUsMin:    runtime.NumCPU(),
		VCPUsMax:    runtime.NumCPU(),
		MemoryMinMB: 2048,
		MemoryMaxMB: 4096,
		SharedDirs:  sharedDirs,
		Layers:      []string{rootfsDir},
		UpperDir:    upperDir,
		Network:     lokavm.NetworkConfig{Mode: "nat"},
		Vsock:       false,
		BootArgs:    bootArgs,
	}

	os.MkdirAll(vmCfg.UpperDir, 0o755)

	logger.Info("starting build VM",
		"vcpus", vmCfg.VCPUsMin,
		"memory_mb", vmCfg.MemoryMaxMB,
		"rootfs", rootfsDir)

	if _, err := hv.CreateVM(vmCfg); err != nil {
		return fmt.Errorf("create VM: %w", err)
	}
	defer hv.DeleteVM(vmID)

	if err := hv.StartVM(vmID); err != nil {
		return fmt.Errorf("start VM: %w", err)
	}

	// Wait for VM to exit (build script runs as init, VM shuts down when done).
	logger.Info("build running, waiting for completion...")
	start := time.Now()

	for {
		vms, _ := hv.ListVMs()
		found := false
		for _, vm := range vms {
			if vm.ID == vmID && vm.State == lokavm.VMStateRunning {
				found = true
				break
			}
		}
		if !found {
			break
		}
		time.Sleep(2 * time.Second)

		elapsed := time.Since(start)
		if elapsed > 30*time.Minute {
			hv.StopVM(vmID)
			return fmt.Errorf("build timed out after %v", elapsed)
		}
	}

	return nil
}
