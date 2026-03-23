package loka

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// WorkerToken is a registration token for self-managed workers.
type WorkerToken struct {
	ID        string
	Name      string
	Token     string // The secret token value.
	ExpiresAt time.Time
	Used      bool
	WorkerID  string // Filled when a worker registers with this token.
	CreatedAt time.Time
}

// IsExpired returns true if the token has expired.
func (t *WorkerToken) IsExpired() bool {
	return !t.ExpiresAt.IsZero() && time.Now().After(t.ExpiresAt)
}

// IsValid returns true if the token can be used for registration.
func (t *WorkerToken) IsValid() bool {
	return !t.Used && !t.IsExpired()
}

// GenerateToken creates a cryptographically random token string.
func GenerateToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "loka_" + hex.EncodeToString(b)
}
