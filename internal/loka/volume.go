package loka

import "time"

// Volume is the unified mount type for both sessions and services.
// Providers:
//   - "local":   Host directory shared via virtiofs (same host only).
//   - "volume":  Named persistent volume (host directory managed by control plane).
//   - "store":   Shared storage (cross-worker sync via objstore, lockable via control plane).
//   - "github":  Git repository checkout (cached by commit SHA, readonly).
//   - "s3":      S3 object storage bucket.
//   - "gcs":     Google Cloud Storage bucket.
//   - "azure":   Azure Blob Storage container.
//
// All are served to guest VMs via virtiofs for transparent POSIX access.
type Volume struct {
	Path        string `json:"path"`
	Type        string `json:"type,omitempty"`         // "network" or "object" (default: auto-detect)
	Provider    string `json:"provider"`               // "local", "volume", "store", "github", "git", "s3", "gcs", "azure"
	Name        string `json:"name,omitempty"`          // Volume/store name (provider=volume/store)
	Bucket      string `json:"bucket,omitempty"`
	Prefix      string `json:"prefix,omitempty"`
	Region      string `json:"region,omitempty"`
	Credentials string `json:"credentials,omitempty"`   // ${secret.name}
	Access      string `json:"access,omitempty"`        // "readonly" or "readwrite" (default)

	// Host directory (local sharing, same host only).
	HostPath string `json:"host_path,omitempty"`       // Direct host dir path.

	// Git repository (provider="github" or "git").
	GitRepo string `json:"git_repo,omitempty"`         // "owner/repo" or full HTTPS URL.
	GitRef  string `json:"git_ref,omitempty"`          // Branch, tag, or commit SHA (default: HEAD).
}

// VolumeRecord is a persistent record for a named volume tracked in the store.
// Multiple sessions/services can mount the same named volume.
type VolumeRecord struct {
	Name       string    `json:"name"`
	Type       string    `json:"type"`                // "network", "object", or "store"
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
//   - "virtiofs": shared via virtiofs (host dir or local volume cache)
//   - "block":    attached as ext4 block device (legacy, readonly)
//   - "fuse":     FUSE-over-vsock (legacy, readwrite with sync)
func (v Volume) EffectiveMode() string {
	switch v.Provider {
	case "github", "git", "store", "local", "volume":
		return "virtiofs"
	}
	if v.HostPath != "" || v.Type == "network" || v.Type == "object" || v.Type == "store" {
		return "virtiofs"
	}
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
	switch v.Provider {
	case "github", "git", "local", "volume":
		return "network"
	case "store":
		return "store"
	case "s3", "gcs", "azure":
		return "object"
	}
	if v.HostPath != "" {
		return "network"
	}
	if v.Bucket != "" {
		return "object"
	}
	return "network"
}
