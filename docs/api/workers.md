# Workers API

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/workers` | List workers |
| `GET` | `/api/v1/workers/:id` | Get worker details |
| `POST` | `/api/v1/workers/:id/drain` | Drain worker (stop scheduling) |
| `POST` | `/api/v1/workers/:id/undrain` | Undrain worker |
| `DELETE` | `/api/v1/workers/:id` | Remove worker |
| `PUT` | `/api/v1/workers/:id/labels` | Set worker labels |

## List Workers

```
GET /api/v1/workers?status=online&limit=50
```

```json
{
  "items": [
    {
      "id": "wrk_def456",
      "hostname": "worker-01",
      "status": "online",
      "labels": {"gpu": "a100", "zone": "us-east-1a"},
      "capacity": {"vcpus": 64, "memory_mb": 131072, "sessions_max": 50},
      "used": {"vcpus": 12, "memory_mb": 24576, "sessions": 8},
      "last_heartbeat": "2026-03-23T10:00:05Z"
    }
  ],
  "total": 3
}
```

## Get Worker

```
GET /api/v1/workers/wrk_def456
```

Returns full worker object including `sessions` list, `version`, and `uptime_s`.

## Drain

```
POST /api/v1/workers/wrk_def456/drain
```

Marks the worker as `draining`. The scheduler will not place new sessions on it. Existing sessions continue running. Returns the updated worker object.

## Undrain

```
POST /api/v1/workers/wrk_def456/undrain
```

Returns the worker to `online` status.

## Remove Worker

```
DELETE /api/v1/workers/wrk_def456
```

Fails if the worker has active sessions. Drain first, wait for sessions to complete or migrate, then remove. Returns `204 No Content`.

## Set Labels

```
PUT /api/v1/workers/wrk_def456/labels
```

```json
{
  "gpu": "a100",
  "zone": "us-east-1a",
  "team": "ml"
}
```

Replaces all labels. Labels are used by the scheduler for affinity matching.

## Worker Statuses

| Status | Description |
|---|---|
| `online` | Healthy, accepting sessions |
| `draining` | No new sessions, existing sessions continue |
| `offline` | Missed heartbeat threshold (30s default) |
| `removed` | Deregistered |
