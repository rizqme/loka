package loka

import (
	"testing"
	"time"
)

func TestCheckpointDAG(t *testing.T) {
	dag := NewCheckpointDAG("session-1")

	// Add root.
	cp1 := &Checkpoint{ID: "cp-1", SessionID: "session-1", ParentID: "", CreatedAt: time.Now()}
	dag.Add(cp1)

	if dag.Root != "cp-1" {
		t.Errorf("root = %s, want cp-1", dag.Root)
	}

	// Add child.
	cp2 := &Checkpoint{ID: "cp-2", SessionID: "session-1", ParentID: "cp-1", CreatedAt: time.Now()}
	dag.Add(cp2)

	// Add another child (branch).
	cp3 := &Checkpoint{ID: "cp-3", SessionID: "session-1", ParentID: "cp-1", CreatedAt: time.Now()}
	dag.Add(cp3)

	// Add grandchild.
	cp4 := &Checkpoint{ID: "cp-4", SessionID: "session-1", ParentID: "cp-2", CreatedAt: time.Now()}
	dag.Add(cp4)

	// Test Children.
	children := dag.Children("cp-1")
	if len(children) != 2 {
		t.Errorf("cp-1 children = %d, want 2", len(children))
	}

	children2 := dag.Children("cp-2")
	if len(children2) != 1 {
		t.Errorf("cp-2 children = %d, want 1", len(children2))
	}

	children3 := dag.Children("cp-3")
	if len(children3) != 0 {
		t.Errorf("cp-3 children = %d, want 0", len(children3))
	}

	// Test PathTo.
	path := dag.PathTo("cp-4")
	if len(path) != 3 {
		t.Fatalf("path to cp-4 = %d nodes, want 3", len(path))
	}
	if path[0].ID != "cp-1" || path[1].ID != "cp-2" || path[2].ID != "cp-4" {
		t.Errorf("path = [%s, %s, %s], want [cp-1, cp-2, cp-4]", path[0].ID, path[1].ID, path[2].ID)
	}

	// Path to root.
	rootPath := dag.PathTo("cp-1")
	if len(rootPath) != 1 {
		t.Errorf("path to root = %d, want 1", len(rootPath))
	}
}

func TestCheckpointDAGEmpty(t *testing.T) {
	dag := NewCheckpointDAG("session-2")

	if dag.Root != "" {
		t.Error("empty DAG should have no root")
	}

	children := dag.Children("nonexistent")
	if len(children) != 0 {
		t.Error("children of nonexistent should be empty")
	}

	path := dag.PathTo("nonexistent")
	if len(path) != 0 {
		t.Error("path to nonexistent should be empty")
	}
}
