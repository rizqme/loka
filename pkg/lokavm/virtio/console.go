package virtio

import (
	"io"
	"sync"
)

// Console implements a virtio-console device (virtio spec 5.3).
// It provides a serial console for kernel output and input.
//
// Queue 0: receiveq (host→guest, device-writable buffers)
// Queue 1: transmitq (guest→host, device-readable buffers)
type Console struct {
	// Output receives bytes written by the guest (kernel console output).
	Output io.Writer

	// Input provides bytes to send to the guest (not used for now).
	Input io.Reader

	mu sync.Mutex
}

// NewConsole creates a new virtio-console device.
func NewConsole(output io.Writer) *Console {
	return &Console{Output: output}
}

func (c *Console) DeviceID() DeviceID { return DeviceIDConsole }
func (c *Console) NumQueues() int     { return 2 }
func (c *Console) Features() uint64   { return 0 }
func (c *Console) ConfigSpace() []byte { return nil }
func (c *Console) Reset()             {}

func (c *Console) HandleQueue(queueIdx int, queue *Queue) {
	switch queueIdx {
	case 0:
		// receiveq: host→guest. We don't send input for now.
	case 1:
		// transmitq: guest→host. Read guest output.
		c.handleTransmit(queue)
	}
}

func (c *Console) handleTransmit(queue *Queue) {
	for {
		head, ok := queue.NextAvail()
		if !ok {
			return
		}

		chain := queue.ReadChain(head)
		for _, desc := range chain {
			if desc.Flags&VirtqDescFWrite != 0 {
				continue // Skip device-writable descriptors in transmit queue.
			}
			data := queue.ReadBuffer(desc.Addr, desc.Len)
			if len(data) > 0 && c.Output != nil {
				c.mu.Lock()
				c.Output.Write(data)
				c.mu.Unlock()
			}
		}

		queue.PutUsed(head, 0)
	}
}
