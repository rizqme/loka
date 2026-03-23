# Run Commands

Execute commands inside a Loka session: single, batched, with error handling, and cancellation.

<div class="steps">

<div class="step">

### 1. Run a single command

<!-- tabs:start -->

#### **Python**

```python
from loka import LokaClient

client = LokaClient()
session = client.create_session(image="python:3.12-slim")

result = client.run_and_wait(session.id, "whoami")
print(result.stdout)    # root
print(result.exit_code) # 0
```

#### **TypeScript**

```typescript
import { LokaClient } from '@rizqme/loka-sdk';

const loka = new LokaClient();
const session = await loka.createSession({ image: 'python:3.12-slim' });

const result = await loka.runCommand(session.id, 'whoami');
console.log(result.stdout);   // root
console.log(result.exitCode); // 0
```

<!-- tabs:end -->

</div>

<div class="step">

### 2. Run a command with arguments

Pass the full command string. The VM shell handles argument parsing.

<!-- tabs:start -->

#### **Python**

```python
result = client.run_and_wait(session.id, "ls -la /tmp")
print(result.stdout)
```

#### **TypeScript**

```typescript
const result = await loka.runCommand(session.id, 'ls -la /tmp');
console.log(result.stdout);
```

<!-- tabs:end -->

</div>

<div class="step">

### 3. Run commands in parallel

Execute multiple commands concurrently and collect all results.

<!-- tabs:start -->

#### **Python**

```python
results = client.run_parallel(session.id, [
    "python3 -c 'import sys; print(sys.version)'",
    "cat /etc/os-release",
    "df -h /",
])
for r in results:
    print(r.stdout)
```

#### **TypeScript**

```typescript
const commands = [
    "python3 -c 'import sys; print(sys.version)'",
    'cat /etc/os-release',
    'df -h /',
];
const results = await Promise.all(
    commands.map(cmd => loka.runCommand(session.id, cmd))
);
results.forEach(r => console.log(r.stdout));
```

<!-- tabs:end -->

</div>

<div class="step">

### 4. Handle failed commands

A non-zero exit code does not throw an exception. Check `exit_code` yourself.

<!-- tabs:start -->

#### **Python**

```python
result = client.run_and_wait(session.id, "ls /nonexistent")
if result.exit_code != 0:
    print(f"Command failed: {result.stderr}")
```

#### **TypeScript**

```typescript
const result = await loka.runCommand(session.id, 'ls /nonexistent');
if (result.exitCode !== 0) {
    console.error(`Command failed: ${result.stderr}`);
}
```

<!-- tabs:end -->

</div>

<div class="step">

### 5. Cancel a running command

Use the async `run()` method to get an execution handle, then cancel it.

<!-- tabs:start -->

#### **Python**

```python
execution = client.run(session.id, "sleep 3600")
# ... later
execution.cancel()
```

#### **TypeScript**

```typescript
const execution = loka.run(session.id, 'sleep 3600');
// ... later
execution.cancel();
```

<!-- tabs:end -->

</div>

</div>

<div class="tip"><strong>Tip</strong> Use <code>run_parallel</code> (Python) or <code>Promise.all</code> (TypeScript) to speed up independent setup steps like installing packages.</div>

## Next steps

<div class="card-grid">
<a class="card" href="#/quickstart/checkpoints"><div class="card-title">Checkpoints</div><div class="card-desc">Snapshot and rollback VM state.</div></a>
<a class="card" href="#/concepts/sessions"><div class="card-title">Sessions</div><div class="card-desc">Lifecycle, modes, and resource limits.</div></a>
</div>

## Stream Output

Get real-time stdout/stderr as the command runs, instead of waiting for completion.

<!-- tabs:start -->

#### **Python**

```python
for event in client.stream(session.ID, "python3", ["-c", """
import time
for i in range(5):
    print(f'Step {i}...')
    time.sleep(1)
print('Done!')
"""]):
    if event.is_output:
        print(event.text, end="")
    if event.is_done:
        break
```

#### **TypeScript**

```typescript
for await (const event of loka.stream(session.ID, {
  command: 'python3',
  args: ['-c', "import time\nfor i in range(5):\n  print(f'Step {i}...')\n  time.sleep(1)\nprint('Done!')"],
})) {
  if (event.event === 'output') process.stdout.write(event.data.text);
  if (event.event === 'done') break;
}
```

<!-- tabs:end -->

### Stream Events

| Event | Description |
|-------|-------------|
| `output` | Stdout/stderr chunk. `data.stream` is `"stdout"` or `"stderr"`. `data.text` is the content. |
| `status` | Execution status changed. `data.status` is the new status. |
| `approval_required` | Command suspended, needs approval. `data.command` is the binary. |
| `result` | Final result for a command. `data.exit_code`, `data.stdout`, `data.stderr`. |
| `error` | Error occurred. `data.message`. |
| `done` | Stream complete. |
