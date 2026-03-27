package main

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// extractTar extracts a tar stream into destDir.
func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		// Sanitize path to prevent traversal.
		target := filepath.Join(destDir, hdr.Name)
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) && target != filepath.Clean(destDir) {
			continue
		}

		// Handle OCI whiteouts.
		base := filepath.Base(hdr.Name)
		if strings.HasPrefix(base, ".wh.") {
			// Whiteout: delete the target file/dir.
			realName := strings.TrimPrefix(base, ".wh.")
			os.RemoveAll(filepath.Join(filepath.Dir(target), realName))
			continue
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			os.MkdirAll(target, os.FileMode(hdr.Mode)|0o755)
		case tar.TypeReg:
			os.MkdirAll(filepath.Dir(target), 0o755)
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				continue // Skip files we can't create (e.g. device nodes).
			}
			io.Copy(f, tr)
			f.Close()
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(target), 0o755)
			os.Remove(target)
			os.Symlink(hdr.Linkname, target)
		case tar.TypeLink:
			os.MkdirAll(filepath.Dir(target), 0o755)
			linkTarget := filepath.Join(destDir, hdr.Linkname)
			os.Remove(target)
			os.Link(linkTarget, target)
		}
	}
}
