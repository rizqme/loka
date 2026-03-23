#!/usr/bin/env bash
# Fetch the Firecracker binary and kernel image.
set -euo pipefail

FC_VERSION="${FC_VERSION:-v1.10.1}"
ARCH="${ARCH:-$(uname -m)}"
DEST="${DEST:-/usr/local/bin}"
KERNEL_DEST="${KERNEL_DEST:-/tmp/loka-data/artifacts/kernel}"

echo "==> Fetching Firecracker ${FC_VERSION} for ${ARCH}..."

# Map architecture.
case "$ARCH" in
  x86_64|amd64) FC_ARCH="x86_64" ;;
  aarch64|arm64) FC_ARCH="aarch64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

# Download Firecracker binary.
FC_URL="https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VERSION}/firecracker-${FC_VERSION}-${FC_ARCH}.tgz"
echo "    Downloading from ${FC_URL}..."
TMP=$(mktemp -d)
curl -fsSL "$FC_URL" | tar -xz -C "$TMP"
sudo cp "$TMP/release-${FC_VERSION}-${FC_ARCH}/firecracker-${FC_VERSION}-${FC_ARCH}" "${DEST}/firecracker"
sudo chmod +x "${DEST}/firecracker"
rm -rf "$TMP"
echo "    Installed: ${DEST}/firecracker"

# Download kernel.
mkdir -p "$KERNEL_DEST"
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/ci-artifacts/kernels/${FC_ARCH}/vmlinux-5.10.217"
echo "    Downloading kernel from ${KERNEL_URL}..."
curl -fsSL "$KERNEL_URL" -o "${KERNEL_DEST}/vmlinux"
echo "    Installed: ${KERNEL_DEST}/vmlinux"

echo "==> Done. Firecracker ${FC_VERSION} ready."
echo ""
echo "Next: run 'make build-rootfs' to create the guest filesystem image."
