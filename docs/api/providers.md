# Providers API

Providers manage the infrastructure where worker nodes run. Loka supports 7 provider backends.

## Supported Providers

| Provider | Key | Auto-Provision | Notes |
|---|---|---|---|
| AWS | `aws` | Yes | EC2 instances, metal or nitro |
| GCP | `gcp` | Yes | Compute Engine, nested virt required |
| Azure | `azure` | Yes | VMs with nested virtualization |
| OVH | `ovh` | Yes | Bare metal servers |
| DigitalOcean | `digitalocean` | Yes | Droplets with nested virt |
| Local | `local` | No | Uses the current host directly |
| Self-Managed | `selfmanaged` | No | Operator provisions; workers register via token |

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/v1/providers` | List configured providers |
| `POST` | `/api/v1/providers/:key/provision` | Provision worker nodes |
| `GET` | `/api/v1/providers/:key/status` | Get provider status |

## List Providers

```
GET /api/v1/providers
```

```json
{
  "items": [
    {
      "key": "aws",
      "status": "configured",
      "workers_count": 3,
      "config": {"region": "us-east-1", "instance_type": "c5.metal"}
    }
  ]
}
```

## Provision

```
POST /api/v1/providers/aws/provision
```

```json
{
  "count": 2,
  "instance_type": "c5.metal",
  "region": "us-east-1",
  "labels": {"pool": "gpu"}
}
```

**Response** `202 Accepted`:

```json
{
  "provision_id": "prov_abc123",
  "status": "provisioning",
  "requested": 2
}
```

## Provider Status

```
GET /api/v1/providers/aws/status
```

```json
{
  "key": "aws",
  "status": "configured",
  "workers_online": 3,
  "workers_provisioning": 2,
  "pending_provisions": [
    {"provision_id": "prov_abc123", "status": "provisioning", "requested": 2}
  ]
}
```

## Self-Managed Flow

For `selfmanaged`, the operator provisions hardware, installs the Loka worker binary, and registers using a token:

```bash
loka worker --control-plane https://cp.example.com --token <token>
```

Tokens are created via the [Tokens API](tokens.md).
