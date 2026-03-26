package virtio

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestConsoleDeviceID(t *testing.T) {
	c := NewConsole(os.Stdout)
	if c.DeviceID() != DeviceIDConsole {
		t.Errorf("DeviceID=%d, want %d", c.DeviceID(), DeviceIDConsole)
	}
	if c.NumQueues() != 2 {
		t.Errorf("NumQueues=%d, want 2", c.NumQueues())
	}
}

func TestConsoleTransmit(t *testing.T) {
	var buf bytes.Buffer
	c := NewConsole(&buf)

	q, mem := setupTestQueue(t, 16)

	// Put "hello" in guest memory at offset 4096.
	data := []byte("hello from guest")
	gpa := uint64(4096)
	copy(mem[gpa:], data)

	// Set up descriptor chain: one readable descriptor.
	writeDesc(mem, q.DescTableAddr, 0, gpa, uint32(len(data)), 0, 0)
	pushAvail(mem, q.AvailAddr, q.Size, 0, 0)

	c.HandleQueue(1, q) // transmitq = queue index 1.

	if buf.String() != "hello from guest" {
		t.Errorf("console output = %q", buf.String())
	}
}

func TestBlockDeviceConfigSpace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "disk.img")

	// Create a 1MB disk image.
	f, _ := os.Create(path)
	f.Truncate(1024 * 1024) // 1MB
	f.Close()

	blk, err := NewBlock(path, false)
	if err != nil {
		t.Fatalf("NewBlock: %v", err)
	}
	defer blk.Close()

	if blk.DeviceID() != DeviceIDBlock {
		t.Errorf("DeviceID=%d, want %d", blk.DeviceID(), DeviceIDBlock)
	}
	if blk.NumQueues() != 1 {
		t.Errorf("NumQueues=%d, want 1", blk.NumQueues())
	}

	config := blk.ConfigSpace()
	sectors := binary.LittleEndian.Uint64(config[0:8])
	if sectors != 2048 { // 1MB / 512
		t.Errorf("sectors=%d, want 2048", sectors)
	}

	blkSize := binary.LittleEndian.Uint32(config[20:24])
	if blkSize != 512 {
		t.Errorf("blk_size=%d, want 512", blkSize)
	}
}

func TestBlockReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "disk.img")
	os.WriteFile(path, make([]byte, 4096), 0o644)

	blk, err := NewBlock(path, true)
	if err != nil {
		t.Fatalf("NewBlock: %v", err)
	}
	defer blk.Close()

	features := blk.Features()
	if features&(1<<5) == 0 { // VIRTIO_BLK_F_RO
		t.Error("expected VIRTIO_BLK_F_RO flag for read-only disk")
	}
}

func TestBalloonSetTarget(t *testing.T) {
	b := NewBalloon()

	if b.DeviceID() != DeviceIDBalloon {
		t.Errorf("DeviceID=%d, want %d", b.DeviceID(), DeviceIDBalloon)
	}
	if b.NumQueues() != 2 {
		t.Errorf("NumQueues=%d, want 2", b.NumQueues())
	}

	b.SetTarget(100) // 100MB

	config := b.ConfigSpace()
	targetPages := binary.LittleEndian.Uint32(config[0:4])
	if targetPages != 100*256 { // 256 pages per MB
		t.Errorf("target pages=%d, want %d", targetPages, 100*256)
	}
}

func TestBalloonActualMB(t *testing.T) {
	b := NewBalloon()

	if b.ActualMB() != 0 {
		t.Errorf("initial ActualMB=%d, want 0", b.ActualMB())
	}
}

func TestVsockDeviceID(t *testing.T) {
	v := NewVsock(3)
	if v.DeviceID() != DeviceIDVsock {
		t.Errorf("DeviceID=%d, want %d", v.DeviceID(), DeviceIDVsock)
	}
	if v.NumQueues() != 3 {
		t.Errorf("NumQueues=%d, want 3", v.NumQueues())
	}

	config := v.ConfigSpace()
	guestCID := binary.LittleEndian.Uint64(config[0:8])
	if guestCID != 3 {
		t.Errorf("guest CID=%d, want 3", guestCID)
	}
}

func TestVsockReset(t *testing.T) {
	v := NewVsock(3)
	v.Reset() // Should not panic.
}

func TestNetDeviceConfig(t *testing.T) {
	mac := []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	n := NewNet(0, "tap0", mac)

	if n.DeviceID() != DeviceIDNet {
		t.Errorf("DeviceID=%d, want %d", n.DeviceID(), DeviceIDNet)
	}
	if n.NumQueues() != 2 {
		t.Errorf("NumQueues=%d, want 2", n.NumQueues())
	}

	config := n.ConfigSpace()
	if !bytes.Equal(config[0:6], mac) {
		t.Errorf("MAC=%x, want %x", config[0:6], mac)
	}

	// Link status should be up (1).
	status := binary.LittleEndian.Uint16(config[6:8])
	if status != 1 {
		t.Errorf("status=%d, want 1", status)
	}
}

func TestFSDeviceConfig(t *testing.T) {
	backend := NewDirectBackend(t.TempDir(), false)
	fs := NewFS("myfs", backend)

	if fs.DeviceID() != DeviceIDFS {
		t.Errorf("DeviceID=%d, want %d", fs.DeviceID(), DeviceIDFS)
	}
	if fs.NumQueues() != 2 {
		t.Errorf("NumQueues=%d, want 2", fs.NumQueues())
	}

	config := fs.ConfigSpace()
	tag := string(bytes.TrimRight(config[0:36], "\x00"))
	if tag != "myfs" {
		t.Errorf("tag=%q, want 'myfs'", tag)
	}

	numQueues := binary.LittleEndian.Uint32(config[36:40])
	if numQueues != 1 {
		t.Errorf("num_request_queues=%d, want 1", numQueues)
	}
}

func TestMMIODeviceCreate(t *testing.T) {
	c := NewConsole(os.Stdout)
	mmio := NewMMIODevice(c)

	if mmio.Dev.DeviceID() != DeviceIDConsole {
		t.Error("expected console device")
	}
	if len(mmio.Queues) != 2 {
		t.Errorf("expected 2 queues, got %d", len(mmio.Queues))
	}
	if mmio.Queues[0].Num != 256 {
		t.Errorf("default queue size=%d, want 256", mmio.Queues[0].Num)
	}
}
