package virtio

import (
	"encoding/binary"
	"sync"
)

// Balloon implements a virtio-balloon device (virtio spec 5.5).
// Used for dynamic memory management — the host can request the guest to
// return unused pages (inflate) or reclaim them (deflate).
//
// Queue 0: inflateq (guest returns pages to host)
// Queue 1: deflateq (guest reclaims pages from host)
type Balloon struct {
	// Target number of 4K pages the guest should give back.
	targetPages uint32

	// Actual number of pages the guest has returned.
	actualPages uint32

	mu sync.Mutex
}

// NewBalloon creates a new virtio-balloon device.
func NewBalloon() *Balloon {
	return &Balloon{}
}

func (b *Balloon) DeviceID() DeviceID { return DeviceIDBalloon }
func (b *Balloon) NumQueues() int     { return 2 }

func (b *Balloon) Features() uint64 {
	return 1 << 32 // VIRTIO_F_VERSION_1
}

func (b *Balloon) ConfigSpace() []byte {
	// virtio_balloon_config: num_pages(u32) + actual(u32)
	config := make([]byte, 8)
	b.mu.Lock()
	binary.LittleEndian.PutUint32(config[0:4], b.targetPages)
	binary.LittleEndian.PutUint32(config[4:8], b.actualPages)
	b.mu.Unlock()
	return config
}

func (b *Balloon) Reset() {
	b.mu.Lock()
	b.targetPages = 0
	b.actualPages = 0
	b.mu.Unlock()
}

func (b *Balloon) HandleQueue(queueIdx int, queue *Queue) {
	switch queueIdx {
	case 0:
		b.handleInflate(queue)
	case 1:
		b.handleDeflate(queue)
	}
}

// SetTarget sets the balloon target in MB. The guest will try to return
// enough pages to free this much memory.
func (b *Balloon) SetTarget(targetMB int) {
	b.mu.Lock()
	b.targetPages = uint32(targetMB * 256) // 256 pages per MB (4K pages).
	b.mu.Unlock()
}

// ActualMB returns the actual amount of memory reclaimed in MB.
func (b *Balloon) ActualMB() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return int(b.actualPages / 256)
}

func (b *Balloon) handleInflate(queue *Queue) {
	for {
		head, ok := queue.NextAvail()
		if !ok {
			return
		}

		chain := queue.ReadChain(head)
		var pageCount uint32

		for _, desc := range chain {
			if desc.Flags&VirtqDescFWrite != 0 {
				continue
			}
			// Each entry is a u32 page frame number.
			data := queue.ReadBuffer(desc.Addr, desc.Len)
			pageCount += desc.Len / 4

			// Advise the kernel that these pages can be freed.
			// In a real implementation, we'd call madvise(MADV_DONTNEED)
			// on the corresponding host memory regions.
			_ = data
		}

		b.mu.Lock()
		b.actualPages += pageCount
		b.mu.Unlock()

		queue.PutUsed(head, 0)
	}
}

func (b *Balloon) handleDeflate(queue *Queue) {
	for {
		head, ok := queue.NextAvail()
		if !ok {
			return
		}

		chain := queue.ReadChain(head)
		var pageCount uint32

		for _, desc := range chain {
			if desc.Flags&VirtqDescFWrite != 0 {
				continue
			}
			pageCount += desc.Len / 4
		}

		b.mu.Lock()
		if b.actualPages >= pageCount {
			b.actualPages -= pageCount
		} else {
			b.actualPages = 0
		}
		b.mu.Unlock()

		queue.PutUsed(head, 0)
	}
}
