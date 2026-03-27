package gitcache

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Config{CacheDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.cacheDir != dir {
		t.Errorf("cacheDir = %q, want %q", c.cacheDir, dir)
	}
	if c.maxAge != 7*24*time.Hour {
		t.Errorf("maxAge = %v, want 7 days", c.maxAge)
	}
}

func TestNew_MissingDir(t *testing.T) {
	_, err := New(Config{CacheDir: ""})
	if err == nil {
		t.Fatal("expected error for empty CacheDir")
	}
}

func TestRepoToURL(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"owner/repo", "https://github.com/owner/repo.git"},
		{"https://github.com/owner/repo.git", "https://github.com/owner/repo.git"},
		{"https://gitlab.com/org/repo.git", "https://gitlab.com/org/repo.git"},
	}
	for _, tt := range tests {
		got := repoToURL(tt.input)
		if got != tt.want {
			t.Errorf("repoToURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsFullSHA(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"abc123", false},
		{"abcdef1234567890abcdef1234567890abcdef12", true},
		{"ABCDEF1234567890abcdef1234567890abcdef12", true},
		{"not-a-sha-at-all-nope-not-even-close-xx", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isFullSHA(tt.input)
		if got != tt.want {
			t.Errorf("isFullSHA(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestEntryPath(t *testing.T) {
	c := &Cache{cacheDir: "/tmp/gitcache"}

	tests := []struct {
		repo, sha, want string
	}{
		{"owner/repo", "abc123", "/tmp/gitcache/owner/repo/abc123"},
		{"https://github.com/owner/repo.git", "def456", "/tmp/gitcache/owner/repo/def456"},
		{"https://gitlab.com/org/repo.git", "789abc", "/tmp/gitcache/gitlab.com/org/repo/789abc"},
	}
	for _, tt := range tests {
		got := c.entryPath(tt.repo, tt.sha)
		if got != tt.want {
			t.Errorf("entryPath(%q, %q) = %q, want %q", tt.repo, tt.sha, got, tt.want)
		}
	}
}

func TestCheckout_PublicRepo(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	dir := t.TempDir()
	c, err := New(Config{CacheDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Clone a small public repo by branch.
	path, sha, err := c.Checkout(ctx, "go-git/go-billy", "master", "")
	if err != nil {
		t.Fatalf("Checkout: %v", err)
	}

	if sha == "" {
		t.Fatal("SHA is empty")
	}
	if len(sha) != 40 {
		t.Errorf("SHA length = %d, want 40", len(sha))
	}

	// Verify files exist.
	if _, err := os.Stat(filepath.Join(path, "go.mod")); err != nil {
		t.Errorf("go.mod not found in checkout: %v", err)
	}

	// Verify .git was removed.
	if _, err := os.Stat(filepath.Join(path, ".git")); !os.IsNotExist(err) {
		t.Error(".git directory should be removed")
	}

	// Second checkout should be a cache hit (fast).
	start := time.Now()
	path2, sha2, err := c.Checkout(ctx, "go-git/go-billy", "master", "")
	if err != nil {
		t.Fatalf("Checkout (cache hit): %v", err)
	}
	elapsed := time.Since(start)

	if path2 != path {
		t.Errorf("cache hit path = %q, want %q", path2, path)
	}
	if sha2 != sha {
		t.Errorf("cache hit SHA = %q, want %q", sha2, sha)
	}
	if elapsed > 5*time.Second {
		t.Errorf("cache hit took %v, expected <5s", elapsed)
	}

	// Release the refs.
	c.Release(path)
	c.Release(path2)
}

func TestClean_Expired(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Config{
		CacheDir: dir,
		MaxAge:   1 * time.Millisecond, // Expire immediately.
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a fake cache entry.
	entryDir := filepath.Join(dir, "owner", "repo", "abc123")
	os.MkdirAll(entryDir, 0o755)
	os.WriteFile(filepath.Join(entryDir, ".loka-accessed"), []byte("2020-01-01T00:00:00Z"), 0o644)
	// Set old mtime.
	old := time.Now().Add(-24 * time.Hour)
	os.Chtimes(filepath.Join(entryDir, ".loka-accessed"), old, old)

	time.Sleep(2 * time.Millisecond)

	removed, freed, err := c.Clean()
	if err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if removed != 1 {
		t.Errorf("removed = %d, want 1", removed)
	}
	_ = freed

	if _, err := os.Stat(entryDir); !os.IsNotExist(err) {
		t.Error("expired entry should be removed")
	}
}

func TestClean_SkipsActiveRefs(t *testing.T) {
	dir := t.TempDir()
	c, err := New(Config{
		CacheDir: dir,
		MaxAge:   1 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	entryDir := filepath.Join(dir, "owner", "repo", "def456")
	os.MkdirAll(entryDir, 0o755)
	os.WriteFile(filepath.Join(entryDir, ".loka-accessed"), []byte("2020-01-01T00:00:00Z"), 0o644)
	old := time.Now().Add(-24 * time.Hour)
	os.Chtimes(filepath.Join(entryDir, ".loka-accessed"), old, old)

	// Add a refcount — should prevent eviction.
	c.refcounts[entryDir] = 1

	time.Sleep(2 * time.Millisecond)

	removed, _, _ := c.Clean()
	if removed != 0 {
		t.Errorf("removed = %d, want 0 (active ref should prevent eviction)", removed)
	}

	if _, err := os.Stat(entryDir); os.IsNotExist(err) {
		t.Error("entry with active ref should NOT be removed")
	}
}

func TestRelease(t *testing.T) {
	c := &Cache{refcounts: map[string]int{"/a": 2, "/b": 1}}

	c.Release("/a")
	if c.refcounts["/a"] != 1 {
		t.Errorf("refcount after first release = %d, want 1", c.refcounts["/a"])
	}

	c.Release("/a")
	if _, ok := c.refcounts["/a"]; ok {
		t.Error("refcount should be deleted after reaching 0")
	}

	c.Release("/b")
	if _, ok := c.refcounts["/b"]; ok {
		t.Error("/b refcount should be deleted")
	}

	// Releasing non-existent path should not panic.
	c.Release("/nonexistent")
}
