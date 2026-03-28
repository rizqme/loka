package main

import (
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

	projectDir, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf("find project root: %w", err)
	}

	// Detect cross-compile toolchain.
	crossPrefix, err := detectCrossCompile(cfg.Arch)
	if err != nil {
		return err
	}

	// Check host build dependencies.
	if err := checkKernelBuildDeps(crossPrefix); err != nil {
		return err
	}

	cfg.Logger.Info("building kernel",
		"version", cfg.KernelVersion,
		"arch", cfg.Arch,
		"cross_compile", crossPrefix,
		"output", outputFile)

	// Download kernel source (cached).
	buildDir := filepath.Join(cfg.OutputDir, ".build-cache", "kernel-src")
	os.MkdirAll(buildDir, 0o755)

	if err := downloadKernelSource(cfg, buildDir); err != nil {
		return fmt.Errorf("download kernel source: %w", err)
	}

	// Cross-compile directly on host.
	return crossCompileKernel(cfg, projectDir, buildDir, crossPrefix)
}

// detectCrossCompile finds the appropriate CROSS_COMPILE prefix for the target arch.
// On Linux building for the native arch, returns "" (no cross-compile needed).
// On macOS or when targeting a different arch, finds a GCC cross-toolchain.
func detectCrossCompile(targetArch string) (string, error) {
	normalized := normalizeArch(targetArch)

	// On Linux, native builds don't need a cross-compiler.
	if runtime.GOOS == "linux" && runtime.GOARCH == normalized {
		return "", nil
	}

	// Find a cross-compiler.
	var prefixes []string
	switch normalized {
	case "arm64":
		prefixes = []string{
			"aarch64-linux-gnu-",
			"aarch64-unknown-linux-gnu-",
		}
	case "amd64":
		prefixes = []string{
			"x86_64-linux-gnu-",
			"x86_64-unknown-linux-gnu-",
		}
	default:
		return "", fmt.Errorf("unsupported target architecture: %s", targetArch)
	}

	for _, prefix := range prefixes {
		if _, err := exec.LookPath(prefix + "gcc"); err == nil {
			return prefix, nil
		}
	}

	// Provide install instructions.
	var installHint string
	switch {
	case runtime.GOOS == "darwin" && normalized == "arm64":
		installHint = "brew tap messense/macos-cross-toolchains && brew install aarch64-unknown-linux-gnu"
	case runtime.GOOS == "darwin" && normalized == "amd64":
		installHint = "brew tap messense/macos-cross-toolchains && brew install x86_64-unknown-linux-gnu"
	case runtime.GOOS == "linux" && normalized == "arm64":
		installHint = "apt install gcc-aarch64-linux-gnu"
	case runtime.GOOS == "linux" && normalized == "amd64":
		installHint = "apt install gcc-x86-64-linux-gnu"
	}

	return "", fmt.Errorf("cross-compile toolchain not found for %s\n  Tried: %s\n  Install: %s",
		targetArch, strings.Join(prefixes, ", "), installHint)
}

// checkKernelBuildDeps verifies that required host tools are available.
func checkKernelBuildDeps(crossPrefix string) error {
	required := []string{"make", "flex", "bison", "bc", "perl"}
	if crossPrefix != "" {
		required = append(required, crossPrefix+"gcc")
	} else {
		required = append(required, "gcc")
	}

	var missing []string
	for _, tool := range required {
		if _, err := exec.LookPath(tool); err != nil {
			missing = append(missing, tool)
		}
	}

	if len(missing) > 0 {
		hint := ""
		if runtime.GOOS == "darwin" {
			hint = fmt.Sprintf("\n  Install: brew install %s", strings.Join(missing, " "))
		} else {
			hint = fmt.Sprintf("\n  Install: apt install %s", strings.Join(missing, " "))
		}
		return fmt.Errorf("missing build tools: %s%s", strings.Join(missing, ", "), hint)
	}

	return nil
}

// crossCompileKernel runs the kernel build directly on the host with CROSS_COMPILE.
func crossCompileKernel(cfg buildConfig, projectDir, buildDir, crossPrefix string) error {
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

	srcDir := filepath.Join(buildDir, "linux-"+ver)
	outputFile := filepath.Join(cfg.OutputDir, "vmlinux-lokavm")
	configPath := filepath.Join(projectDir, "scripts", "kernel.config")

	makeArgs := func(targets ...string) []string {
		args := []string{fmt.Sprintf("ARCH=%s", archMake)}
		if crossPrefix != "" {
			args = append(args, fmt.Sprintf("CROSS_COMPILE=%s", crossPrefix))
		}
		args = append(args, fmt.Sprintf("-j%d", jobs))
		args = append(args, targets...)
		return args
	}

	runMake := func(targets ...string) error {
		cmd := exec.Command("make", makeArgs(targets...)...)
		cmd.Dir = srcDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Step 1: make defconfig.
	cfg.Logger.Info("configuring kernel", "arch", archMake)
	if err := runMake("defconfig"); err != nil {
		return fmt.Errorf("make defconfig: %w", err)
	}

	// Step 2: append loka kernel config.
	if _, err := os.Stat(configPath); err == nil {
		cfg.Logger.Info("appending loka kernel config", "path", configPath)
		extra, err := os.ReadFile(configPath)
		if err != nil {
			return fmt.Errorf("read kernel.config: %w", err)
		}
		f, err := os.OpenFile(filepath.Join(srcDir, ".config"), os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("open .config: %w", err)
		}
		f.Write([]byte("\n"))
		f.Write(extra)
		f.Close()
	}

	// Step 3: make olddefconfig.
	if err := runMake("olddefconfig"); err != nil {
		return fmt.Errorf("make olddefconfig: %w", err)
	}

	// Step 4: compile.
	cfg.Logger.Info("compiling kernel", "target", archTarget, "jobs", jobs)
	start := time.Now()
	if err := runMake(archTarget); err != nil {
		return fmt.Errorf("make %s: %w", archTarget, err)
	}

	// Step 5: copy output.
	bootImage := filepath.Join(srcDir, archBootPath)
	data, err := os.ReadFile(bootImage)
	if err != nil {
		return fmt.Errorf("read kernel image %s: %w", archBootPath, err)
	}
	if err := os.WriteFile(outputFile, data, 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	info, _ := os.Stat(outputFile)
	elapsed := time.Since(start)
	cfg.Logger.Info("kernel build complete",
		"output", outputFile,
		"size_mb", info.Size()/(1024*1024),
		"elapsed", elapsed.Round(time.Second))

	return nil
}

func normalizeArch(arch string) string {
	switch arch {
	case "aarch64":
		return "arm64"
	case "x86_64":
		return "amd64"
	default:
		return arch
	}
}

// downloadKernelSource downloads and extracts the kernel source on the host.
// The extracted source is cached in buildDir.
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
