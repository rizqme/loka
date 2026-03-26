package virtio

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
)

const whiteoutPrefix = ".wh."

// OverlayBackend implements FSBackend with overlay semantics.
// It merges multiple read-only layer directories with a writable upper directory.
// Guest sees a single unified filesystem without needing overlayfs in the kernel.
//
// Lookup order: upper dir first, then layers top-to-bottom.
// Writes always go to the upper dir (copy-on-write).
// Deletes create whiteout files (.wh.<name>) in the upper dir.
type OverlayBackend struct {
	upper  string   // Writable per-VM directory.
	layers []string // Read-only layer directories (index 0 = top layer).

	mu        sync.RWMutex
	inodes    map[uint64]string
	pathToIno map[string]uint64
	nextIno   atomic.Uint64

	handleMu sync.RWMutex
	handles  map[uint64]*os.File
	nextFH   atomic.Uint64

	// Lookup cache: path → which source (upper or layer index).
	cacheMu    sync.RWMutex
	lookupCache map[string]int // path → -1 for upper, 0..N for layer index.
}

// NewOverlayBackend creates an overlay backend.
// layers should be ordered top-to-bottom (most recent first).
func NewOverlayBackend(upper string, layers []string) *OverlayBackend {
	b := &OverlayBackend{
		upper:       upper,
		layers:      layers,
		inodes:      make(map[uint64]string),
		pathToIno:   make(map[string]uint64),
		handles:     make(map[uint64]*os.File),
		lookupCache: make(map[string]int),
	}
	b.inodes[1] = "."
	b.pathToIno["."] = 1
	b.nextIno.Store(2)
	b.nextFH.Store(1)
	return b
}

// resolve finds the host path for a relative path, checking upper then layers.
// Returns (hostPath, sourceIdx) where sourceIdx is -1 for upper, 0..N for layers.
func (b *OverlayBackend) resolve(rel string) (string, int, error) {
	// Check cache first.
	b.cacheMu.RLock()
	if idx, ok := b.lookupCache[rel]; ok {
		b.cacheMu.RUnlock()
		if idx == -1 {
			return filepath.Join(b.upper, rel), -1, nil
		}
		return filepath.Join(b.layers[idx], rel), idx, nil
	}
	b.cacheMu.RUnlock()

	// Check for whiteout in upper.
	dir := filepath.Dir(rel)
	base := filepath.Base(rel)
	whiteout := filepath.Join(b.upper, dir, whiteoutPrefix+base)
	if _, err := os.Lstat(whiteout); err == nil {
		return "", 0, fmt.Errorf("file deleted (whiteout)")
	}

	// Check upper.
	upperPath := filepath.Join(b.upper, rel)
	if _, err := os.Lstat(upperPath); err == nil {
		b.cacheResult(rel, -1)
		return upperPath, -1, nil
	}

	// Check layers top-to-bottom.
	for i, layer := range b.layers {
		layerPath := filepath.Join(layer, rel)
		if _, err := os.Lstat(layerPath); err == nil {
			b.cacheResult(rel, i)
			return layerPath, i, nil
		}
	}

	return "", 0, fmt.Errorf("not found: %s", rel)
}

func (b *OverlayBackend) cacheResult(rel string, idx int) {
	b.cacheMu.Lock()
	b.lookupCache[rel] = idx
	b.cacheMu.Unlock()
}

func (b *OverlayBackend) invalidateCache(rel string) {
	b.cacheMu.Lock()
	delete(b.lookupCache, rel)
	b.cacheMu.Unlock()
}

func (b *OverlayBackend) getPath(ino uint64) (string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	rel, ok := b.inodes[ino]
	if !ok {
		return "", fmt.Errorf("unknown inode %d", ino)
	}
	return rel, nil
}

func (b *OverlayBackend) assignIno(rel string) uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ino, ok := b.pathToIno[rel]; ok {
		return ino
	}
	ino := b.nextIno.Add(1) - 1
	b.inodes[ino] = rel
	b.pathToIno[rel] = ino
	return ino
}

func (b *OverlayBackend) childRel(parentRel, name string) string {
	if parentRel == "." {
		return name
	}
	return filepath.Join(parentRel, name)
}

func (b *OverlayBackend) Lookup(parentIno uint64, name string) (*FuseAttr, uint64, error) {
	parentRel, err := b.getPath(parentIno)
	if err != nil {
		return nil, 0, err
	}

	childRel := b.childRel(parentRel, name)
	hostPath, _, err := b.resolve(childRel)
	if err != nil {
		return nil, 0, err
	}

	info, err := os.Lstat(hostPath)
	if err != nil {
		return nil, 0, err
	}

	ino := b.assignIno(childRel)
	return statToAttr(info, ino), ino, nil
}

func (b *OverlayBackend) Getattr(ino uint64) (*FuseAttr, error) {
	rel, err := b.getPath(ino)
	if err != nil {
		return nil, err
	}

	hostPath, _, err := b.resolve(rel)
	if err != nil {
		return nil, err
	}

	info, err := os.Lstat(hostPath)
	if err != nil {
		return nil, err
	}

	return statToAttr(info, ino), nil
}

func (b *OverlayBackend) Setattr(ino uint64, attr *FuseAttr, valid uint32) (*FuseAttr, error) {
	rel, err := b.getPath(ino)
	if err != nil {
		return nil, err
	}

	// Copy-up to upper if not already there.
	if err := b.copyUp(rel); err != nil {
		return nil, err
	}

	path := filepath.Join(b.upper, rel)
	if valid&(1<<3) != 0 && attr != nil {
		os.Truncate(path, int64(attr.Size))
	}
	if valid&(1<<0) != 0 && attr != nil {
		os.Chmod(path, os.FileMode(attr.Mode&0o7777))
	}

	return b.Getattr(ino)
}

func (b *OverlayBackend) Readdir(ino uint64, offset uint64) ([]FuseDirEntry, error) {
	rel, err := b.getPath(ino)
	if err != nil {
		return nil, err
	}

	// Merge entries from upper + all layers, deduplicate.
	seen := make(map[string]bool)
	whiteouts := make(map[string]bool)
	var allEntries []FuseDirEntry

	// Collect whiteouts from upper first.
	upperDir := filepath.Join(b.upper, rel)
	if entries, err := os.ReadDir(upperDir); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), whiteoutPrefix) {
				whiteouts[strings.TrimPrefix(e.Name(), whiteoutPrefix)] = true
				continue
			}
			seen[e.Name()] = true
			childRel := b.childRel(rel, e.Name())
			childIno := b.assignIno(childRel)
			allEntries = append(allEntries, FuseDirEntry{
				Ino:  childIno,
				Type: dirEntryType(e),
				Name: e.Name(),
			})
		}
	}

	// Add entries from layers (top-to-bottom), skipping duplicates and whiteouts.
	for _, layer := range b.layers {
		layerDir := filepath.Join(layer, rel)
		entries, err := os.ReadDir(layerDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if seen[e.Name()] || whiteouts[e.Name()] {
				continue
			}
			if strings.HasPrefix(e.Name(), whiteoutPrefix) {
				continue
			}
			seen[e.Name()] = true
			childRel := b.childRel(rel, e.Name())
			childIno := b.assignIno(childRel)
			allEntries = append(allEntries, FuseDirEntry{
				Ino:  childIno,
				Type: dirEntryType(e),
				Name: e.Name(),
			})
		}
	}

	// Apply offset and set sequential offsets.
	var result []FuseDirEntry
	for i, e := range allEntries {
		if uint64(i) < offset {
			continue
		}
		e.Off = uint64(i + 1)
		result = append(result, e)
	}
	return result, nil
}

func (b *OverlayBackend) Open(ino uint64, flags uint32) (uint64, error) {
	rel, err := b.getPath(ino)
	if err != nil {
		return 0, err
	}

	// If opening for write, copy-up first.
	writeFlags := flags & (uint32(os.O_WRONLY) | uint32(os.O_RDWR) | uint32(os.O_APPEND) | uint32(os.O_TRUNC))
	if writeFlags != 0 {
		if err := b.copyUp(rel); err != nil {
			return 0, err
		}
	}

	hostPath, _, err := b.resolve(rel)
	if err != nil {
		return 0, err
	}

	flag := os.O_RDONLY
	if writeFlags != 0 {
		flag = int(flags) & (os.O_RDONLY | os.O_WRONLY | os.O_RDWR | os.O_APPEND | os.O_TRUNC)
	}

	f, err := os.OpenFile(hostPath, flag, 0)
	if err != nil {
		return 0, err
	}

	fh := b.nextFH.Add(1) - 1
	b.handleMu.Lock()
	b.handles[fh] = f
	b.handleMu.Unlock()
	return fh, nil
}

func (b *OverlayBackend) Read(ino uint64, fh uint64, offset uint64, size uint32) ([]byte, error) {
	b.handleMu.RLock()
	f, ok := b.handles[fh]
	b.handleMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("invalid file handle")
	}
	buf := make([]byte, size)
	n, err := f.ReadAt(buf, int64(offset))
	if n > 0 {
		return buf[:n], nil
	}
	return nil, err
}

func (b *OverlayBackend) Write(ino uint64, fh uint64, offset uint64, data []byte) (uint32, error) {
	b.handleMu.RLock()
	f, ok := b.handles[fh]
	b.handleMu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("invalid file handle")
	}
	n, err := f.WriteAt(data, int64(offset))
	return uint32(n), err
}

func (b *OverlayBackend) Create(parentIno uint64, name string, mode uint32, flags uint32) (*FuseAttr, uint64, uint64, error) {
	parentRel, err := b.getPath(parentIno)
	if err != nil {
		return nil, 0, 0, err
	}

	childRel := b.childRel(parentRel, name)

	// Create in upper dir.
	upperPath := filepath.Join(b.upper, childRel)
	os.MkdirAll(filepath.Dir(upperPath), 0o755)

	f, err := os.OpenFile(upperPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(mode&0o7777))
	if err != nil {
		return nil, 0, 0, err
	}

	// Remove any whiteout.
	whiteout := filepath.Join(b.upper, filepath.Dir(childRel), whiteoutPrefix+name)
	os.Remove(whiteout)

	b.invalidateCache(childRel)

	ino := b.assignIno(childRel)
	fh := b.nextFH.Add(1) - 1
	b.handleMu.Lock()
	b.handles[fh] = f
	b.handleMu.Unlock()

	info, _ := f.Stat()
	return statToAttr(info, ino), ino, fh, nil
}

func (b *OverlayBackend) Mkdir(parentIno uint64, name string, mode uint32) (*FuseAttr, uint64, error) {
	parentRel, err := b.getPath(parentIno)
	if err != nil {
		return nil, 0, err
	}

	childRel := b.childRel(parentRel, name)
	upperPath := filepath.Join(b.upper, childRel)

	if err := os.MkdirAll(upperPath, os.FileMode(mode&0o7777)); err != nil {
		return nil, 0, err
	}

	// Remove whiteout.
	whiteout := filepath.Join(b.upper, filepath.Dir(childRel), whiteoutPrefix+name)
	os.Remove(whiteout)

	b.invalidateCache(childRel)

	ino := b.assignIno(childRel)
	info, _ := os.Lstat(upperPath)
	return statToAttr(info, ino), ino, nil
}

func (b *OverlayBackend) Unlink(parentIno uint64, name string) error {
	parentRel, err := b.getPath(parentIno)
	if err != nil {
		return err
	}

	childRel := b.childRel(parentRel, name)

	// Remove from upper if exists.
	upperPath := filepath.Join(b.upper, childRel)
	os.Remove(upperPath)

	// Create whiteout if file exists in any layer.
	for _, layer := range b.layers {
		if _, err := os.Lstat(filepath.Join(layer, childRel)); err == nil {
			whiteout := filepath.Join(b.upper, filepath.Dir(childRel), whiteoutPrefix+name)
			os.MkdirAll(filepath.Dir(whiteout), 0o755)
			os.Create(whiteout)
			break
		}
	}

	b.invalidateCache(childRel)
	return nil
}

func (b *OverlayBackend) Rmdir(parentIno uint64, name string) error {
	return b.Unlink(parentIno, name)
}

func (b *OverlayBackend) Rename(oldParentIno uint64, oldName string, newParentIno uint64, newName string) error {
	oldParentRel, err := b.getPath(oldParentIno)
	if err != nil {
		return err
	}
	newParentRel, err := b.getPath(newParentIno)
	if err != nil {
		return err
	}

	oldRel := b.childRel(oldParentRel, oldName)
	newRel := b.childRel(newParentRel, newName)

	// Copy-up old file.
	if err := b.copyUp(oldRel); err != nil {
		return err
	}

	oldUpper := filepath.Join(b.upper, oldRel)
	newUpper := filepath.Join(b.upper, newRel)
	os.MkdirAll(filepath.Dir(newUpper), 0o755)

	if err := os.Rename(oldUpper, newUpper); err != nil {
		return err
	}

	// Create whiteout for old location if it exists in layers.
	for _, layer := range b.layers {
		if _, err := os.Lstat(filepath.Join(layer, oldRel)); err == nil {
			whiteout := filepath.Join(b.upper, filepath.Dir(oldRel), whiteoutPrefix+oldName)
			os.Create(whiteout)
			break
		}
	}

	b.invalidateCache(oldRel)
	b.invalidateCache(newRel)
	return nil
}

func (b *OverlayBackend) Release(ino uint64, fh uint64) error {
	b.handleMu.Lock()
	f, ok := b.handles[fh]
	if ok {
		delete(b.handles, fh)
	}
	b.handleMu.Unlock()
	if ok && f != nil {
		return f.Close()
	}
	return nil
}

func (b *OverlayBackend) Statfs(ino uint64) (*FuseStatfs, error) {
	return &FuseStatfs{
		Blocks: 1 << 30, Bfree: 1 << 29, Bavail: 1 << 29,
		Files: 1 << 20, Ffree: 1 << 19,
		Bsize: 4096, Namelen: 255, Frsize: 4096,
	}, nil
}

func (b *OverlayBackend) Symlink(parentIno uint64, name string, target string) (*FuseAttr, uint64, error) {
	parentRel, err := b.getPath(parentIno)
	if err != nil {
		return nil, 0, err
	}
	childRel := b.childRel(parentRel, name)
	upperPath := filepath.Join(b.upper, childRel)
	os.MkdirAll(filepath.Dir(upperPath), 0o755)

	if err := os.Symlink(target, upperPath); err != nil {
		return nil, 0, err
	}

	b.invalidateCache(childRel)
	ino := b.assignIno(childRel)
	info, _ := os.Lstat(upperPath)
	return statToAttr(info, ino), ino, nil
}

func (b *OverlayBackend) Readlink(ino uint64) (string, error) {
	rel, err := b.getPath(ino)
	if err != nil {
		return "", err
	}
	hostPath, _, err := b.resolve(rel)
	if err != nil {
		return "", err
	}
	return os.Readlink(hostPath)
}

// copyUp copies a file from a layer to the upper dir (copy-on-write).
func (b *OverlayBackend) copyUp(rel string) error {
	// Already in upper?
	upperPath := filepath.Join(b.upper, rel)
	if _, err := os.Lstat(upperPath); err == nil {
		return nil // Already copied up.
	}

	// Find in layers.
	var srcPath string
	for _, layer := range b.layers {
		p := filepath.Join(layer, rel)
		if _, err := os.Lstat(p); err == nil {
			srcPath = p
			break
		}
	}
	if srcPath == "" {
		return nil // Not found in layers; will be created fresh.
	}

	info, err := os.Lstat(srcPath)
	if err != nil {
		return err
	}

	// Ensure parent dirs exist in upper.
	os.MkdirAll(filepath.Dir(upperPath), 0o755)

	if info.IsDir() {
		return os.MkdirAll(upperPath, info.Mode())
	}

	// Copy file.
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(upperPath, os.O_CREATE|os.O_WRONLY, info.Mode())
	if err != nil {
		return err
	}
	defer dst.Close()

	_, err = io.Copy(dst, src)
	b.invalidateCache(rel)
	return err
}

func dirEntryType(e os.DirEntry) uint32 {
	if e.IsDir() {
		return 4 // DT_DIR
	}
	if e.Type()&os.ModeSymlink != 0 {
		return 10 // DT_LNK
	}
	return 8 // DT_REG
}
