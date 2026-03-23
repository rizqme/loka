# Deploy to OVH

Run LOKA on OVH bare-metal servers. Native KVM support, no nested virtualization needed.

## Quick deploy

```bash
loka deploy ovh --workers 2
```

## Manual setup

Order a bare-metal server (Kimsufi, So you Start, or OVH dedicated), install Ubuntu, then:

```bash
curl -fsSL https://rizqme.github.io/loka/install.sh | bash
```

OVH bare-metal servers always have KVM available.
