// ── Session ─────────────────────────────────────────────

export type SessionStatus = 'creating' | 'running' | 'paused' | 'terminating' | 'terminated' | 'error';
export type ExecMode = 'inspect' | 'plan' | 'execute' | 'commit' | 'ask';
export type ExecStatus = 'pending' | 'pending_approval' | 'running' | 'success' | 'failed' | 'canceled' | 'rejected';
export type CheckpointType = 'light' | 'full';

export interface Session {
  ID: string;
  Name: string;
  Status: SessionStatus;
  Mode: ExecMode;
  WorkerID: string;
  ImageRef: string;
  ImageID: string;
  SnapshotID: string;
  VCPUs: number;
  MemoryMB: number;
  Labels: Record<string, string>;
  ExecPolicy: ExecPolicy;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface CreateSessionOpts {
  name?: string;
  image?: string;
  snapshot_id?: string;
  mode?: ExecMode;
  vcpus?: number;
  memory_mb?: number;
  labels?: Record<string, string>;
  allowed_commands?: string[];
  blocked_commands?: string[];
  network_policy?: NetworkPolicy;
  exec_policy?: ExecPolicy;
}

// ── Execution ───────────────────────────────────────────

export interface Command {
  id?: string;
  command: string;
  args?: string[];
  workdir?: string;
  env?: Record<string, string>;
}

export interface CommandResult {
  CommandID: string;
  ExitCode: number;
  Stdout: string;
  Stderr: string;
  StartedAt: string;
  EndedAt: string;
}

export interface Execution {
  ID: string;
  SessionID: string;
  Status: ExecStatus;
  Parallel: boolean;
  Commands: Command[];
  Results: CommandResult[];
  CreatedAt: string;
  UpdatedAt: string;
}

export interface RunOpts {
  commands?: Command[];
  parallel?: boolean;
  /** Single command shorthand */
  command?: string;
  args?: string[];
  workdir?: string;
  env?: Record<string, string>;
}

// ── Checkpoint ──────────────────────────────────────────

export interface Checkpoint {
  ID: string;
  SessionID: string;
  ParentID: string;
  Type: CheckpointType;
  Status: string;
  Label: string;
  CreatedAt: string;
}

// ── Image ───────────────────────────────────────────────

export interface Image {
  ID: string;
  Reference: string;
  Digest: string;
  SizeMB: number;
  Status: string;
  CreatedAt: string;
}

// ── Worker ──────────────────────────────────────────────

export interface Worker {
  ID: string;
  Hostname: string;
  Provider: string;
  Region: string;
  Status: string;
  Capacity: { CPUCores: number; MemoryMB: number; DiskMB: number };
  Labels: Record<string, string>;
}

// ── Streaming ───────────────────────────────────────────

export interface StreamEvent {
  event: 'output' | 'status' | 'result' | 'approval_required' | 'error' | 'done';
  data: Record<string, any>;
}

// ── Policy ──────────────────────────────────────────────

export interface ExecPolicy {
  allowed_commands?: string[];
  blocked_commands?: string[];
  mode_restrictions?: Record<ExecMode, ModeExecPolicy>;
  max_parallel?: number;
  max_duration_seconds?: number;
  network_policy?: NetworkPolicy;
  filesystem_policy?: FilesystemPolicy;
}

export interface ModeExecPolicy {
  allowed_commands?: string[];
  read_only?: boolean;
  require_approval?: boolean;
  blocked?: boolean;
}

export interface NetworkPolicy {
  outbound: NetworkRuleSet;
  inbound: NetworkRuleSet;
}

export interface NetworkRuleSet {
  default_action: 'allow' | 'deny';
  rules: NetworkRule[];
}

export interface NetworkRule {
  action: 'allow' | 'deny';
  target: string;
  ports?: string;
  protocol?: string;
  description?: string;
}

export interface FilesystemPolicy {
  default_action: 'allow' | 'deny';
  rules: FilesystemRule[];
}

export interface FilesystemRule {
  action: 'allow' | 'deny';
  path: string;
  access?: string[];
  description?: string;
}
