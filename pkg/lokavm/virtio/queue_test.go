package virtio

import (
	"encoding/binary"
	"testing"
)

// setupTestQueue creates a Queue backed by a byte slice with proper layout.
func setupTestQueue(t *testing.T, size uint16) (*Queue, []byte) {
	t.Helper()

	descSize := DescTableSize(size)
	availSize := AvailRingSize(size)
	usedSize := UsedRingSize(size)

	descAddr := uint64(0)
	availAddr := Align(descAddr+descSize, 2)
	usedAddr := Align(availAddr+availSize, 4)

	total := usedAddr + usedSize + 4096 // Extra space for buffers.
	mem := make([]byte, total)

	q := NewQueue(size, descAddr, availAddr, usedAddr, mem)
	return q, mem
}

// writeDesc writes a descriptor into the descriptor table.
func writeDesc(mem []byte, descAddr uint64, idx uint16, addr uint64, length uint32, flags uint16, next uint16) {
	offset := descAddr + uint64(idx)*16
	binary.LittleEndian.PutUint64(mem[offset:], addr)
	binary.LittleEndian.PutUint32(mem[offset+8:], length)
	binary.LittleEndian.PutUint16(mem[offset+12:], flags)
	binary.LittleEndian.PutUint16(mem[offset+14:], next)
}

// pushAvail adds a descriptor index to the available ring.
func pushAvail(mem []byte, availAddr uint64, size uint16, idx uint16, availIdx uint16) {
	// Write ring entry.
	ringOffset := availAddr + 4 + uint64(availIdx%size)*2
	binary.LittleEndian.PutUint16(mem[ringOffset:], idx)
	// Increment avail idx.
	binary.LittleEndian.PutUint16(mem[availAddr+2:], availIdx+1)
}

func TestQueueHasAvailable_Empty(t *testing.T) {
	q, _ := setupTestQueue(t, 16)
	if q.HasAvailable() {
		t.Error("expected empty queue to have no available descriptors")
	}
}

func TestQueueHasAvailable_WithEntry(t *testing.T) {
	q, mem := setupTestQueue(t, 16)
	pushAvail(mem, q.AvailAddr, q.Size, 0, 0)

	if !q.HasAvailable() {
		t.Error("expected queue with entry to have available descriptors")
	}
}

func TestQueueNextAvail(t *testing.T) {
	q, mem := setupTestQueue(t, 16)

	// Empty queue.
	_, ok := q.NextAvail()
	if ok {
		t.Error("expected NextAvail to return false on empty queue")
	}

	// Add two entries.
	pushAvail(mem, q.AvailAddr, q.Size, 3, 0)
	pushAvail(mem, q.AvailAddr, q.Size, 7, 1)

	idx, ok := q.NextAvail()
	if !ok || idx != 3 {
		t.Errorf("expected (3, true), got (%d, %v)", idx, ok)
	}

	idx, ok = q.NextAvail()
	if !ok || idx != 7 {
		t.Errorf("expected (7, true), got (%d, %v)", idx, ok)
	}

	// Empty again.
	_, ok = q.NextAvail()
	if ok {
		t.Error("expected NextAvail to return false after consuming all")
	}
}

func TestQueueReadDesc(t *testing.T) {
	q, mem := setupTestQueue(t, 16)

	writeDesc(mem, q.DescTableAddr, 0, 0x1000, 256, VirtqDescFWrite, 0)
	writeDesc(mem, q.DescTableAddr, 5, 0x2000, 512, VirtqDescFNext, 6)

	d := q.ReadDesc(0)
	if d.Addr != 0x1000 || d.Len != 256 || d.Flags != VirtqDescFWrite {
		t.Errorf("desc 0: got addr=%x len=%d flags=%d", d.Addr, d.Len, d.Flags)
	}

	d = q.ReadDesc(5)
	if d.Addr != 0x2000 || d.Len != 512 || d.Flags != VirtqDescFNext || d.Next != 6 {
		t.Errorf("desc 5: got addr=%x len=%d flags=%d next=%d", d.Addr, d.Len, d.Flags, d.Next)
	}
}

func TestQueueReadChain(t *testing.T) {
	q, mem := setupTestQueue(t, 16)

	// Chain: 2 -> 3 -> 4 (no next on 4).
	writeDesc(mem, q.DescTableAddr, 2, 0x1000, 100, VirtqDescFNext, 3)
	writeDesc(mem, q.DescTableAddr, 3, 0x2000, 200, VirtqDescFNext|VirtqDescFWrite, 4)
	writeDesc(mem, q.DescTableAddr, 4, 0x3000, 300, VirtqDescFWrite, 0)

	chain := q.ReadChain(2)
	if len(chain) != 3 {
		t.Fatalf("expected 3 descriptors in chain, got %d", len(chain))
	}

	if chain[0].Addr != 0x1000 || chain[0].Len != 100 {
		t.Errorf("chain[0]: addr=%x len=%d", chain[0].Addr, chain[0].Len)
	}
	if chain[1].Addr != 0x2000 || chain[1].Flags&VirtqDescFWrite == 0 {
		t.Errorf("chain[1]: addr=%x flags=%d", chain[1].Addr, chain[1].Flags)
	}
	if chain[2].Addr != 0x3000 {
		t.Errorf("chain[2]: addr=%x", chain[2].Addr)
	}
}

func TestQueueReadWriteBuffer(t *testing.T) {
	q, mem := setupTestQueue(t, 16)

	// Write some data to guest memory.
	data := []byte("hello world")
	gpa := uint64(len(mem) - 100) // Near the end.
	copy(mem[gpa:], data)

	// Read it back.
	got := q.ReadBuffer(gpa, uint32(len(data)))
	if string(got) != "hello world" {
		t.Errorf("ReadBuffer: got %q", got)
	}

	// Write new data.
	q.WriteBuffer(gpa, []byte("REPLACED"))
	got = q.ReadBuffer(gpa, 8)
	if string(got) != "REPLACED" {
		t.Errorf("WriteBuffer: got %q", got)
	}
}

func TestQueuePutUsed(t *testing.T) {
	q, mem := setupTestQueue(t, 16)

	q.PutUsed(5, 1024)
	q.PutUsed(8, 2048)

	// Check used index incremented to 2.
	usedIdx := binary.LittleEndian.Uint16(mem[q.UsedAddr+2:])
	if usedIdx != 2 {
		t.Errorf("used idx: got %d, want 2", usedIdx)
	}

	// Check first used entry: id=5, len=1024.
	entry0 := q.UsedAddr + 4
	id0 := binary.LittleEndian.Uint32(mem[entry0:])
	len0 := binary.LittleEndian.Uint32(mem[entry0+4:])
	if id0 != 5 || len0 != 1024 {
		t.Errorf("used[0]: id=%d len=%d, want id=5 len=1024", id0, len0)
	}

	// Check second used entry: id=8, len=2048.
	entry1 := q.UsedAddr + 4 + 8
	id1 := binary.LittleEndian.Uint32(mem[entry1:])
	len1 := binary.LittleEndian.Uint32(mem[entry1+4:])
	if id1 != 8 || len1 != 2048 {
		t.Errorf("used[1]: id=%d len=%d, want id=8 len=2048", id1, len1)
	}
}

func TestQueueSizeCalculations(t *testing.T) {
	if DescTableSize(256) != 256*16 {
		t.Error("DescTableSize(256)")
	}
	if AvailRingSize(256) != 2+2+256*2+2 {
		t.Error("AvailRingSize(256)")
	}
	if UsedRingSize(256) != 2+2+256*8+2 {
		t.Error("UsedRingSize(256)")
	}
}

func TestAlign(t *testing.T) {
	cases := []struct{ addr, align, want uint64 }{
		{0, 4, 0},
		{1, 4, 4},
		{4, 4, 4},
		{5, 8, 8},
		{0x1000, 0x1000, 0x1000},
		{0x1001, 0x1000, 0x2000},
	}
	for _, c := range cases {
		got := Align(c.addr, c.align)
		if got != c.want {
			t.Errorf("Align(%x, %x) = %x, want %x", c.addr, c.align, got, c.want)
		}
	}
}

func TestQueueSingleDescChain(t *testing.T) {
	q, mem := setupTestQueue(t, 16)

	// Single descriptor, no next.
	writeDesc(mem, q.DescTableAddr, 0, 0x5000, 64, 0, 0)

	chain := q.ReadChain(0)
	if len(chain) != 1 {
		t.Fatalf("expected 1 descriptor, got %d", len(chain))
	}
	if chain[0].Addr != 0x5000 || chain[0].Len != 64 {
		t.Errorf("chain[0]: addr=%x len=%d", chain[0].Addr, chain[0].Len)
	}
}

func TestQueueReadBufferOutOfBounds(t *testing.T) {
	q, _ := setupTestQueue(t, 16)

	// Read beyond memory.
	got := q.ReadBuffer(uint64(len(q.mem)+100), 10)
	if got != nil {
		t.Error("expected nil for out-of-bounds read")
	}
}
