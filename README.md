# LOKA

Controlled execution environment for AI agents. Deploy apps and run code inside microVMs where every command, network connection, and file access is governed by policy.

**[Documentation](https://vyprai.github.io/loka)**

## Install

```bash
curl -fsSL https://vyprai.github.io/loka/install.sh | bash
```

No sudo required. Binaries install to `~/.loka/bin/`, symlinked to `~/.local/bin/`.

Works on macOS (Apple Virtualization Framework) and Linux (Cloud Hypervisor + KVM).

## Quick start

```bash
loka setup local                      # Start LOKA (auto: DNS, HTTPS, port proxy)
cd myapp && loka deploy               # Deploy your app
```

Your app is live at `http://myapp.loka/`.

```bash
loka session create                   # Create an interactive session
loka shell                            # Open a shell in the VM
loka instance list                    # See all running instances
```

## Deploy

Deploy any project. LOKA auto-detects the framework and builds locally.

```bash
loka deploy                           # Auto-detect recipe, deploy current dir
loka deploy --recipe vite             # Explicit recipe
loka deploy --name my-app             # Custom name → my-app.loka
```

**Supported recipes:** Next.js, Vite/VitePress, Node.js, Python, Go, static sites.

Deploy creates a microVM, boots the container image, mounts your bundle, and starts the service. The domain proxy routes `http://my-app.loka/` to the VM.

```bash
loka service list                     # List deployed services
loka service logs my-app              # View logs
loka service stop my-app              # Stop
loka service rm my-app                # Destroy
```

### `loka.yaml`

```yaml
name: my-app
image: node:20-slim
port: 3000
build:
  - npm install
  - npm run build
start: node server.js
domain: my-app.loka
```

## Sessions

Interactive VMs for AI agents. Full PTY shell, exec, checkpoints, and artifacts.

```bash
loka session create --image python:3.12-slim
loka shell                            # Interactive terminal (auto-selects session)
loka exec <id> -- python3 -c "print('hello')"
```

```python
from loka import LokaClient

client = LokaClient()
session = client.create_session(image="python:3.12-slim", mode="execute")
result = client.run_and_wait(session.ID, "python3", ["-c", "print(42)"])
print(result.Results[0].Stdout)
client.destroy_session(session.ID)
```

### Access control

Sessions have an exec policy: whitelist/blacklist commands, gate unknown commands for approval.

| Mode | Filesystem | Network | Approval |
|------|-----------|---------|----------|
| `explore` | Read-only | Blocked | No |
| `execute` | Read/write | Allowed | No |
| `ask` | Read/write | Allowed | Every command |

### Checkpoints & artifacts

```python
cp = client.create_checkpoint(session.ID, label="before-experiment")
client.run_and_wait(session.ID, "pip", ["install", "some-package"])
client.restore_checkpoint(session.ID, cp.ID)  # Roll back

artifacts = client.list_artifacts(session.ID)
data = client.download_artifact(session.ID, "/workspace/output.csv")
```

## Instances

Unified view of all running VMs (sessions and services).

```bash
loka instance list                    # Show all instances
loka instance rm <name>               # Destroy any instance (session or service)
```

## Spaces

Manage LOKA deployments (local or cloud).

```bash
loka space list                       # List spaces
loka space use prod                   # Switch active space
loka space current                    # Show active

# Cloud deploy
loka deploy aws --name prod --region us-east-1 --workers 3
loka deploy gcp --name staging --project my-proj --workers 2
```

## Domains & DNS

Services get `.loka` domains automatically. DNS, HTTPS, and port proxy are set up by `loka setup local`.

```bash
loka dns enable                       # Manual setup (DNS, port 80/443, CA trust)
loka dns status                       # Check DNS status
loka domains                          # List all domain routes
```

- `http://my-app.loka/` — HTTP via port proxy (80 → 6843)
- `https://my-app.loka/` — HTTPS with auto-generated cert (443 → 6843)
- TLS certificates auto-regenerate when new services are deployed

## Volumes

Local-first volumes with cross-worker sync via object storage.

```bash
loka volume create shared-data
```

Volumes are local directories on the worker, shared with VMs via virtiofs. Changes sync to object storage in the background. File locking via the control plane API.

## SDKs

```bash
pip install loka-sdk                  # Python
npm install @vypr-ai/loka-sdk         # TypeScript
```

## Architecture

```
CLI / SDK → Control Plane (lokad) → Worker → MicroVM → Supervisor → Process
                   │                   │
            Domain Proxy          VirtioFS volumes
            Lock Manager          PTY shell
            Volume Sync           Port forwarding
            DNS Server            File locking
```

- **Control plane** (`lokad`) — API server, scheduler, session/service manager, domain proxy, DNS, lock manager
- **Supervisor** (`loka-supervisor`) — runs inside VM as PID 1, enforces policy, PTY, file locks
- **CLI** (`loka`) — deploy, shell, manage instances
- **Proxy** (`loka-proxy`) — routes ports 80/443 to domain proxy (runs as root)

macOS uses Apple Virtualization Framework. Linux uses Cloud Hypervisor + KVM. SQLite for dev, PostgreSQL + Raft for production HA. Auto-TLS on all connections.

## Documentation

**[vyprai.github.io/loka](https://vyprai.github.io/loka)**

## License

Apache 2.0
