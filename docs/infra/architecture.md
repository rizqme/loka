# Architecture

Loka runs AI agent workloads in Firecracker microVMs with sub-30ms startup times. This page describes the system components and how they interact.

## System diagram

```
┌─────────────────────────────────────────────┐
│                  SDK / API                   │
│            (Python, TypeScript)              │
└──────────────────┬──────────────────────────┘
                   │ gRPC / HTTP
┌──────────────────▼──────────────────────────┐
│             Control Plane (CP)               │
│  ┌──────────┐ ┌──────────┐ ┌─────────────┐  │
│  │ Session  │ │Checkpoint│ │   Image     │  │
│  │ Manager  │ │ Manager  │ │  Registry   │  │
│  └──────────┘ └──────────┘ └─────────────┘  │
│  ┌──────────┐ ┌──────────┐ ┌─────────────┐  │
│  │ Scheduler│ │  Policy  │ │  Approval   │  │
│  │          │ │  Engine  │ │   Queue     │  │
│  └──────────┘ └──────────┘ └─────────────┘  │
└──────────────────┬──────────────────────────┘
                   │ gRPC
┌──────────────────▼──────────────────────────┐
│               Worker Node                    │
│  ┌──────────────────────────────────────┐    │
│  │            Supervisor                │    │
│  │  ┌────────┐ ┌────────┐ ┌────────┐   │    │
│  │  │ VM 1   │ │ VM 2   │ │ VM N   │   │    │
│  │  │(FC)    │ │(FC)    │ │(FC)    │   │    │
│  │  └────────┘ └────────┘ └────────┘   │    │
│  └──────────────────────────────────────┘    │
│  ┌──────────────────────────────────────┐    │
│  │         Snapshot Store               │    │
│  └──────────────────────────────────────┘    │
└─────────────────────────────────────────────┘
```

## Components

### Control Plane (CP)

The central API server. Handles session lifecycle, scheduling, policy evaluation, and checkpoint management. Stateless -- all persistent state lives in the database and snapshot store.

### Scheduler

Assigns sessions to worker nodes based on available capacity. Supports affinity hints (e.g., place a session on the same worker as its parent checkpoint).

### Worker

A host machine that runs Firecracker microVMs. Each worker runs a **supervisor** process that manages the VMs on that host.

### Supervisor

Per-worker daemon that starts, stops, snapshots, and restores Firecracker VMs. It enforces command policies, manages the approval queue, and handles checkpoint I/O.

### Firecracker VM

Each session runs in a dedicated Firecracker microVM with its own kernel, filesystem, and network namespace. VMs boot from warm snapshots in ~28ms.

### Snapshot Store

Stores base images (rootfs.ext4), warm snapshots (memory + disk), and checkpoint overlays. Can be backed by local disk or shared storage (NFS, S3).

## Data flow

1. **SDK** calls `create_session` on the **CP**.
2. **CP** asks the **Scheduler** for a worker with capacity.
3. **Scheduler** returns a worker. **CP** sends a `StartVM` request to that worker's **Supervisor**.
4. **Supervisor** restores a Firecracker VM from the warm snapshot. Session is `running` in ~28ms.
5. **SDK** calls `run` on the **CP**. **CP** forwards to the **Supervisor**.
6. **Supervisor** checks the command against the **policy engine**. If approved, it executes inside the VM.
7. Result flows back: **VM** -> **Supervisor** -> **CP** -> **SDK**.

## Key files

| Path | Description |
|---|---|
| `cmd/cp/` | Control plane entry point |
| `cmd/worker/` | Worker + supervisor entry point |
| `internal/cp/` | CP business logic (sessions, checkpoints, scheduling) |
| `internal/supervisor/` | VM lifecycle, command execution, policy enforcement |
| `internal/snapshot/` | Snapshot store (create, restore, diff) |
| `internal/policy/` | Command, network, and filesystem policy evaluation |
| `proto/` | gRPC service definitions |
| `sdk/python/` | Python SDK source |
| `sdk/typescript/` | TypeScript SDK source |

## Next steps

<div class="card-grid">
<a class="card" href="#/concepts/sessions"><div class="card-title">Sessions</div><div class="card-desc">Session lifecycle and execution modes.</div></a>
<a class="card" href="#/concepts/images"><div class="card-title">Images & Snapshots</div><div class="card-desc">How Docker images become Firecracker snapshots.</div></a>
</div>
