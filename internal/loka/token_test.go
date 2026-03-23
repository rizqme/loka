package loka

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateToken(t *testing.T) {
	t1 := GenerateToken()
	t2 := GenerateToken()

	if !strings.HasPrefix(t1, "loka_") {
		t.Errorf("token should start with loka_, got %s", t1[:10])
	}
	if t1 == t2 {
		t.Error("tokens should be unique")
	}
	if len(t1) < 40 {
		t.Errorf("token too short: %d chars", len(t1))
	}
}

func TestWorkerTokenIsValid(t *testing.T) {
	// Valid token.
	tok := &WorkerToken{ExpiresAt: time.Now().Add(1 * time.Hour), Used: false}
	if !tok.IsValid() {
		t.Error("fresh token should be valid")
	}

	// Expired.
	tok2 := &WorkerToken{ExpiresAt: time.Now().Add(-1 * time.Hour), Used: false}
	if tok2.IsValid() {
		t.Error("expired token should be invalid")
	}

	// Used.
	tok3 := &WorkerToken{ExpiresAt: time.Now().Add(1 * time.Hour), Used: true}
	if tok3.IsValid() {
		t.Error("used token should be invalid")
	}

	// No expiry (zero time).
	tok4 := &WorkerToken{ExpiresAt: time.Time{}, Used: false}
	if !tok4.IsValid() {
		t.Error("token with no expiry should be valid")
	}
}
