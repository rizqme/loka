# Sessions

A session is a single Firecracker microVM. It provides an isolated Linux environment with its own filesystem, network, and process tree.

## Lifecycle

Every session moves through these states:

| State | Description |
|---|---|
| `creating` | VM is booting from a snapshot or image |
| `running` | VM is ready to accept commands |
| `paused` | VM is suspended; memory is preserved on disk |
| `terminated` | VM is destroyed; resources are freed |

```
creating → running ⇄ paused → terminated
```

## Create a session

<!-- tabs:start -->

#### **Python**

```python
from loka import LokaClient

client = LokaClient()
session = client.create_session(
    image="python:3.12-slim",
    mode="execute",
)
```

#### **TypeScript**

```typescript
import { LokaClient } from '@rizqme/loka-sdk';

const loka = new LokaClient();
const session = await loka.createSession({
    image: 'python:3.12-slim',
    mode: 'execute',
});
```

<!-- tabs:end -->

## Pause and resume

Pausing suspends the VM and writes memory to disk. Resuming restores it in milliseconds.

<!-- tabs:start -->

#### **Python**

```python
client.pause_session(session.id)
# ... later
client.resume_session(session.id)
```

#### **TypeScript**

```typescript
await loka.pauseSession(session.id);
// ... later
await loka.resumeSession(session.id);
```

<!-- tabs:end -->

## Destroy a session

<!-- tabs:start -->

#### **Python**

```python
client.destroy_session(session.id)
```

#### **TypeScript**

```typescript
await loka.destroySession(session.id);
```

<!-- tabs:end -->

<div class="warning"><strong>Warning</strong> Destroying a session is permanent. Create a checkpoint first if you need to preserve the VM state.</div>

## Execution modes

Each session runs in a mode that controls what commands can do.

| Mode | Filesystem | Network | Use case |
|---|---|---|---|
| `inspect` | Read-only | Blocked | Safe code review |
| `plan` | Read-only | Blocked | Generate plans without side effects |
| `execute` | Read-write | Allowed | Run builds, tests, scripts |
| `commit` | Full access | Allowed | Deploy, push, write anywhere |
| `ask` | Read-write | Allowed | Like execute but every command needs approval |

See [Execution Modes](/concepts/modes.md) for details.

## Next steps

<div class="card-grid">
<a class="card" href="#/concepts/modes"><div class="card-title">Execution Modes</div><div class="card-desc">Control what commands can do inside a session.</div></a>
<a class="card" href="#/concepts/checkpoints"><div class="card-title">Checkpoints</div><div class="card-desc">Snapshot and restore session state.</div></a>
</div>
