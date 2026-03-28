// loka-build builds Linux kernel and initramfs for lokavm.
//
// Kernel and initramfs are cross-compiled directly on the host using a
// cross-compile toolchain (e.g. aarch64-linux-gnu-gcc). No Docker or VM needed.
//
// E2E tests still use a VM (pkg/lokavm) for running the test suite on Linux.
//
// Usage:
//
//	loka-build kernel              Build Linux kernel (cross-compile)
//	loka-build initramfs           Build initramfs
//	loka-build all                 Build both
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"runtime"
)

const defaultKernelVersion = "6.12.8"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: loka-build <kernel|initramfs|all|e2e> [flags]")
		os.Exit(1)
	}

	cmd := os.Args[1]
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	arch := fs.String("arch", runtime.GOARCH, "Target architecture (arm64, amd64)")
	kernelVer := fs.String("kernel-version", defaultKernelVersion, "Linux kernel version")
	outputDir := fs.String("output", "build", "Output directory")
	jobs := fs.Int("jobs", 0, "Parallel make jobs (0 = auto)")
	image := fs.String("image", "alpine:3.21", "Build VM base image")
	fs.Parse(os.Args[2:])

	cfg := buildConfig{
		Arch:          *arch,
		KernelVersion: *kernelVer,
		OutputDir:     *outputDir,
		Jobs:          *jobs,
		Image:         *image,
		Logger:        logger,
	}

	var err error
	switch cmd {
	case "kernel":
		err = buildKernel(cfg)
	case "initramfs":
		err = buildInitramfs(cfg)
	case "all":
		if err = buildKernel(cfg); err == nil {
			err = buildInitramfs(cfg)
		}
	case "e2e":
		err = buildE2E(cfg)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		os.Exit(1)
	}

	if err != nil {
		logger.Error("build failed", "error", err)
		os.Exit(1)
	}
}

type buildConfig struct {
	Arch          string
	KernelVersion string
	OutputDir     string
	Jobs          int
	Image         string
	Logger        *slog.Logger
}
