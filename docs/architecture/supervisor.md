# Supervisor

The supervisor is a statically-linked binary that runs as PID 1 inside the Firecracker microVM.

## Responsibilities

- Initialize the guest environment (mount filesystems, set hostname, configure networking).
- Serve a JSON-RPC interface over vsock for the host worker.
- Gate every command through the approval and policy pipeline.
- Enforce sandbox restrictions (seccomp, filesystem, network).

## vsock JSON-RPC Server

Listens on vsock port `1024`. Protocol: newline-delimited JSON-RPC 2.0.

### Methods

| Method | Params | Description |
|---|---|---|
| `run` | `{"command": "...", "args": [...], "env": {...}, "timeout_s": 30}` | Submit a command for processing through the policy gate |
| `set_mode` | `{"mode": "auto\|supervised\|locked"}` | Change the session operating mode |
| `set_policy` | `{"policy": <PolicyObject>}` | Replace the active policy |
| `approve` | `{"id": "<cmd-id>", "add_to_whitelist": false}` | Approve a pending command |
| `deny` | `{"id": "<cmd-id>", "reason": "..."}` | Deny a pending command |
| `list_pending` | `{}` | Return all commands awaiting approval |
| `ping` | `{}` | Health check, returns `{"uptime_s": ...}` |

## Operating Modes

| Mode | Behavior |
|---|---|
| `auto` | Commands matching the allow-list run immediately. Blocked commands are rejected. Everything else runs. |
| `supervised` | Allowed commands run immediately. All others enter `pending_approval`. |
| `locked` | All commands are rejected. Used during checkpointing. |

## Command Processing Pipeline

```
incoming command
  --> binary gate (is the binary permitted?)
  --> policy evaluation (allowed / blocked / needs_approval)
  --> sandbox enforcement (seccomp, FS, network)
  --> spawn process
  --> stream stdout/stderr back over vsock
```

## Approval Queue

- Pending commands are held in memory with a configurable TTL (default 300s).
- If not approved or denied within the TTL, the command is auto-denied.
- The `approve` method optionally adds the command pattern to the session allow-list via `add_to_whitelist`.

## Lifecycle

1. Firecracker boots the kernel with `init=/usr/bin/supervisor`.
2. Supervisor mounts `/proc`, `/sys`, `/dev`, `/tmp`.
3. Applies overlay union mount for the rootfs.
4. Starts vsock listener.
5. Signals ready to the host worker via a `ready` notification.
6. Serves requests until the VM is paused or destroyed.
