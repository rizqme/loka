# Firecracker VM Configuration

## VM Config

```json
{
  "boot-source": {
    "kernel_image_path": "/var/lib/loka/kernels/vmlinux-5.10",
    "boot_args": "console=ttyS0 reboot=k panic=1 pci=off init=/usr/bin/supervisor"
  },
  "drives": [
    {
      "drive_id": "rootfs",
      "path_on_host": "/var/lib/loka/images/<image-id>/rootfs.ext4",
      "is_root_device": true,
      "is_read_only": true
    },
    {
      "drive_id": "overlay",
      "path_on_host": "/var/lib/loka/sessions/<session-id>/overlay.ext4",
      "is_root_device": false,
      "is_read_only": false
    }
  ],
  "machine-config": {
    "vcpu_count": 2,
    "mem_size_mib": 512
  },
  "vsock": {
    "guest_cid": 3,
    "uds_path": "/var/lib/loka/sessions/<session-id>/vsock.sock"
  }
}
```

## Boot Arguments

| Argument | Purpose |
|---|---|
| `init=/usr/bin/supervisor` | Supervisor starts as PID 1 |
| `reboot=k` | Kernel halts on reboot (no restart loop) |
| `panic=1` | Reboot after 1s on kernel panic |
| `pci=off` | Disable PCI bus (not needed, saves boot time) |
| `console=ttyS0` | Serial console for log capture |

## Snapshot Types

| Type | Contents | Use Case |
|---|---|---|
| **Warm** | VM memory only | Fast restore to a running state, no filesystem delta |
| **Session** | Overlay disk only | Lightweight filesystem checkpoint, re-boots kernel |
| **Full** | Memory + overlay + device state | Complete point-in-time restore |

### Snapshot Flow

```
1. Pause VM        (PATCH /vm  {"state": "Paused"})
2. Create snapshot (PUT  /snapshot/create {"snapshot_type": "Full", ...})
3. Copy artifacts  (memory_file, snapshot_file, overlay.ext4)
4. Resume VM       (PATCH /vm  {"state": "Resumed"})
```

Restore reverses the process: load snapshot artifacts, then resume.

## Docker Image to Rootfs Conversion

```
docker pull <image>
    |
    v
docker export (flatten to tar)
    |
    v
mkfs.ext4 + mount + extract tar into mount
    |
    v
inject /usr/bin/supervisor (static binary)
    |
    v
umount --> rootfs.ext4
```

The resulting `rootfs.ext4` is content-addressed by image digest and shared read-only across all sessions using that image.

## Kernel Requirements

- Linux 5.10+ (LTS recommended).
- Config: `CONFIG_VIRTIO_BLK`, `CONFIG_VIRTIO_NET`, `CONFIG_VSOCK`, `CONFIG_EXT4_FS`, `CONFIG_OVERLAY_FS`.
- Minimal config (~30s boot to supervisor ready with stripped kernel).
