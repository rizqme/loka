//go:build darwin

package main

import (
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

const (
	kernelURL  = "https://s3.amazonaws.com/spec.ccfc.min/ci-artifacts/kernels/%s/vmlinux-5.10.bin"
	releaseURL = "https://github.com/vyprai/loka/releases/latest/download"
)

// ensureAssets checks that kernel and rootfs exist in dataDir.
// Downloads them from GitHub releases if missing.
// Returns paths to kernel and rootfs.
func ensureAssets(dataDir string) (kernelPath, rootfsPath string, err error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", "", fmt.Errorf("create data dir: %w", err)
	}

	kernelPath = filepath.Join(dataDir, "vmlinux")
	rootfsPath = filepath.Join(dataDir, "rootfs.ext4")

	// Download kernel if missing.
	if _, statErr := os.Stat(kernelPath); os.IsNotExist(statErr) {
		fmt.Print("  Downloading kernel...")
		arch := runtime.GOARCH
		if arch == "arm64" {
			arch = "aarch64"
		}
		url := fmt.Sprintf(kernelURL, arch)
		if dlErr := downloadFile(url, kernelPath); dlErr != nil {
			return "", "", fmt.Errorf("download kernel: %w", dlErr)
		}
		fmt.Println(" ok")
	}

	// Download rootfs if missing.
	if _, statErr := os.Stat(rootfsPath); os.IsNotExist(statErr) {
		fmt.Print("  Downloading rootfs...")
		arch := "arm64"
		if runtime.GOARCH == "amd64" {
			arch = "amd64"
		}
		url := fmt.Sprintf("%s/loka-rootfs-%s.ext4.gz", releaseURL, arch)
		gzPath := rootfsPath + ".gz"
		if dlErr := downloadFile(url, gzPath); dlErr != nil {
			// Fallback: create minimal rootfs locally.
			fmt.Print(" (creating locally)...")
			if createErr := createMinimalRootfs(rootfsPath); createErr != nil {
				return "", "", fmt.Errorf("create rootfs: %w", createErr)
			}
		} else {
			// Decompress.
			if gzErr := gunzipFile(gzPath, rootfsPath); gzErr != nil {
				return "", "", fmt.Errorf("decompress rootfs: %w", gzErr)
			}
			os.Remove(gzPath)
		}
		fmt.Println(" ok")
	}

	return kernelPath, rootfsPath, nil
}

func downloadFile(url, destPath string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func gunzipFile(src, dst string) error {
	sf, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open %s: %w", src, err)
	}
	defer sf.Close()

	gr, err := gzip.NewReader(sf)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gr.Close()

	df, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create %s: %w", dst, err)
	}
	defer df.Close()

	_, err = io.Copy(df, gr)
	return err
}

func createMinimalRootfs(path string) error {
	// Create a sparse 50GB ext4 image.
	// This is the fallback if GitHub releases aren't available.
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := f.Truncate(50 * 1024 * 1024 * 1024); err != nil { // 50GB sparse
		f.Close()
		return err
	}
	f.Close()

	// mkfs.ext4
	if err := exec.Command("mkfs.ext4", "-F", "-q", path).Run(); err != nil {
		return fmt.Errorf("mkfs.ext4: %w", err)
	}
	return nil
}
