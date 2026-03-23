# Python SDK

Install the Loka Python SDK and use it to manage sessions, run commands, and work with checkpoints.

## Install

```bash
pip install loka
```

## Connect

```python
from loka import LokaClient

client = LokaClient()                          # uses LOKA_API_URL env var
client = LokaClient(url="http://localhost:8080") # or pass explicitly
```

Check connectivity:

```python
status = client.health()
print(status)  # {"status": "ok", "workers": 3}
```

## Sessions

```python
# Create from an image
session = client.create_session(image="python:3.12-slim", mode="execute")

# Create from a checkpoint
session = client.create_session(checkpoint="chk_7b2f...")

# Pause / resume
client.pause_session(session.id)
client.resume_session(session.id)

# Change mode
client.set_mode(session.id, "inspect")

# Destroy
client.destroy_session(session.id)
```

## Run commands

```python
# Synchronous -- blocks until the command finishes
result = client.run_and_wait(session.id, "python3 -c 'print(1+1)'")
print(result.stdout)    # 2
print(result.exit_code) # 0
print(result.stderr)    # ""

# Asynchronous -- returns immediately
execution = client.run(session.id, "sleep 60")
execution.cancel()

# Parallel
results = client.run_parallel(session.id, [
    "pip install requests",
    "pip install flask",
])
```

## Checkpoints

```python
# Create
cp = client.create_checkpoint(session.id, label="after-setup")

# List
checkpoints = client.list_checkpoints(session.id)

# Restore
client.restore_checkpoint(session.id, cp.id)

# Delete
client.delete_checkpoint(cp.id)
```

## Approval flow

When a session runs in `ask` mode or a command hits the `needs_approval` verdict:

```python
# Wait for the next pending execution
execution = client.wait_for_execution(session.id)
print(f"Command: {execution.command}")

# Approve it
client.approve_execution(execution.id)

# Or reject it
client.reject_execution(execution.id, reason="Not allowed to curl")

# Approve and add to the session's allowlist for future runs
client.approve_execution(execution.id, add_to_whitelist=True)
```

## Images

```python
image = client.pull_image("ubuntu:22.04")
images = client.list_images()
```

## Error handling

```python
from loka import LokaError, SessionNotFoundError, CheckpointNotFoundError

try:
    client.restore_checkpoint(session.id, "chk_invalid")
except CheckpointNotFoundError:
    print("Checkpoint does not exist")
except LokaError as e:
    print(f"API error: {e.status_code} {e.message}")
```

## Next steps

<div class="card-grid">
<a class="card" href="#/quickstart/first-session"><div class="card-title">Quickstart</div><div class="card-desc">Create your first session step by step.</div></a>
<a class="card" href="#/sdks/typescript"><div class="card-title">TypeScript SDK</div><div class="card-desc">The TypeScript equivalent of this guide.</div></a>
</div>

## Streaming

Stream stdout/stderr in real-time as commands run:

```python
for event in client.stream(session.ID, "apt", ["update"]):
    if event.is_output:
        print(event.text, end="")
    if event.event == "approval_required":
        # Command needs approval
        client.approve_execution(session.ID, event.data["command_id"])
    if event.is_done:
        break
```

### Stream an existing execution

```python
for event in client.stream_execution(session.ID, exec_id):
    if event.is_output:
        print(event.text, end="")
```

### StreamEvent

| Property | Type | Description |
|----------|------|-------------|
| `event` | `str` | Event type: output, status, result, approval_required, error, done |
| `data` | `dict` | Event payload |
| `is_output` | `bool` | True if output event |
| `is_done` | `bool` | True if done event |
| `text` | `str` | Output text (for output events) |
| `stream_name` | `str` | `"stdout"` or `"stderr"` |
