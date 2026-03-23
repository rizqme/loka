# Worker Architecture

The worker agent runs on bare-metal or VM hosts and manages Firecracker microVM lifecycles.

## Responsibilities

1. Accept session placement from the control plane scheduler.
2. Prepare overlay filesystems and boot Firecracker VMs.
3. Proxy commands to the in-VM supervisor via vsock.
4. Manage checkpoints (snapshot/restore).
5. Report capacity, health, and metrics upstream.

## Session State Directory

Each session gets an isolated directory tree:

```
/var/lib/loka/sessions/<session-id>/
  rootfs.ext4          # base image (read-only, shared)
  overlay.ext4         # copy-on-write layer (per-session)
  firecracker.cfg      # generated VM config
  vsock.path           # AF_VSOCK CID mapping
  checkpoint/          # snapshot artifacts
    base/
    <checkpoint-id>/
  logs/
    supervisor.log
    firecracker.log
```

## Overlay Filesystem

The worker constructs a two-layer block device for each VM:

| Layer | Mode | Purpose |
|---|---|---|
| `rootfs.ext4` | RO | Base image shared across sessions using the same image |
| `overlay.ext4` | RW | Session-specific writes (thin-provisioned, default 2 GB) |

Firecracker mounts both as virtio block devices. The supervisor unions them at boot via device-mapper snapshot targets.

## Checkpoint Manager

| Operation | Action |
|---|---|
| `light` | Pause VM, snapshot overlay only, resume |
| `full` | Pause VM, snapshot VM memory + overlay + device state |
| `restore` | Reconstruct overlay chain, optionally restore memory, boot |

Checkpoints form a DAG -- each references a parent. Restoring a checkpoint replays the overlay chain from root to target node.

## vsock Communication

```
Host (worker)  <--AF_VSOCK CID 3, port 1024-->  Guest (supervisor)
```

- Protocol: newline-delimited JSON-RPC 2.0 over vsock.
- Worker dials the guest; supervisor listens on port `1024`.
- Keepalive ping every 5 seconds.

## Resource Management

The worker enforces per-VM limits via Firecracker config:

```json
{
  "vcpu_count": 2,
  "mem_size_mib": 512,
  "disk_size_mib": 2048
}
```

cgroups v2 on the host constrain the Firecracker process itself (CPU shares, memory.max, PIDs).

## Registration

```bash
loka worker --control-plane https://cp.example.com \
  --token <registration-token> \
  --labels gpu=a100,zone=us-east-1a
```

The worker registers with the control plane on startup and begins heartbeating.
