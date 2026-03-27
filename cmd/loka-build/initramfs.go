package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/vyprai/loka/pkg/lokavm"
)

func buildInitramfs(cfg buildConfig) error {
	outputFile := filepath.Join(cfg.OutputDir, "initramfs.cpio.gz")

	// Skip if output exists.
	if info, err := os.Stat(outputFile); err == nil && info.Size() > 0 {
		cfg.Logger.Info("initramfs already exists, skipping", "path", outputFile, "size_kb", info.Size()/1024)
		cfg.Logger.Info("delete to rebuild", "path", outputFile)
		return nil
	}

	os.MkdirAll(cfg.OutputDir, 0o755)

	projectDir, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf("find project root: %w", err)
	}

	cfg.Logger.Info("building initramfs", "arch", cfg.Arch, "output", outputFile)

	// Pull build image (reuse cached).
	rootfsDir, err := pullBuildImage(cfg)
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	// Create scratch directory.
	buildDir, err := os.MkdirTemp("", "loka-initramfs-build-*")
	if err != nil {
		return fmt.Errorf("create build dir: %w", err)
	}
	defer os.RemoveAll(buildDir)

	// Write build script.
	script := generateInitramfsBuildScript(cfg)
	if err := os.WriteFile(filepath.Join(buildDir, "build.sh"), []byte(script), 0o755); err != nil {
		return fmt.Errorf("write build script: %w", err)
	}

	absOutput, _ := filepath.Abs(cfg.OutputDir)
	absProject, _ := filepath.Abs(projectDir)

	return runBuildVM(cfg, rootfsDir, []lokavm.SharedDir{
		{Tag: "workspace", HostPath: absProject, GuestPath: "/workspace", ReadOnly: true},
		{Tag: "output", HostPath: absOutput, GuestPath: "/output", ReadOnly: false},
		{Tag: "build", HostPath: buildDir, GuestPath: "/build", ReadOnly: false},
	}, "/build/build.sh")
}

func generateInitramfsBuildScript(cfg buildConfig) string {
	busyboxArch := "armv8l"
	if cfg.Arch == "amd64" || cfg.Arch == "x86_64" {
		busyboxArch = "x86_64"
	}

	return fmt.Sprintf(`#!/bin/sh
set -e

echo "==> Building initramfs..."
apt-get update -qq
apt-get install -y -qq wget cpio gzip

cd /build
INITRAMFS_DIR=/build/initramfs
rm -rf "$INITRAMFS_DIR"
mkdir -p "$INITRAMFS_DIR"/{bin,sbin,dev,proc,sys,mnt,tmp,usr/local/bin,etc,workspace}

# Download busybox static.
BUSYBOX_VER="1.36.1"
echo "==> Downloading busybox ${BUSYBOX_VER} (%s)..."
wget -q "https://busybox.net/downloads/binaries/${BUSYBOX_VER}-defconfig-multiarch-musl/busybox-%s" -O "$INITRAMFS_DIR/bin/busybox"
chmod +x "$INITRAMFS_DIR/bin/busybox"

# Create busybox symlinks.
for cmd in sh ash cat ls mkdir mount umount ln cp mv rm echo sleep mknod grep sed awk ip ifconfig route hostname; do
    ln -sf busybox "$INITRAMFS_DIR/bin/$cmd"
done

# Copy supervisor if available.
if [ -f /workspace/build/linux-%s/loka-supervisor ]; then
    cp /workspace/build/linux-%s/loka-supervisor "$INITRAMFS_DIR/usr/local/bin/loka-supervisor"
elif [ -f /workspace/bin/linux-%s/loka-supervisor ]; then
    cp /workspace/bin/linux-%s/loka-supervisor "$INITRAMFS_DIR/usr/local/bin/loka-supervisor"
fi

# Create init script.
cat > "$INITRAMFS_DIR/init" << 'INITEOF'
#!/bin/sh
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
mkdir -p /dev/pts /dev/shm
mount -t devpts devpts /dev/pts
mount -t tmpfs tmpfs /dev/shm
mount -t tmpfs tmpfs /tmp

# Create /dev/vsock for vsock communication.
mknod /dev/vsock c 10 91 2>/dev/null || true

# Parse kernel command line for virtiofs mounts.
for param in $(cat /proc/cmdline); do
    case "$param" in
        loka.virtiofs=*)
            tag_path="${param#loka.virtiofs=}"
            tag="${tag_path%%:*}"
            mpath="${tag_path#*:}"
            mkdir -p "$mpath"
            mount -t virtiofs "$tag" "$mpath" 2>/dev/null || echo "virtiofs mount failed: $tag -> $mpath"
            ;;
        loka.nlayers=*)
            nlayers="${param#loka.nlayers=}"
            # Mount overlay layers.
            lower=""
            for i in $(seq 0 $((nlayers - 1))); do
                mkdir -p "/layers/$i"
                mount -t virtiofs "layer-$i" "/layers/$i" 2>/dev/null || true
                if [ -z "$lower" ]; then lower="/layers/$i"; else lower="/layers/$i:$lower"; fi
            done
            mkdir -p /upper /work /merged
            mount -t virtiofs upper /upper 2>/dev/null || mount -t tmpfs tmpfs /upper
            mkdir -p /upper/data /upper/work
            mount -t overlay overlay -o "lowerdir=$lower,upperdir=/upper/data,workdir=/upper/work" /merged
            # Pivot to merged root.
            mkdir -p /merged/dev /merged/proc /merged/sys /merged/tmp
            exec switch_root /merged /sbin/init 2>/dev/null || exec chroot /merged /bin/sh
            ;;
    esac
done

# Network setup.
ip link set lo up 2>/dev/null || true
ip link set eth0 up 2>/dev/null || true
ip addr add 10.0.2.15/24 dev eth0 2>/dev/null || true
ip route add default via 10.0.2.2 2>/dev/null || true

# Check for loka.exec= parameter (used by loka-build to run build scripts).
for param in $(cat /proc/cmdline); do
    case "$param" in
        loka.exec=*)
            exec_path="${param#loka.exec=}"
            if [ -x "$exec_path" ]; then
                echo "loka.exec: $exec_path"
                exec "$exec_path"
            else
                echo "loka.exec: $exec_path not found or not executable"
            fi
            ;;
    esac
done

# Start supervisor if available.
if [ -x /usr/local/bin/loka-supervisor ]; then
    exec /usr/local/bin/loka-supervisor
fi

# Fallback: drop to shell.
exec /bin/sh
INITEOF
chmod +x "$INITRAMFS_DIR/init"

# Build cpio archive.
echo "==> Creating initramfs.cpio.gz..."
cd "$INITRAMFS_DIR"
find . | cpio -H newc -o 2>/dev/null | gzip -9 > /output/initramfs.cpio.gz
ls -lh /output/initramfs.cpio.gz
echo "==> Initramfs build complete"
`, busyboxArch, busyboxArch,
		cfg.Arch, cfg.Arch, cfg.Arch, cfg.Arch)
}
