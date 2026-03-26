#!/usr/bin/env bash
# Build a custom Linux kernel for Apple Virtualization Framework.
#
# The kernel has virtio-pci + virtio-console + virtio-blk + virtio-net
# compiled as built-in (not modules) for VZ compatibility.
#
# Output: build/vmlinux-vz (uncompressed ARM64 Image)
# Requires: Docker (for cross-compilation)
#
# Or: run on a Linux ARM64 machine directly.

set -euo pipefail

KERNEL_VERSION="${1:-6.1.100}"
KERNEL_MAJOR=$(echo "$KERNEL_VERSION" | cut -d. -f1)
OUT="build/vmlinux-vz"
ARCH="arm64"

mkdir -p build

echo "==> Building Linux ${KERNEL_VERSION} kernel for Apple VZ (${ARCH})"

# Check if we can use Docker for cross-compilation
if command -v docker &>/dev/null; then
  echo "    Using Docker for cross-compilation"

  docker run --rm --platform linux/arm64 \
    -v "$(pwd)/build:/out" \
    alpine:3.21 sh -c "
    set -e
    apk add --no-cache build-base flex bison bc perl linux-headers openssl-dev elfutils-dev 2>&1 | tail -1

    cd /tmp
    echo '==> Downloading kernel source...'
    wget -q 'https://cdn.kernel.org/pub/linux/kernel/v${KERNEL_MAJOR}.x/linux-${KERNEL_VERSION}.tar.xz'
    tar xf linux-${KERNEL_VERSION}.tar.xz
    cd linux-${KERNEL_VERSION}

    echo '==> Configuring kernel for Apple VZ...'
    # Start with a minimal config
    make ARCH=${ARCH} tinyconfig

    # Enable required features for Apple VZ
    cat >> .config << 'KCONFIG'
# Core
CONFIG_64BIT=y
CONFIG_SMP=y
CONFIG_PRINTK=y
CONFIG_BLK_DEV=y
CONFIG_BLOCK=y
CONFIG_NET=y
CONFIG_INET=y

# PCI (required: VZ uses virtio-pci, not virtio-mmio)
CONFIG_PCI=y
CONFIG_PCI_HOST_GENERIC=y

# Virtio (all built-in, not modules)
CONFIG_VIRTIO=y
CONFIG_VIRTIO_PCI=y
CONFIG_VIRTIO_BLK=y
CONFIG_VIRTIO_NET=y
CONFIG_VIRTIO_CONSOLE=y
CONFIG_HVC_DRIVER=y
CONFIG_VIRTIO_BALLOON=y
CONFIG_VIRTIO_VSOCK=y
CONFIG_VHOST_VSOCK=y

# Filesystem
CONFIG_EXT4_FS=y
CONFIG_PROC_FS=y
CONFIG_SYSFS=y
CONFIG_DEVTMPFS=y
CONFIG_DEVTMPFS_MOUNT=y
CONFIG_TMPFS=y

# TTY/Console
CONFIG_TTY=y
CONFIG_VT=y
CONFIG_SERIAL_8250=y
CONFIG_SERIAL_8250_CONSOLE=y

# Networking
CONFIG_PACKET=y
CONFIG_UNIX=y
CONFIG_NETDEVICES=y

# Init
CONFIG_BINFMT_ELF=y
CONFIG_BINFMT_SCRIPT=y

# cgroups (for containers)
CONFIG_CGROUPS=y
CONFIG_CGROUP_DEVICE=y
CONFIG_CGROUP_CPUACCT=y
CONFIG_CGROUP_PIDS=y
CONFIG_MEMCG=y

# KVM (for nested Firecracker)
CONFIG_KVM=y
CONFIG_KVM_ARM_HOST=y

# Overlay FS (for layered images)
CONFIG_OVERLAY_FS=y

# FUSE (for volume mounts)
CONFIG_FUSE_FS=y

# Loop devices (for ext4 images)
CONFIG_BLK_DEV_LOOP=y

# Network filtering (for Firecracker TAP)
CONFIG_NETFILTER=y
CONFIG_IP_NF_IPTABLES=y
CONFIG_IP_NF_NAT=y
CONFIG_IP_NF_FILTER=y
CONFIG_NF_CONNTRACK=y
CONFIG_NF_NAT=y
CONFIG_BRIDGE=y
CONFIG_TUN=y
CONFIG_MACVTAP=y

# DHCP client support
CONFIG_IP_PNP=y
CONFIG_IP_PNP_DHCP=y
KCONFIG

    # Merge config
    make ARCH=${ARCH} olddefconfig

    echo '==> Building kernel...'
    make ARCH=${ARCH} -j\$(nproc) Image 2>&1 | tail -3

    echo '==> Copying output...'
    cp arch/${ARCH}/boot/Image /out/vmlinux-vz
    echo 'DONE'
  "

elif [ "$(uname -m)" = "aarch64" ] && [ "$(uname -s)" = "Linux" ]; then
  echo "    Building natively on Linux ARM64"
  # Native build on Linux ARM64
  cd /tmp
  wget -q "https://cdn.kernel.org/pub/linux/kernel/v${KERNEL_MAJOR}.x/linux-${KERNEL_VERSION}.tar.xz"
  tar xf "linux-${KERNEL_VERSION}.tar.xz"
  cd "linux-${KERNEL_VERSION}"
  # Same config as above...
  # (would duplicate the config block — in practice use Docker)
  echo "Native build not yet implemented in this script. Use Docker."
  exit 1
else
  echo "Error: Need Docker or Linux ARM64 to build the kernel."
  echo "Install Docker Desktop: https://www.docker.com/products/docker-desktop/"
  exit 1
fi

ls -lh "$OUT"
file "$OUT"
echo ""
echo "==> Kernel ready: $OUT"
echo "    Copy to ~/.loka/vm/vmlinuz-vz to use with lokavm"
