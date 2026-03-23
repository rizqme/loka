# Use Docker Images

Pull any Docker image, create sessions from it, customize it, checkpoint it, and reuse it.

<div class="steps">

<div class="step">

### 1. Pull an image

<!-- tabs:start -->

#### **Python**

```python
from loka import LokaClient

client = LokaClient()
image = client.pull_image("ubuntu:22.04")
print(image.id)  # img_a91c...
```

#### **TypeScript**

```typescript
import { LokaClient } from '@rizqme/loka-sdk';

const loka = new LokaClient();
const image = await loka.pullImage('ubuntu:22.04');
console.log(image.id); // img_a91c...
```

<!-- tabs:end -->

<div class="info"><strong>Info</strong> The first pull converts the Docker image to a rootfs and creates a warm Firecracker snapshot. Subsequent session starts from this image take ~28ms.</div>

</div>

<div class="step">

### 2. Create a session from the image

<!-- tabs:start -->

#### **Python**

```python
session = client.create_session(image="ubuntu:22.04")
```

#### **TypeScript**

```typescript
const session = await loka.createSession({ image: 'ubuntu:22.04' });
```

<!-- tabs:end -->

</div>

<div class="step">

### 3. Install packages inside the session

<!-- tabs:start -->

#### **Python**

```python
client.run_and_wait(session.id, "apt-get update -qq")
client.run_and_wait(session.id, "apt-get install -y python3 git")
```

#### **TypeScript**

```typescript
await loka.runCommand(session.id, 'apt-get update -qq');
await loka.runCommand(session.id, 'apt-get install -y python3 git');
```

<!-- tabs:end -->

</div>

<div class="step">

### 4. Checkpoint the customized session

Save a labeled checkpoint so you can reuse this environment later.

<!-- tabs:start -->

#### **Python**

```python
cp = client.create_checkpoint(session.id, label="with-tools")
print(cp.id)  # chk_e44d...
```

#### **TypeScript**

```typescript
const cp = await loka.createCheckpoint(session.id, { label: 'with-tools' });
console.log(cp.id); // chk_e44d...
```

<!-- tabs:end -->

</div>

<div class="step">

### 5. Create a new session from the checkpoint

Start a fresh session that already has Python and Git installed.

<!-- tabs:start -->

#### **Python**

```python
new_session = client.create_session(checkpoint=cp.id)
result = client.run_and_wait(new_session.id, "python3 --version")
print(result.stdout)  # Python 3.10.12
```

#### **TypeScript**

```typescript
const newSession = await loka.createSession({ checkpoint: cp.id });
const result = await loka.runCommand(newSession.id, 'python3 --version');
console.log(result.stdout); // Python 3.10.12
```

<!-- tabs:end -->

</div>

</div>

<div class="tip"><strong>Tip</strong> Use labeled checkpoints as reusable "golden images." Your agents can start from a checkpoint that already has all dependencies installed, saving setup time on every run.</div>

## Next steps

<div class="card-grid">
<a class="card" href="#/concepts/images"><div class="card-title">Images & Snapshots</div><div class="card-desc">How Docker images become Firecracker snapshots.</div></a>
<a class="card" href="#/concepts/checkpoints"><div class="card-title">Checkpoints</div><div class="card-desc">DAG structure and storage internals.</div></a>
</div>
