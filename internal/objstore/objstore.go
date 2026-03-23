package objstore

import (
	"context"
	"io"
	"time"
)

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
	ETag         string
}

// ObjectStore provides storage for snapshots, rootfs images, and checkpoint data.
// Implementations: S3-compatible (AWS S3, MinIO), GCS, local filesystem.
type ObjectStore interface {
	// Put uploads an object. If size is -1, the implementation reads until EOF.
	Put(ctx context.Context, bucket, key string, reader io.Reader, size int64) error

	// Get downloads an object.
	Get(ctx context.Context, bucket, key string) (io.ReadCloser, error)

	// Delete removes an object.
	Delete(ctx context.Context, bucket, key string) error

	// Exists checks if an object exists.
	Exists(ctx context.Context, bucket, key string) (bool, error)

	// GetPresignedURL returns a time-limited URL for direct download.
	// Workers use presigned URLs for efficient direct upload/download.
	GetPresignedURL(ctx context.Context, bucket, key string, expiry time.Duration) (string, error)

	// List lists objects under a prefix.
	List(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error)
}
