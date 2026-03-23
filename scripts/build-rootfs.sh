#!/usr/bin/env bash
# Build a minimal rootfs ext4 image with the loka-supervisor baked in.
set -euo pipefail

ROOTFS_SIZE="${ROOTFS_SIZE:-256}" # MB
ROOTFS_DEST="${ROOTFS_DEST:-/tmp/loka-data/artifacts/rootfs}"
SUPERVISOR_BIN="${SUPERVISOR_BIN:-bin/linux-amd64/loka-supervisor}"

echo "==> Building LOKA rootfs image..."

# Build the supervisor binary for Linux.
if [ ! -f "$SUPERVISOR_BIN" ]; then
  echo "    Building loka-supervisor for Linux..."
  GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o "$SUPERVISOR_BIN" ./cmd/loka-supervisor
fi

# Create rootfs using Docker (Alpine-based).
TMP=$(mktemp -d)
mkdir -p "$ROOTFS_DEST"

cat > "$TMP/Dockerfile" <<'DOCKER'
FROM alpine:3.21

# Install minimal packages.
RUN apk add --no-cache \
    busybox \
    coreutils \
    bash \
    python3 \
    git \
    curl \
    iptables \
    && rm -rf /var/cache/apk/*

# Create loka directories.
RUN mkdir -p /env/bin /workspace /tmp /var/loka

# Copy supervisor binary.
COPY loka-supervisor /usr/local/bin/loka-supervisor
RUN chmod +x /usr/local/bin/loka-supervisor

# Set PATH to only include /env/bin and /usr/local/bin.
ENV PATH="/env/bin:/usr/local/bin:/usr/bin:/bin"
DOCKER

cp "$SUPERVISOR_BIN" "$TMP/loka-supervisor"

echo "    Building Docker image..."
docker build -t loka-rootfs-builder "$TMP" -q

echo "    Extracting rootfs..."
CONTAINER_ID=$(docker create loka-rootfs-builder)
docker export "$CONTAINER_ID" > "$TMP/rootfs.tar"
docker rm "$CONTAINER_ID" >/dev/null

echo "    Creating ext4 image (${ROOTFS_SIZE}MB)..."
dd if=/dev/zero of="$ROOTFS_DEST/rootfs.ext4" bs=1M count="$ROOTFS_SIZE" 2>/dev/null
mkfs.ext4 -F "$ROOTFS_DEST/rootfs.ext4" >/dev/null 2>&1

# Mount and populate.
MOUNT_DIR="$TMP/mount"
mkdir -p "$MOUNT_DIR"

if command -v guestmount &>/dev/null; then
  # Use libguestfs if available (no root needed).
  guestmount -a "$ROOTFS_DEST/rootfs.ext4" -i "$MOUNT_DIR"
  tar -xf "$TMP/rootfs.tar" -C "$MOUNT_DIR"
  guestunmount "$MOUNT_DIR"
else
  # Fallback: use sudo mount (needs root).
  sudo mount -o loop "$ROOTFS_DEST/rootfs.ext4" "$MOUNT_DIR"
  sudo tar -xf "$TMP/rootfs.tar" -C "$MOUNT_DIR"
  sudo umount "$MOUNT_DIR"
fi

rm -rf "$TMP"

echo "==> Rootfs image created: $ROOTFS_DEST/rootfs.ext4"
echo ""
echo "To start LOKA:"
echo "  export LOKA_ROOTFS_PATH=$ROOTFS_DEST/rootfs.ext4"
echo "  lokad"
