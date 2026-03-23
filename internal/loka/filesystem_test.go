package loka

import "testing"

// ── Path Matching ───────────────────────────────────────

func TestMatchPath(t *testing.T) {
	tests := []struct {
		pattern string
		path    string
		expect  bool
	}{
		// Exact.
		{"/workspace/data.txt", "/workspace/data.txt", true},
		{"/workspace/data.txt", "/workspace/other.txt", false},

		// Any.
		{"any", "/anything/at/all", true},
		{"*", "/foo", true},

		// Directory prefix.
		{"/workspace/", "/workspace/foo.py", true},
		{"/workspace/", "/workspace/sub/deep/file.txt", true},
		{"/workspace/", "/workspace", true},
		{"/workspace/", "/other/file.txt", false},

		// Recursive glob.
		{"/workspace/**", "/workspace/foo.py", true},
		{"/workspace/**", "/workspace/sub/deep/file.txt", true},
		{"/workspace/**", "/workspace", true},
		{"/workspace/**", "/other/file.txt", false},
		{"/dev/**", "/dev/sda", true},
		{"/dev/**", "/dev/null", true},
		{"/dev/**", "/devious/path", false},

		// Standard glob.
		{"/workspace/*.py", "/workspace/foo.py", true},
		{"/workspace/*.py", "/workspace/foo.txt", false},

		// /proc patterns.
		{"/proc/*/mem", "/proc/1234/mem", true},
		{"/proc/*/mem", "/proc/1234/status", false},
	}

	for _, tt := range tests {
		got := matchPath(tt.pattern, tt.path)
		if got != tt.expect {
			t.Errorf("matchPath(%q, %q) = %v, want %v", tt.pattern, tt.path, got, tt.expect)
		}
	}
}

// ── Policy Evaluation ───────────────────────────────────

func TestFSPolicy_DefaultDeny(t *testing.T) {
	policy := FilesystemPolicy{DefaultAction: FSDeny}

	action, _ := policy.Evaluate("/anywhere", FSRead)
	if action != FSDeny {
		t.Error("default deny should deny everything")
	}
}

func TestFSPolicy_DefaultAllow(t *testing.T) {
	policy := FilesystemPolicy{DefaultAction: FSAllow}

	action, _ := policy.Evaluate("/anywhere", FSWrite)
	if action != FSAllow {
		t.Error("default allow should allow everything")
	}
}

func TestFSPolicy_RulesFirstMatchWins(t *testing.T) {
	policy := FilesystemPolicy{
		DefaultAction: FSDeny,
		Rules: []FSRule{
			{Action: FSAllow, Path: "/workspace/**"},
			{Action: FSDeny, Path: "/workspace/secrets/**"},
		},
	}

	// /workspace/file.txt → first rule matches → allow.
	action, _ := policy.Evaluate("/workspace/file.txt", FSRead)
	if action != FSAllow {
		t.Error("workspace file should be allowed by first rule")
	}

	// /workspace/secrets/key.pem → first rule also matches (/**) → allow.
	// To block secrets, put the deny rule BEFORE the allow rule.
	action, _ = policy.Evaluate("/workspace/secrets/key.pem", FSRead)
	if action != FSAllow {
		t.Error("first match wins — allow rule comes first")
	}
}

func TestFSPolicy_DenyBeforeAllow(t *testing.T) {
	policy := FilesystemPolicy{
		DefaultAction: FSDeny,
		Rules: []FSRule{
			{Action: FSDeny, Path: "/workspace/.env", Description: "block env file"},
			{Action: FSDeny, Path: "/workspace/.git/**", Description: "block git internals"},
			{Action: FSAllow, Path: "/workspace/**", Description: "workspace access"},
		},
	}

	// .env blocked.
	action, rule := policy.Evaluate("/workspace/.env", FSRead)
	if action != FSDeny || rule.Description != "block env file" {
		t.Error(".env should be blocked")
	}

	// .git blocked.
	action, _ = policy.Evaluate("/workspace/.git/config", FSRead)
	if action != FSDeny {
		t.Error(".git should be blocked")
	}

	// Regular files allowed.
	action, _ = policy.Evaluate("/workspace/main.py", FSRead)
	if action != FSAllow {
		t.Error("regular workspace file should be allowed")
	}
}

// ── Access Type Filtering ───────────────────────────────

func TestFSPolicy_AccessTypeFilter(t *testing.T) {
	policy := FilesystemPolicy{
		DefaultAction: FSDeny,
		Rules: []FSRule{
			{Action: FSAllow, Path: "/env/**", Access: []FSAccess{FSRead, FSExecute, FSList}},
		},
	}

	// Read allowed.
	action, _ := policy.Evaluate("/env/bin/python3", FSRead)
	if action != FSAllow {
		t.Error("read on /env should be allowed")
	}

	// Execute allowed.
	action, _ = policy.Evaluate("/env/bin/python3", FSExecute)
	if action != FSAllow {
		t.Error("execute on /env should be allowed")
	}

	// Write denied (not in access list).
	action, _ = policy.Evaluate("/env/bin/python3", FSWrite)
	if action != FSDeny {
		t.Error("write on /env should be denied (read-only)")
	}

	// Delete denied.
	action, _ = policy.Evaluate("/env/bin/python3", FSDelete)
	if action != FSDeny {
		t.Error("delete on /env should be denied")
	}
}

// ── Mode Overrides ──────────────────────────────────────

func TestFSPolicy_InspectModeReadOnly(t *testing.T) {
	policy := DefaultFilesystemPolicy()

	// Execute mode: workspace write allowed.
	ok, _ := policy.IsWriteAllowed("/workspace/output.txt", ModeExecute)
	if !ok {
		t.Error("workspace write should be allowed in execute mode")
	}

	// Inspect mode: workspace write denied by override.
	ok, reason := policy.IsWriteAllowed("/workspace/output.txt", ModeExplore)
	if ok {
		t.Error("workspace write should be denied in inspect mode")
	}
	if reason == "" {
		t.Error("denial should have a reason")
	}

	// Inspect mode: workspace read still allowed.
	ok, _ = policy.IsReadAllowed("/workspace/data.txt", ModeExplore)
	if !ok {
		t.Error("workspace read should be allowed in inspect mode")
	}
}

// ── Default Policy ──────────────────────────────────────

func TestDefaultFSPolicy(t *testing.T) {
	policy := DefaultFilesystemPolicy()

	tests := []struct {
		path   string
		access FSAccess
		mode   ExecMode
		expect bool
		desc   string
	}{
		// Workspace.
		{"/workspace/file.py", FSRead, ModeExecute, true, "workspace read"},
		{"/workspace/file.py", FSWrite, ModeExecute, true, "workspace write"},
		{"/workspace/sub/deep.txt", FSRead, ModeExecute, true, "workspace deep read"},

		// /tmp.
		{"/tmp/cache.dat", FSWrite, ModeExecute, true, "tmp write"},
		{"/tmp/cache.dat", FSRead, ModeExecute, true, "tmp read"},

		// /env.
		{"/env/bin/python3", FSRead, ModeExecute, true, "env read"},
		{"/env/bin/python3", FSExecute, ModeExecute, true, "env execute"},
		{"/env/bin/python3", FSWrite, ModeExecute, false, "env write blocked"},

		// System paths.
		{"/usr/bin/ls", FSRead, ModeExecute, true, "system read"},
		{"/usr/bin/ls", FSExecute, ModeExecute, true, "system execute"},
		{"/usr/bin/ls", FSWrite, ModeExecute, false, "system write blocked"},

		// /dev.
		{"/dev/null", FSRead, ModeExecute, true, "dev null"},
		{"/dev/urandom", FSRead, ModeExecute, true, "dev urandom"},
		{"/dev/sda", FSRead, ModeExecute, false, "dev sda blocked"},
		{"/dev/mem", FSRead, ModeExecute, false, "dev mem blocked"},

		// Sensitive paths.
		{"/proc/1/mem", FSRead, ModeExecute, false, "proc mem blocked"},
		{"/proc/kcore", FSRead, ModeExecute, false, "proc kcore blocked"},
		{"/sys/class/net", FSRead, ModeExecute, false, "sysfs blocked"},
		{"/proc/1/status", FSRead, ModeExecute, true, "proc status allowed"},

		// Mode overrides.
		{"/workspace/file.py", FSWrite, ModeExplore, false, "workspace write blocked in inspect"},
		{"/workspace/file.py", FSRead, ModeExplore, true, "workspace read allowed in inspect"},
		{"/workspace/file.py", FSWrite, ModeExplore, false, "workspace write blocked in plan"},
	}

	for _, tt := range tests {
		var ok bool
		switch tt.access {
		case FSRead:
			ok, _ = policy.IsReadAllowed(tt.path, tt.mode)
		case FSWrite:
			ok, _ = policy.IsWriteAllowed(tt.path, tt.mode)
		case FSExecute:
			ok, _ = policy.IsExecAllowed(tt.path, tt.mode)
		case FSDelete:
			ok, _ = policy.IsDeleteAllowed(tt.path, tt.mode)
		}

		if ok != tt.expect {
			t.Errorf("%s: %s %s (mode=%s) = %v, want %v", tt.desc, tt.access, tt.path, tt.mode, ok, tt.expect)
		}
	}
}

// ── FSGate ──────────────────────────────────────────────

func TestFSGate(t *testing.T) {
	gate := FSGate{
		Policy: DefaultFilesystemPolicy(),
		Mode:   ModeExecute,
	}

	if err := gate.Check("/workspace/file.py", FSRead); err != nil {
		t.Errorf("workspace read should be allowed: %v", err)
	}

	if err := gate.Check("/dev/sda", FSRead); err == nil {
		t.Error("/dev/sda should be blocked")
	}

	// Switch to inspect mode.
	gate.Mode = ModeExplore
	if err := gate.Check("/workspace/file.py", FSWrite); err == nil {
		t.Error("workspace write should be blocked in inspect mode")
	}
}

// ── Custom Policy ───────────────────────────────────────

func TestCustomFSPolicy(t *testing.T) {
	policy := FilesystemPolicy{
		DefaultAction: FSDeny,
		Rules: []FSRule{
			{Action: FSDeny, Path: "/workspace/.env", Description: "secrets"},
			{Action: FSDeny, Path: "/workspace/node_modules/**", Description: "no node_modules"},
			{Action: FSAllow, Path: "/workspace/**"},
			{Action: FSAllow, Path: "/tmp/**"},
		},
	}

	// .env blocked.
	action, _ := policy.Evaluate("/workspace/.env", FSRead)
	if action != FSDeny {
		t.Error(".env should be blocked")
	}

	// node_modules blocked.
	action, _ = policy.Evaluate("/workspace/node_modules/evil/index.js", FSRead)
	if action != FSDeny {
		t.Error("node_modules should be blocked")
	}

	// Regular file allowed.
	action, _ = policy.Evaluate("/workspace/main.py", FSRead)
	if action != FSAllow {
		t.Error("workspace file should be allowed")
	}

	// Outside workspace blocked.
	action, _ = policy.Evaluate("/etc/passwd", FSRead)
	if action != FSDeny {
		t.Error("/etc should be blocked by default deny")
	}
}
