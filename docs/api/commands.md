# Commands API

## Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/sessions/:id/commands` | Run a single command |
| `POST` | `/api/v1/sessions/:id/commands/batch` | Run parallel batch |
| `GET` | `/api/v1/sessions/:id/commands` | List commands |
| `GET` | `/api/v1/sessions/:id/commands/:cmd_id` | Get command status |
| `POST` | `/api/v1/sessions/:id/commands/:cmd_id/approve` | Approve pending command |
| `POST` | `/api/v1/sessions/:id/commands/:cmd_id/reject` | Reject pending command |
| `DELETE` | `/api/v1/sessions/:id/commands/:cmd_id` | Cancel running command |

## Run Single Command

```
POST /api/v1/sessions/ses_abc123/commands
```

```json
{
  "command": "python3",
  "args": ["-c", "print('hello')"],
  "env": {"PYTHONUNBUFFERED": "1"},
  "timeout_s": 30
}
```

**Response** `201 Created`:

```json
{
  "id": "cmd_xyz789",
  "status": "running",
  "command": "python3",
  "created_at": "2026-03-23T10:01:00Z"
}
```

## Run Parallel Batch

```
POST /api/v1/sessions/ses_abc123/commands/batch
```

```json
{
  "commands": [
    {"command": "pip", "args": ["install", "numpy"]},
    {"command": "pip", "args": ["install", "pandas"]}
  ],
  "max_parallel": 2
}
```

**Response** `201 Created`: returns an array of command objects.

## Approval Flow

When a session is in `supervised` mode and a command is not on the allow-list, the command enters `pending_approval` status.

### Approve

```
POST /api/v1/sessions/ses_abc123/commands/cmd_xyz789/approve
```

```json
{
  "add_to_whitelist": true
}
```

Setting `add_to_whitelist: true` adds the command binary to the session allow-list so future invocations proceed without approval.

### Reject

```
POST /api/v1/sessions/ses_abc123/commands/cmd_xyz789/reject
```

```json
{ "reason": "Disallowed binary" }
```

## Command Status

```
GET /api/v1/sessions/ses_abc123/commands/cmd_xyz789
```

```json
{
  "id": "cmd_xyz789",
  "status": "completed",
  "exit_code": 0,
  "stdout": "hello\n",
  "stderr": "",
  "duration_ms": 120,
  "started_at": "2026-03-23T10:01:00Z",
  "finished_at": "2026-03-23T10:01:00Z"
}
```

Status values: `pending_approval`, `queued`, `running`, `completed`, `failed`, `rejected`, `cancelled`, `timed_out`.

## Cancel

```
DELETE /api/v1/sessions/ses_abc123/commands/cmd_xyz789
```

Sends SIGTERM, then SIGKILL after 5s grace period. Returns `204 No Content`.

## Stream Execution

### Start and stream

```
POST /api/v1/sessions/:id/exec/stream
Content-Type: application/json
Accept: text/event-stream
```

Same body as `POST /exec`, but response is an SSE stream instead of JSON.

### Stream existing execution

```
GET /api/v1/sessions/:id/exec/:execId/stream
Accept: text/event-stream
```

### SSE Event Format

```
event: status
data: {"status":"running"}

event: output
data: {"command_id":"cmd-1","stream":"stdout","text":"Hello world\n"}

event: output
data: {"command_id":"cmd-1","stream":"stderr","text":"Warning: ...\n"}

event: approval_required
data: {"command_id":"cmd-2","command":"wget","reason":"command requires approval"}

event: result
data: {"command_id":"cmd-1","exit_code":0,"stdout":"Hello world\n","stderr":""}

event: done
data: {}
```

### Events

| Event | When | Data |
|-------|------|------|
| `status` | Execution status changes | `{status}` |
| `output` | Stdout/stderr chunk | `{command_id, stream, text}` |
| `approval_required` | Command suspended at gate | `{command_id, command, reason}` |
| `result` | Command finished | `{command_id, exit_code, stdout, stderr}` |
| `error` | Error occurred | `{message}` |
| `done` | Stream complete | `{}` |
