package virtio

import (
	"os"
	"path/filepath"
	"testing"
)

func setupDirectBackend(t *testing.T) (*DirectBackend, string) {
	t.Helper()
	dir := t.TempDir()

	// Create some test files.
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello world"), 0o644)
	os.MkdirAll(filepath.Join(dir, "subdir"), 0o755)
	os.WriteFile(filepath.Join(dir, "subdir", "nested.txt"), []byte("nested content"), 0o644)
	os.Symlink("hello.txt", filepath.Join(dir, "link.txt"))

	return NewDirectBackend(dir, false), dir
}

func TestDirectLookup(t *testing.T) {
	b, _ := setupDirectBackend(t)

	attr, ino, err := b.Lookup(1, "hello.txt")
	if err != nil {
		t.Fatalf("Lookup hello.txt: %v", err)
	}
	if ino == 0 || ino == 1 {
		t.Errorf("expected non-root ino, got %d", ino)
	}
	if attr.Size != 11 {
		t.Errorf("expected size 11, got %d", attr.Size)
	}
	if attr.Mode&0o777 != 0o644 {
		t.Errorf("expected mode 644, got %o", attr.Mode&0o777)
	}
}

func TestDirectLookupNotFound(t *testing.T) {
	b, _ := setupDirectBackend(t)

	_, _, err := b.Lookup(1, "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestDirectLookupNested(t *testing.T) {
	b, _ := setupDirectBackend(t)

	// Lookup subdir.
	_, dirIno, err := b.Lookup(1, "subdir")
	if err != nil {
		t.Fatalf("Lookup subdir: %v", err)
	}

	// Lookup file inside subdir.
	attr, _, err := b.Lookup(dirIno, "nested.txt")
	if err != nil {
		t.Fatalf("Lookup nested.txt: %v", err)
	}
	if attr.Size != 14 {
		t.Errorf("expected size 14, got %d", attr.Size)
	}
}

func TestDirectGetattr(t *testing.T) {
	b, _ := setupDirectBackend(t)

	// Root inode.
	attr, err := b.Getattr(1)
	if err != nil {
		t.Fatalf("Getattr root: %v", err)
	}
	if attr.Mode&uint32(os.ModeDir) == 0 && attr.Mode&0o40000 == 0 {
		t.Errorf("expected root to be a directory, mode=%o (%032b)", attr.Mode, attr.Mode)
	}
}

func TestDirectReaddir(t *testing.T) {
	b, _ := setupDirectBackend(t)

	entries, err := b.Readdir(1, 0)
	if err != nil {
		t.Fatalf("Readdir: %v", err)
	}

	names := make(map[string]bool)
	for _, e := range entries {
		names[e.Name] = true
	}

	for _, want := range []string{"hello.txt", "subdir", "link.txt"} {
		if !names[want] {
			t.Errorf("missing entry %q in readdir", want)
		}
	}
}

func TestDirectOpenReadClose(t *testing.T) {
	b, _ := setupDirectBackend(t)

	_, ino, _ := b.Lookup(1, "hello.txt")

	fh, err := b.Open(ino, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := b.Read(ino, fh, 0, 100)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("Read: got %q", data)
	}

	// Read with offset.
	data, err = b.Read(ino, fh, 6, 5)
	if err != nil {
		t.Fatalf("Read offset: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("Read offset: got %q", data)
	}

	if err := b.Release(ino, fh); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestDirectCreateWriteRead(t *testing.T) {
	b, _ := setupDirectBackend(t)

	attr, ino, fh, err := b.Create(1, "new.txt", 0o644, 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if attr == nil || ino == 0 {
		t.Fatal("expected non-nil attr and non-zero ino")
	}

	n, err := b.Write(ino, fh, 0, []byte("created content"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 15 {
		t.Errorf("Write: wrote %d bytes", n)
	}

	data, err := b.Read(ino, fh, 0, 100)
	if err != nil {
		t.Fatalf("Read after write: %v", err)
	}
	if string(data) != "created content" {
		t.Errorf("Read after write: got %q", data)
	}

	b.Release(ino, fh)
}

func TestDirectMkdir(t *testing.T) {
	b, _ := setupDirectBackend(t)

	attr, ino, err := b.Mkdir(1, "newdir", 0o755)
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if attr == nil || ino == 0 {
		t.Fatal("expected non-nil attr and non-zero ino")
	}

	// Create file inside new dir.
	_, _, _, err = b.Create(ino, "inside.txt", 0o644, 0)
	if err != nil {
		t.Fatalf("Create inside newdir: %v", err)
	}

	// Verify via readdir.
	entries, _ := b.Readdir(ino, 0)
	if len(entries) != 1 || entries[0].Name != "inside.txt" {
		t.Errorf("expected inside.txt in newdir, got %v", entries)
	}
}

func TestDirectUnlink(t *testing.T) {
	b, dir := setupDirectBackend(t)

	if err := b.Unlink(1, "hello.txt"); err != nil {
		t.Fatalf("Unlink: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

func TestDirectRmdir(t *testing.T) {
	b, _ := setupDirectBackend(t)

	// Create empty dir and remove it.
	b.Mkdir(1, "emptydir", 0o755)
	if err := b.Rmdir(1, "emptydir"); err != nil {
		t.Fatalf("Rmdir: %v", err)
	}
}

func TestDirectRename(t *testing.T) {
	b, dir := setupDirectBackend(t)

	if err := b.Rename(1, "hello.txt", 1, "renamed.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "renamed.txt")); err != nil {
		t.Error("renamed.txt should exist")
	}
	if _, err := os.Stat(filepath.Join(dir, "hello.txt")); !os.IsNotExist(err) {
		t.Error("hello.txt should not exist after rename")
	}
}

func TestDirectSymlinkReadlink(t *testing.T) {
	b, _ := setupDirectBackend(t)

	attr, ino, err := b.Symlink(1, "newsym", "subdir/nested.txt")
	if err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if attr == nil || ino == 0 {
		t.Fatal("expected valid symlink attr")
	}

	target, err := b.Readlink(ino)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if target != "subdir/nested.txt" {
		t.Errorf("Readlink: got %q", target)
	}
}

func TestDirectStatfs(t *testing.T) {
	b, _ := setupDirectBackend(t)
	st, err := b.Statfs(1)
	if err != nil {
		t.Fatalf("Statfs: %v", err)
	}
	if st.Bsize != 4096 || st.Namelen != 255 {
		t.Errorf("Statfs: bsize=%d namelen=%d", st.Bsize, st.Namelen)
	}
}

func TestDirectReadOnly(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("data"), 0o644)
	b := NewDirectBackend(dir, true)

	_, _, _, err := b.Create(1, "new.txt", 0o644, 0)
	if err == nil {
		t.Error("expected error for create on read-only backend")
	}

	_, _, err = b.Mkdir(1, "newdir", 0o755)
	if err == nil {
		t.Error("expected error for mkdir on read-only backend")
	}

	// Read should still work.
	_, ino, _ := b.Lookup(1, "file.txt")
	fh, err := b.Open(ino, 0)
	if err != nil {
		t.Fatalf("Open on read-only: %v", err)
	}
	data, _ := b.Read(ino, fh, 0, 100)
	if string(data) != "data" {
		t.Errorf("Read on read-only: got %q", data)
	}
	b.Release(ino, fh)
}

func TestDirectSetattr(t *testing.T) {
	b, dir := setupDirectBackend(t)
	_, ino, _ := b.Lookup(1, "hello.txt")

	// Truncate.
	attr, err := b.Setattr(ino, &FuseAttr{Size: 5}, 1<<3)
	if err != nil {
		t.Fatalf("Setattr truncate: %v", err)
	}
	if attr.Size != 5 {
		t.Errorf("Setattr: size=%d, want 5", attr.Size)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if string(data) != "hello" {
		t.Errorf("after truncate: %q", data)
	}
}
