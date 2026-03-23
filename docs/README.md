<div class="hero">
<h1>LOKA</h1>
<p>A controlled execution environment for AI agents. Run code inside Firecracker microVMs with policy over commands, network, and filesystem.</p>
</div>

## Overview

LOKA runs agent-generated code inside Firecracker microVMs. Each session is an isolated Linux environment where a supervisor enforces access control at the OS level — which binaries can execute, which network endpoints are reachable, and which files can be read or written.

Sessions start from Docker images and boot in ~28ms from a warm snapshot. The agent can checkpoint state, branch execution, and roll back to any prior point.

## Install

```bash
curl -fsSL https://rizqme.github.io/loka/install.sh | bash
```

<!-- tabs:start -->

#### **Python**

```bash
pip install loka-sdk
```

```python
from loka import LokaClient

client = LokaClient()
session = client.create_session(image="python:3.12-slim", mode="execute")

for event in client.stream(session.ID, "python3", ["-c", "print('hello')"]):
    if event.is_output:
        print(event.text, end="")
    if event.is_done:
        break

client.destroy_session(session.ID)
```

#### **TypeScript**

```bash
npm install @rizqme/loka-sdk
```

```typescript
import { LokaClient } from '@rizqme/loka-sdk';

const loka = new LokaClient();
const session = await loka.createSession({ image: 'python:3.12-slim', mode: 'execute' });

for await (const event of loka.stream(session.ID, { command: 'python3', args: ['-c', "print('hello')"] })) {
  if (event.event === 'output') process.stdout.write(event.data.text);
  if (event.event === 'done') break;
}

await loka.destroySession(session.ID);
```

<!-- tabs:end -->

## Access Control

Sessions have an exec policy that governs what the agent is allowed to do. Commands not in the whitelist are suspended at an approval gate — the calling system decides whether to allow or deny them.

```python
session = client.create_session(
    image="ubuntu:22.04",
    mode="ask",                              # Every command needs approval
    allowed_commands=["python3", "pip"],      # Whitelisted
    blocked_commands=["rm", "dd"],            # Always blocked
)

ex = client.run(session.ID, "wget", ["http://example.com"])
# Status: pending_approval

client.approve_execution(session.ID, ex.ID, scope="command")  # Allow wget for this session
```

Network and filesystem access are also policy-controlled:

| Layer | Controls | Example |
|-------|----------|---------|
| Commands | Which binaries can run | `allowed: [python3, git]`, `blocked: [rm]` |
| Network | IP, CIDR, domain, port rules | `allow *.pypi.org:443`, `deny 10.0.0.0/8` |
| Filesystem | Per-path read/write/execute | `allow /workspace/**`, `deny .env` |
| Modes | Overall session posture | `explore` (RO), `execute` (RW), `ask` (approval) |

<div class="card-grid">
<a class="card" href="#/security/command-policy">
<div class="card-title">Command Policy</div>
<div class="card-desc">Whitelist, blacklist, and approval gate for binaries.</div>
</a>
<a class="card" href="#/security/network-policy">
<div class="card-title">Network Policy</div>
<div class="card-desc">IP, CIDR, domain, and port-level rules.</div>
</a>
<a class="card" href="#/security/filesystem-policy">
<div class="card-title">Filesystem Policy</div>
<div class="card-desc">Path-level access control with mode overrides.</div>
</a>
<a class="card" href="#/security/approval-flow">
<div class="card-title">Approval Flow</div>
<div class="card-desc">Approve once, per command, or permanently.</div>
</a>
</div>

## Quickstart

<div class="card-grid">
<a class="card" href="#/quickstart/first-session">
<div class="card-title">Your First Session</div>
<div class="card-desc">Create a VM, run commands, destroy it.</div>
</a>
<a class="card" href="#/quickstart/run-commands">
<div class="card-title">Run Commands</div>
<div class="card-desc">Single, parallel, streaming.</div>
</a>
<a class="card" href="#/quickstart/checkpoints">
<div class="card-title">Checkpoints & Rollback</div>
<div class="card-desc">Snapshot, branch, undo.</div>
</a>
<a class="card" href="#/quickstart/docker-images">
<div class="card-title">Docker Images</div>
<div class="card-desc">Start from any image. Install. Snapshot. Reuse.</div>
</a>
</div>

## Architecture

```
Agent → SDK → Control Plane → Worker → Firecracker VM → Supervisor → Process
                                              │
                                    Command proxy (binary gate)
                                    Network filter (iptables)
                                    Filesystem guard (landlock)
                                    Seccomp (syscall filter)
```

The control plane manages sessions and schedules them onto workers. Each worker runs Firecracker VMs. Inside each VM, a supervisor process enforces all policies at the kernel level — not by inspecting code, but by controlling what the process can access.
