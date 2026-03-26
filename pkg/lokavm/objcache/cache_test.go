package objcache

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// mockStore implements Store for testing.
type mockStore struct {
	mu      sync.Mutex
	objects map[string][]byte // "bucket/key" → data
}

func newMockStore() *mockStore {
	return &mockStore{objects: make(map[string][]byte)}
}

func (m *mockStore) key(bucket, key string) string { return bucket + "/" + key }

func (m *mockStore) Put(_ context.Context, bucket, key string, reader io.Reader, size int64) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.objects[m.key(bucket, key)] = data
	m.mu.Unlock()
	return nil
}

func (m *mockStore) Get(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	data, ok := m.objects[m.key(bucket, key)]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("not found: %s/%s", bucket, key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (m *mockStore) Delete(_ context.Context, bucket, key string) error {
	m.mu.Lock()
	delete(m.objects, m.key(bucket, key))
	m.mu.Unlock()
	return nil
}

func (m *mockStore) List(_ context.Context, bucket, prefix string) ([]ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []ObjectInfo
	fullPrefix := bucket + "/" + prefix
	for k, v := range m.objects {
		if strings.HasPrefix(k, fullPrefix) {
			key := strings.TrimPrefix(k, bucket+"/")
			result = append(result, ObjectInfo{Key: key, Size: int64(len(v))})
		}
	}
	return result, nil
}

func TestCacheNew(t *testing.T) {
	dir := t.TempDir()
	store := newMockStore()

	c, err := New(Config{
		Store:    store,
		Bucket:   "test-bucket",
		Prefix:   "data/",
		CacheDir: filepath.Join(dir, "cache"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if c.Dir() != filepath.Join(dir, "cache") {
		t.Errorf("Dir=%q", c.Dir())
	}

	// Cache dir should be created.
	if _, err := os.Stat(c.Dir()); err != nil {
		t.Errorf("cache dir not created: %v", err)
	}
}

func TestCachePrefetch(t *testing.T) {
	dir := t.TempDir()
	store := newMockStore()

	// Pre-populate store.
	ctx := context.Background()
	store.Put(ctx, "bucket", "prefix/file1.txt", strings.NewReader("content1"), -1)
	store.Put(ctx, "bucket", "prefix/sub/file2.txt", strings.NewReader("content2"), -1)

	c, _ := New(Config{
		Store:    store,
		Bucket:   "bucket",
		Prefix:   "prefix/",
		CacheDir: filepath.Join(dir, "cache"),
	})

	if err := c.Prefetch(ctx); err != nil {
		t.Fatalf("Prefetch: %v", err)
	}

	// Check files exist locally.
	data, err := os.ReadFile(filepath.Join(c.Dir(), "file1.txt"))
	if err != nil {
		t.Fatalf("read file1.txt: %v", err)
	}
	if string(data) != "content1" {
		t.Errorf("file1.txt = %q", data)
	}

	data, err = os.ReadFile(filepath.Join(c.Dir(), "sub", "file2.txt"))
	if err != nil {
		t.Fatalf("read sub/file2.txt: %v", err)
	}
	if string(data) != "content2" {
		t.Errorf("sub/file2.txt = %q", data)
	}
}

func TestCacheSyncLocalChanges(t *testing.T) {
	dir := t.TempDir()
	store := newMockStore()

	c, _ := New(Config{
		Store:    store,
		Bucket:   "bucket",
		Prefix:   "data/",
		CacheDir: filepath.Join(dir, "cache"),
	})
	c.Start()
	defer c.Stop()

	// Write a file locally.
	os.WriteFile(filepath.Join(c.Dir(), "local.txt"), []byte("local data"), 0o644)

	// Trigger sync manually.
	c.syncLocalChanges()

	// Wait for upload.
	time.Sleep(100 * time.Millisecond)

	// Check it was uploaded.
	store.mu.Lock()
	data, ok := store.objects["bucket/data/local.txt"]
	store.mu.Unlock()

	if !ok {
		t.Fatal("local.txt was not uploaded to store")
	}
	if string(data) != "local data" {
		t.Errorf("uploaded data = %q", data)
	}
}

func TestCacheEviction(t *testing.T) {
	dir := t.TempDir()
	store := newMockStore()

	c, _ := New(Config{
		Store:    store,
		Bucket:   "bucket",
		Prefix:   "data/",
		CacheDir: filepath.Join(dir, "cache"),
		MaxSize:  100, // 100 bytes max.
	})

	// Add entries that exceed max size.
	c.mu.Lock()
	c.entries["old.txt"] = &cacheEntry{relPath: "old.txt", size: 60, lastAccess: time.Now().Add(-1 * time.Hour)}
	c.entries["new.txt"] = &cacheEntry{relPath: "new.txt", size: 60, lastAccess: time.Now()}
	c.size = 120
	c.mu.Unlock()

	// Create corresponding files.
	os.WriteFile(filepath.Join(c.Dir(), "old.txt"), make([]byte, 60), 0o644)
	os.WriteFile(filepath.Join(c.Dir(), "new.txt"), make([]byte, 60), 0o644)

	c.evict()

	// old.txt should be evicted (older), new.txt should remain.
	c.mu.Lock()
	_, hasOld := c.entries["old.txt"]
	_, hasNew := c.entries["new.txt"]
	c.mu.Unlock()

	if hasOld {
		t.Error("old.txt should have been evicted")
	}
	if !hasNew {
		t.Error("new.txt should remain")
	}
}

func TestCacheEvictionSkipsDirty(t *testing.T) {
	dir := t.TempDir()
	store := newMockStore()

	c, _ := New(Config{
		Store:    store,
		Bucket:   "bucket",
		Prefix:   "data/",
		CacheDir: filepath.Join(dir, "cache"),
		MaxSize:  50,
	})

	c.mu.Lock()
	c.entries["dirty.txt"] = &cacheEntry{relPath: "dirty.txt", size: 60, lastAccess: time.Now().Add(-1 * time.Hour), dirty: true}
	c.size = 60
	c.mu.Unlock()

	os.WriteFile(filepath.Join(c.Dir(), "dirty.txt"), make([]byte, 60), 0o644)

	c.evict()

	// Dirty file should NOT be evicted.
	c.mu.Lock()
	_, hasDirty := c.entries["dirty.txt"]
	c.mu.Unlock()

	if !hasDirty {
		t.Error("dirty file should not be evicted")
	}
}

func TestCacheFetch(t *testing.T) {
	dir := t.TempDir()
	store := newMockStore()
	ctx := context.Background()

	store.Put(ctx, "bucket", "pfx/remote.txt", strings.NewReader("remote data"), -1)

	c, _ := New(Config{
		Store:    store,
		Bucket:   "bucket",
		Prefix:   "pfx/",
		CacheDir: filepath.Join(dir, "cache"),
	})

	if err := c.fetch(ctx, "remote.txt"); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	data, _ := os.ReadFile(filepath.Join(c.Dir(), "remote.txt"))
	if string(data) != "remote data" {
		t.Errorf("fetched = %q", data)
	}

	// Fetch again should be a no-op (already cached).
	if err := c.fetch(ctx, "remote.txt"); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
}
