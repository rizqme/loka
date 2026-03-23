package loka

import "time"

// CheckpointType distinguishes between light and full checkpoints.
type CheckpointType string

const (
	// CheckpointLight captures filesystem overlay only (fast, <100ms with reflinks).
	CheckpointLight CheckpointType = "light"
	// CheckpointFull captures VM memory state + filesystem (slower, exact state restore).
	CheckpointFull CheckpointType = "full"
)

// CheckpointStatus represents the state of a checkpoint operation.
type CheckpointStatus string

const (
	CheckpointStatusCreating CheckpointStatus = "creating"
	CheckpointStatusReady    CheckpointStatus = "ready"
	CheckpointStatusFailed   CheckpointStatus = "failed"
)

// Checkpoint represents a single checkpoint node in the DAG.
type Checkpoint struct {
	ID        string
	SessionID string
	ParentID  string // Empty for root checkpoint.
	Type      CheckpointType
	Status    CheckpointStatus
	Label     string
	// Object store paths for the checkpoint artifacts.
	OverlayPath  string // Path to overlay.tar.zst in object store.
	VMStatePath  string // Path to vmstate.snap (full checkpoints only).
	MetadataPath string // Path to metadata.json.
	CreatedAt    time.Time
}

// CheckpointDAG represents the full DAG of checkpoints for a session.
type CheckpointDAG struct {
	SessionID   string
	Root        string // ID of root checkpoint.
	Current     string // ID of the currently active checkpoint.
	Checkpoints map[string]*Checkpoint
}

// NewCheckpointDAG creates a new empty DAG for a session.
func NewCheckpointDAG(sessionID string) *CheckpointDAG {
	return &CheckpointDAG{
		SessionID:   sessionID,
		Checkpoints: make(map[string]*Checkpoint),
	}
}

// Add inserts a checkpoint into the DAG.
func (d *CheckpointDAG) Add(cp *Checkpoint) {
	d.Checkpoints[cp.ID] = cp
	if cp.ParentID == "" {
		d.Root = cp.ID
	}
}

// Children returns the direct children of a checkpoint.
func (d *CheckpointDAG) Children(id string) []*Checkpoint {
	var children []*Checkpoint
	for _, cp := range d.Checkpoints {
		if cp.ParentID == id {
			children = append(children, cp)
		}
	}
	return children
}

// PathTo returns the chain of checkpoints from root to the given checkpoint.
func (d *CheckpointDAG) PathTo(id string) []*Checkpoint {
	var path []*Checkpoint
	current := id
	for current != "" {
		cp, ok := d.Checkpoints[current]
		if !ok {
			break
		}
		path = append([]*Checkpoint{cp}, path...)
		current = cp.ParentID
	}
	return path
}
