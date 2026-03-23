package loka

import (
	"fmt"
	"path/filepath"
	"strings"
)

// FilesystemPolicy defines file and directory access rules for a session.
// Rules are evaluated top-to-bottom; first match wins.
type FilesystemPolicy struct {
	// DefaultAction when no rules match: "allow" or "deny".
	// Default: "deny" (deny by default, allow by rule).
	DefaultAction FSAction `json:"default_action"`

	// Rules are evaluated in order; first match wins.
	Rules []FSRule `json:"rules"`
}

// FSAction is the action for a filesystem rule.
type FSAction string

const (
	FSAllow FSAction = "allow"
	FSDeny  FSAction = "deny"
)

// FSAccess defines what kind of access is being requested.
type FSAccess string

const (
	FSRead    FSAccess = "read"
	FSWrite   FSAccess = "write"
	FSExecute FSAccess = "execute"
	FSList    FSAccess = "list" // Directory listing.
	FSDelete  FSAccess = "delete"
	FSCreate  FSAccess = "create"
)

// FSRule defines a single filesystem access rule.
type FSRule struct {
	// Action: "allow" or "deny".
	Action FSAction `json:"action"`

	// Path pattern to match. Supports:
	//   - Exact:     "/workspace/data.txt"
	//   - Directory: "/workspace/" (matches everything under it)
	//   - Glob:      "/workspace/*.py"
	//   - Recursive: "/workspace/**" (all files and subdirectories)
	//   - Special:   "any" (matches everything)
	Path string `json:"path"`

	// Access restricts which operations this rule applies to.
	// Empty = all operations.
	// Multiple: ["read", "list"] = only read and list.
	Access []FSAccess `json:"access,omitempty"`

	// Description is a human-readable note.
	Description string `json:"description,omitempty"`
}

// DefaultFilesystemPolicy returns a workspace-scoped policy.
func DefaultFilesystemPolicy() FilesystemPolicy {
	return FilesystemPolicy{
		DefaultAction: FSDeny,
		Rules: []FSRule{
			// Workspace: full access.
			{Action: FSAllow, Path: "/workspace/**", Description: "workspace full access"},
			// /tmp: full access.
			{Action: FSAllow, Path: "/tmp/**", Description: "temp directory"},
			// /env: read + execute only.
			{Action: FSAllow, Path: "/env/**", Access: []FSAccess{FSRead, FSExecute, FSList}, Description: "package binaries"},
			// Standard system paths: read only.
			{Action: FSAllow, Path: "/usr/**", Access: []FSAccess{FSRead, FSExecute, FSList}, Description: "system binaries"},
			{Action: FSAllow, Path: "/lib/**", Access: []FSAccess{FSRead, FSList}, Description: "system libraries"},
			{Action: FSAllow, Path: "/etc/**", Access: []FSAccess{FSRead, FSList}, Description: "system config"},
			// /dev: only null, zero, urandom.
			{Action: FSAllow, Path: "/dev/null", Description: "null device"},
			{Action: FSAllow, Path: "/dev/zero", Access: []FSAccess{FSRead}, Description: "zero device"},
			{Action: FSAllow, Path: "/dev/urandom", Access: []FSAccess{FSRead}, Description: "random device"},
			{Action: FSDeny, Path: "/dev/**", Description: "block all other devices"},
			// Block sensitive paths.
			{Action: FSDeny, Path: "/proc/*/mem", Description: "block process memory access"},
			{Action: FSDeny, Path: "/proc/kcore", Description: "block kernel memory"},
			{Action: FSDeny, Path: "/sys/**", Description: "block sysfs"},
			{Action: FSAllow, Path: "/proc/**", Access: []FSAccess{FSRead, FSList}, Description: "proc read-only"},
		},
	}
}

// ModeFilesystemOverrides returns per-mode overrides layered on top of the session policy.
var ModeFilesystemOverrides = map[ExecMode][]FSRule{
	ModeExplore: {
		{Action: FSAllow, Path: "/workspace/**", Access: []FSAccess{FSRead, FSList}, Description: "workspace read-only in explore"},
		{Action: FSDeny, Path: "/workspace/**", Access: []FSAccess{FSWrite, FSDelete, FSCreate}, Description: "no writes in explore"},
	},
	ModeExecute: {
		// Full workspace access.
	},
}

// ── Matching ────────────────────────────────────────────

// Evaluate checks if a file access is allowed.
func (p *FilesystemPolicy) Evaluate(path string, access FSAccess) (FSAction, *FSRule) {
	// Clean and normalize the path.
	path = filepath.Clean(path)

	for i := range p.Rules {
		rule := &p.Rules[i]
		if rule.Matches(path, access) {
			return rule.Action, rule
		}
	}

	action := p.DefaultAction
	if action == "" {
		action = FSDeny
	}
	return action, nil
}

// EvaluateWithMode checks access considering mode overrides.
func (p *FilesystemPolicy) EvaluateWithMode(path string, access FSAccess, mode ExecMode) (FSAction, *FSRule) {
	path = filepath.Clean(path)

	// Check mode overrides first — they take precedence.
	if overrides, ok := ModeFilesystemOverrides[mode]; ok {
		for i := range overrides {
			rule := &overrides[i]
			if rule.Matches(path, access) {
				return rule.Action, rule
			}
		}
	}

	// Then check session rules.
	return p.Evaluate(path, access)
}

// Matches checks if a path and access type match this rule.
func (r *FSRule) Matches(path string, access FSAccess) bool {
	// Check access type filter.
	if len(r.Access) > 0 {
		found := false
		for _, a := range r.Access {
			if a == access {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check path pattern.
	return matchPath(r.Path, path)
}

func matchPath(pattern, path string) bool {
	if pattern == "any" || pattern == "*" {
		return true
	}

	// Exact match.
	if pattern == path {
		return true
	}

	// Directory prefix: "/workspace/" matches "/workspace/foo/bar.txt"
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(path, pattern) || path == strings.TrimSuffix(pattern, "/")
	}

	// Recursive glob: "/workspace/**" matches everything under /workspace/
	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}

	// Standard glob: "/workspace/*.py" matches "/workspace/foo.py"
	matched, err := filepath.Match(pattern, path)
	if err == nil && matched {
		return true
	}

	// For glob patterns, also try matching just the filename.
	if strings.Contains(pattern, "*") {
		matched, err = filepath.Match(filepath.Base(pattern), filepath.Base(path))
		if err == nil && matched {
			// Ensure directory prefix also matches.
			patternDir := filepath.Dir(pattern)
			pathDir := filepath.Dir(path)
			return strings.HasPrefix(pathDir, patternDir)
		}
	}

	return false
}

// ── Convenience Methods ─────────────────────────────────

// IsReadAllowed checks if a file can be read.
func (p *FilesystemPolicy) IsReadAllowed(path string, mode ExecMode) (bool, string) {
	action, rule := p.EvaluateWithMode(path, FSRead, mode)
	if action == FSAllow {
		return true, ""
	}
	return false, denyReason(rule, path, "read")
}

// IsWriteAllowed checks if a file can be written.
func (p *FilesystemPolicy) IsWriteAllowed(path string, mode ExecMode) (bool, string) {
	action, rule := p.EvaluateWithMode(path, FSWrite, mode)
	if action == FSAllow {
		return true, ""
	}
	return false, denyReason(rule, path, "write")
}

// IsExecAllowed checks if a file can be executed.
func (p *FilesystemPolicy) IsExecAllowed(path string, mode ExecMode) (bool, string) {
	action, rule := p.EvaluateWithMode(path, FSExecute, mode)
	if action == FSAllow {
		return true, ""
	}
	return false, denyReason(rule, path, "execute")
}

// IsDeleteAllowed checks if a file can be deleted.
func (p *FilesystemPolicy) IsDeleteAllowed(path string, mode ExecMode) (bool, string) {
	action, rule := p.EvaluateWithMode(path, FSDelete, mode)
	if action == FSAllow {
		return true, ""
	}
	return false, denyReason(rule, path, "delete")
}

func denyReason(rule *FSRule, path, op string) string {
	if rule != nil && rule.Description != "" {
		return fmt.Sprintf("%s denied on %s: %s", op, path, rule.Description)
	}
	return fmt.Sprintf("%s denied on %s by default policy", op, path)
}

// ── FUSE / seccomp-notify Integration ───────────────────
//
// In production, the FilesystemPolicy is enforced by one of:
//
// 1. seccomp-notify: The supervisor installs a seccomp filter that
//    traps open/openat/unlink/rename/mkdir syscalls and checks each
//    path against the policy before allowing the syscall to proceed.
//    This is kernel-enforced and cannot be bypassed.
//
// 2. FUSE overlay: The supervisor mounts a FUSE filesystem that
//    intercepts all file operations and checks against the policy.
//    Slightly higher overhead but more granular control.
//
// 3. Landlock (Linux 5.13+): A non-privileged security module that
//    can restrict filesystem access per-process. Ideal for LOKA.
//
// The Go implementation below is for the supervisor to call when
// intercepting file operations via any of these mechanisms.

// FSGate is the in-VM filesystem access gate.
// Plugged into seccomp-notify, FUSE, or Landlock to intercept file operations.
type FSGate struct {
	Policy FilesystemPolicy
	Mode   ExecMode
}

// Check evaluates a file operation against the policy.
func (g *FSGate) Check(path string, access FSAccess) error {
	action, rule := g.Policy.EvaluateWithMode(path, access, g.Mode)
	if action == FSAllow {
		return nil
	}
	return fmt.Errorf("filesystem access denied: %s on %s (%s)", access, path, denyReason(rule, path, string(access)))
}
