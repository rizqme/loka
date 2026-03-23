# Checkpoints

A checkpoint captures the complete state of a session: filesystem, memory, and running processes. Checkpoints are stored as overlay diffs on top of the base image, making them lightweight and fast to create.

## How checkpoints work

Each checkpoint stores only the changes (diff) since the parent checkpoint or base image. Checkpoints form a directed acyclic graph (DAG), not a linear history.

```
base image
  ├── chk_a (install python)
  │     ├── chk_b (add app code)
  │     └── chk_c (add test data)
  └── chk_d (install node)
```

You can branch from any checkpoint, creating parallel timelines.

## Light vs full checkpoints

| Type | Captures | Speed | Size | Use case |
|---|---|---|---|---|
| Light | Filesystem diff only | ~5ms | Small | Quick save points |
| Full | Filesystem + memory + CPU state | ~50ms | Larger | Resume exact process state |

By default, `create_checkpoint` creates a full checkpoint.

## Create a checkpoint

<!-- tabs:start -->

#### **Python**

```python
from loka import LokaClient

client = LokaClient()
session = client.create_session(image="python:3.12-slim")

cp = client.create_checkpoint(session.id, label="clean-state")
```

#### **TypeScript**

```typescript
import { LokaClient } from '@rizqme/loka-sdk';

const loka = new LokaClient();
const session = await loka.createSession({ image: 'python:3.12-slim' });

const cp = await loka.createCheckpoint(session.id, { label: 'clean-state' });
```

<!-- tabs:end -->

## List checkpoints

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
checkpoints.forEach(c => console.log(`${c.id}  ${c.label}  parent=${c.parentId}`));
```

<!-- tabs:end -->

## Restore a checkpoint

<!-- tabs:start -->

#### **Python**

```python
client.restore_checkpoint(session.id, cp.id)
```

#### **TypeScript**

```typescript
await loka.restoreCheckpoint(session.id, cp.id);
```

<!-- tabs:end -->

<div class="warning"><strong>Warning</strong> Restoring discards all changes made after the checkpoint. The session returns to the exact state when the checkpoint was created.</div>

## Delete a checkpoint

<!-- tabs:start -->

#### **Python**

```python
client.delete_checkpoint(cp.id)
```

#### **TypeScript**

```typescript
await loka.deleteCheckpoint(cp.id);
```

<!-- tabs:end -->

<div class="info"><strong>Info</strong> You cannot delete a checkpoint that has child checkpoints. Delete the children first, or use <code>force=True</code> to delete the entire subtree.</div>

## Next steps

<div class="card-grid">
<a class="card" href="#/quickstart/checkpoints"><div class="card-title">Checkpoint Quickstart</div><div class="card-desc">Step-by-step guide to creating and restoring checkpoints.</div></a>
<a class="card" href="#/concepts/images"><div class="card-title">Images & Snapshots</div><div class="card-desc">How base images and warm snapshots work.</div></a>
</div>
