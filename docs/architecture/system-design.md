# System Design

## Architecture

```
                 ┌─────────────────┐
                 │  Load Balancer   │
                 └────────┬────────┘
          ┌───────────────┼───────────────┐
   ┌──────▼──────┐ ┌──────▼──────┐ ┌──────▼──────┐
   │   CP  1     │ │   CP  2     │ │   CP  N     │
   └──────┬──────┘ └──────┬──────┘ └──────┬──────┘
          └───────────────┼───────────────┘
     ┌────────┐    ┌──────┴──┐    ┌──────────┐
     │ Redis  │    │Postgres │    │ ObjStore │
     └────────┘    └─────────┘    └────┬─────┘
          ┌───────────────┼────────────┘
   ┌──────▼──┐   ┌───────▼──┐   ┌──────▼───┐
   │Worker A │   │Worker B  │   │Worker C  │
   │(AWS)    │   │(GCP)     │   │(Self)    │
   └─────────┘   └──────────┘   └──────────┘
```

## Core Principles

1. **No dev mode** — Firecracker is the only execution path
2. **Docker images as base** — start from any image, snapshot your env
3. **Agent controls flow** — LOKA provides primitives, agent orchestrates
4. **Security by access control** — proxy gates binaries, sandbox gates resources
5. **Checkpoint DAG** — branch and rollback execution paths

## Data Flow

```
Agent request
  → REST API (session manager)
  → Scheduler (pick worker)
  → Worker command channel
  → Firecracker VM launch
  → Supervisor (vsock listener)
  → Command proxy (binary gate)
  → Approval gate (if unknown)
  → OS process (sandboxed)
  → Result → vsock → worker → CP → agent
```

## Key Files

| Component | Path |
|-----------|------|
| Domain models | `internal/loka/` |
| Store interface | `internal/store/store.go` |
| Control plane | `internal/controlplane/` |
| Worker agent | `internal/worker/agent.go` |
| VM manager | `internal/worker/vm/firecracker.go` |
| Supervisor | `internal/supervisor/` |
| Command proxy | `internal/supervisor/proxy.go` |
| Approval gate | `internal/supervisor/gate.go` |
| Network policy | `internal/loka/network.go` |
| Filesystem policy | `internal/loka/filesystem.go` |
