package loka

import "time"

// Artifact represents a file that was added, modified, or deleted in a session
// relative to its base image.
type Artifact struct {
	ID           string    `json:"id"`
	SessionID    string    `json:"session_id"`
	CheckpointID string    `json:"checkpoint_id,omitempty"`
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	Hash         string    `json:"hash"`
	Type         string    `json:"type"` // "added", "modified", "deleted"
	IsDir        bool      `json:"is_dir,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}
