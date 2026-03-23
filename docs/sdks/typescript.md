# TypeScript SDK

Install the Loka TypeScript SDK and use it to manage sessions, run commands, and work with checkpoints.

## Install

```bash
npm install @rizqme/loka-sdk
```

## Connect

```typescript
import { LokaClient } from '@rizqme/loka-sdk';

const loka = new LokaClient();                                // uses LOKA_API_URL env var
const loka = new LokaClient({ url: 'http://localhost:8080' }); // or pass explicitly
```

Check connectivity:

```typescript
const status = await loka.health();
console.log(status); // { status: 'ok', workers: 3 }
```

## Sessions

```typescript
// Create from an image
const session = await loka.createSession({ image: 'python:3.12-slim', mode: 'execute' });

// Create from a checkpoint
const session = await loka.createSession({ checkpoint: 'chk_7b2f...' });

// Pause / resume
await loka.pauseSession(session.id);
await loka.resumeSession(session.id);

// Change mode
await loka.setMode(session.id, 'inspect');

// Destroy
await loka.destroySession(session.id);
```

## Run commands

```typescript
// Synchronous -- awaits until the command finishes
const result = await loka.runCommand(session.id, "python3 -c 'print(1+1)'");
console.log(result.stdout);   // 2
console.log(result.exitCode); // 0
console.log(result.stderr);   // ""

// Asynchronous -- returns immediately
const execution = loka.run(session.id, 'sleep 60');
execution.cancel();

// Parallel
const results = await Promise.all([
    loka.runCommand(session.id, 'pip install requests'),
    loka.runCommand(session.id, 'pip install flask'),
]);
```

## Checkpoints

```typescript
// Create
const cp = await loka.createCheckpoint(session.id, { label: 'after-setup' });

// List
const checkpoints = await loka.listCheckpoints(session.id);

// Restore
await loka.restoreCheckpoint(session.id, cp.id);

// Delete
await loka.deleteCheckpoint(cp.id);
```

## Approval flow

When a session runs in `ask` mode or a command hits the `needs_approval` verdict:

```typescript
// Wait for the next pending execution
const execution = await loka.waitForExecution(session.id);
console.log(`Command: ${execution.command}`);

// Approve it
await loka.approveExecution(execution.id);

// Or reject it
await loka.rejectExecution(execution.id, { reason: 'Not allowed to curl' });

// Approve and add to the session's allowlist for future runs
await loka.approveExecution(execution.id, { addToWhitelist: true });
```

## Images

```typescript
const image = await loka.pullImage('ubuntu:22.04');
const images = await loka.listImages();
```

## Error handling

```typescript
import { LokaError, SessionNotFoundError, CheckpointNotFoundError } from '@rizqme/loka-sdk';

try {
    await loka.restoreCheckpoint(session.id, 'chk_invalid');
} catch (e) {
    if (e instanceof CheckpointNotFoundError) {
        console.error('Checkpoint does not exist');
    } else if (e instanceof LokaError) {
        console.error(`API error: ${e.statusCode} ${e.message}`);
    }
}
```

## Next steps

<div class="card-grid">
<a class="card" href="#/quickstart/first-session"><div class="card-title">Quickstart</div><div class="card-desc">Create your first session step by step.</div></a>
<a class="card" href="#/sdks/python"><div class="card-title">Python SDK</div><div class="card-desc">The Python equivalent of this guide.</div></a>
</div>

## Streaming

Stream stdout/stderr in real-time using async iterators:

```typescript
for await (const event of loka.stream(session.ID, { command: 'apt', args: ['update'] })) {
  if (event.event === 'output') {
    process.stdout.write(event.data.text);
  }
  if (event.event === 'approval_required') {
    await loka.approveExecution(session.ID, event.data.command_id);
  }
  if (event.event === 'done') break;
}
```

### Stream an existing execution

```typescript
for await (const event of loka.streamExecution(session.ID, execId)) {
  if (event.event === 'output') process.stdout.write(event.data.text);
}
```

### StreamEvent

```typescript
interface StreamEvent {
  event: 'output' | 'status' | 'result' | 'approval_required' | 'error' | 'done';
  data: Record<string, any>;
}
```

| event | data |
|-------|------|
| `output` | `{command_id, stream: "stdout"\|"stderr", text}` |
| `status` | `{status}` |
| `result` | `{command_id, exit_code, stdout, stderr}` |
| `approval_required` | `{command_id, command, reason}` |
| `done` | `{}` |
