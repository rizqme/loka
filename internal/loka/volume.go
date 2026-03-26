package loka

import "time"

// Volume is the unified mount type for both sessions and services.
// Two types:
//   - "network": NFS-backed, for realtime cross-worker sharing (fast, limited by worker disk)
//   - "object":  Object storage-backed (S3/GCS), for cheap scalable storage
//
// Both are served to guest VMs via virtiofs for transparent POSIX access.
type Volume struct {
	Path        string `json:"path"`
	Type        string `json:"type,omitempty"`         // "network" or "object" (default: auto-detect)
	Provider    string `json:"provider"`               // "s3", "gcs", "azure", "volume", "nfs", "local"
	Name        string `json:"name,omitempty"`          // Named volume (provider=volume)
	Bucket      string `json:"bucket,omitempty"`
	Prefix      string `json:"prefix,omitempty"`
	Region      string `json:"region,omitempty"`
	Credentials string `json:"credentials,omitempty"`   // ${secret.name}
	Access      string `json:"access,omitempty"`        // "readonly" or "readwrite" (default)

	// NFS (network volumes).
	NFSServer string `json:"nfs_server,omitempty"`     // Worker address or NFS server.
	NFSPath   string `json:"nfs_path,omitempty"`       // Export path on NFS server.

	// Host directory (local sharing, same host only).
	HostPath string `json:"host_path,omitempty"`       // Direct host dir path.
}

// VolumeRecord is a persistent record for a named volume tracked in the store.
// Multiple sessions/services can mount the same named volume.
type VolumeRecord struct {
	Name       string    `json:"name"`
	Type       string    `json:"type"`                // "network" or "object"
	Provider   string    `json:"provider"`
	MountCount int       `json:"mount_count"`         // Number of VMs currently mounting this volume.
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// IsReadOnly returns true if the volume is read-only.
func (v Volume) IsReadOnly() bool {
	return v.Access == "readonly"
}

// EffectiveMode returns the mount mode for the VMM:
//   - "virtiofs": shared via virtiofs (host dir, NFS mount, or object cache dir)
//   - "block":    attached as ext4 block device (legacy, readonly)
//   - "fuse":     FUSE-over-vsock (legacy, readwrite with sync)
func (v Volume) EffectiveMode() string {
	// New-style volumes always use virtiofs.
	if v.HostPath != "" || v.Type == "network" || v.Type == "object" {
		return "virtiofs"
	}
	// Legacy fallback.
	if v.IsReadOnly() {
		return "block"
	}
	return "fuse"
}

// EffectiveType returns the volume type, auto-detecting if not specified.
func (v Volume) EffectiveType() string {
	if v.Type != "" {
		return v.Type
	}
	if v.NFSServer != "" || v.Provider == "nfs" {
		return "network"
	}
	if v.HostPath != "" || v.Provider == "local" {
		return "network"
	}
	if v.Bucket != "" || v.Provider == "s3" || v.Provider == "gcs" || v.Provider == "azure" {
		return "object"
	}
	return "network" // Default to network for named volumes.
}
