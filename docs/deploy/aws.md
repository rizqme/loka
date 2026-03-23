# Deploy to AWS

Run LOKA on AWS with EC2 bare-metal instances for KVM support.

## Quick deploy

```bash
loka deploy aws --region us-east-1 --workers 3
```

## Architecture

```
VPC (us-east-1)
├── Control Plane: 1x t3.medium
│   └── lokad + PostgreSQL + Redis
└── Workers: 3x i3.metal
    └── loka-worker + Firecracker VMs
```

## Manual setup

### 1. Control plane instance

Launch a `t3.medium` (or larger) instance with Ubuntu 22.04.

```bash
# On the control plane instance:
curl -fsSL https://rizqme.github.io/loka/install.sh | bash

# Edit config for production:
cat > /etc/loka/controlplane.yaml << YAML
mode: ha
listen_addr: ":8080"
database:
  driver: postgres
  dsn: "postgres://loka:pass@localhost:5432/loka"
coordinator:
  type: redis
  address: "localhost:6379"
objectstore:
  type: s3
  bucket: "my-loka-artifacts"
  region: "us-east-1"
YAML

lokad
```

### 2. Worker instances

Launch `i3.metal` instances (bare-metal with KVM). Each worker runs Firecracker VMs.

```bash
# On each worker:
curl -fsSL https://rizqme.github.io/loka/install.sh | bash

# Create a registration token on the control plane:
loka token create --name "worker-1"
# → loka_a2d9fb8f...

# Start the worker with the token:
cat > /etc/loka/worker.yaml << YAML
control_plane:
  address: "<control-plane-ip>:9090"
data_dir: /var/loka/worker
provider: aws
token: "loka_a2d9fb8f..."
YAML

loka-worker
```

### 3. Verify

```bash
loka deploy status
loka worker list
```

## Instance types

| Instance | Use case |
|----------|----------|
| `i3.metal` | Best for Firecracker — bare-metal KVM, NVMe storage |
| `m5.metal` | Alternative bare-metal |
| `c5.metal` | CPU-heavy workloads |

<div class="info"><strong>Why bare-metal?</strong> Firecracker requires KVM. Regular EC2 instances don't expose /dev/kvm. Bare-metal (.metal) instances do.</div>

## Security groups

| Rule | Port | Source |
|------|------|--------|
| Control plane API | 8080 | Your IP / agent IPs |
| Worker gRPC | 9090 | Control plane SG |
| SSH | 22 | Your IP |
