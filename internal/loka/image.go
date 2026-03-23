package loka

import "time"

// Image is a base image backed by a Docker/OCI container image.
// It is pulled from a registry, converted to an ext4 rootfs,
// and used as the read-only base layer for Firecracker microVMs.
//
// Flow:
//   1. User specifies a Docker image: "ubuntu:22.04", "python:3.12-slim"
//   2. LOKA pulls the image via Docker/containerd
//   3. Exports the filesystem to a rootfs.ext4 file
//   4. Injects the loka-supervisor binary into the rootfs
//   5. Boots the rootfs in Firecracker, lets it initialize
//   6. Creates a "warm snapshot" (memory + disk state after boot)
//   7. Future sessions restore from this snapshot (~instant startup)
//
// The rootfs is READ-ONLY. All session changes go to an overlay layer.
// Snapshots capture the overlay diff from the base image.
type Image struct {
	ID          string    // Unique image ID (hash-based).
	Reference   string    // Docker reference: "ubuntu:22.04", "ghcr.io/org/image:tag"
	Digest      string    // Image digest: "sha256:abc123..."
	RootfsPath  string    // Path to the ext4 rootfs file in object store.
	SnapshotMem string    // Path to warm snapshot memory file (optional).
	SnapshotVMState string // Path to warm snapshot VM state file (optional).
	SizeMB      int64     // Rootfs size in MB.
	Status      ImageStatus
	CreatedAt   time.Time
}

// ImageStatus tracks the state of an image.
type ImageStatus string

const (
	ImageStatusPulling    ImageStatus = "pulling"    // Downloading from registry.
	ImageStatusConverting ImageStatus = "converting"  // Converting to ext4.
	ImageStatusWarming    ImageStatus = "warming"     // Booting to create warm snapshot.
	ImageStatusReady      ImageStatus = "ready"       // Ready to use.
	ImageStatusFailed     ImageStatus = "failed"
)

// Snapshot is the diff between a session's current state and its base image.
// It captures everything that changed after the base image booted:
// installed packages, config changes, workspace files, etc.
//
// Architecture:
//   Base Image (rootfs.ext4, RO)
//   + Overlay Layer (snapshot diff, RW during session)
//   = Complete VM filesystem
//
// When creating a snapshot:
//   1. Pause the VM
//   2. Capture the overlay diff (everything written since boot)
//   3. Optionally capture VM memory state (for instant restore)
//   4. Upload overlay + memory to object store
//
// When restoring from a snapshot:
//   1. Start from the same base image
//   2. Apply the overlay diff on top
//   3. Optionally restore VM memory state
//   4. Resume — VM is in the exact same state
type Snapshot struct {
	ID          string
	Name        string
	ImageID     string    // Base image this snapshot is relative to.
	ImageRef    string    // Docker reference of the base image.
	OverlayPath string    // Object store path to overlay diff.
	MemPath     string    // Object store path to VM memory (empty for light snapshots).
	VMStatePath string    // Object store path to VM state.
	SizeMB      int64     // Overlay size.
	Status      SnapshotStatus
	Labels      map[string]string
	CreatedAt   time.Time
}

// SnapshotStatus tracks the state of a snapshot.
type SnapshotStatus string

const (
	SnapshotStatusCreating SnapshotStatus = "creating"
	SnapshotStatusReady    SnapshotStatus = "ready"
	SnapshotStatusFailed   SnapshotStatus = "failed"
)
