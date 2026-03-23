# Monitoring

## Prometheus Metrics

Exposed at `GET /metrics` in Prometheus exposition format.

### Session Metrics

| Metric | Type | Description |
|---|---|---|
| `loka_sessions_total` | counter | Total sessions created (label: `image`) |
| `loka_sessions_active` | gauge | Currently running sessions |
| `loka_session_duration_seconds` | histogram | Session lifetime |
| `loka_session_boot_seconds` | histogram | Time from create to ready |

### Command Metrics

| Metric | Type | Description |
|---|---|---|
| `loka_commands_total` | counter | Total commands submitted (labels: `verdict`, `binary`) |
| `loka_commands_active` | gauge | Currently running commands |
| `loka_command_duration_seconds` | histogram | Command wall-clock time |
| `loka_commands_approval_wait_seconds` | histogram | Time spent in `pending_approval` |

### Checkpoint Metrics

| Metric | Type | Description |
|---|---|---|
| `loka_checkpoints_total` | counter | Total checkpoints created (label: `type`) |
| `loka_checkpoint_size_bytes` | histogram | Checkpoint artifact size |
| `loka_checkpoint_create_seconds` | histogram | Time to create checkpoint |
| `loka_checkpoint_restore_seconds` | histogram | Time to restore checkpoint |

### API Metrics

| Metric | Type | Description |
|---|---|---|
| `loka_api_requests_total` | counter | HTTP requests (labels: `method`, `path`, `status`) |
| `loka_api_request_duration_seconds` | histogram | Request latency |

### Worker Metrics

| Metric | Type | Description |
|---|---|---|
| `loka_workers_online` | gauge | Workers in `online` state |
| `loka_worker_sessions_count` | gauge | Sessions per worker (label: `worker_id`) |
| `loka_worker_cpu_used_ratio` | gauge | vCPU utilization per worker |
| `loka_worker_memory_used_ratio` | gauge | Memory utilization per worker |

## Health Endpoint

```
GET /healthz
```

```json
{
  "status": "ok",
  "is_leader": true,
  "pg_connected": true,
  "redis_connected": true,
  "workers_online": 3,
  "uptime_s": 86400
}
```

Returns `200` when healthy, `503` when degraded.

## Structured Logging

```yaml
logging:
  format: json        # or "text"
  level: info         # debug, info, warn, error
  output: stderr      # or file path
```

Log fields: `ts`, `level`, `msg`, `session_id`, `command_id`, `worker_id`, `component`.

```json
{"ts":"2026-03-23T10:01:00.123Z","level":"info","msg":"session started","session_id":"ses_abc123","worker_id":"wrk_def456","image":"python:3.11-slim","component":"scheduler"}
```
