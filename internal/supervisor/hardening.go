package supervisor

// Hardening documents the security posture and known limitations.
//
// TRUST BOUNDARY: The Firecracker microVM is the trust boundary,
// NOT the command proxy. Code inside the VM can do anything
// the OS allows. The proxy provides UX feedback, not security.
//
// The VM is a CAPABILITY PRISON:
// - Can only run binaries that exist in /env/bin
// - Can only write to filesystem if mode allows (kernel RO mount)
// - Can only access network if iptables allows (host-side)
// - Can only make syscalls if seccomp allows (kernel-side)
// - Can only use CPU/memory within cgroup limits
//
// WHAT THE PROXY STOPS:
// - Obvious command injection (rm, dd, etc.)
// - Shell wrapper attacks (sh -c "rm")
// - Known interpreter escapes (subprocess, popen)
// - Fast UX feedback before hitting kernel limits
// - Audit logging of all command attempts
//
// WHAT THE PROXY CANNOT STOP:
// - Obfuscated code (eval, base64, getattr tricks)
// - Code in script files (only scans inline -c/-e code)
// - Zero-day exploits in interpreters
// - Data exfiltration via allowed channels
//
// WHAT THE VM KERNEL STOPS (when properly configured):
// - File writes in RO mode (mount -o ro, kernel-enforced)
// - Network access when blocked (iptables on host)
// - Process spawning when blocked (seccomp EACCES)
// - Resource exhaustion (cgroups OOM killer, CPU quota)
// - Privilege escalation (no CAP_SYS_ADMIN, no mount)
//
// REMAINING RISKS:
// - DNS exfiltration (mitigate: DNS proxy with logging)
// - Timing side-channels (mitigate: noisy neighbor defense)
// - Guest kernel exploit (mitigate: minimal kernel, patch)
// - Firecracker VMM exploit (mitigate: jailer, seccomp)

// HardeningChecklist is the production deployment checklist.
type HardeningChecklist struct {
	// VM-level.
	FirecrackerJailer  bool // Run each VM under separate UID with chroot.
	MinimalGuestKernel bool // 5.10+ with minimal modules, no debug.
	GuestRootfsRO      bool // Base rootfs is read-only.
	NoCapSysAdmin      bool // Guest cannot remount, modprobe, etc.
	SeccompEnabled     bool // Guest supervisor has seccomp profile applied.

	// Filesystem.
	WorkspaceOverlayFS bool // Workspace uses overlayfs (not direct mount).
	EnvBinProjection   bool // /env/bin only contains allowed package binaries.
	TmpfsTmp           bool // /tmp is tmpfs (not persistent).
	NoDev              bool // No /dev access except null, zero, urandom.

	// Network.
	HostIptables           bool // Host-side iptables per VM TAP interface.
	DNSProxy               bool // DNS goes through proxy with query logging.
	NoMetadataAccess       bool // 169.254.169.254 blocked.
	NoPrivateNetworkAccess bool // 10/8, 172.16/12, 192.168/16 blocked.

	// Resources.
	CgroupsV2        bool // CPU and memory limits via cgroups v2.
	OOMKillerEnabled bool // OOM kills the VM, not the host.
	DiskQuota        bool // Overlay upper dir has disk quota.
	MaxProcesses     bool // PID limit in cgroup (prevent fork bombs).

	// Monitoring.
	AuditLogging      bool // All commands logged with timestamps.
	MetricsExported   bool // Prometheus metrics for anomaly detection.
	AlertOnBlockedCmd bool // Alert when blocked commands are attempted.
	NetworkFlowLog    bool // Log all network connections.
}

// SeccompProfile defines the syscall filter for in-VM processes.
type SeccompProfile struct {
	Name  string
	Rules []SeccompRule
}

// SeccompRule defines a single syscall filter rule.
type SeccompRule struct {
	Syscall string
	Action  string // "allow", "errno", "kill"
}

// ReadOnlySeccompProfile blocks all write/exec/network syscalls.
// Used for inspect and plan modes.
var ReadOnlySeccompProfile = SeccompProfile{
	Name: "readonly",
	Rules: []SeccompRule{
		// Block process creation.
		{Syscall: "execve", Action: "errno"},
		{Syscall: "execveat", Action: "errno"},
		{Syscall: "fork", Action: "errno"},
		{Syscall: "vfork", Action: "errno"},
		{Syscall: "clone", Action: "errno"},
		{Syscall: "clone3", Action: "errno"},

		// Block file modification.
		{Syscall: "unlink", Action: "errno"},
		{Syscall: "unlinkat", Action: "errno"},
		{Syscall: "rename", Action: "errno"},
		{Syscall: "renameat2", Action: "errno"},
		{Syscall: "rmdir", Action: "errno"},
		{Syscall: "mkdir", Action: "errno"},
		{Syscall: "mkdirat", Action: "errno"},
		{Syscall: "chmod", Action: "errno"},
		{Syscall: "fchmod", Action: "errno"},
		{Syscall: "chown", Action: "errno"},
		{Syscall: "fchown", Action: "errno"},
		{Syscall: "truncate", Action: "errno"},
		{Syscall: "ftruncate", Action: "errno"},

		// Block network.
		{Syscall: "connect", Action: "errno"},
		{Syscall: "bind", Action: "errno"},
		{Syscall: "listen", Action: "errno"},
		{Syscall: "accept4", Action: "errno"},
		{Syscall: "socket", Action: "errno"},

		// Block dangerous operations.
		{Syscall: "mount", Action: "errno"},
		{Syscall: "umount2", Action: "errno"},
		{Syscall: "reboot", Action: "errno"},
		{Syscall: "ptrace", Action: "errno"},
	},
}

// StandardSeccompProfile allows most operations but blocks dangerous ones.
// Used for execute mode.
var StandardSeccompProfile = SeccompProfile{
	Name: "standard",
	Rules: []SeccompRule{
		{Syscall: "mount", Action: "errno"},
		{Syscall: "umount2", Action: "errno"},
		{Syscall: "reboot", Action: "errno"},
		{Syscall: "ptrace", Action: "errno"},
		{Syscall: "kexec_load", Action: "errno"},
		{Syscall: "init_module", Action: "errno"},
		{Syscall: "finit_module", Action: "errno"},
		{Syscall: "delete_module", Action: "errno"},
	},
}

// FullSeccompProfile allows everything. Used for commit mode.
var FullSeccompProfile = SeccompProfile{
	Name:  "full",
	Rules: nil,
}
