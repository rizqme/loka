# Deploy to Azure

Run LOKA on Azure VMs with nested virtualization.

## Quick deploy

```bash
loka deploy azure --region eastus --workers 2
```

## Manual setup

Use `Standard_D8s_v3` or larger — these support nested virtualization.

```bash
az vm create \
  --name loka-worker-1 \
  --resource-group loka-rg \
  --image Ubuntu2204 \
  --size Standard_D8s_v3

# SSH in and install
curl -fsSL https://rizqme.github.io/loka/install.sh | bash
```

Azure VMs with Dv3/Ev3 series and newer support nested virtualization by default.
