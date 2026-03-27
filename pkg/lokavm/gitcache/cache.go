// Package gitcache manages local clones of git repositories, keyed by repo + commit SHA.
// Each checkout is a working tree stored at $cacheDir/{owner}/{repo}/{sha}/.
// Used by the worker agent to resolve provider="github" mounts into local HostPath
// directories that can be shared with guest VMs via virtiofs.
package gitcache

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"
)

// Config configures the git cache.
type Config struct {
	CacheDir  string        // Root directory for cached checkouts.
	MaxSizeMB int64         // Max total cache size in MiB (default: 10240 = 10GB). 0 = unlimited.
	MaxAge    time.Duration // Max age for unused entries (default: 7 days). 0 = never expire.
	Logger    *slog.Logger
}

// Cache manages local git repository checkouts.
type Cache struct {
	cacheDir  string
	maxSize   int64 // bytes
	maxAge    time.Duration
	logger    *slog.Logger
	mu        sync.Mutex
	refcounts map[string]int // path → number of active mounts
}

// New creates a new git cache.
func New(cfg Config) (*Cache, error) {
	if cfg.CacheDir == "" {
		return nil, fmt.Errorf("gitcache: CacheDir is required")
	}
	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("gitcache: create dir: %w", err)
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	maxSize := cfg.MaxSizeMB * 1024 * 1024
	if cfg.MaxSizeMB == 0 {
		maxSize = 10 * 1024 * 1024 * 1024 // 10 GB default
	}
	maxAge := cfg.MaxAge
	if maxAge == 0 {
		maxAge = 7 * 24 * time.Hour // 7 days default
	}
	return &Cache{
		cacheDir:  cfg.CacheDir,
		maxSize:   maxSize,
		maxAge:    maxAge,
		logger:    cfg.Logger,
		refcounts: make(map[string]int),
	}, nil
}

// Checkout ensures the given repo@ref is available locally and returns the
// path to the working tree directory and the resolved commit SHA.
//
// Thread-safe and idempotent — concurrent calls for the same repo@ref will
// wait for the first clone to complete.
func (c *Cache) Checkout(ctx context.Context, repo, ref string, token string) (path string, commitSHA string, err error) {
	if repo == "" {
		return "", "", fmt.Errorf("gitcache: repo is required")
	}
	if ref == "" {
		ref = "HEAD"
	}

	cloneURL := repoToURL(repo)
	var auth transport.AuthMethod
	if token != "" {
		auth = &http.BasicAuth{
			Username: "x-access-token",
			Password: token,
		}
	}

	// If ref looks like a full SHA (40 hex chars), check cache directly.
	if isFullSHA(ref) {
		path = c.entryPath(repo, ref)
		if c.isValidEntry(path) {
			c.touchAccess(path)
			c.addRef(path)
			return path, ref, nil
		}
	}

	// Resolve ref to a commit SHA via ls-remote.
	sha, err := c.resolveRef(ctx, cloneURL, ref, auth)
	if err != nil {
		return "", "", fmt.Errorf("resolve ref %q: %w", ref, err)
	}

	path = c.entryPath(repo, sha)

	// Check cache.
	if c.isValidEntry(path) {
		c.logger.Debug("git cache hit", "repo", repo, "ref", ref, "sha", sha[:12])
		c.touchAccess(path)
		c.addRef(path)
		return path, sha, nil
	}

	// Clone.
	c.mu.Lock()
	// Double-check after acquiring lock (another goroutine may have cloned).
	if c.isValidEntry(path) {
		c.mu.Unlock()
		c.touchAccess(path)
		c.addRef(path)
		return path, sha, nil
	}

	c.logger.Info("cloning git repo", "repo", repo, "ref", ref, "sha", sha[:12])

	// Clone into a temp dir, then rename atomically.
	tmpDir := path + ".tmp." + fmt.Sprintf("%d", time.Now().UnixNano())
	err = c.doClone(ctx, cloneURL, tmpDir, sha, ref, auth)
	c.mu.Unlock()

	if err != nil {
		os.RemoveAll(tmpDir)
		return "", "", fmt.Errorf("clone %s@%s: %w", repo, ref, err)
	}

	// Remove .git to save space.
	os.RemoveAll(filepath.Join(tmpDir, ".git"))

	// Atomic rename.
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.Rename(tmpDir, path); err != nil {
		// Another process beat us — that's fine.
		os.RemoveAll(tmpDir)
		if !c.isValidEntry(path) {
			return "", "", fmt.Errorf("rename cache entry: %w", err)
		}
	}

	c.touchAccess(path)
	c.addRef(path)
	c.logger.Info("git repo cached", "repo", repo, "sha", sha[:12], "path", path)
	return path, sha, nil
}

// Release decrements the refcount for a cached entry.
// Call this when a session using the mount is destroyed.
func (c *Cache) Release(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refcounts[path] > 0 {
		c.refcounts[path]--
		if c.refcounts[path] == 0 {
			delete(c.refcounts, path)
		}
	}
}

// Clean removes expired and oversized cache entries.
// Entries with active refcounts are never evicted.
func (c *Cache) Clean() (removed int, freedBytes int64, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	type entry struct {
		path     string
		size     int64
		accessed time.Time
	}

	var entries []entry
	var totalSize int64

	// Walk cache directory.
	filepath.Walk(c.cacheDir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		// Look for .loka-accessed marker files.
		if info.Name() == ".loka-accessed" {
			dir := filepath.Dir(p)
			size := dirSize(dir)
			accessed := info.ModTime()
			entries = append(entries, entry{path: dir, size: size, accessed: accessed})
			totalSize += size
		}
		return nil
	})

	now := time.Now()

	// Phase 1: Remove expired entries.
	for _, e := range entries {
		if c.maxAge > 0 && now.Sub(e.accessed) > c.maxAge && c.refcounts[e.path] == 0 {
			if err := os.RemoveAll(e.path); err == nil {
				removed++
				freedBytes += e.size
				totalSize -= e.size
			}
		}
	}

	// Phase 2: Evict LRU if over size limit.
	if c.maxSize > 0 && totalSize > c.maxSize {
		// Sort by access time (oldest first).
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].accessed.Before(entries[j].accessed)
		})
		for _, e := range entries {
			if totalSize <= c.maxSize {
				break
			}
			if c.refcounts[e.path] > 0 {
				continue // Don't evict active mounts.
			}
			if _, err := os.Stat(e.path); os.IsNotExist(err) {
				continue // Already removed in phase 1.
			}
			if err := os.RemoveAll(e.path); err == nil {
				removed++
				freedBytes += e.size
				totalSize -= e.size
			}
		}
	}

	return removed, freedBytes, nil
}

// resolveRef resolves a branch/tag/HEAD ref to a commit SHA via ls-remote.
func (c *Cache) resolveRef(ctx context.Context, cloneURL, ref string, auth transport.AuthMethod) (string, error) {
	rem := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{
		Name: "origin",
		URLs: []string{cloneURL},
	})

	refs, err := rem.ListContext(ctx, &git.ListOptions{Auth: auth})
	if err != nil {
		return "", fmt.Errorf("ls-remote %s: %w", cloneURL, err)
	}

	// Try exact match first: refs/heads/<ref>, refs/tags/<ref>, HEAD.
	candidates := []string{
		"refs/heads/" + ref,
		"refs/tags/" + ref,
		ref,
	}
	if ref == "HEAD" {
		candidates = []string{"HEAD"}
	}

	for _, candidate := range candidates {
		for _, r := range refs {
			if r.Name().String() == candidate {
				return r.Hash().String(), nil
			}
		}
	}

	// If ref looks like a partial SHA, try matching.
	if len(ref) >= 7 {
		for _, r := range refs {
			if strings.HasPrefix(r.Hash().String(), ref) {
				return r.Hash().String(), nil
			}
		}
	}

	return "", fmt.Errorf("ref %q not found in %s", ref, cloneURL)
}

// doClone clones the repo into destDir and checks out the target commit.
func (c *Cache) doClone(ctx context.Context, cloneURL, destDir string, sha, ref string, auth transport.AuthMethod) error {
	cloneOpts := &git.CloneOptions{
		URL:  cloneURL,
		Auth: auth,
	}

	// For branches/tags, use shallow clone (depth=1) for speed.
	if !isFullSHA(ref) {
		cloneOpts.Depth = 1
		cloneOpts.SingleBranch = true
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(ref)
		// Try as branch first; if that fails, try as tag.
	}

	repo, err := git.PlainCloneContext(ctx, destDir, false, cloneOpts)
	if err != nil {
		// Retry without SingleBranch (might be a tag or the ref format didn't match).
		os.RemoveAll(destDir)
		cloneOpts.SingleBranch = false
		cloneOpts.ReferenceName = ""
		cloneOpts.Depth = 0 // Full clone for arbitrary SHA checkout.
		repo, err = git.PlainCloneContext(ctx, destDir, false, cloneOpts)
		if err != nil {
			return err
		}
	}

	// Checkout the exact commit.
	wt, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree: %w", err)
	}

	hash := plumbing.NewHash(sha)
	if err := wt.Checkout(&git.CheckoutOptions{Hash: hash, Force: true}); err != nil {
		return fmt.Errorf("checkout %s: %w", sha[:12], err)
	}

	return nil
}

func (c *Cache) entryPath(repo, sha string) string {
	// Normalize repo to a safe filesystem path.
	safe := repo
	safe = strings.TrimSuffix(safe, ".git")
	// Strip protocol prefix.
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://"} {
		safe = strings.TrimPrefix(safe, prefix)
	}
	// Strip github.com/ for short paths.
	safe = strings.TrimPrefix(safe, "github.com/")
	safe = strings.ReplaceAll(safe, ":", "/")
	return filepath.Join(c.cacheDir, safe, sha)
}

func (c *Cache) isValidEntry(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (c *Cache) touchAccess(path string) {
	f := filepath.Join(path, ".loka-accessed")
	os.WriteFile(f, []byte(time.Now().Format(time.RFC3339)), 0o644)
}

func (c *Cache) addRef(path string) {
	c.mu.Lock()
	c.refcounts[path]++
	c.mu.Unlock()
}

func repoToURL(repo string) string {
	if strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "https://") {
		return repo
	}
	// "owner/repo" → "https://github.com/owner/repo.git"
	return "https://github.com/" + repo + ".git"
}

func isFullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func dirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}
