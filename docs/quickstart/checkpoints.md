# Checkpoints & Rollback

Snapshot your session at any point and roll back instantly. Checkpoints capture the full VM state: filesystem, memory, running processes.

<div class="steps">

<div class="step">

### 1. Create a session and make changes

<!-- tabs:start -->

#### **Python**

```python
from loka import LokaClient

client = LokaClient()
session = client.create_session(image="python:3.12-slim")

client.run_and_wait(session.id, "echo 'version 1' > /tmp/state.txt")
```

#### **TypeScript**

```typescript
import { LokaClient } from '@rizqme/loka-sdk';

const loka = new LokaClient();
const session = await loka.createSession({ image: 'python:3.12-slim' });

await loka.runCommand(session.id, "echo 'version 1' > /tmp/state.txt");
```

<!-- tabs:end -->

</div>

<div class="step">

### 2. Create a checkpoint

<!-- tabs:start -->

#### **Python**

```python
cp = client.create_checkpoint(session.id, label="after-v1")
print(cp.id)  # chk_7b2f...
```

#### **TypeScript**

```typescript
const cp = await loka.createCheckpoint(session.id, { label: 'after-v1' });
console.log(cp.id); // chk_7b2f...
```

<!-- tabs:end -->

</div>

<div class="step">

### 3. Make more changes

<!-- tabs:start -->

#### **Python**

```python
client.run_and_wait(session.id, "echo 'version 2' > /tmp/state.txt")
result = client.run_and_wait(session.id, "cat /tmp/state.txt")
print(result.stdout)  # version 2
```

#### **TypeScript**

```typescript
await loka.runCommand(session.id, "echo 'version 2' > /tmp/state.txt");
const result = await loka.runCommand(session.id, 'cat /tmp/state.txt');
console.log(result.stdout); // version 2
```

<!-- tabs:end -->

</div>

<div class="step">

### 4. Restore the checkpoint

Roll back to the exact state when the checkpoint was created.

<!-- tabs:start -->

#### **Python**

```python
client.restore_checkpoint(session.id, cp.id)

result = client.run_and_wait(session.id, "cat /tmp/state.txt")
print(result.stdout)  # version 1
```

#### **TypeScript**

```typescript
await loka.restoreCheckpoint(session.id, cp.id);

const restored = await loka.runCommand(session.id, 'cat /tmp/state.txt');
console.log(restored.stdout); // version 1
```

<!-- tabs:end -->

</div>

<div class="step">

### 5. View the checkpoint tree

List all checkpoints for a session to see the full history.

<!-- tabs:start -->

#### **Python**

```python
checkpoints = client.list_checkpoints(session.id)
for c in checkpoints:
    print(f"{c.id}  {c.label}  parent={c.parent_id}")
```

#### **TypeScript**

```typescript
const checkpoints = await loka.listCheckpoints(session.id);
checkpoints.forEach(c =>
    console.log(`${c.id}  ${c.label}  parent=${c.parentId}`)
);
```

<!-- tabs:end -->

</div>

</div>

<div class="warning"><strong>Warning</strong> Restoring a checkpoint discards all changes made after it. This includes files, environment variables, and running processes.</div>

## Next steps

<div class="card-grid">
<a class="card" href="#/concepts/checkpoints"><div class="card-title">Checkpoint Concepts</div><div class="card-desc">DAG structure, light vs full checkpoints, storage.</div></a>
<a class="card" href="#/quickstart/docker-images"><div class="card-title">Docker Images</div><div class="card-desc">Pull images and create sessions from checkpoints.</div></a>
</div>
