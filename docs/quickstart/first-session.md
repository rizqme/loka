# Your First Session

Create a Loka session, run a command inside a microVM, and tear it down.

## Prerequisites

You need a running Loka control plane. See [Installation](/installation.md) if you haven't set one up yet.

<div class="steps">

<div class="step">

### 1. Install the SDK

<!-- tabs:start -->

#### **Python**

```bash
pip install loka
```

#### **TypeScript**

```bash
npm install @rizqme/loka-sdk
```

<!-- tabs:end -->

</div>

<div class="step">

### 2. Create a session

Start a microVM from the `python:3.12-slim` Docker image.

<!-- tabs:start -->

#### **Python**

```python
from loka import LokaClient

client = LokaClient()
session = client.create_session(image="python:3.12-slim")
print(session.id)  # ses_3f8a...
```

#### **TypeScript**

```typescript
import { LokaClient } from '@rizqme/loka-sdk';

const loka = new LokaClient();
const session = await loka.createSession({ image: 'python:3.12-slim' });
console.log(session.id); // ses_3f8a...
```

<!-- tabs:end -->

</div>

<div class="step">

### 3. Run a shell command

<!-- tabs:start -->

#### **Python**

```python
result = client.run_and_wait(session.id, "echo hello")
print(result.stdout)  # hello
```

#### **TypeScript**

```typescript
const result = await loka.runCommand(session.id, 'echo hello');
console.log(result.stdout); // hello
```

<!-- tabs:end -->

</div>

<div class="step">

### 4. Run Python inside the VM

<!-- tabs:start -->

#### **Python**

```python
result = client.run_and_wait(session.id, "python3 -c 'print(42)'")
print(result.stdout)  # 42
```

#### **TypeScript**

```typescript
const result = await loka.runCommand(session.id, "python3 -c 'print(42)'");
console.log(result.stdout); // 42
```

<!-- tabs:end -->

</div>

<div class="step">

### 5. Destroy the session

When you're done, destroy the session to free resources.

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

</div>

</div>

<div class="info"><strong>What just happened?</strong> Loka launched a Firecracker microVM from a warm snapshot of <code>python:3.12-slim</code>, executed your commands inside it, and tore down the VM. Total cold-start time is ~28ms.</div>

## Next steps

<div class="card-grid">
<a class="card" href="#/quickstart/run-commands"><div class="card-title">Run Commands</div><div class="card-desc">Execute commands, handle errors, run in parallel.</div></a>
<a class="card" href="#/quickstart/checkpoints"><div class="card-title">Checkpoints</div><div class="card-desc">Snapshot VM state and roll back instantly.</div></a>
</div>
