# Deploy to DigitalOcean

Run LOKA on DigitalOcean Droplets.

## Quick deploy

```bash
loka deploy digitalocean --region nyc1 --workers 2
```

## Manual setup

Use premium CPU droplets for KVM support:

```bash
doctl compute droplet create loka-worker-1 \
  --region nyc1 \
  --size s-8vcpu-16gb \
  --image ubuntu-22-04-x64

# SSH in and install
curl -fsSL https://rizqme.github.io/loka/install.sh | bash
```

<div class="warning"><strong>Note:</strong> Not all DigitalOcean droplets expose KVM. Check that <code>/dev/kvm</code> exists after provisioning.</div>
