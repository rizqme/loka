#!/usr/bin/env bash
# Build an elastic Alpine rootfs for lokavm. No Docker required.
#
# Just: download Alpine minirootfs tarball + inject LOKA binaries.
# Needs: mkfs.ext4, sudo (for loop mount), curl, Go (for cross-compile)
#
# Output: build/loka-rootfs.ext4 (sparse 50GB, ~30MB actual)

set -euo pipefail

OUT="${1:-build/loka-rootfs.ext4}"
ARCH=$(uname -m)
case "$ARCH" in
  arm64|aarch64) ARCH=aarch64; GOARCH=arm64 ;;
  x86_64)        ARCH=x86_64;  GOARCH=amd64 ;;
  *) echo "Unsupported: $ARCH"; exit 1 ;;
esac

ALPINE_VER="3.21"
ALPINE_REL="3.21.3"
ALPINE_URL="https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VER}/releases/${ARCH}/alpine-minirootfs-${ALPINE_REL}-${ARCH}.tar.gz"
FC_VER="v1.10.1"
FC_URL="https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VER}/firecracker-${FC_VER}-${ARCH}.tgz"
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/ci-artifacts/kernels/${ARCH}/vmlinux-5.10.bin"

mkdir -p "$(dirname "$OUT")" build/cache

# ── Step 1: Cross-compile Linux binaries ─────────────────

echo "==> Cross-compiling Linux binaries (${GOARCH})"
BIN="build/linux-${GOARCH}"
mkdir -p "$BIN"
for cmd in lokad loka-worker loka-supervisor loka-vmagent; do
  [ -f "$BIN/$cmd" ] && continue
  echo "    $cmd"
  CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" go build -trimpath -ldflags "-s -w" -o "$BIN/$cmd" "./cmd/$cmd" 2>/dev/null || \
  GOOS=linux GOARCH="$GOARCH" go build -trimpath -ldflags "-s -w" -o "$BIN/$cmd" "./cmd/$cmd"
done

# ── Step 2: Download dependencies (cached) ───────────────

echo "==> Downloading dependencies"

# Alpine minirootfs
ALPINE_TAR="build/cache/alpine-${ARCH}.tar.gz"
if [ ! -f "$ALPINE_TAR" ]; then
  echo "    Alpine minirootfs..."
  curl -fsSL "$ALPINE_URL" -o "$ALPINE_TAR"
fi

# Firecracker binary
FC_BIN="build/cache/firecracker-${ARCH}"
if [ ! -f "$FC_BIN" ]; then
  echo "    Firecracker ${FC_VER}..."
  curl -fsSL "$FC_URL" | tar -xzf - -C build/cache
  cp "build/cache/release-${FC_VER}-${ARCH}/firecracker-${FC_VER}-${ARCH}" "$FC_BIN"
  rm -rf "build/cache/release-${FC_VER}-${ARCH}"
fi

# Kernel
KERNEL="build/vmlinux"
if [ ! -f "$KERNEL" ]; then
  echo "    Kernel (vmlinux-5.10)..."
  curl -fsSL "$KERNEL_URL" -o "$KERNEL"
fi

# ── Step 3: Create sparse ext4 image ─────────────────────

echo "==> Creating rootfs (50GB sparse)"
rm -f "$OUT"
truncate -s 50G "$OUT"
mkfs.ext4 -F -q "$OUT"

# ── Step 4: Populate (sudo mount) ────────────────────────

echo "==> Populating rootfs"
MNT=$(mktemp -d)
sudo mount -o loop "$OUT" "$MNT"

# Extract Alpine base
sudo tar xzf "$ALPINE_TAR" -C "$MNT"

# Copy DNS for package install
sudo cp /etc/resolv.conf "$MNT/etc/resolv.conf" 2>/dev/null || true

# Install packages via chroot (apk is in the minirootfs)
sudo chroot "$MNT" /sbin/apk add --no-cache \
  openrc busybox-initscripts e2fsprogs iproute2 iptables util-linux 2>&1 | tail -1

# Inject LOKA binaries
sudo mkdir -p "$MNT/usr/local/bin"
for bin in lokad loka-worker loka-supervisor loka-vmagent; do
  [ -f "$BIN/$bin" ] && sudo cp "$BIN/$bin" "$MNT/usr/local/bin/$bin"
done
sudo cp "$FC_BIN" "$MNT/usr/local/bin/firecracker"
sudo chmod +x "$MNT/usr/local/bin/"*

# Kernel for Firecracker microVMs inside the VM
sudo mkdir -p "$MNT/tmp/loka-data/kernel" "$MNT/tmp/loka-data/rootfs" "$MNT/tmp/loka-data/objstore"
sudo cp "$KERNEL" "$MNT/tmp/loka-data/kernel/vmlinux"

# lokad init script
cat << 'EOF' | sudo tee "$MNT/etc/init.d/lokad" > /dev/null
#!/sbin/openrc-run
command="/usr/local/bin/lokad"
command_background=true
pidfile="/run/lokad.pid"
output_log="/var/log/lokad.log"
error_log="/var/log/lokad.log"
EOF
sudo chmod +x "$MNT/etc/init.d/lokad"

# Enable services
sudo chroot "$MNT" rc-update add devfs sysinit 2>/dev/null
sudo chroot "$MNT" rc-update add procfs boot 2>/dev/null
sudo chroot "$MNT" rc-update add sysfs boot 2>/dev/null
sudo chroot "$MNT" rc-update add networking boot 2>/dev/null
sudo chroot "$MNT" rc-update add lokad default 2>/dev/null

# Network
printf 'auto lo\niface lo inet loopback\nauto eth0\niface eth0 inet dhcp\n' | sudo tee "$MNT/etc/network/interfaces" > /dev/null

# fstab + hostname
echo '/dev/vda / ext4 defaults,discard 0 1' | sudo tee "$MNT/etc/fstab" > /dev/null
echo 'loka' | sudo tee "$MNT/etc/hostname" > /dev/null

# fstrim cron
sudo mkdir -p "$MNT/etc/crontabs"
echo '*/5 * * * * fstrim / 2>/dev/null' | sudo tee "$MNT/etc/crontabs/root" > /dev/null

# Clean up resolv.conf (will be set by DHCP at boot)
sudo rm -f "$MNT/etc/resolv.conf"

sudo umount "$MNT"
rmdir "$MNT"

# ── Done ─────────────────────────────────────────────────

ACTUAL=$(du -h "$OUT" | awk '{print $1}')
echo ""
echo "==> Done"
echo "    Rootfs: $OUT (50GB virtual, ${ACTUAL} actual)"
echo "    Kernel: $KERNEL"
echo ""
echo "    Run: ./bin/lokavm --data-dir build/"
