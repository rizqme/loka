#!/usr/bin/env bash
# Setup Lima VM on macOS for LOKA development with Firecracker.
# Lima provides a Linux VM with KVM support on Apple Silicon/Intel Macs.
set -euo pipefail

echo "==> Setting up Lima for LOKA development..."

# Check if Lima is installed.
if ! command -v limactl &>/dev/null; then
  echo "    Installing Lima via Homebrew..."
  brew install lima
fi

# Create LOKA Lima instance config.
LIMA_CONFIG=$(mktemp)
cat > "$LIMA_CONFIG" <<'YAML'
# LOKA Development VM
# Provides Linux with KVM for Firecracker microVMs.

images:
  - location: "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-amd64.img"
    arch: "x86_64"
  - location: "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-arm64.img"
    arch: "aarch64"

cpus: 4
memory: "8GiB"
disk: "50GiB"

# Mount the LOKA source code.
mounts:
  - location: "~"
    writable: true

# Enable KVM for Firecracker.
firmware:
  legacyBIOS: false

provision:
  - mode: system
    script: |
      #!/bin/bash
      set -eux

      # Enable KVM.
      apt-get update -q
      apt-get install -y -q qemu-kvm
      chmod 666 /dev/kvm

      # Install Go.
      GO_VERSION=1.24.0
      curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz" | tar -C /usr/local -xz
      echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile.d/golang.sh

      # Install Docker.
      curl -fsSL https://get.docker.com | sh
      usermod -aG docker "${LIMA_CIDATA_USER}"

      echo "LOKA development environment ready."
YAML

echo "    Creating Lima instance 'loka'..."
limactl create --name=loka "$LIMA_CONFIG" 2>/dev/null || true
rm "$LIMA_CONFIG"

echo "    Starting Lima instance..."
limactl start loka

echo ""
echo "==> Lima VM ready!"
echo ""
echo "Usage:"
echo "  lima bash                            # Enter the VM"
echo "  lima go build ./...                  # Build inside the VM"
echo "  lima make fetch-firecracker          # Fetch Firecracker binary"
echo "  lima make build-rootfs               # Build guest rootfs"
echo "  lima ./bin/lokad                     # Run LOKA"
echo ""
echo "The LOKA source is mounted at ~/Workspace/loka inside the VM."
echo "KVM is available at /dev/kvm."
