package virtio

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// DirectBackend implements FSBackend by passing through to a host directory.
// Every FUSE operation maps directly to an os.* call on the host.
type DirectBackend struct {
	rootPath string
	readOnly bool

	// Inode table: maps inode numbers to paths.
	mu       sync.RWMutex
	inodes   map[uint64]string // ino → relative path
	pathToIno map[string]uint64 // relative path → ino
	nextIno  atomic.Uint64

	// Open file handles.
	handleMu sync.RWMutex
	handles  map[uint64]*os.File
	nextFH   atomic.Uint64
}

// NewDirectBackend creates a backend that serves a single host directory.
func NewDirectBackend(rootPath string, readOnly bool) *DirectBackend {
	b := &DirectBackend{
		rootPath:  rootPath,
		readOnly:  readOnly,
		inodes:    make(map[uint64]string),
		pathToIno: make(map[string]uint64),
		handles:   make(map[uint64]*os.File),
	}
	// Root inode is always 1.
	b.inodes[1] = "."
	b.pathToIno["."] = 1
	b.nextIno.Store(2)
	b.nextFH.Store(1)
	return b
}

func (b *DirectBackend) hostPath(rel string) string {
	if rel == "." || rel == "" {
		return b.rootPath
	}
	return filepath.Join(b.rootPath, rel)
}

func (b *DirectBackend) getPath(ino uint64) (string, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	rel, ok := b.inodes[ino]
	if !ok {
		return "", fmt.Errorf("unknown inode %d", ino)
	}
	return rel, nil
}

func (b *DirectBackend) assignIno(rel string) uint64 {
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

func (b *DirectBackend) Lookup(parentIno uint64, name string) (*FuseAttr, uint64, error) {
	parentRel, err := b.getPath(parentIno)
	if err != nil {
		return nil, 0, err
	}

	var childRel string
	if parentRel == "." {
		childRel = name
	} else {
		childRel = filepath.Join(parentRel, name)
	}

	info, err := os.Lstat(b.hostPath(childRel))
	if err != nil {
		return nil, 0, err
	}

	ino := b.assignIno(childRel)
	attr := statToAttr(info, ino)
	return attr, ino, nil
}

func (b *DirectBackend) Getattr(ino uint64) (*FuseAttr, error) {
	rel, err := b.getPath(ino)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(b.hostPath(rel))
	if err != nil {
		return nil, err
	}
	return statToAttr(info, ino), nil
}

func (b *DirectBackend) Setattr(ino uint64, attr *FuseAttr, valid uint32) (*FuseAttr, error) {
	if b.readOnly {
		return nil, fmt.Errorf("read-only filesystem")
	}
	rel, err := b.getPath(ino)
	if err != nil {
		return nil, err
	}
	path := b.hostPath(rel)

	// Apply requested changes based on valid bitmask.
	if valid&(1<<3) != 0 && attr != nil { // FATTR_SIZE
		if err := os.Truncate(path, int64(attr.Size)); err != nil {
			return nil, err
		}
	}
	if valid&(1<<0) != 0 && attr != nil { // FATTR_MODE
		if err := os.Chmod(path, os.FileMode(attr.Mode&0o7777)); err != nil {
			return nil, err
		}
	}

	return b.Getattr(ino)
}

func (b *DirectBackend) Readdir(ino uint64, offset uint64) ([]FuseDirEntry, error) {
	rel, err := b.getPath(ino)
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(b.hostPath(rel))
	if err != nil {
		return nil, err
	}

	var result []FuseDirEntry
	for i, e := range entries {
		if uint64(i) < offset {
			continue
		}

		var childRel string
		if rel == "." {
			childRel = e.Name()
		} else {
			childRel = filepath.Join(rel, e.Name())
		}
		childIno := b.assignIno(childRel)

		entryType := uint32(8) // DT_REG
		if e.IsDir() {
			entryType = 4 // DT_DIR
		} else if e.Type()&os.ModeSymlink != 0 {
			entryType = 10 // DT_LNK
		}

		result = append(result, FuseDirEntry{
			Ino:  childIno,
			Off:  uint64(i + 1),
			Type: entryType,
			Name: e.Name(),
		})
	}

	return result, nil
}

func (b *DirectBackend) Open(ino uint64, flags uint32) (uint64, error) {
	rel, err := b.getPath(ino)
	if err != nil {
		return 0, err
	}

	flag := os.O_RDONLY
	if !b.readOnly {
		flag = int(flags) & (os.O_RDONLY | os.O_WRONLY | os.O_RDWR | os.O_APPEND | os.O_TRUNC)
		if flag == 0 {
			flag = os.O_RDONLY
		}
	}

	f, err := os.OpenFile(b.hostPath(rel), flag, 0)
	if err != nil {
		return 0, err
	}

	fh := b.nextFH.Add(1) - 1
	b.handleMu.Lock()
	b.handles[fh] = f
	b.handleMu.Unlock()

	return fh, nil
}

func (b *DirectBackend) Read(ino uint64, fh uint64, offset uint64, size uint32) ([]byte, error) {
	b.handleMu.RLock()
	f, ok := b.handles[fh]
	b.handleMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("invalid file handle %d", fh)
	}

	buf := make([]byte, size)
	n, err := f.ReadAt(buf, int64(offset))
	if n > 0 {
		return buf[:n], nil
	}
	return nil, err
}

func (b *DirectBackend) Write(ino uint64, fh uint64, offset uint64, data []byte) (uint32, error) {
	if b.readOnly {
		return 0, fmt.Errorf("read-only filesystem")
	}

	b.handleMu.RLock()
	f, ok := b.handles[fh]
	b.handleMu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("invalid file handle %d", fh)
	}

	n, err := f.WriteAt(data, int64(offset))
	return uint32(n), err
}

func (b *DirectBackend) Create(parentIno uint64, name string, mode uint32, flags uint32) (*FuseAttr, uint64, uint64, error) {
	if b.readOnly {
		return nil, 0, 0, fmt.Errorf("read-only filesystem")
	}

	parentRel, err := b.getPath(parentIno)
	if err != nil {
		return nil, 0, 0, err
	}

	var childRel string
	if parentRel == "." {
		childRel = name
	} else {
		childRel = filepath.Join(parentRel, name)
	}

	f, err := os.OpenFile(b.hostPath(childRel), os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(mode&0o7777))
	if err != nil {
		return nil, 0, 0, err
	}

	ino := b.assignIno(childRel)
	fh := b.nextFH.Add(1) - 1
	b.handleMu.Lock()
	b.handles[fh] = f
	b.handleMu.Unlock()

	info, _ := f.Stat()
	attr := statToAttr(info, ino)
	return attr, ino, fh, nil
}

func (b *DirectBackend) Mkdir(parentIno uint64, name string, mode uint32) (*FuseAttr, uint64, error) {
	if b.readOnly {
		return nil, 0, fmt.Errorf("read-only filesystem")
	}

	parentRel, err := b.getPath(parentIno)
	if err != nil {
		return nil, 0, err
	}

	var childRel string
	if parentRel == "." {
		childRel = name
	} else {
		childRel = filepath.Join(parentRel, name)
	}

	if err := os.Mkdir(b.hostPath(childRel), os.FileMode(mode&0o7777)); err != nil {
		return nil, 0, err
	}

	ino := b.assignIno(childRel)
	info, _ := os.Lstat(b.hostPath(childRel))
	attr := statToAttr(info, ino)
	return attr, ino, nil
}

func (b *DirectBackend) Unlink(parentIno uint64, name string) error {
	if b.readOnly {
		return fmt.Errorf("read-only filesystem")
	}

	parentRel, err := b.getPath(parentIno)
	if err != nil {
		return err
	}

	var childRel string
	if parentRel == "." {
		childRel = name
	} else {
		childRel = filepath.Join(parentRel, name)
	}

	return os.Remove(b.hostPath(childRel))
}

func (b *DirectBackend) Rmdir(parentIno uint64, name string) error {
	return b.Unlink(parentIno, name) // os.Remove handles both.
}

func (b *DirectBackend) Rename(oldParentIno uint64, oldName string, newParentIno uint64, newName string) error {
	if b.readOnly {
		return fmt.Errorf("read-only filesystem")
	}

	oldParentRel, err := b.getPath(oldParentIno)
	if err != nil {
		return err
	}
	newParentRel, err := b.getPath(newParentIno)
	if err != nil {
		return err
	}

	oldPath := b.hostPath(filepath.Join(oldParentRel, oldName))
	newPath := b.hostPath(filepath.Join(newParentRel, newName))

	return os.Rename(oldPath, newPath)
}

func (b *DirectBackend) Release(ino uint64, fh uint64) error {
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

func (b *DirectBackend) Statfs(ino uint64) (*FuseStatfs, error) {
	return &FuseStatfs{
		Blocks:  1 << 30, // Large.
		Bfree:   1 << 29,
		Bavail:  1 << 29,
		Files:   1 << 20,
		Ffree:   1 << 19,
		Bsize:   4096,
		Namelen: 255,
		Frsize:  4096,
	}, nil
}

func (b *DirectBackend) Symlink(parentIno uint64, name string, target string) (*FuseAttr, uint64, error) {
	if b.readOnly {
		return nil, 0, fmt.Errorf("read-only filesystem")
	}

	parentRel, err := b.getPath(parentIno)
	if err != nil {
		return nil, 0, err
	}

	var childRel string
	if parentRel == "." {
		childRel = name
	} else {
		childRel = filepath.Join(parentRel, name)
	}

	if err := os.Symlink(target, b.hostPath(childRel)); err != nil {
		return nil, 0, err
	}

	ino := b.assignIno(childRel)
	info, _ := os.Lstat(b.hostPath(childRel))
	return statToAttr(info, ino), ino, nil
}

func (b *DirectBackend) Readlink(ino uint64) (string, error) {
	rel, err := b.getPath(ino)
	if err != nil {
		return "", err
	}
	return os.Readlink(b.hostPath(rel))
}

// statToAttr converts os.FileInfo to FuseAttr.
func statToAttr(info os.FileInfo, ino uint64) *FuseAttr {
	if info == nil {
		return &FuseAttr{Ino: ino}
	}

	attr := &FuseAttr{
		Ino:       ino,
		Size:      uint64(info.Size()),
		Mode:      uint32(info.Mode()),
		Nlink:     1,
		BlockSize: 4096,
	}
	attr.Blocks = (attr.Size + 511) / 512

	t := info.ModTime()
	attr.Mtime = uint64(t.Unix())
	attr.MtimeNs = uint32(t.Nanosecond())
	attr.Atime = attr.Mtime
	attr.AtimeNs = attr.MtimeNs
	attr.Ctime = attr.Mtime
	attr.CtimeNs = attr.MtimeNs

	// Get UID/GID from syscall stat.
	if sys, ok := info.Sys().(*syscall.Stat_t); ok {
		attr.UID = sys.Uid
		attr.GID = sys.Gid
		attr.Nlink = uint32(sys.Nlink)
	}

	if info.IsDir() {
		attr.Nlink = 2
	}

	return attr
}

// Ensure unused import doesn't cause issues.
var _ = time.Now
