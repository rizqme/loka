package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/vyprai/loka/pkg/lokavm"
)

func buildE2E(cfg buildConfig) error {
	projectDir, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf("find project root: %w", err)
	}

	absProject, _ := filepath.Abs(projectDir)
	arch := cfg.Arch
	if arch == "arm64" {
		arch = "arm64"
	} else if arch == "amd64" || arch == "x86_64" {
		arch = "amd64"
	}

	// Verify Linux binaries exist.
	binDir := filepath.Join(absProject, "build", fmt.Sprintf("linux-%s", arch))
	for _, bin := range []string{"lokad", "loka", "loka-supervisor"} {
		p := filepath.Join(binDir, bin)
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("binary not found: %s (run 'make build-linux-%s' first)", p, arch)
		}
	}

	// Verify kernel exists.
	kernelPath := filepath.Join(absProject, "build", "vmlinux-lokavm")
	if _, err := os.Stat(kernelPath); err != nil {
		return fmt.Errorf("kernel not found: %s (run 'make kernel' first)", kernelPath)
	}

	cfg.Logger.Info("running e2e tests in VM",
		"arch", arch,
		"project", absProject,
		"binaries", binDir)

	// Pull build image.
	rootfsDir, err := pullBuildImage(cfg)
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	// Create scratch and output dirs.
	buildDir, err := os.MkdirTemp("", "loka-e2e-*")
	if err != nil {
		return fmt.Errorf("create build dir: %w", err)
	}
	defer os.RemoveAll(buildDir)

	outputDir := filepath.Join(absProject, "build", "e2e-output")
	os.MkdirAll(outputDir, 0o755)

	// Write e2e runner script.
	script := generateE2EScript(cfg, arch)
	if err := os.WriteFile(filepath.Join(buildDir, "build.sh"), []byte(script), 0o755); err != nil {
		return fmt.Errorf("write e2e script: %w", err)
	}

	// Boot VM with all mounts — no copying.
	return runBuildVM(cfg, rootfsDir, []lokavm.SharedDir{
		{Tag: "workspace", HostPath: absProject, GuestPath: "/workspace", ReadOnly: true},
		{Tag: "output", HostPath: outputDir, GuestPath: "/output", ReadOnly: false},
		{Tag: "build", HostPath: buildDir, GuestPath: "/build", ReadOnly: false},
	}, "/build/build.sh")
}

func generateE2EScript(cfg buildConfig, arch string) string {
	jobs := cfg.Jobs
	if jobs <= 0 {
		jobs = runtime.NumCPU()
	}

	return fmt.Sprintf(`#!/bin/sh
set -e

echo "==> Setting up e2e test environment..."

# Install test dependencies.
apt-get update -qq
apt-get install -y -qq curl jq python3 2>&1 | tail -1

# Enable KVM if available (nested virtualization).
if [ -e /dev/kvm ]; then
    chmod 666 /dev/kvm
    echo "  KVM available"
else
    echo "  WARNING: /dev/kvm not found — VM tests will fail"
fi

# Symlink binaries from workspace (no copying).
BIN_DIR="/workspace/build/linux-%s"
for bin in lokad loka loka-supervisor; do
    ln -sf "$BIN_DIR/$bin" "/usr/local/bin/$bin"
done
echo "  binaries linked from $BIN_DIR"

# Prepare runtime directory.
mkdir -p /run/loka-e2e/kernel
ln -sf /workspace/build/vmlinux-lokavm /run/loka-e2e/kernel/vmlinux
if [ -f /workspace/build/initramfs.cpio.gz ]; then
    ln -sf /workspace/build/initramfs.cpio.gz /run/loka-e2e/kernel/initramfs.cpio.gz
fi
echo "  kernel linked"

# Set environment for e2e script.
export LOKA_BIN=/usr/local/bin/loka
export LOKAD_BIN=/usr/local/bin/lokad
export LOKA_KERNEL_PATH=/run/loka-e2e/kernel/vmlinux
export LOKA_INITRD_PATH=/run/loka-e2e/kernel/initramfs.cpio.gz

# Disable Go build in e2e script (binaries already built).
export SKIP_GO_BUILD=1

echo "==> Running e2e tests..."
bash /workspace/scripts/e2e-test.sh 2>&1 | tee /output/e2e.log
EXIT_CODE=$?

echo "==> E2E tests finished (exit code: $EXIT_CODE)"
echo "$EXIT_CODE" > /output/exit-code

exit $EXIT_CODE
`, arch)
}
