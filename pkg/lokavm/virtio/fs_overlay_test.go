package virtio

import (
	"os"
	"path/filepath"
	"testing"
)

func setupOverlayBackend(t *testing.T) (*OverlayBackend, string, string, []string) {
	t.Helper()
	base := t.TempDir()

	// Layer 0 (bottom): base OS files.
	layer0 := filepath.Join(base, "layer0")
	os.MkdirAll(filepath.Join(layer0, "etc"), 0o755)
	os.WriteFile(filepath.Join(layer0, "etc", "hostname"), []byte("base"), 0o644)
	os.WriteFile(filepath.Join(layer0, "base.txt"), []byte("from layer0"), 0o644)
	os.MkdirAll(filepath.Join(layer0, "bin"), 0o755)
	os.WriteFile(filepath.Join(layer0, "bin", "sh"), []byte("shell"), 0o755)

	// Layer 1 (top): app files, overrides hostname.
	layer1 := filepath.Join(base, "layer1")
	os.MkdirAll(filepath.Join(layer1, "etc"), 0o755)
	os.WriteFile(filepath.Join(layer1, "etc", "hostname"), []byte("app"), 0o644)
	os.WriteFile(filepath.Join(layer1, "app.txt"), []byte("from layer1"), 0o644)

	// Upper dir (writable, per-VM).
	upper := filepath.Join(base, "upper")
	os.MkdirAll(upper, 0o755)

	layers := []string{layer1, layer0} // Top-to-bottom order.
	b := NewOverlayBackend(upper, layers)
	return b, upper, base, layers
}

func TestOverlayLookupFromLayers(t *testing.T) {
	b, _, _, _ := setupOverlayBackend(t)

	// File from layer0.
	attr, _, err := b.Lookup(1, "base.txt")
	if err != nil {
		t.Fatalf("Lookup base.txt: %v", err)
	}
	if attr.Size != 11 {
		t.Errorf("base.txt size=%d, want 11", attr.Size)
	}

	// File from layer1.
	attr, _, err = b.Lookup(1, "app.txt")
	if err != nil {
		t.Fatalf("Lookup app.txt: %v", err)
	}
	if attr.Size != 11 {
		t.Errorf("app.txt size=%d, want 11", attr.Size)
	}
}

func TestOverlayLookupTopLayerWins(t *testing.T) {
	b, _, _, _ := setupOverlayBackend(t)

	// etc/hostname exists in both layers — layer1 (top) should win.
	_, etcIno, err := b.Lookup(1, "etc")
	if err != nil {
		t.Fatalf("Lookup etc: %v", err)
	}

	_, ino, err := b.Lookup(etcIno, "hostname")
	if err != nil {
		t.Fatalf("Lookup hostname: %v", err)
	}

	fh, _ := b.Open(ino, 0)
	data, _ := b.Read(ino, fh, 0, 100)
	b.Release(ino, fh)

	if string(data) != "app" {
		t.Errorf("hostname content=%q, want 'app' (from layer1)", data)
	}
}

func TestOverlayWriteCopyOnWrite(t *testing.T) {
	b, upper, _, _ := setupOverlayBackend(t)

	// Open base.txt (from layer0) for writing — should trigger copy-up.
	_, ino, _ := b.Lookup(1, "base.txt")
	fh, err := b.Open(ino, uint32(os.O_RDWR))
	if err != nil {
		t.Fatalf("Open for write: %v", err)
	}

	n, err := b.Write(ino, fh, 0, []byte("modified"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 8 {
		t.Errorf("Write: n=%d, want 8", n)
	}
	b.Release(ino, fh)

	// Verify the file was copied to upper.
	data, err := os.ReadFile(filepath.Join(upper, "base.txt"))
	if err != nil {
		t.Fatalf("read upper/base.txt: %v", err)
	}
	if string(data[:8]) != "modified" {
		t.Errorf("upper/base.txt = %q", data)
	}
}

func TestOverlayCreateInUpper(t *testing.T) {
	b, upper, _, _ := setupOverlayBackend(t)

	_, ino, fh, err := b.Create(1, "new.txt", 0o644, 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	b.Write(ino, fh, 0, []byte("new content"))
	b.Release(ino, fh)

	// Should be in upper.
	data, err := os.ReadFile(filepath.Join(upper, "new.txt"))
	if err != nil {
		t.Fatalf("read upper/new.txt: %v", err)
	}
	if string(data) != "new content" {
		t.Errorf("new.txt = %q", data)
	}
}

func TestOverlayDeleteCreatesWhiteout(t *testing.T) {
	b, upper, _, _ := setupOverlayBackend(t)

	// Delete base.txt (from layer0).
	if err := b.Unlink(1, "base.txt"); err != nil {
		t.Fatalf("Unlink: %v", err)
	}

	// Whiteout should exist in upper.
	whiteout := filepath.Join(upper, ".wh.base.txt")
	if _, err := os.Stat(whiteout); os.IsNotExist(err) {
		t.Error("expected whiteout file to exist")
	}

	// Lookup should fail now.
	_, _, err := b.Lookup(1, "base.txt")
	if err == nil {
		t.Error("expected Lookup to fail after delete")
	}
}

func TestOverlayReaddirMerged(t *testing.T) {
	b, _, _, _ := setupOverlayBackend(t)

	entries, err := b.Readdir(1, 0)
	if err != nil {
		t.Fatalf("Readdir: %v", err)
	}

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name] = true
	}

	// Should have files from all layers merged.
	for _, want := range []string{"base.txt", "app.txt", "etc", "bin"} {
		if !names[want] {
			t.Errorf("missing %q in merged readdir (got %v)", want, names)
		}
	}
}

func TestOverlayReaddirHidesWhiteouts(t *testing.T) {
	b, _, _, _ := setupOverlayBackend(t)

	b.Unlink(1, "base.txt") // Creates whiteout.

	entries, err := b.Readdir(1, 0)
	if err != nil {
		t.Fatalf("Readdir: %v", err)
	}

	for _, e := range entries {
		if e.Name == "base.txt" {
			t.Error("base.txt should be hidden by whiteout")
		}
		if e.Name == ".wh.base.txt" {
			t.Error("whiteout file should not appear in readdir")
		}
	}
}

func TestOverlayMkdirInUpper(t *testing.T) {
	b, upper, _, _ := setupOverlayBackend(t)

	_, _, err := b.Mkdir(1, "newdir", 0o755)
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	if _, err := os.Stat(filepath.Join(upper, "newdir")); err != nil {
		t.Error("expected newdir in upper")
	}
}

func TestOverlayRename(t *testing.T) {
	b, upper, _, _ := setupOverlayBackend(t)

	// Rename base.txt → moved.txt (copy-up + rename + whiteout).
	if err := b.Rename(1, "base.txt", 1, "moved.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	// moved.txt in upper.
	if _, err := os.Stat(filepath.Join(upper, "moved.txt")); err != nil {
		t.Error("expected moved.txt in upper")
	}

	// Whiteout for base.txt.
	if _, err := os.Stat(filepath.Join(upper, ".wh.base.txt")); os.IsNotExist(err) {
		t.Error("expected whiteout for base.txt after rename")
	}
}

func TestOverlayUpperTakesPrecedence(t *testing.T) {
	b, upper, _, _ := setupOverlayBackend(t)

	// Write directly to upper (simulating a previous write).
	os.WriteFile(filepath.Join(upper, "base.txt"), []byte("upper version"), 0o644)

	_, ino, _ := b.Lookup(1, "base.txt")
	fh, _ := b.Open(ino, 0)
	data, _ := b.Read(ino, fh, 0, 100)
	b.Release(ino, fh)

	if string(data) != "upper version" {
		t.Errorf("expected upper version, got %q", data)
	}
}

func TestOverlaySymlink(t *testing.T) {
	b, _, _, _ := setupOverlayBackend(t)

	_, ino, err := b.Symlink(1, "link", "app.txt")
	if err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	target, err := b.Readlink(ino)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if target != "app.txt" {
		t.Errorf("Readlink: got %q", target)
	}
}
