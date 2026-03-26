package virtio

import (
	"encoding/binary"
	"sync"
)

// FUSE opcodes (from linux/fuse.h).
const (
	fuseLookup   = 1
	fuseForget   = 2
	fuseGetattr  = 3
	fuseSetattr  = 4
	fuseReadlink = 5
	fuseSymlink  = 6
	fuseMknod    = 8
	fuseMkdir    = 9
	fuseUnlink   = 10
	fuseRmdir    = 11
	fuseRename   = 12
	fuseOpen     = 14
	fuseRead     = 15
	fuseWrite    = 16
	fuseStatfs   = 17
	fuseRelease  = 18
	fuseFsync    = 20
	fuseFlush    = 25
	fuseInit     = 26
	fuseOpendir  = 27
	fuseReaddir  = 28
	fuseReleasedir = 29
	fuseCreate   = 35
	fuseReaddirplus = 44
)

// FUSE error codes (negated errno).
const (
	fuseOK     = 0
	fuseENOENT = -2  // No such file or directory.
	fuseEIO    = -5  // I/O error.
	fuseEACCES = -13 // Permission denied.
	fuseEEXIST = -17 // File exists.
	fuseENOTDIR = -20 // Not a directory.
	fuseEISDIR = -21  // Is a directory.
	fuseEINVAL = -22  // Invalid argument.
	fuseENOSYS = -38  // Function not implemented.
	fuseENOTEMPTY = -39 // Directory not empty.
)

// fuseInHeader is the request header (40 bytes on 64-bit).
type fuseInHeader struct {
	Len     uint32
	Opcode  uint32
	Unique  uint64
	NodeID  uint64
	UID     uint32
	GID     uint32
	PID     uint32
	Padding uint32
}

// fuseOutHeader is the response header (16 bytes).
type fuseOutHeader struct {
	Len    uint32
	Error  int32
	Unique uint64
}

// FSBackend is the interface that filesystem backends implement.
// The FS device dispatches FUSE operations to this backend.
type FSBackend interface {
	Lookup(parentIno uint64, name string) (*FuseAttr, uint64, error)  // Returns (attr, ino, err).
	Getattr(ino uint64) (*FuseAttr, error)
	Setattr(ino uint64, attr *FuseAttr, valid uint32) (*FuseAttr, error)
	Readdir(ino uint64, offset uint64) ([]FuseDirEntry, error)
	Open(ino uint64, flags uint32) (uint64, error) // Returns file handle.
	Read(ino uint64, fh uint64, offset uint64, size uint32) ([]byte, error)
	Write(ino uint64, fh uint64, offset uint64, data []byte) (uint32, error)
	Create(parentIno uint64, name string, mode uint32, flags uint32) (*FuseAttr, uint64, uint64, error) // (attr, ino, fh, err)
	Mkdir(parentIno uint64, name string, mode uint32) (*FuseAttr, uint64, error)
	Unlink(parentIno uint64, name string) error
	Rmdir(parentIno uint64, name string) error
	Rename(oldParentIno uint64, oldName string, newParentIno uint64, newName string) error
	Release(ino uint64, fh uint64) error
	Statfs(ino uint64) (*FuseStatfs, error)
	Symlink(parentIno uint64, name string, target string) (*FuseAttr, uint64, error)
	Readlink(ino uint64) (string, error)
}

// FuseAttr holds file attributes.
type FuseAttr struct {
	Ino       uint64
	Size      uint64
	Blocks    uint64
	Atime     uint64
	Mtime     uint64
	Ctime     uint64
	AtimeNs   uint32
	MtimeNs   uint32
	CtimeNs   uint32
	Mode      uint32
	Nlink     uint32
	UID       uint32
	GID       uint32
	Rdev      uint32
	BlockSize uint32
}

// FuseDirEntry represents a directory entry.
type FuseDirEntry struct {
	Ino    uint64
	Off    uint64
	Type   uint32
	Name   string
}

// FuseStatfs holds filesystem statistics.
type FuseStatfs struct {
	Blocks  uint64
	Bfree   uint64
	Bavail  uint64
	Files   uint64
	Ffree   uint64
	Bsize   uint32
	Namelen uint32
	Frsize  uint32
}

// FS implements a virtio-fs device (virtio spec 5.11).
// It serves a filesystem to the guest via the FUSE protocol over virtqueues.
//
// Queue 0: hiprio (high priority requests)
// Queue 1: request queue
type FS struct {
	tag     string    // Filesystem mount tag.
	backend FSBackend // The actual filesystem implementation.
	mu      sync.Mutex
}

// NewFS creates a new virtio-fs device with the given mount tag and backend.
func NewFS(tag string, backend FSBackend) *FS {
	return &FS{
		tag:     tag,
		backend: backend,
	}
}

func (f *FS) DeviceID() DeviceID { return DeviceIDFS }
func (f *FS) NumQueues() int     { return 2 }

func (f *FS) Features() uint64 {
	return 1 << 32 // VIRTIO_F_VERSION_1
}

func (f *FS) ConfigSpace() []byte {
	// virtio_fs_config: tag[36] + num_request_queues(u32)
	config := make([]byte, 40)
	copy(config[0:36], f.tag)
	binary.LittleEndian.PutUint32(config[36:40], 1) // 1 request queue.
	return config
}

func (f *FS) Reset() {}

func (f *FS) HandleQueue(queueIdx int, queue *Queue) {
	if queueIdx == 0 {
		return // hiprio queue — not used in basic implementation.
	}
	f.handleRequests(queue)
}

// handleRequests processes FUSE requests from the guest.
func (f *FS) handleRequests(queue *Queue) {
	for {
		head, ok := queue.NextAvail()
		if !ok {
			return
		}

		chain := queue.ReadChain(head)

		// Collect readable (request) data.
		var reqData []byte
		var writeDescs []VirtqDesc
		for _, desc := range chain {
			if desc.Flags&VirtqDescFWrite != 0 {
				writeDescs = append(writeDescs, desc)
			} else {
				data := queue.ReadBuffer(desc.Addr, desc.Len)
				reqData = append(reqData, data...)
			}
		}

		// Parse FUSE request and dispatch.
		response := f.dispatch(reqData)

		// Write response into device-writable descriptors.
		var written uint32
		offset := 0
		for _, desc := range writeDescs {
			toWrite := len(response) - offset
			if toWrite <= 0 {
				break
			}
			if toWrite > int(desc.Len) {
				toWrite = int(desc.Len)
			}
			queue.WriteBuffer(desc.Addr, response[offset:offset+toWrite])
			offset += toWrite
			written += uint32(toWrite)
		}

		queue.PutUsed(head, written)
	}
}

// dispatch parses a FUSE request and calls the appropriate backend method.
func (f *FS) dispatch(data []byte) []byte {
	if len(data) < 40 {
		return f.errorResponse(0, fuseEINVAL)
	}

	hdr := parseFuseInHeader(data)

	f.mu.Lock()
	defer f.mu.Unlock()

	switch hdr.Opcode {
	case fuseInit:
		return f.handleInit(hdr, data[40:])
	case fuseLookup:
		return f.handleLookup(hdr, data[40:])
	case fuseGetattr:
		return f.handleGetattr(hdr, data[40:])
	case fuseSetattr:
		return f.handleSetattr(hdr, data[40:])
	case fuseOpen, fuseOpendir:
		return f.handleOpen(hdr, data[40:])
	case fuseRead:
		return f.handleRead(hdr, data[40:])
	case fuseWrite:
		return f.handleWrite(hdr, data[40:])
	case fuseReaddir, fuseReaddirplus:
		return f.handleReaddir(hdr, data[40:])
	case fuseCreate:
		return f.handleCreate(hdr, data[40:])
	case fuseMkdir:
		return f.handleMkdir(hdr, data[40:])
	case fuseUnlink:
		return f.handleUnlink(hdr, data[40:])
	case fuseRmdir:
		return f.handleRmdir(hdr, data[40:])
	case fuseRename:
		return f.handleRename(hdr, data[40:])
	case fuseRelease, fuseReleasedir:
		return f.handleRelease(hdr, data[40:])
	case fuseStatfs:
		return f.handleStatfs(hdr)
	case fuseSymlink:
		return f.handleSymlink(hdr, data[40:])
	case fuseReadlink:
		return f.handleReadlink(hdr)
	case fuseFlush, fuseFsync:
		return f.successResponse(hdr.Unique)
	case fuseForget:
		return nil // No response for FORGET.
	default:
		return f.errorResponse(hdr.Unique, fuseENOSYS)
	}
}

func (f *FS) handleInit(hdr fuseInHeader, data []byte) []byte {
	// FUSE_INIT response: major(u32) + minor(u32) + max_readahead(u32) + flags(u32) + ...
	resp := make([]byte, 64)
	binary.LittleEndian.PutUint32(resp[0:4], 7)      // Major version.
	binary.LittleEndian.PutUint32(resp[4:8], 31)     // Minor version.
	binary.LittleEndian.PutUint32(resp[8:12], 131072) // max_readahead = 128K.
	binary.LittleEndian.PutUint32(resp[12:16], 0)     // flags.
	binary.LittleEndian.PutUint32(resp[36:40], 1024*1024) // max_write = 1MB.
	return f.wrapResponse(hdr.Unique, resp)
}

func (f *FS) handleLookup(hdr fuseInHeader, data []byte) []byte {
	name := cstring(data)
	attr, ino, err := f.backend.Lookup(hdr.NodeID, name)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseENOENT)
	}
	return f.entryResponse(hdr.Unique, ino, attr)
}

func (f *FS) handleGetattr(hdr fuseInHeader, data []byte) []byte {
	attr, err := f.backend.Getattr(hdr.NodeID)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseENOENT)
	}
	return f.attrResponse(hdr.Unique, attr)
}

func (f *FS) handleSetattr(hdr fuseInHeader, data []byte) []byte {
	if len(data) < 4 {
		return f.errorResponse(hdr.Unique, fuseEINVAL)
	}
	valid := binary.LittleEndian.Uint32(data[0:4])
	attr, err := f.backend.Setattr(hdr.NodeID, nil, valid)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseEIO)
	}
	return f.attrResponse(hdr.Unique, attr)
}

func (f *FS) handleOpen(hdr fuseInHeader, data []byte) []byte {
	var flags uint32
	if len(data) >= 4 {
		flags = binary.LittleEndian.Uint32(data[0:4])
	}
	fh, err := f.backend.Open(hdr.NodeID, flags)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseEIO)
	}
	resp := make([]byte, 16)
	binary.LittleEndian.PutUint64(resp[0:8], fh)
	binary.LittleEndian.PutUint32(resp[8:12], 0) // open_flags (FOPEN_KEEP_CACHE etc.)
	return f.wrapResponse(hdr.Unique, resp)
}

func (f *FS) handleRead(hdr fuseInHeader, data []byte) []byte {
	if len(data) < 24 {
		return f.errorResponse(hdr.Unique, fuseEINVAL)
	}
	fh := binary.LittleEndian.Uint64(data[0:8])
	offset := binary.LittleEndian.Uint64(data[8:16])
	size := binary.LittleEndian.Uint32(data[16:20])

	buf, err := f.backend.Read(hdr.NodeID, fh, offset, size)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseEIO)
	}
	return f.wrapResponse(hdr.Unique, buf)
}

func (f *FS) handleWrite(hdr fuseInHeader, data []byte) []byte {
	if len(data) < 24 {
		return f.errorResponse(hdr.Unique, fuseEINVAL)
	}
	fh := binary.LittleEndian.Uint64(data[0:8])
	offset := binary.LittleEndian.Uint64(data[8:16])
	size := binary.LittleEndian.Uint32(data[16:20])
	// Write data follows the write_in header (40 bytes total).
	writeData := data[40:]
	if uint32(len(writeData)) > size {
		writeData = writeData[:size]
	}

	written, err := f.backend.Write(hdr.NodeID, fh, offset, writeData)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseEIO)
	}
	resp := make([]byte, 8)
	binary.LittleEndian.PutUint32(resp[0:4], written)
	return f.wrapResponse(hdr.Unique, resp)
}

func (f *FS) handleReaddir(hdr fuseInHeader, data []byte) []byte {
	var offset uint64
	if len(data) >= 24 {
		offset = binary.LittleEndian.Uint64(data[8:16])
	}

	entries, err := f.backend.Readdir(hdr.NodeID, offset)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseEIO)
	}

	// Encode directory entries.
	var buf []byte
	for _, e := range entries {
		entry := encodeDirEntry(e)
		buf = append(buf, entry...)
	}
	return f.wrapResponse(hdr.Unique, buf)
}

func (f *FS) handleCreate(hdr fuseInHeader, data []byte) []byte {
	if len(data) < 12 {
		return f.errorResponse(hdr.Unique, fuseEINVAL)
	}
	flags := binary.LittleEndian.Uint32(data[0:4])
	mode := binary.LittleEndian.Uint32(data[4:8])
	name := cstring(data[12:])

	attr, ino, fh, err := f.backend.Create(hdr.NodeID, name, mode, flags)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseEIO)
	}

	// Entry + Open response.
	entryResp := encodeEntry(ino, attr)
	openResp := make([]byte, 16)
	binary.LittleEndian.PutUint64(openResp[0:8], fh)
	return f.wrapResponse(hdr.Unique, append(entryResp, openResp...))
}

func (f *FS) handleMkdir(hdr fuseInHeader, data []byte) []byte {
	if len(data) < 8 {
		return f.errorResponse(hdr.Unique, fuseEINVAL)
	}
	mode := binary.LittleEndian.Uint32(data[0:4])
	name := cstring(data[8:])

	attr, ino, err := f.backend.Mkdir(hdr.NodeID, name, mode)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseEIO)
	}
	return f.entryResponse(hdr.Unique, ino, attr)
}

func (f *FS) handleUnlink(hdr fuseInHeader, data []byte) []byte {
	name := cstring(data)
	if err := f.backend.Unlink(hdr.NodeID, name); err != nil {
		return f.errorResponse(hdr.Unique, fuseENOENT)
	}
	return f.successResponse(hdr.Unique)
}

func (f *FS) handleRmdir(hdr fuseInHeader, data []byte) []byte {
	name := cstring(data)
	if err := f.backend.Rmdir(hdr.NodeID, name); err != nil {
		return f.errorResponse(hdr.Unique, fuseENOENT)
	}
	return f.successResponse(hdr.Unique)
}

func (f *FS) handleRename(hdr fuseInHeader, data []byte) []byte {
	if len(data) < 8 {
		return f.errorResponse(hdr.Unique, fuseEINVAL)
	}
	newDir := binary.LittleEndian.Uint64(data[0:8])
	// Old name and new name are null-separated after the header.
	names := data[8:]
	oldName := cstring(names)
	newName := ""
	if idx := len(oldName) + 1; idx < len(names) {
		newName = cstring(names[idx:])
	}

	if err := f.backend.Rename(hdr.NodeID, oldName, newDir, newName); err != nil {
		return f.errorResponse(hdr.Unique, fuseEIO)
	}
	return f.successResponse(hdr.Unique)
}

func (f *FS) handleRelease(hdr fuseInHeader, data []byte) []byte {
	var fh uint64
	if len(data) >= 8 {
		fh = binary.LittleEndian.Uint64(data[0:8])
	}
	f.backend.Release(hdr.NodeID, fh)
	return f.successResponse(hdr.Unique)
}

func (f *FS) handleStatfs(hdr fuseInHeader) []byte {
	st, err := f.backend.Statfs(hdr.NodeID)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseEIO)
	}
	resp := make([]byte, 56)
	binary.LittleEndian.PutUint64(resp[0:8], st.Blocks)
	binary.LittleEndian.PutUint64(resp[8:16], st.Bfree)
	binary.LittleEndian.PutUint64(resp[16:24], st.Bavail)
	binary.LittleEndian.PutUint64(resp[24:32], st.Files)
	binary.LittleEndian.PutUint64(resp[32:40], st.Ffree)
	binary.LittleEndian.PutUint32(resp[40:44], st.Bsize)
	binary.LittleEndian.PutUint32(resp[44:48], st.Namelen)
	binary.LittleEndian.PutUint32(resp[48:52], st.Frsize)
	return f.wrapResponse(hdr.Unique, resp)
}

func (f *FS) handleSymlink(hdr fuseInHeader, data []byte) []byte {
	name := cstring(data)
	target := ""
	if idx := len(name) + 1; idx < len(data) {
		target = cstring(data[idx:])
	}
	attr, ino, err := f.backend.Symlink(hdr.NodeID, name, target)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseEIO)
	}
	return f.entryResponse(hdr.Unique, ino, attr)
}

func (f *FS) handleReadlink(hdr fuseInHeader) []byte {
	target, err := f.backend.Readlink(hdr.NodeID)
	if err != nil {
		return f.errorResponse(hdr.Unique, fuseENOENT)
	}
	return f.wrapResponse(hdr.Unique, []byte(target))
}

// --- Response helpers ---

func (f *FS) wrapResponse(unique uint64, data []byte) []byte {
	hdrLen := 16
	resp := make([]byte, hdrLen+len(data))
	binary.LittleEndian.PutUint32(resp[0:4], uint32(len(resp)))
	binary.LittleEndian.PutUint32(resp[4:8], 0) // error = 0 (success)
	binary.LittleEndian.PutUint64(resp[8:16], unique)
	copy(resp[16:], data)
	return resp
}

func (f *FS) errorResponse(unique uint64, errno int32) []byte {
	resp := make([]byte, 16)
	binary.LittleEndian.PutUint32(resp[0:4], 16)
	binary.LittleEndian.PutUint32(resp[4:8], uint32(errno))
	binary.LittleEndian.PutUint64(resp[8:16], unique)
	return resp
}

func (f *FS) successResponse(unique uint64) []byte {
	return f.wrapResponse(unique, nil)
}

func (f *FS) entryResponse(unique uint64, ino uint64, attr *FuseAttr) []byte {
	return f.wrapResponse(unique, encodeEntry(ino, attr))
}

func (f *FS) attrResponse(unique uint64, attr *FuseAttr) []byte {
	resp := make([]byte, 96) // fuse_attr_out: attr_valid(u64) + attr_valid_nsec(u32) + dummy(u32) + fuse_attr
	binary.LittleEndian.PutUint64(resp[0:8], 1) // attr_valid = 1 second.
	encodeAttr(resp[16:], attr)
	return f.wrapResponse(unique, resp)
}

func encodeEntry(ino uint64, attr *FuseAttr) []byte {
	// fuse_entry_out: nodeid(u64) + generation(u64) + entry_valid(u64) + attr_valid(u64) +
	//                 entry_valid_nsec(u32) + attr_valid_nsec(u32) + fuse_attr
	entry := make([]byte, 128)
	binary.LittleEndian.PutUint64(entry[0:8], ino)   // nodeid
	binary.LittleEndian.PutUint64(entry[8:16], 0)     // generation
	binary.LittleEndian.PutUint64(entry[16:24], 1)    // entry_valid = 1s
	binary.LittleEndian.PutUint64(entry[24:32], 1)    // attr_valid = 1s
	encodeAttr(entry[40:], attr)
	return entry
}

func encodeAttr(buf []byte, attr *FuseAttr) {
	if attr == nil || len(buf) < 88 {
		return
	}
	binary.LittleEndian.PutUint64(buf[0:8], attr.Ino)
	binary.LittleEndian.PutUint64(buf[8:16], attr.Size)
	binary.LittleEndian.PutUint64(buf[16:24], attr.Blocks)
	binary.LittleEndian.PutUint64(buf[24:32], attr.Atime)
	binary.LittleEndian.PutUint64(buf[32:40], attr.Mtime)
	binary.LittleEndian.PutUint64(buf[40:48], attr.Ctime)
	binary.LittleEndian.PutUint32(buf[48:52], attr.AtimeNs)
	binary.LittleEndian.PutUint32(buf[52:56], attr.MtimeNs)
	binary.LittleEndian.PutUint32(buf[56:60], attr.CtimeNs)
	binary.LittleEndian.PutUint32(buf[60:64], attr.Mode)
	binary.LittleEndian.PutUint32(buf[64:68], attr.Nlink)
	binary.LittleEndian.PutUint32(buf[68:72], attr.UID)
	binary.LittleEndian.PutUint32(buf[72:76], attr.GID)
	binary.LittleEndian.PutUint32(buf[76:80], attr.Rdev)
	binary.LittleEndian.PutUint32(buf[80:84], attr.BlockSize)
}

func encodeDirEntry(e FuseDirEntry) []byte {
	// fuse_dirent: ino(u64) + off(u64) + namelen(u32) + type(u32) + name[namelen] + padding
	nameLen := len(e.Name)
	padded := (nameLen + 7) &^ 7 // Align to 8 bytes.
	entry := make([]byte, 24+padded)
	binary.LittleEndian.PutUint64(entry[0:8], e.Ino)
	binary.LittleEndian.PutUint64(entry[8:16], e.Off)
	binary.LittleEndian.PutUint32(entry[16:20], uint32(nameLen))
	binary.LittleEndian.PutUint32(entry[20:24], e.Type)
	copy(entry[24:], e.Name)
	return entry
}

// --- Helpers ---

func parseFuseInHeader(data []byte) fuseInHeader {
	return fuseInHeader{
		Len:    binary.LittleEndian.Uint32(data[0:4]),
		Opcode: binary.LittleEndian.Uint32(data[4:8]),
		Unique: binary.LittleEndian.Uint64(data[8:16]),
		NodeID: binary.LittleEndian.Uint64(data[16:24]),
		UID:    binary.LittleEndian.Uint32(data[24:28]),
		GID:    binary.LittleEndian.Uint32(data[28:32]),
		PID:    binary.LittleEndian.Uint32(data[32:36]),
	}
}

func cstring(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}

