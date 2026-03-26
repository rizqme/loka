#!/usr/bin/env bash
# Build a custom Linux kernel for lokavm using Lima.
#
# Creates a temporary Lima VM (Ubuntu), installs build deps, compiles the kernel
# natively on ARM64, and outputs to build/vmlinux-lokavm via volume mount.
#
# Usage:
#   make kernel
#   ./scripts/build-kernel-lokavm.sh [version]

set -euo pipefail

PINNED_VERSION="6.12.8"
KERNEL_VERSION="${1:-$PINNED_VERSION}"
KERNEL_MAJOR=$(echo "$KERNEL_VERSION" | cut -d. -f1)
OUT="build/vmlinux-lokavm"
LIMA_VM="loka-kernel-build"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

mkdir -p build

echo "==> Building Linux ${KERNEL_VERSION} kernel for lokavm (arm64)"

if ! command -v limactl &>/dev/null; then
  echo "Error: Lima required. Install: brew install lima"
  exit 1
fi

# Clean up any previous build VM.
limactl stop "$LIMA_VM" 2>/dev/null || true
limactl delete "$LIMA_VM" 2>/dev/null || true

# Lima config using default Ubuntu template (has guest agent, SSH works).
LIMA_CONFIG="${PROJECT_DIR}/build/lima-kernel-build.yaml"
cat > "$LIMA_CONFIG" << YAML
images:
  - location: "https://cloud-images.ubuntu.com/releases/24.04/release/ubuntu-24.04-server-cloudimg-arm64.img"
    arch: "aarch64"
cpus: $(sysctl -n hw.ncpu 2>/dev/null || echo 4)
memory: "4GiB"
disk: "15GiB"
mounts:
  - location: "$PROJECT_DIR"
    writable: true
YAML

echo "    Creating + starting Lima VM..."
limactl create --name "$LIMA_VM" "$LIMA_CONFIG" --tty=false 2>&1 | tail -1
limactl start "$LIMA_VM" 2>&1 | tail -1

echo "    Installing build dependencies..."
limactl shell "$LIMA_VM" -- sudo apt-get update -qq 2>&1 | tail -1
limactl shell "$LIMA_VM" -- sudo apt-get install -y -qq build-essential flex bison bc libelf-dev libssl-dev wget 2>&1 | tail -1

echo "    Building kernel (5-15 min)..."
limactl shell "$LIMA_VM" -- bash -c "
  set -e
  cd /tmp

  if [ ! -f linux-${KERNEL_VERSION}.tar.xz ]; then
    echo '==> Downloading kernel source...'
    wget -q 'https://cdn.kernel.org/pub/linux/kernel/v${KERNEL_MAJOR}.x/linux-${KERNEL_VERSION}.tar.xz'
  fi

  rm -rf linux-${KERNEL_VERSION}
  tar xf linux-${KERNEL_VERSION}.tar.xz
  cd linux-${KERNEL_VERSION}

  echo '==> Configuring...'
  make ARCH=arm64 defconfig

  cat >> .config << 'KCONFIG'
CONFIG_VIRTIO=y
CONFIG_VIRTIO_PCI=y
CONFIG_VIRTIO_BLK=y
CONFIG_VIRTIO_NET=y
CONFIG_VIRTIO_CONSOLE=y
CONFIG_HVC_DRIVER=y
CONFIG_VIRTIO_BALLOON=y
CONFIG_VIRTIO_VSOCK=y
CONFIG_VIRTIO_FS=y
CONFIG_OVERLAY_FS=y
CONFIG_FUSE_FS=y
CONFIG_NFS_FS=y
CONFIG_NFS_V4=y
CONFIG_SUNRPC=y
CONFIG_NETFILTER=y
CONFIG_IP_NF_IPTABLES=y
CONFIG_IP_NF_NAT=y
CONFIG_IP_NF_FILTER=y
CONFIG_NF_CONNTRACK=y
CONFIG_NF_NAT=y
CONFIG_BRIDGE=y
CONFIG_TUN=y
CONFIG_IP_PNP=y
CONFIG_IP_PNP_DHCP=y
CONFIG_CGROUPS=y
CONFIG_CGROUP_DEVICE=y
CONFIG_CGROUP_CPUACCT=y
CONFIG_CGROUP_PIDS=y
CONFIG_MEMCG=y
CONFIG_HOTPLUG_CPU=y
CONFIG_BLK_DEV_LOOP=y
KCONFIG

  make ARCH=arm64 olddefconfig
  echo '==> Compiling...'
  make ARCH=arm64 -j\$(nproc) Image 2>&1 | tail -3

  echo '==> Copying to workspace...'
  cp arch/arm64/boot/Image '${PROJECT_DIR}/build/vmlinux-lokavm'
  echo 'DONE'
"

echo "    Cleaning up..."
limactl stop "$LIMA_VM" 2>/dev/null || true
limactl delete "$LIMA_VM" 2>/dev/null || true
rm -f "$LIMA_CONFIG"

ls -lh "$OUT"
file "$OUT"
echo ""
echo "==> Kernel ready: $OUT (Linux ${KERNEL_VERSION}, arm64)"
