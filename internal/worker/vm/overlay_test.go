package vm

import (
	"os"
	"path/filepath"
	"testing"
)

func setupTestOverlay(t *testing.T) (*OverlayManager, string) {
	t.Helper()
	dir := t.TempDir()
	mgr := NewOverlayManager(dir)
	sid := "test-session"
	if err := mgr.Init(sid); err != nil {
		t.Fatal(err)
	}
	return mgr, sid
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFullDiff_Added(t *testing.T) {
	mgr, sid := setupTestOverlay(t)
	ws := mgr.WorkspacePath(sid)

	// Snapshot A: empty.
	writeFile(t, filepath.Join(ws, "existing.txt"), "hello")
	layerA, _ := mgr.CreateLayer(sid)

	// Add files.
	writeFile(t, filepath.Join(ws, "new.txt"), "new content")
	writeFile(t, filepath.Join(ws, "sub/deep.txt"), "deep")
	layerB, _ := mgr.CreateLayer(sid)

	summary, err := mgr.FullDiff(sid, layerA, layerB)
	if err != nil {
		t.Fatal(err)
	}

	if summary.Added < 2 {
		t.Errorf("added = %d, want >= 2 (files + possible dirs)", summary.Added)
	}

	// Check that new files are in the entries.
	found := map[string]bool{}
	for _, e := range summary.Entries {
		if e.Type == DiffAdded {
			found[e.Path] = true
		}
	}
	if !found["new.txt"] {
		t.Error("new.txt should be in added entries")
	}
}

func TestFullDiff_Modified(t *testing.T) {
	mgr, sid := setupTestOverlay(t)
	ws := mgr.WorkspacePath(sid)

	writeFile(t, filepath.Join(ws, "data.txt"), "version 1")
	layerA, _ := mgr.CreateLayer(sid)

	writeFile(t, filepath.Join(ws, "data.txt"), "version 2 - different content")
	layerB, _ := mgr.CreateLayer(sid)

	summary, err := mgr.FullDiff(sid, layerA, layerB)
	if err != nil {
		t.Fatal(err)
	}

	if summary.Modified != 1 {
		t.Errorf("modified = %d, want 1", summary.Modified)
	}

	// Check hash differs.
	for _, e := range summary.Entries {
		if e.Path == "data.txt" && e.Type == DiffModified {
			if e.Hash == e.OldHash {
				t.Error("hashes should differ for modified file")
			}
			if e.OldSize == e.Size {
				t.Error("sizes should differ")
			}
			return
		}
	}
	t.Error("data.txt not found in modified entries")
}

func TestFullDiff_Deleted(t *testing.T) {
	mgr, sid := setupTestOverlay(t)
	ws := mgr.WorkspacePath(sid)

	writeFile(t, filepath.Join(ws, "keep.txt"), "keep")
	writeFile(t, filepath.Join(ws, "remove.txt"), "remove me")
	layerA, _ := mgr.CreateLayer(sid)

	os.Remove(filepath.Join(ws, "remove.txt"))
	layerB, _ := mgr.CreateLayer(sid)

	summary, err := mgr.FullDiff(sid, layerA, layerB)
	if err != nil {
		t.Fatal(err)
	}

	if summary.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", summary.Deleted)
	}

	for _, e := range summary.Entries {
		if e.Path == "remove.txt" {
			if e.Type != DiffDeleted {
				t.Errorf("remove.txt type = %s, want deleted", e.Type)
			}
			if e.OldSize == 0 {
				t.Error("deleted file should have old_size")
			}
			return
		}
	}
	t.Error("remove.txt not in entries")
}

func TestFullDiff_ModeChange(t *testing.T) {
	mgr, sid := setupTestOverlay(t)
	ws := mgr.WorkspacePath(sid)

	writeFile(t, filepath.Join(ws, "script.sh"), "#!/bin/sh\necho hi")
	layerA, _ := mgr.CreateLayer(sid)

	os.Chmod(filepath.Join(ws, "script.sh"), 0o755)
	layerB, _ := mgr.CreateLayer(sid)

	summary, err := mgr.FullDiff(sid, layerA, layerB)
	if err != nil {
		t.Fatal(err)
	}

	if summary.ModeChanged != 1 {
		t.Errorf("mode_changed = %d, want 1", summary.ModeChanged)
	}
}

func TestFullDiff_Mixed(t *testing.T) {
	mgr, sid := setupTestOverlay(t)
	ws := mgr.WorkspacePath(sid)

	writeFile(t, filepath.Join(ws, "keep.txt"), "unchanged")
	writeFile(t, filepath.Join(ws, "modify.txt"), "v1")
	writeFile(t, filepath.Join(ws, "delete.txt"), "gone soon")
	layerA, _ := mgr.CreateLayer(sid)

	// Modify, delete, and add.
	writeFile(t, filepath.Join(ws, "modify.txt"), "v2 changed")
	os.Remove(filepath.Join(ws, "delete.txt"))
	writeFile(t, filepath.Join(ws, "add.txt"), "brand new")
	layerB, _ := mgr.CreateLayer(sid)

	summary, err := mgr.FullDiff(sid, layerA, layerB)
	if err != nil {
		t.Fatal(err)
	}

	if summary.Added != 1 {
		t.Errorf("added = %d, want 1", summary.Added)
	}
	if summary.Modified != 1 {
		t.Errorf("modified = %d, want 1", summary.Modified)
	}
	if summary.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", summary.Deleted)
	}

	// Unchanged file should NOT appear.
	for _, e := range summary.Entries {
		if e.Path == "keep.txt" {
			t.Error("unchanged file should not appear in diff")
		}
	}
}

func TestFullDiff_IdenticalLayers(t *testing.T) {
	mgr, sid := setupTestOverlay(t)
	ws := mgr.WorkspacePath(sid)

	writeFile(t, filepath.Join(ws, "same.txt"), "identical")
	layerA, _ := mgr.CreateLayer(sid)
	layerB, _ := mgr.CreateLayer(sid)

	summary, err := mgr.FullDiff(sid, layerA, layerB)
	if err != nil {
		t.Fatal(err)
	}

	if len(summary.Entries) != 0 {
		t.Errorf("identical layers should have 0 entries, got %d", len(summary.Entries))
	}
}

func TestDiffDirs_CrossSession(t *testing.T) {
	dir := t.TempDir()
	dirA := filepath.Join(dir, "a")
	dirB := filepath.Join(dir, "b")
	os.MkdirAll(dirA, 0o755)
	os.MkdirAll(dirB, 0o755)

	writeFile(t, filepath.Join(dirA, "shared.txt"), "same content")
	writeFile(t, filepath.Join(dirB, "shared.txt"), "same content")
	writeFile(t, filepath.Join(dirA, "only-a.txt"), "only in A")
	writeFile(t, filepath.Join(dirB, "only-b.txt"), "only in B")

	summary, err := DiffDirs(dirA, dirB)
	if err != nil {
		t.Fatal(err)
	}

	if summary.Added != 1 { // only-b.txt
		t.Errorf("added = %d, want 1", summary.Added)
	}
	if summary.Deleted != 1 { // only-a.txt
		t.Errorf("deleted = %d, want 1", summary.Deleted)
	}
	if summary.Modified != 0 { // shared.txt is identical
		t.Errorf("modified = %d, want 0", summary.Modified)
	}
}
