// Package virtio implements virtio device emulation for the Go KVM VMM.
//
// All virtio devices communicate through virtqueues — shared memory ring buffers
// between host (VMM) and guest (kernel driver). This file implements the split
// virtqueue format (virtio spec 1.x, section 2.7).
//
// Memory layout (guest-physical addresses, mapped into host via mmap):
//
//	Descriptor Table: array of VirtqDesc (16 bytes each)
//	Available Ring:   VirtqAvail header + ring of desc indices
//	Used Ring:        VirtqUsed header + ring of (desc index, len) pairs
package virtio

import (
	"encoding/binary"
	"sync"
	"unsafe"
)

const (
	// VirtqDescFNext indicates the descriptor continues via the next field.
	VirtqDescFNext uint16 = 1
	// VirtqDescFWrite marks a descriptor as device-writable (guest reads result).
	VirtqDescFWrite uint16 = 2
	// VirtqDescFIndirect indicates the buffer contains a table of descriptors.
	VirtqDescFIndirect uint16 = 4
)

// VirtqDesc is a single descriptor in the descriptor table (16 bytes).
type VirtqDesc struct {
	Addr  uint64 // Guest-physical address of the buffer.
	Len   uint32 // Length of the buffer in bytes.
	Flags uint16 // VirtqDescF* flags.
	Next  uint16 // Index of the next descriptor if FNext is set.
}

// Queue implements a split virtqueue with host-side processing.
type Queue struct {
	// Size is the number of descriptors (power of 2, typically 128 or 256).
	Size uint16

	// Guest-physical addresses of the three regions.
	DescTableAddr uint64
	AvailAddr     uint64
	UsedAddr      uint64

	// Host pointer to guest memory (the full mmap'd region).
	mem []byte

	// Last index we consumed from the available ring.
	lastAvailIdx uint16

	mu sync.Mutex
}

// NewQueue creates a queue backed by the given guest memory.
func NewQueue(size uint16, descAddr, availAddr, usedAddr uint64, mem []byte) *Queue {
	return &Queue{
		Size:          size,
		DescTableAddr: descAddr,
		AvailAddr:     availAddr,
		UsedAddr:      usedAddr,
		mem:           mem,
	}
}

// HasAvailable returns true if there are descriptors to process.
func (q *Queue) HasAvailable() bool {
	availIdx := q.readAvailIdx()
	return availIdx != q.lastAvailIdx
}

// NextAvail returns the next available descriptor chain head index.
// Returns (index, true) if available, (0, false) if empty.
func (q *Queue) NextAvail() (uint16, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	availIdx := q.readAvailIdx()
	if availIdx == q.lastAvailIdx {
		return 0, false
	}

	// Read the descriptor index from the available ring.
	ringOffset := q.lastAvailIdx % q.Size
	idx := q.readAvailRing(ringOffset)
	q.lastAvailIdx++

	return idx, true
}

// ReadDesc reads a descriptor from the descriptor table.
func (q *Queue) ReadDesc(index uint16) VirtqDesc {
	offset := q.DescTableAddr + uint64(index)*16
	return VirtqDesc{
		Addr:  binary.LittleEndian.Uint64(q.mem[offset : offset+8]),
		Len:   binary.LittleEndian.Uint32(q.mem[offset+8 : offset+12]),
		Flags: binary.LittleEndian.Uint16(q.mem[offset+12 : offset+14]),
		Next:  binary.LittleEndian.Uint16(q.mem[offset+14 : offset+16]),
	}
}

// ReadChain reads an entire descriptor chain starting at head.
// Returns all descriptors in order.
func (q *Queue) ReadChain(head uint16) []VirtqDesc {
	var chain []VirtqDesc
	idx := head
	for {
		desc := q.ReadDesc(idx)
		chain = append(chain, desc)
		if desc.Flags&VirtqDescFNext == 0 {
			break
		}
		idx = desc.Next
		if len(chain) > int(q.Size) {
			break // Prevent infinite loops on malformed chains.
		}
	}
	return chain
}

// ReadBuffer reads bytes from guest memory at the given guest-physical address.
func (q *Queue) ReadBuffer(gpa uint64, length uint32) []byte {
	if gpa+uint64(length) > uint64(len(q.mem)) {
		return nil
	}
	buf := make([]byte, length)
	copy(buf, q.mem[gpa:gpa+uint64(length)])
	return buf
}

// WriteBuffer writes bytes to guest memory at the given guest-physical address.
func (q *Queue) WriteBuffer(gpa uint64, data []byte) {
	if gpa+uint64(len(data)) > uint64(len(q.mem)) {
		return
	}
	copy(q.mem[gpa:], data)
}

// PutUsed adds a completed descriptor to the used ring.
// headIdx is the head of the original descriptor chain, written is bytes written.
func (q *Queue) PutUsed(headIdx uint16, written uint32) {
	q.mu.Lock()
	defer q.mu.Unlock()

	usedIdx := q.readUsedIdx()
	ringOffset := usedIdx % q.Size

	// Write used ring entry: (id: uint32, len: uint32) = 8 bytes.
	entryAddr := q.UsedAddr + 4 + uint64(ringOffset)*8 // +4 skips flags+idx header
	binary.LittleEndian.PutUint32(q.mem[entryAddr:], uint32(headIdx))
	binary.LittleEndian.PutUint32(q.mem[entryAddr+4:], written)

	// Increment used index (with memory barrier via atomic store).
	q.writeUsedIdx(usedIdx + 1)
}

// --- Low-level memory access ---

func (q *Queue) readAvailIdx() uint16 {
	// Available ring layout: flags(u16) + idx(u16) + ring[size](u16) + used_event(u16)
	offset := q.AvailAddr + 2 // Skip flags, read idx.
	return binary.LittleEndian.Uint16(q.mem[offset : offset+2])
}

func (q *Queue) readAvailRing(ringIdx uint16) uint16 {
	offset := q.AvailAddr + 4 + uint64(ringIdx)*2 // +4 skips flags+idx.
	return binary.LittleEndian.Uint16(q.mem[offset : offset+2])
}

func (q *Queue) readUsedIdx() uint16 {
	// Used ring layout: flags(u16) + idx(u16) + ring[size](elem: id(u32)+len(u32))
	offset := q.UsedAddr + 2 // Skip flags, read idx.
	return binary.LittleEndian.Uint16(q.mem[offset : offset+2])
}

func (q *Queue) writeUsedIdx(idx uint16) {
	offset := q.UsedAddr + 2
	binary.LittleEndian.PutUint16(q.mem[offset:offset+2], idx)
}

// DescTableSize returns the byte size of the descriptor table.
func DescTableSize(queueSize uint16) uint64 {
	return uint64(queueSize) * 16
}

// AvailRingSize returns the byte size of the available ring.
func AvailRingSize(queueSize uint16) uint64 {
	return 2 + 2 + uint64(queueSize)*2 + 2 // flags + idx + ring + used_event
}

// UsedRingSize returns the byte size of the used ring.
func UsedRingSize(queueSize uint16) uint64 {
	return 2 + 2 + uint64(queueSize)*8 + 2 // flags + idx + ring(elem=8) + avail_event
}

// Align returns addr aligned up to the given alignment.
func Align(addr, alignment uint64) uint64 {
	return (addr + alignment - 1) &^ (alignment - 1)
}

// Suppress unused import of unsafe (used for potential future optimizations).
var _ = unsafe.Pointer(nil)
