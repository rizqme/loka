package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rizqme/loka/internal/loka"
	"github.com/rizqme/loka/internal/store"
)

func setupTestDB(t *testing.T) *Store {
	t.Helper()
	s, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSessionCRUD(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	sess := &loka.Session{
		ID:        uuid.New().String(),
		Name:      "test-session",
		Status:    loka.SessionStatusRunning,
		Mode:      loka.ModeExplore,
		Labels:    map[string]string{"env": "test"},
		VCPUs:     2,
		MemoryMB:  1024,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	// Create.
	if err := s.Sessions().Create(ctx, sess); err != nil {
		t.Fatal(err)
	}

	// Get.
	got, err := s.Sessions().Get(ctx, sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "test-session" {
		t.Errorf("name = %s, want test-session", got.Name)
	}

	// Update.
	got.Status = loka.SessionStatusPaused
	if err := s.Sessions().Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, _ := s.Sessions().Get(ctx, sess.ID)
	if got2.Status != loka.SessionStatusPaused {
		t.Errorf("status = %s, want paused", got2.Status)
	}

	// List.
	list, err := s.Sessions().List(ctx, store.SessionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Errorf("list = %d, want 1", len(list))
	}

	// Delete.
	if err := s.Sessions().Delete(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.Sessions().Get(ctx, sess.ID)
	if err == nil {
		t.Error("should get error after delete")
	}
}
