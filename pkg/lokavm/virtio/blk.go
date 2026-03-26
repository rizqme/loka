package virtio

import (
	"encoding/binary"
	"os"
	"sync"
)

// Block device request types (virtio spec 5.2.6).
const (
	blkTypeIn      = 0 // Read from device.
	blkTypeOut     = 1 // Write to device.
	blkTypeFlush   = 4 // Flush.
	blkTypeGetID   = 8 // Get device ID.
	blkTypeDiscard = 11
	blkTypeWriteZeroes = 13
)

// Block device status codes.
const (
	blkStatusOK       = 0
	blkStatusIOErr    = 1
	blkStatusUnsup    = 2
)

// Block implements a virtio-blk device (virtio spec 5.2).
// Backed by a file on the host.
//
// Queue 0: requestq
type Block struct {
	file     *os.File
	readOnly bool
	size     uint64 // Size in bytes.

	mu sync.Mutex
}

// NewBlock creates a new virtio-blk device backed by the given file.
func NewBlock(path string, readOnly bool) (*Block, error) {
	flag := os.O_RDWR
	if readOnly {
		flag = os.O_RDONLY
	}
	f, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}

	return &Block{
		file:     f,
		readOnly: readOnly,
		size:     uint64(info.Size()),
	}, nil
}

func (b *Block) DeviceID() DeviceID { return DeviceIDBlock }
func (b *Block) NumQueues() int     { return 1 }

func (b *Block) Features() uint64 {
	var features uint64
	if b.readOnly {
		features |= 1 << 5 // VIRTIO_BLK_F_RO
	}
	features |= 1 << 6  // VIRTIO_BLK_F_BLK_SIZE
	features |= 1 << 9  // VIRTIO_BLK_F_FLUSH
	return features
}

func (b *Block) ConfigSpace() []byte {
	// virtio_blk_config: capacity(u64) + various fields.
	// Minimum: capacity in 512-byte sectors (8 bytes).
	config := make([]byte, 64)
	sectors := b.size / 512
	binary.LittleEndian.PutUint64(config[0:8], sectors)

	// blk_size at offset 20 (4 bytes).
	binary.LittleEndian.PutUint32(config[20:24], 512)

	return config
}

func (b *Block) Reset() {}

func (b *Block) HandleQueue(queueIdx int, queue *Queue) {
	if queueIdx != 0 {
		return
	}
	b.handleRequests(queue)
}

func (b *Block) handleRequests(queue *Queue) {
	for {
		head, ok := queue.NextAvail()
		if !ok {
			return
		}

		chain := queue.ReadChain(head)
		written := b.processRequest(chain, queue)
		queue.PutUsed(head, written)
	}
}

// processRequest handles a single block I/O request.
// A request chain is: header(readable) + data(readable/writable) + status(writable, 1 byte).
func (b *Block) processRequest(chain []VirtqDesc, queue *Queue) uint32 {
	if len(chain) < 2 {
		return 0
	}

	// Read the request header (16 bytes: type(u32) + reserved(u32) + sector(u64)).
	hdr := queue.ReadBuffer(chain[0].Addr, chain[0].Len)
	if len(hdr) < 16 {
		return 0
	}

	reqType := binary.LittleEndian.Uint32(hdr[0:4])
	sector := binary.LittleEndian.Uint64(hdr[8:16])
	offset := int64(sector) * 512

	// Status descriptor is always the last one.
	statusDesc := chain[len(chain)-1]
	status := byte(blkStatusOK)
	var totalWritten uint32

	b.mu.Lock()
	defer b.mu.Unlock()

	switch reqType {
	case blkTypeIn: // Read
		// Data descriptors are in the middle (device-writable).
		for i := 1; i < len(chain)-1; i++ {
			desc := chain[i]
			if desc.Flags&VirtqDescFWrite == 0 {
				continue
			}
			buf := make([]byte, desc.Len)
			n, err := b.file.ReadAt(buf, offset)
			if err != nil && n == 0 {
				status = blkStatusIOErr
				break
			}
			queue.WriteBuffer(desc.Addr, buf[:n])
			offset += int64(n)
			totalWritten += uint32(n)
		}

	case blkTypeOut: // Write
		if b.readOnly {
			status = blkStatusIOErr
			break
		}
		for i := 1; i < len(chain)-1; i++ {
			desc := chain[i]
			if desc.Flags&VirtqDescFWrite != 0 {
				continue
			}
			data := queue.ReadBuffer(desc.Addr, desc.Len)
			n, err := b.file.WriteAt(data, offset)
			if err != nil {
				status = blkStatusIOErr
				break
			}
			offset += int64(n)
		}

	case blkTypeFlush:
		if err := b.file.Sync(); err != nil {
			status = blkStatusIOErr
		}

	case blkTypeGetID:
		// Return a device ID string.
		if len(chain) > 1 {
			id := []byte("loka-blk\x00")
			desc := chain[1]
			if desc.Flags&VirtqDescFWrite != 0 {
				queue.WriteBuffer(desc.Addr, id)
				totalWritten += uint32(len(id))
			}
		}

	default:
		status = blkStatusUnsup
	}

	// Write status byte.
	queue.WriteBuffer(statusDesc.Addr, []byte{status})
	totalWritten++ // Status byte.

	return totalWritten
}

// Close closes the underlying file.
func (b *Block) Close() error {
	return b.file.Close()
}
