// Package objcache provides a local filesystem cache backed by object storage.
// It presents an S3/GCS bucket prefix as a local directory that can be shared
// with guest VMs via virtiofs.
//
// Reads are served from the local cache; on cache miss, the object is fetched
// from the remote store. Writes go to the local cache and are asynchronously
// uploaded to the remote store.
package objcache

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Store is the object storage interface (subset of objstore.ObjectStore).
type Store interface {
	Put(ctx context.Context, bucket, key string, reader io.Reader, size int64) error
	Get(ctx context.Context, bucket, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, bucket, key string) error
	List(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error)
}

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}

// Cache provides a local directory that mirrors an object storage prefix.
// Files are lazily fetched on read and asynchronously uploaded on write.
type Cache struct {
	store    Store
	bucket   string
	prefix   string
	cacheDir string // Local directory path (shared via virtiofs with guest).
	maxSize  int64  // Maximum cache size in bytes (0 = unlimited).
	logger   *slog.Logger

	mu      sync.Mutex
	entries map[string]*cacheEntry // relative path → entry
	size    int64                  // Current cache size.

	// Background upload queue.
	uploadCh chan string
	stopCh   chan struct{}
}

type cacheEntry struct {
	relPath    string
	size       int64
	lastAccess time.Time
	dirty      bool // Needs upload to remote.
}

// Config configures the cache.
type Config struct {
	Store    Store
	Bucket   string
	Prefix   string
	CacheDir string // Local directory to use for caching.
	MaxSize  int64  // Max cache size in bytes (0 = unlimited).
	Logger   *slog.Logger
}

// New creates a new object storage cache.
func New(cfg Config) (*Cache, error) {
	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		return nil, err
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	c := &Cache{
		store:    cfg.Store,
		bucket:   cfg.Bucket,
		prefix:   cfg.Prefix,
		cacheDir: cfg.CacheDir,
		maxSize:  cfg.MaxSize,
		logger:   cfg.Logger,
		entries:  make(map[string]*cacheEntry),
		uploadCh: make(chan string, 1000),
		stopCh:   make(chan struct{}),
	}

	return c, nil
}

// Dir returns the local cache directory path. Share this with the guest via virtiofs.
func (c *Cache) Dir() string {
	return c.cacheDir
}

// Start begins background workers for async uploads and cache management.
func (c *Cache) Start() {
	// Upload worker.
	go c.uploadWorker()

	// Periodic sync: detect local changes and upload.
	go c.syncWorker()
}

// Stop shuts down background workers.
func (c *Cache) Stop() {
	close(c.stopCh)
}

// Prefetch downloads all objects under the prefix to the local cache.
// Called on mount to warm the cache.
func (c *Cache) Prefetch(ctx context.Context) error {
	objects, err := c.store.List(ctx, c.bucket, c.prefix)
	if err != nil {
		return err
	}

	for _, obj := range objects {
		relPath := obj.Key
		if len(c.prefix) > 0 {
			relPath = obj.Key[len(c.prefix):]
		}
		if relPath == "" || relPath[0] == '/' {
			if len(relPath) > 1 {
				relPath = relPath[1:]
			} else {
				continue
			}
		}

		if err := c.fetch(ctx, relPath); err != nil {
			c.logger.Debug("prefetch failed", "key", obj.Key, "error", err)
		}
	}

	return nil
}

// fetch downloads an object to the local cache.
func (c *Cache) fetch(ctx context.Context, relPath string) error {
	localPath := filepath.Join(c.cacheDir, relPath)

	// Skip if already cached.
	if _, err := os.Stat(localPath); err == nil {
		return nil
	}

	key := c.prefix + relPath
	reader, err := c.store.Get(ctx, c.bucket, key)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	n, err := io.Copy(f, reader)
	if err != nil {
		os.Remove(localPath)
		return err
	}

	c.mu.Lock()
	c.entries[relPath] = &cacheEntry{
		relPath:    relPath,
		size:       n,
		lastAccess: time.Now(),
	}
	c.size += n
	c.mu.Unlock()

	return nil
}

// upload sends a local file to object storage.
func (c *Cache) upload(relPath string) error {
	localPath := filepath.Join(c.cacheDir, relPath)
	info, err := os.Stat(localPath)
	if err != nil {
		return err
	}

	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	key := c.prefix + relPath
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := c.store.Put(ctx, c.bucket, key, f, info.Size()); err != nil {
		return err
	}

	c.mu.Lock()
	if entry, ok := c.entries[relPath]; ok {
		entry.dirty = false
	}
	c.mu.Unlock()

	return nil
}

// uploadWorker processes the upload queue.
func (c *Cache) uploadWorker() {
	for {
		select {
		case <-c.stopCh:
			return
		case relPath := <-c.uploadCh:
			if err := c.upload(relPath); err != nil {
				c.logger.Warn("upload failed", "path", relPath, "error", err)
			}
		}
	}
}

// syncWorker periodically scans the cache dir for local changes.
func (c *Cache) syncWorker() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.syncLocalChanges()
			if c.maxSize > 0 {
				c.evict()
			}
		}
	}
}

// syncLocalChanges walks the cache dir and queues uploads for modified files.
func (c *Cache) syncLocalChanges() {
	filepath.Walk(c.cacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(c.cacheDir, path)
		if relPath == "." {
			return nil
		}

		c.mu.Lock()
		entry, exists := c.entries[relPath]
		if !exists {
			// New file — mark as dirty.
			c.entries[relPath] = &cacheEntry{
				relPath:    relPath,
				size:       info.Size(),
				lastAccess: time.Now(),
				dirty:      true,
			}
			c.size += info.Size()
			c.mu.Unlock()

			select {
			case c.uploadCh <- relPath:
			default:
			}
			return nil
		}

		// Existing file — check if modified.
		if info.Size() != entry.size || info.ModTime().After(entry.lastAccess) {
			entry.size = info.Size()
			entry.dirty = true
			entry.lastAccess = time.Now()
			c.mu.Unlock()

			select {
			case c.uploadCh <- relPath:
			default:
			}
			return nil
		}

		c.mu.Unlock()
		return nil
	})
}

// evict removes least-recently-accessed files until cache is within maxSize.
func (c *Cache) evict() {
	c.mu.Lock()
	if c.size <= c.maxSize {
		c.mu.Unlock()
		return
	}

	// Sort by last access time.
	var entries []*cacheEntry
	for _, e := range c.entries {
		if !e.dirty { // Don't evict dirty files.
			entries = append(entries, e)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].lastAccess.Before(entries[j].lastAccess)
	})

	for _, e := range entries {
		if c.size <= c.maxSize {
			break
		}
		localPath := filepath.Join(c.cacheDir, e.relPath)
		if err := os.Remove(localPath); err == nil {
			c.size -= e.size
			delete(c.entries, e.relPath)
		}
	}
	c.mu.Unlock()
}
