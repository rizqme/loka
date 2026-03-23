package loka

import "testing"

func TestSessionCanTransitionTo(t *testing.T) {
	tests := []struct {
		from   SessionStatus
		to     SessionStatus
		expect bool
	}{
		{SessionStatusCreating, SessionStatusRunning, true},
		{SessionStatusCreating, SessionStatusError, true},
		{SessionStatusCreating, SessionStatusTerminated, false},
		{SessionStatusRunning, SessionStatusPaused, true},
		{SessionStatusRunning, SessionStatusTerminating, true},
		{SessionStatusRunning, SessionStatusCreating, false},
		{SessionStatusPaused, SessionStatusRunning, true},
		{SessionStatusPaused, SessionStatusTerminating, true},
		{SessionStatusPaused, SessionStatusCreating, false},
		{SessionStatusTerminating, SessionStatusTerminated, true},
		{SessionStatusTerminating, SessionStatusRunning, false},
		{SessionStatusTerminated, SessionStatusRunning, false},
		{SessionStatusError, SessionStatusTerminating, true},
		{SessionStatusError, SessionStatusRunning, false},
	}

	for _, tt := range tests {
		s := &Session{Status: tt.from}
		got := s.CanTransitionTo(tt.to)
		if got != tt.expect {
			t.Errorf("Session(%s).CanTransitionTo(%s) = %v, want %v", tt.from, tt.to, got, tt.expect)
		}
	}
}
