package loka

// ExecMode represents the execution mode of a LOKA session.
//
// Three modes:
//   - explore: Read-only. Can read files, run read-only commands. No writes, no network.
//   - execute: Full access. Read/write files, network access, run any allowed command.
//   - ask:     Like execute, but every command requires approval before running.
type ExecMode string

const (
	ModeExplore ExecMode = "explore" // Read-only: inspect files, run safe commands.
	ModeExecute ExecMode = "execute" // Full access: read/write, network, all allowed commands.
	ModeAsk     ExecMode = "ask"     // Like execute but every command needs approval first.
)

// ModePolicy defines the permissions for a given execution mode.
type ModePolicy struct {
	Mode           ExecMode
	WorkspaceWrite bool
	NetworkAccess  bool
	CredentialTier CredentialTier
}

// CredentialTier defines levels of credential access.
type CredentialTier string

const (
	CredentialNone   CredentialTier = "none"
	CredentialScoped CredentialTier = "scoped"
	CredentialFull   CredentialTier = "full"
)

// ModePolicies maps each execution mode to its permission policy.
var ModePolicies = map[ExecMode]ModePolicy{
	ModeExplore: {Mode: ModeExplore, WorkspaceWrite: false, NetworkAccess: false, CredentialTier: CredentialNone},
	ModeExecute: {Mode: ModeExecute, WorkspaceWrite: true, NetworkAccess: true, CredentialTier: CredentialFull},
	ModeAsk:     {Mode: ModeAsk, WorkspaceWrite: true, NetworkAccess: true, CredentialTier: CredentialScoped},
}

// ValidModeTransitions — all modes can transition to any other mode.
var ValidModeTransitions = map[ExecMode][]ExecMode{
	ModeExplore: {ModeExecute, ModeAsk},
	ModeExecute: {ModeExplore, ModeAsk},
	ModeAsk:     {ModeExplore, ModeExecute},
}

// CanTransitionModeTo checks if a mode transition is valid.
func CanTransitionModeTo(current, target ExecMode) bool {
	if current == target {
		return true
	}
	allowed, ok := ValidModeTransitions[current]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == target {
			return true
		}
	}
	return false
}
