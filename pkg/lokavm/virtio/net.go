package virtio

import (
	"encoding/binary"
	"net"
	"sync"
)

// Net implements a virtio-net device (virtio spec 5.1).
// Backed by a TAP device on the host.
//
// Queue 0: receiveq (host→guest)
// Queue 1: transmitq (guest→host)
type Net struct {
	tap    *TAPDevice
	mac    net.HardwareAddr
	mu     sync.Mutex
	stopCh chan struct{}
}

// TAPDevice wraps a TAP file descriptor for network I/O.
type TAPDevice struct {
	fd   int
	name string
}

// NewNet creates a new virtio-net device with the given MAC address.
// The TAP device must be created and configured externally.
func NewNet(tapFD int, tapName string, mac net.HardwareAddr) *Net {
	return &Net{
		tap:    &TAPDevice{fd: tapFD, name: tapName},
		mac:    mac,
		stopCh: make(chan struct{}),
	}
}

func (n *Net) DeviceID() DeviceID { return DeviceIDNet }
func (n *Net) NumQueues() int     { return 2 }

func (n *Net) Features() uint64 {
	var features uint64
	features |= 1 << 5  // VIRTIO_NET_F_MAC
	features |= 1 << 32 // VIRTIO_F_VERSION_1
	return features
}

func (n *Net) ConfigSpace() []byte {
	// virtio_net_config: mac[6] + status(u16) + max_virtqueue_pairs(u16) + ...
	config := make([]byte, 24)
	if len(n.mac) >= 6 {
		copy(config[0:6], n.mac)
	}
	// Status = 1 (link up).
	binary.LittleEndian.PutUint16(config[6:8], 1)
	return config
}

func (n *Net) Reset() {
	select {
	case <-n.stopCh:
	default:
		close(n.stopCh)
	}
	n.stopCh = make(chan struct{})
}

func (n *Net) HandleQueue(queueIdx int, queue *Queue) {
	switch queueIdx {
	case 0:
		// receiveq: host→guest. Handled by the RX goroutine.
	case 1:
		// transmitq: guest→host.
		n.handleTransmit(queue)
	}
}

// handleTransmit processes packets from the guest and writes them to the TAP device.
func (n *Net) handleTransmit(queue *Queue) {
	for {
		head, ok := queue.NextAvail()
		if !ok {
			return
		}

		chain := queue.ReadChain(head)

		// Collect all readable buffers into one packet.
		// First buffer is a virtio_net_hdr (12 bytes), rest is the frame.
		var packet []byte
		for _, desc := range chain {
			if desc.Flags&VirtqDescFWrite != 0 {
				continue
			}
			data := queue.ReadBuffer(desc.Addr, desc.Len)
			packet = append(packet, data...)
		}

		// Strip the virtio_net_hdr (12 bytes minimum) and write the raw frame.
		if len(packet) > 12 {
			n.mu.Lock()
			n.writeTAP(packet[12:])
			n.mu.Unlock()
		}

		queue.PutUsed(head, 0)
	}
}

// StartRX starts the receive goroutine that reads from TAP and injects into the guest.
func (n *Net) StartRX(queue *Queue, notify func()) {
	go func() {
		buf := make([]byte, 65536)
		for {
			select {
			case <-n.stopCh:
				return
			default:
			}

			nread := n.readTAP(buf)
			if nread <= 0 {
				continue
			}

			// Find a guest buffer to put the packet into.
			head, ok := queue.NextAvail()
			if !ok {
				continue // Guest hasn't posted buffers; drop packet.
			}

			chain := queue.ReadChain(head)

			// Write virtio_net_hdr (12 bytes of zeros) + frame data into writable descriptors.
			hdr := make([]byte, 12) // Zero-filled virtio_net_hdr.
			frame := append(hdr, buf[:nread]...)

			var written uint32
			offset := 0
			for _, desc := range chain {
				if desc.Flags&VirtqDescFWrite == 0 {
					continue
				}
				toWrite := len(frame) - offset
				if toWrite <= 0 {
					break
				}
				if toWrite > int(desc.Len) {
					toWrite = int(desc.Len)
				}
				queue.WriteBuffer(desc.Addr, frame[offset:offset+toWrite])
				offset += toWrite
				written += uint32(toWrite)
			}

			queue.PutUsed(head, written)
			if notify != nil {
				notify()
			}
		}
	}()
}

// writeTAP writes a frame to the TAP device. Platform-specific implementation.
func (n *Net) writeTAP(frame []byte) {
	// This will be implemented in net_linux.go with syscall.Write.
}

// readTAP reads a frame from the TAP device. Platform-specific implementation.
func (n *Net) readTAP(buf []byte) int {
	// This will be implemented in net_linux.go with syscall.Read.
	return 0
}
