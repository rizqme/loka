package loka

import "testing"

func TestCanTransitionModeTo(t *testing.T) {
	tests := []struct {
		from, to ExecMode
		expect   bool
	}{
		{ModeExplore, ModeExecute, true},
		{ModeExplore, ModeAsk, true},
		{ModeExplore, ModeExplore, true},
		{ModeExecute, ModeExplore, true},
		{ModeExecute, ModeAsk, true},
		{ModeExecute, ModeExecute, true},
		{ModeAsk, ModeExplore, true},
		{ModeAsk, ModeExecute, true},
		{ModeAsk, ModeAsk, true},
	}
	for _, tt := range tests {
		got := CanTransitionModeTo(tt.from, tt.to)
		if got != tt.expect {
			t.Errorf("CanTransitionModeTo(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.expect)
		}
	}
}

func TestModePolicies(t *testing.T) {
	// Explore should be read-only.
	p := ModePolicies[ModeExplore]
	if p.WorkspaceWrite {
		t.Error("explore should not allow workspace writes")
	}
	if p.NetworkAccess {
		t.Error("explore should not allow network access")
	}

	// Execute should have full access.
	p2 := ModePolicies[ModeExecute]
	if !p2.WorkspaceWrite {
		t.Error("execute should allow workspace writes")
	}
	if p2.CredentialTier != CredentialFull {
		t.Error("execute should have full credentials")
	}

	// Ask should have scoped credentials.
	p3 := ModePolicies[ModeAsk]
	if p3.CredentialTier != CredentialScoped {
		t.Error("ask should have scoped credentials")
	}
}
