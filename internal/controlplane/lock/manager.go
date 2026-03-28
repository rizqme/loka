// Package lock provides a distributed file lock manager for LOKA volumes.
// Workers acquire locks via the control plane API before writing to shared volumes.
// Locks have TTLs to prevent deadlocks from crashed workers.
package lock

import (
	"fmt"
	"sync"
	"time"
)

// Manager provides distributed file locking for volumes.
// Runs in the control plane. Workers acquire/release locks via HTTP API.
type Manager struct {
	mu    sync.Mutex
	locks map[string]*FileLock // key: "volume:path"

	// Background reaper for expired locks.
	done chan struct{}
}

// FileLock represents an active file lock.
type FileLock struct {
	Volume     string    `json:"volume"`
	Path       string    `json:"path"`
	WorkerID   string    `json:"worker_id"`
	Exclusive  bool      `json:"exclusive"`
	AcquiredAt time.Time `json:"acquired_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// NewManager creates a new lock manager and starts the TTL reaper.
func NewManager() *Manager {
	m := &Manager{
		locks: make(map[string]*FileLock),
		done:  make(chan struct{}),
	}
	go m.reapExpired()
	return m
}

// Stop shuts down the lock manager.
func (m *Manager) Stop() {
	close(m.done)
}

// Acquire attempts to acquire a lock on a file in a volume.
// Returns error if the file is already locked by another worker (exclusive).
func (m *Manager) Acquire(volume, path, workerID string, exclusive bool, ttl time.Duration) error {
	if ttl <= 0 {
		ttl = 30 * time.Second // Default TTL.
	}
	if ttl > 10*time.Minute {
		ttl = 10 * time.Minute // Max TTL.
	}

	key := lockKey(volume, path)

	m.mu.Lock()
	defer m.mu.Unlock()

	if existing, ok := m.locks[key]; ok {
		// Check if expired.
		if time.Now().After(existing.ExpiresAt) {
			delete(m.locks, key)
		} else if existing.WorkerID != workerID {
			return fmt.Errorf("file %s in volume %s is locked by worker %s (expires %s)",
				path, volume, existing.WorkerID, existing.ExpiresAt.Format(time.RFC3339))
		} else {
			// Same worker re-acquiring — extend TTL.
			existing.ExpiresAt = time.Now().Add(ttl)
			return nil
		}
	}

	m.locks[key] = &FileLock{
		Volume:     volume,
		Path:       path,
		WorkerID:   workerID,
		Exclusive:  exclusive,
		AcquiredAt: time.Now(),
		ExpiresAt:  time.Now().Add(ttl),
	}
	return nil
}

// Release releases a lock held by a worker.
func (m *Manager) Release(volume, path, workerID string) error {
	key := lockKey(volume, path)

	m.mu.Lock()
	defer m.mu.Unlock()

	lock, ok := m.locks[key]
	if !ok {
		return nil // Already released.
	}
	if lock.WorkerID != workerID {
		return fmt.Errorf("lock on %s:%s is held by worker %s, not %s",
			volume, path, lock.WorkerID, workerID)
	}

	delete(m.locks, key)
	return nil
}

// ReleaseAll releases all locks held by a worker (e.g., on disconnect).
func (m *Manager) ReleaseAll(workerID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	count := 0
	for key, lock := range m.locks {
		if lock.WorkerID == workerID {
			delete(m.locks, key)
			count++
		}
	}
	return count
}

// IsLocked checks if a file is locked.
func (m *Manager) IsLocked(volume, path string) (bool, *FileLock) {
	key := lockKey(volume, path)

	m.mu.Lock()
	defer m.mu.Unlock()

	lock, ok := m.locks[key]
	if !ok {
		return false, nil
	}
	if time.Now().After(lock.ExpiresAt) {
		delete(m.locks, key)
		return false, nil
	}
	return true, lock
}

// ListLocks returns all active locks for a volume.
func (m *Manager) ListLocks(volume string) []*FileLock {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []*FileLock
	now := time.Now()
	for key, lock := range m.locks {
		if now.After(lock.ExpiresAt) {
			delete(m.locks, key)
			continue
		}
		if lock.Volume == volume {
			result = append(result, lock)
		}
	}
	return result
}

// reapExpired periodically removes expired locks.
func (m *Manager) reapExpired() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-m.done:
			return
		case <-ticker.C:
			m.mu.Lock()
			now := time.Now()
			for key, lock := range m.locks {
				if now.After(lock.ExpiresAt) {
					delete(m.locks, key)
				}
			}
			m.mu.Unlock()
		}
	}
}

func lockKey(volume, path string) string {
	return volume + ":" + path
}
