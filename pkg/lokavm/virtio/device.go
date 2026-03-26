package virtio

// DeviceID identifies the type of virtio device (virtio spec 5.x).
type DeviceID uint16

const (
	DeviceIDNet     DeviceID = 1  // virtio-net
	DeviceIDBlock   DeviceID = 2  // virtio-blk
	DeviceIDConsole DeviceID = 3  // virtio-console
	DeviceIDBalloon DeviceID = 5  // virtio-balloon
	DeviceIDFS      DeviceID = 26 // virtio-fs
	DeviceIDVsock   DeviceID = 19 // virtio-vsock
)

// Device is the interface that all virtio devices implement.
type Device interface {
	// DeviceID returns the virtio device type identifier.
	DeviceID() DeviceID

	// NumQueues returns the number of virtqueues this device uses.
	NumQueues() int

	// Features returns the device feature bits (negotiated with guest).
	Features() uint64

	// ConfigSpace returns the device-specific configuration space bytes.
	// The guest reads this to discover device parameters.
	ConfigSpace() []byte

	// HandleQueue is called when the guest notifies a queue has new buffers.
	// queueIdx identifies which virtqueue (0-based).
	HandleQueue(queueIdx int, queue *Queue)

	// Reset resets the device to initial state.
	Reset()
}

// MMIO transport constants.
// These are the register offsets for virtio-mmio (virtio spec 4.2).
// We use MMIO rather than PCI for simplicity — the guest kernel supports both.
const (
	// MMIO register offsets.
	MMIOMagicValue        = 0x000 // 0x74726976 ("virt")
	MMIOVersion           = 0x004 // 2 for virtio 1.0+
	MMIODeviceID          = 0x008
	MMIOVendorID          = 0x00c
	MMIODeviceFeatures    = 0x010
	MMIODeviceFeaturesSel = 0x014
	MMIODriverFeatures    = 0x020
	MMIODriverFeaturesSel = 0x024
	MMIOQueueSel          = 0x030
	MMIOQueueNumMax       = 0x034
	MMIOQueueNum          = 0x038
	MMIOQueueReady        = 0x044
	MMIOQueueNotify       = 0x050
	MMIOInterruptStatus   = 0x060
	MMIOInterruptACK      = 0x064
	MMIOStatus            = 0x070
	MMIOQueueDescLow      = 0x080
	MMIOQueueDescHigh     = 0x084
	MMIOQueueDriverLow    = 0x090
	MMIOQueueDriverHigh   = 0x094
	MMIOQueueDeviceLow    = 0x0a0
	MMIOQueueDeviceHigh   = 0x0a4
	MMIOConfigGeneration  = 0x0fc
	MMIOConfig            = 0x100 // Device-specific config starts here.

	// Magic value.
	MMIOMagic = 0x74726976

	// Virtio MMIO version (we implement v2 = virtio 1.0+).
	MMIOVer = 2

	// Default vendor ID.
	VendorIDLoka = 0x4C4F4B41 // "LOKA" in ASCII
)

// MMIODevice wraps a virtio Device with MMIO transport state.
type MMIODevice struct {
	Dev Device

	// MMIO transport state.
	Status           uint32
	DriverFeatures   uint64
	FeaturesSel      uint32
	DriverFeaturesSel uint32
	QueueSel         uint32
	InterruptStatus  uint32

	// Per-queue state.
	Queues []*MMIOQueue
}

// MMIOQueue holds per-queue MMIO configuration state.
type MMIOQueue struct {
	Num      uint32
	Ready    uint32
	DescAddr uint64
	AvailAddr uint64
	UsedAddr uint64
	Queue    *Queue // Initialized when Ready=1.
}

// NewMMIODevice wraps a Device with MMIO transport.
func NewMMIODevice(dev Device) *MMIODevice {
	numQ := dev.NumQueues()
	queues := make([]*MMIOQueue, numQ)
	for i := range queues {
		queues[i] = &MMIOQueue{Num: 256} // Default queue size.
	}
	return &MMIODevice{
		Dev:    dev,
		Queues: queues,
	}
}

// DefaultQueueSize is the default number of descriptors per queue.
const DefaultQueueSize = 256
