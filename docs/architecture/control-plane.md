# Control Plane Architecture

## Components

| Component | Responsibility |
|---|---|
| **API Server** | HTTP/JSON REST gateway. Validates requests, enforces auth, routes to internal services. |
| **Session Manager** | Lifecycle management of VM sessions (create, pause, resume, destroy). Owns session state machine. |
| **Scheduler** | Places sessions on workers. Supports `binpack` (fill nodes first) and `spread` (distribute evenly) strategies. Respects labels and resource constraints. |
| **Worker Registry** | Tracks connected workers, their capacity, labels, and health. Heartbeat-based liveness detection (default 10s interval, 30s timeout). |
| **Image Manager** | Stores and serves rootfs images. Handles Docker-to-ext4 conversion pipeline. Deduplicates layers. |
| **HA Coordinator** | Leader election, distributed locking, event fanout. Active only in HA deployment mode. |

## Scheduler Strategies

```yaml
scheduler:
  strategy: binpack   # or "spread"
  label_affinity:
    gpu: required
  resource_fit:
    min_memory_mb: 512
    min_vcpus: 1
```

- **binpack** -- scores workers by `used / capacity` (highest wins). Minimizes active nodes.
- **spread** -- scores workers by `available / capacity` (highest wins). Maximizes fault isolation.

## Deployment Modes

| Aspect | Single | HA |
|---|---|---|
| Database | SQLite (embedded) | PostgreSQL 15+ |
| Cache / Pub-Sub | In-process | Redis 7+ |
| Object Store | Local filesystem | S3-compatible |
| Instances | 1 | 2+ (active/standby) |
| Leader Election | N/A | Redis SETNX |
| External Deps | None | Postgres, Redis, S3 |

### Single Mode

```bash
loka server --mode single --data-dir /var/lib/loka
```

All state persists to `--data-dir`. Zero external dependencies.

### HA Mode

```bash
loka server --mode ha \
  --pg-dsn "postgres://loka:pass@db:5432/loka" \
  --redis-url "redis://redis:6379/0" \
  --s3-bucket loka-state
```

Minimum two control-plane instances behind a load balancer. Only the leader schedules sessions; all instances serve API requests.

## Internal Communication

- Control plane to worker: gRPC (mTLS).
- Worker to control plane: gRPC heartbeat + event stream.
- HA instances: Redis pub/sub for event fanout, Postgres for durable state.
