# Images & Snapshots

Loka converts Docker images into Firecracker-ready snapshots that boot in ~28ms.

## Image pipeline

```
Docker image → rootfs.ext4 → Firecracker boot → warm snapshot → instant sessions
```

1. **Pull** -- Loka pulls the Docker image and extracts the filesystem layers.
2. **Convert** -- The layers are flattened into a single `rootfs.ext4` ext4 disk image.
3. **Boot** -- Firecracker boots a microVM using the rootfs and a minimal kernel.
4. **Snapshot** -- Once the VM reaches a ready state, Loka takes a warm snapshot (memory + disk). This is a one-time cost.
5. **Serve** -- Every new session restores from the warm snapshot. Boot time: ~28ms.

<div class="info"><strong>Info</strong> The first <code>pull_image</code> call takes 10-60 seconds depending on image size. After that, sessions start in milliseconds.</div>

## Pull an image

<!-- tabs:start -->

#### **Python**

```python
from loka import LokaClient

client = LokaClient()
image = client.pull_image("python:3.12-slim")
print(f"{image.id}  size={image.size_mb}MB")
```

#### **TypeScript**

```typescript
import { LokaClient } from '@rizqme/loka-sdk';

const loka = new LokaClient();
const image = await loka.pullImage('python:3.12-slim');
console.log(`${image.id}  size=${image.sizeMb}MB`);
```

<!-- tabs:end -->

## List available images

<!-- tabs:start -->

#### **Python**

```python
images = client.list_images()
for img in images:
    print(f"{img.name}:{img.tag}  {img.id}")
```

#### **TypeScript**

```typescript
const images = await loka.listImages();
images.forEach(img => console.log(`${img.name}:${img.tag}  ${img.id}`));
```

<!-- tabs:end -->

## Create a session from an image

<!-- tabs:start -->

#### **Python**

```python
session = client.create_session(image="python:3.12-slim")
```

#### **TypeScript**

```typescript
const session = await loka.createSession({ image: 'python:3.12-slim' });
```

<!-- tabs:end -->

If the image hasn't been pulled yet, Loka pulls and converts it automatically. Use `pull_image` ahead of time to avoid the delay.

## Supported images

Loka supports any Docker image that runs on `linux/amd64`. Commonly used base images:

| Image | Size | Notes |
|---|---|---|
| `python:3.12-slim` | ~150MB | Python with pip |
| `node:20-slim` | ~200MB | Node.js with npm |
| `ubuntu:22.04` | ~75MB | Minimal Ubuntu |
| `golang:1.22` | ~800MB | Go toolchain |
| `alpine:3.19` | ~8MB | Minimal Linux |

## Next steps

<div class="card-grid">
<a class="card" href="#/quickstart/docker-images"><div class="card-title">Docker Images Quickstart</div><div class="card-desc">Pull, customize, checkpoint, and reuse images.</div></a>
<a class="card" href="#/concepts/checkpoints"><div class="card-title">Checkpoints</div><div class="card-desc">Save and restore VM state on top of base images.</div></a>
</div>
