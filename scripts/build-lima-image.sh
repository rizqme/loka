#!/usr/bin/env bash
# ──────────────────────────────────────────────────────────
#  Build a minimal Lima VM image for LOKA (~50MB)
#
#  Creates a qcow2 image with only what LOKA needs:
#  - Alpine base (~4MB minirootfs)
#  - OpenRC init, networking, SSH
#  - Docker, curl, iptables, e2fsprogs
#  - LOKA binaries pre-installed
#
#  Usage:
#    bash scripts/build-lima-image.sh           # build for current arch
#    ARCH=amd64 bash scripts/build-lima-image.sh
#
#  Requires: Docker
# ──────────────────────────────────────────────────────────
set -euo pipefail

ARCH="${ARCH:-$(uname -m)}"
case "$ARCH" in
  aarch64|arm64) ARCH="aarch64"; GOARCH="arm64"; PLATFORM="linux/arm64" ;;
  x86_64|amd64)  ARCH="x86_64";  GOARCH="amd64"; PLATFORM="linux/amd64" ;;
  *) echo "Unsupported arch: $ARCH"; exit 1 ;;
esac

OUT_DIR="${OUT_DIR:-./build}"
IMAGE_NAME="loka-lima-${GOARCH}.qcow2"
ALPINE_VERSION="3.21"
ALPINE_MINOR="3.21.3"
DISK_SIZE_MB=2048

echo ""
echo "  Building LOKA Lima image"
echo "  Arch: ${ARCH} (${GOARCH})"
echo "  Output: ${OUT_DIR}/${IMAGE_NAME}"
echo ""

mkdir -p "$OUT_DIR"

# ── Build LOKA binaries for Linux ────────────────────────

echo "==> Building LOKA binaries (linux/${GOARCH})"
GOOS=linux GOARCH=$GOARCH go build -trimpath -ldflags="-s -w" -o "$OUT_DIR/lokad" ./cmd/lokad 2>/dev/null
GOOS=linux GOARCH=$GOARCH go build -trimpath -ldflags="-s -w" -o "$OUT_DIR/loka-worker" ./cmd/loka-worker 2>/dev/null
GOOS=linux GOARCH=$GOARCH CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$OUT_DIR/loka-supervisor" ./cmd/loka-supervisor 2>/dev/null
echo "  Done"

# ── Build image inside privileged Docker container ───────

echo "==> Building qcow2 image in Docker"

cat > "$OUT_DIR/build-image.sh" << 'BUILDSCRIPT'
#!/bin/sh
set -eux

ARCH="$1"
ALPINE_VERSION="$2"
ALPINE_MINOR="$3"
DISK_SIZE_MB="$4"

cd /build

# Download Alpine minirootfs.
curl -fsSL "https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}/releases/${ARCH}/alpine-minirootfs-${ALPINE_MINOR}-${ARCH}.tar.gz" \
  -o minirootfs.tar.gz

# Create raw disk image.
dd if=/dev/zero of=disk.raw bs=1M count=${DISK_SIZE_MB} 2>/dev/null
mkfs.ext4 -F -L loka disk.raw >/dev/null 2>&1

# Mount and populate.
mkdir -p /mnt/rootfs
mount -o loop disk.raw /mnt/rootfs
tar xzf minirootfs.tar.gz -C /mnt/rootfs

# Set up Alpine repositories.
echo "https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}/main" > /mnt/rootfs/etc/apk/repositories
echo "https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}/community" >> /mnt/rootfs/etc/apk/repositories

# Install packages via chroot.
cp /etc/resolv.conf /mnt/rootfs/etc/resolv.conf
mount -t proc proc /mnt/rootfs/proc
mount -t sysfs sys /mnt/rootfs/sys
mount --bind /dev /mnt/rootfs/dev

chroot /mnt/rootfs /bin/sh -c '
  apk update
  apk add --no-cache \
    openrc busybox-openrc \
    openssh-server \
    dhcpcd \
    iproute2 iptables e2fsprogs \
    curl docker \
    sudo shadow
'

# Configure system.
chroot /mnt/rootfs /bin/sh -c '
  # Enable essential services.
  rc-update add devfs sysinit
  rc-update add dmesg sysinit
  rc-update add mdev sysinit
  rc-update add hwclock boot
  rc-update add modules boot
  rc-update add sysctl boot
  rc-update add hostname boot
  rc-update add bootmisc boot
  rc-update add networking boot
  rc-update add sshd default
  rc-update add docker default
  rc-update add dhcpcd default
  rc-update add local default

  # Configure networking.
  printf "auto lo\niface lo inet loopback\n\nauto eth0\niface eth0 inet dhcp\n" > /etc/network/interfaces

  # Configure SSH.
  sed -i "s/#PermitRootLogin.*/PermitRootLogin yes/" /etc/ssh/sshd_config
  sed -i "s/#PasswordAuthentication.*/PasswordAuthentication yes/" /etc/ssh/sshd_config
  ssh-keygen -A

  # Create user for Lima.
  adduser -D -s /bin/sh lima
  echo "lima:lima" | chpasswd
  echo "lima ALL=(ALL) NOPASSWD: ALL" >> /etc/sudoers
  mkdir -p /home/lima/.ssh && chmod 700 /home/lima/.ssh
  chown -R lima:lima /home/lima

  # Root password.
  echo "root:root" | chpasswd

  # KVM module.
  echo "kvm" >> /etc/modules

  # Hostname.
  echo "loka" > /etc/hostname

  # Inittab.
  printf "::sysinit:/sbin/openrc sysinit\n::sysinit:/sbin/openrc boot\n::wait:/sbin/openrc default\nttyS0::respawn:/sbin/getty 115200 ttyS0\n::ctrlaltdel:/sbin/reboot\n::shutdown:/sbin/openrc shutdown\n" > /etc/inittab

  # Cloud-init seed directory for Lima.
  mkdir -p /var/lib/cloud/seed/nocloud-net
'

# Create LOKA data dirs.
mkdir -p /mnt/rootfs/var/loka/kernel /mnt/rootfs/var/loka/artifacts /mnt/rootfs/var/loka/worker /mnt/rootfs/var/loka/raft /mnt/rootfs/var/loka/tls
mkdir -p /mnt/rootfs/tmp/loka-data/kernel /mnt/rootfs/tmp/loka-data/rootfs /mnt/rootfs/tmp/loka-data/objstore /mnt/rootfs/tmp/loka-data/worker-data

# Unmount chroot mounts.
umount /mnt/rootfs/dev /mnt/rootfs/proc /mnt/rootfs/sys

# Install LOKA binaries.
cp /build/lokad /mnt/rootfs/usr/local/bin/lokad
cp /build/loka-worker /mnt/rootfs/usr/local/bin/loka-worker
cp /build/loka-supervisor /mnt/rootfs/usr/local/bin/loka-supervisor
chmod +x /mnt/rootfs/usr/local/bin/lokad /mnt/rootfs/usr/local/bin/loka-worker /mnt/rootfs/usr/local/bin/loka-supervisor

# Cleanup caches.
rm -rf /mnt/rootfs/var/cache/apk/*

umount /mnt/rootfs

# Convert to qcow2 (compressed).
qemu-img convert -f raw -O qcow2 -c disk.raw /build/output.qcow2
echo "==> Image size: $(du -h /build/output.qcow2 | awk '{print $1}')"
BUILDSCRIPT

chmod +x "$OUT_DIR/build-image.sh"

docker run --rm --privileged \
  --platform "$PLATFORM" \
  -v "$(cd "$OUT_DIR" && pwd):/build" \
  alpine:3.21 \
  /bin/sh -c "apk add --no-cache qemu-img e2fsprogs curl tar >/dev/null 2>&1 && /build/build-image.sh $ARCH $ALPINE_VERSION $ALPINE_MINOR $DISK_SIZE_MB"

mv "$OUT_DIR/output.qcow2" "$OUT_DIR/${IMAGE_NAME}"
rm -f "$OUT_DIR/build-image.sh" "$OUT_DIR/lokad" "$OUT_DIR/loka-worker" "$OUT_DIR/loka-supervisor" "$OUT_DIR/disk.raw" "$OUT_DIR/minirootfs.tar.gz"

SIZE=$(du -h "$OUT_DIR/${IMAGE_NAME}" | awk '{print $1}')
echo ""
echo "  Image built: ${OUT_DIR}/${IMAGE_NAME} (${SIZE})"
echo ""
