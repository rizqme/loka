# Deployment

## Single Node

Zero external dependencies. All state stored locally.

```bash
loka server --mode single --data-dir /var/lib/loka --listen :8080
```

| Component | Backend |
|---|---|
| Database | SQLite (`/var/lib/loka/loka.db`) |
| Object Store | Local filesystem (`/var/lib/loka/objects/`) |
| Cache | In-process |

## Production HA

Requires PostgreSQL, Redis, and an S3-compatible object store.

```bash
loka server --mode ha \
  --pg-dsn "postgres://loka:pass@db:5432/loka?sslmode=require" \
  --redis-url "redis://redis:6379/0" \
  --s3-bucket loka-state \
  --s3-endpoint https://s3.amazonaws.com \
  --listen :8080
```

Run 2+ instances behind a load balancer. See [ha-mode.md](ha-mode.md) for leader election details.

## Docker Compose Example

```yaml
version: "3.8"
services:
  loka:
    image: ghcr.io/loka/loka:latest
    command: ["server", "--mode", "ha"]
    ports: ["8080:8080"]
    environment:
      LOKA_PG_DSN: "postgres://loka:pass@postgres:5432/loka"
      LOKA_REDIS_URL: "redis://redis:6379/0"
      LOKA_S3_BUCKET: "loka-state"
    depends_on: [postgres, redis]

  postgres:
    image: postgres:16
    environment:
      POSTGRES_USER: loka
      POSTGRES_PASSWORD: pass
      POSTGRES_DB: loka
    volumes: ["pg_data:/var/lib/postgresql/data"]

  redis:
    image: redis:7-alpine
    command: ["redis-server", "--appendonly", "yes"]

  worker:
    image: ghcr.io/loka/loka:latest
    command: ["worker", "--control-plane", "http://loka:8080", "--token", "${WORKER_TOKEN}"]
    privileged: true
    volumes: ["/dev/kvm:/dev/kvm"]

volumes:
  pg_data:
```

## Helm Chart

A Helm chart is available at `charts/loka/`:

```bash
helm install loka charts/loka \
  --set controlPlane.replicas=2 \
  --set postgres.dsn="postgres://..." \
  --set redis.url="redis://..." \
  --set s3.bucket=loka-state
```

See `charts/loka/values.yaml` for all configurable values.
