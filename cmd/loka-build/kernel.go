package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/crane"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/vyprai/loka/pkg/lokavm"
)

func buildKernel(cfg buildConfig) error {
	outputFile := filepath.Join(cfg.OutputDir, "vmlinux-lokavm")

	// Skip if output exists.
	if info, err := os.Stat(outputFile); err == nil && info.Size() > 0 {
		cfg.Logger.Info("kernel already exists, skipping", "path", outputFile, "size_mb", info.Size()/(1024*1024))
		cfg.Logger.Info("delete to rebuild", "path", outputFile)
		return nil
	}

	os.MkdirAll(cfg.OutputDir, 0o755)

	// Detect project root (where scripts/kernel.config lives).
	projectDir, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf("find project root: %w", err)
	}

	cfg.Logger.Info("building kernel",
		"version", cfg.KernelVersion,
		"arch", cfg.Arch,
		"output", outputFile)

	// Step 1: Create persistent build directory (reused across builds for caching).
	buildDir := filepath.Join(cfg.OutputDir, ".build-cache", "kernel-src")
	os.MkdirAll(buildDir, 0o755)

	// Step 2b: Download kernel source on host.
	if err := downloadKernelSource(cfg, buildDir); err != nil {
		return fmt.Errorf("download kernel source: %w", err)
	}

	// Step 3: Write the build script.
	script := generateKernelBuildScript(cfg)
	scriptPath := filepath.Join(buildDir, "build.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return fmt.Errorf("write build script: %w", err)
	}

	absOutput, _ := filepath.Abs(cfg.OutputDir)
	absProject, _ := filepath.Abs(projectDir)
	absBuild, _ := filepath.Abs(buildDir)

	// Run build in Docker container with bind mounts (no copying).
	return runBuildDocker(cfg, absProject, absOutput, absBuild)
}

func generateKernelBuildScript(cfg buildConfig) string {
	ver := cfg.KernelVersion
	jobs := cfg.Jobs
	if jobs <= 0 {
		jobs = runtime.NumCPU()
	}

	archMake := "arm64"
	archTarget := "Image"
	archBootPath := "arch/arm64/boot/Image"
	if cfg.Arch == "amd64" || cfg.Arch == "x86_64" {
		archMake = "x86_64"
		archTarget = "bzImage"
		archBootPath = "arch/x86/boot/bzImage"
	}

	// Kernel source is pre-downloaded on host and mounted at /build.
	return fmt.Sprintf(`#!/bin/sh
set -e

mkdir -p /run /var/run /tmp
mount -t tmpfs tmpfs /run 2>/dev/null || true

# Install build tools if missing (Alpine: apk, Debian: apt).
if ! command -v make >/dev/null 2>&1; then
    echo "==> Installing build tools..."
    if command -v apk >/dev/null 2>&1; then
        apk add --no-cache build-base flex bison bc elfutils-dev openssl-dev linux-headers perl 2>&1 | tail -1
    elif command -v apt-get >/dev/null 2>&1; then
        apt-get update -qq && apt-get install -y -qq build-essential flex bison bc libelf-dev libssl-dev 2>&1 | tail -1
    fi
fi

# Verify build tools.
for tool in make gcc flex bison bc; do
    command -v "$tool" >/dev/null 2>&1 || { echo "ERROR: $tool not found"; exit 1; }
done
echo "==> Build tools OK"

cd /build/linux-%s

echo "==> Configuring..."
make ARCH=%s defconfig

# Append loka kernel config from mounted workspace.
if [ -f /workspace/scripts/kernel.config ]; then
    cat /workspace/scripts/kernel.config >> .config
fi

make ARCH=%s olddefconfig

echo "==> Compiling (jobs=%d)..."
make ARCH=%s -j%d %s

echo "==> Copying output..."
cp %s /output/vmlinux-lokavm
ls -lh /output/vmlinux-lokavm
echo "==> Kernel build complete"
`, ver, archMake, archMake, jobs, archMake, jobs, archTarget, archBootPath)
}

// downloadKernelSource downloads and extracts the kernel source on the host.
// The extracted source is cached in buildDir and mounted into the VM.
func downloadKernelSource(cfg buildConfig, buildDir string) error {
	ver := cfg.KernelVersion
	major := strings.Split(ver, ".")[0]
	srcDir := filepath.Join(buildDir, "linux-"+ver)

	// Already extracted?
	if _, err := os.Stat(filepath.Join(srcDir, "Makefile")); err == nil {
		cfg.Logger.Info("kernel source cached", "version", ver)
		return nil
	}

	tarball := filepath.Join(buildDir, "linux-"+ver+".tar.xz")

	// Download if not cached.
	if _, err := os.Stat(tarball); err != nil {
		url := fmt.Sprintf("https://cdn.kernel.org/pub/linux/kernel/v%s.x/linux-%s.tar.xz", major, ver)
		cfg.Logger.Info("downloading kernel source", "url", url)

		resp, err := http.Get(url)
		if err != nil {
			return fmt.Errorf("download %s: %w", url, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("download %s: HTTP %d", url, resp.StatusCode)
		}

		f, err := os.Create(tarball)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, resp.Body); err != nil {
			f.Close()
			os.Remove(tarball)
			return fmt.Errorf("save tarball: %w", err)
		}
		f.Close()
		cfg.Logger.Info("kernel source downloaded", "path", tarball)
	}

	// Extract.
	cfg.Logger.Info("extracting kernel source", "version", ver)
	cmd := exec.Command("tar", "xf", tarball, "-C", buildDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("extract tarball: %w", err)
	}

	return nil
}

// runBuildDocker runs the build script inside a Docker container with bind mounts.
func runBuildDocker(cfg buildConfig, projectDir, outputDir, buildDir string) error {
	logger := cfg.Logger
	image := cfg.Image

	logger.Info("running build in Docker", "image", image)

	args := []string{
		"run", "--rm",
		"--platform", "linux/" + cfg.Arch,
		"-v", projectDir + ":/workspace:ro",
		"-v", outputDir + ":/output",
		"-v", buildDir + ":/build",
		image,
		"/bin/sh", "/build/build.sh",
	}

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	elapsed := time.Since(start)

	// Verify output.
	outputFile := filepath.Join(outputDir, "vmlinux-lokavm")
	info, err := os.Stat(outputFile)
	if err != nil || info.Size() == 0 {
		return fmt.Errorf("kernel output not found at %s", outputFile)
	}

	logger.Info("kernel build complete",
		"output", outputFile,
		"size_mb", info.Size()/(1024*1024),
		"elapsed", elapsed.Round(time.Second))
	return nil
}

// pullBuildImage pulls the Ubuntu image via crane and returns the rootfs path.
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

	// Build boot args.
	// Don't use init= — let the initramfs handle virtiofs mounts first,
	// then exec the build script via loka.exec= parameter.
	// Boot args: use virtiofs direct rootfs (not overlay layers — the bootstrap
	// kernel may lack CONFIG_OVERLAY_FS_REDIRECT_DIR for dynamic linker symlinks).
	// loka.exec runs the build script instead of loka-supervisor after switch_root.
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

	elapsed := time.Since(start)

	// Verify output.
	outputFile := filepath.Join(cfg.OutputDir, "vmlinux-lokavm")
	info, err := os.Stat(outputFile)
	if err != nil || info.Size() == 0 {
		return fmt.Errorf("kernel output not found at %s (build may have failed)", outputFile)
	}

	logger.Info("kernel build complete",
		"output", outputFile,
		"size_mb", info.Size()/(1024*1024),
		"elapsed", elapsed.Round(time.Second))

	return nil
}

func findProjectRoot() (string, error) {
	// Walk up from CWD looking for go.mod.
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("could not find project root (no go.mod)")
		}
		dir = parent
	}
}

func newLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
}
