# Sessions API

## Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/sessions` | Create a new session |
| `GET` | `/api/v1/sessions` | List sessions |
| `GET` | `/api/v1/sessions/:id` | Get session by ID |
| `DELETE` | `/api/v1/sessions/:id` | Destroy session |
| `POST` | `/api/v1/sessions/:id/pause` | Pause (snapshot memory) |
| `POST` | `/api/v1/sessions/:id/resume` | Resume from pause |
| `POST` | `/api/v1/sessions/:id/mode` | Change operating mode |

## Create Session

```
POST /api/v1/sessions
```

```json
{
  "image": "ubuntu:22.04",
  "vcpus": 2,
  "memory_mb": 512,
  "disk_mb": 2048,
  "mode": "supervised",
  "labels": {"team": "ml"},
  "policy": {
    "allowed_commands": ["python3", "pip"],
    "network": {"outbound": "allow_all"}
  }
}
```

**Response** `201 Created`:

```json
{
  "id": "ses_abc123",
  "status": "starting",
  "image": "ubuntu:22.04",
  "worker_id": "wrk_def456",
  "mode": "supervised",
  "created_at": "2026-03-23T10:00:00Z"
}
```

## List Sessions

```
GET /api/v1/sessions?status=running&limit=50&offset=0
```

**Response** `200 OK`:

```json
{
  "items": [{ "id": "ses_abc123", "status": "running", "image": "ubuntu:22.04", "...": "..." }],
  "total": 142,
  "limit": 50,
  "offset": 0
}
```

## Get Session

```
GET /api/v1/sessions/ses_abc123
```

Returns full session object including `policy`, `resource_usage`, and `checkpoint_count`.

## Destroy Session

```
DELETE /api/v1/sessions/ses_abc123
```

**Response** `204 No Content`. Stops the VM, cleans up overlay and session directory.

## Pause / Resume

```
POST /api/v1/sessions/ses_abc123/pause
POST /api/v1/sessions/ses_abc123/resume
```

Pause creates an in-memory snapshot. Resume restores from it. Both return the updated session object.

## Change Mode

```
POST /api/v1/sessions/ses_abc123/mode
```

```json
{ "mode": "auto" }
```

Valid modes: `auto`, `supervised`, `locked`.
