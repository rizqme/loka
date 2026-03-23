# Production Hardening Checklist

## VM Isolation

| Item | Detail |
|---|---|
| Firecracker jailer | Run every VM under the jailer with `--uid`, `--gid`, `--chroot-base-dir` |
| Minimal kernel | Custom vmlinux with only required drivers (~8 MB). No modules, no debugfs |
| Rootfs read-only | Base rootfs mounted RO via Firecracker drive config |
| No `CAP_SYS_ADMIN` | Drop all capabilities. Supervisor runs without any caps |
| seccomp on Firecracker | Jailer applies a host-side seccomp filter to the Firecracker process itself |
| Overlay filesystem | Writes isolated to per-session overlay. Base image never modified |
| tmpfs for `/tmp` | Mount `/tmp` as tmpfs with size limit (default 64 MB) |
| Minimal `/dev` | Only `/dev/null`, `/dev/zero`, `/dev/urandom`, `/dev/vda`, `/dev/vdb` |

## Network

| Item | Detail |
|---|---|
| iptables default deny | All outbound/inbound denied unless explicitly allowed |
| DNS proxy | Queries filtered by policy. No direct DNS to external resolvers |
| Metadata service block | `169.254.169.254` blocked in all profiles |
| No host network | VMs use TAP devices with isolated network namespaces |

## Resource Limits

| Item | Default | Detail |
|---|---|---|
| cgroups v2 | enabled | Memory, CPU, PIDs, I/O limits per VM |
| OOM killer | enabled | `memory.oom.group = 1` on the VM cgroup |
| Disk quota | 2 GB | Overlay ext4 created with fixed size |
| PID limit | 128 | `pids.max` in cgroup |
| File descriptors | 256 | `RLIMIT_NOFILE` |

## Observability

| Item | Detail |
|---|---|
| Audit log | Every command logged with binary, args, verdict, exit code, duration |
| Metrics | Prometheus endpoint with session, command, checkpoint, and resource counters |
| Alerts | Alert on: VM escape attempt (blocked syscall), OOM kill, heartbeat loss, checkpoint failure |
| Structured logs | JSON logs from supervisor streamed to host. Correlation via `session_id` and `command_id` |

## Host Security

| Item | Detail |
|---|---|
| Dedicated user | Firecracker runs as unprivileged user `loka-worker` |
| SELinux/AppArmor | Confine worker process. Deny `ptrace`, `mount` on host |
| Automatic updates | Kernel and Firecracker binary updated on a regular cadence |
| Disk encryption | Session directories on encrypted volumes (LUKS) |
| mTLS | Control plane to worker communication uses mutual TLS |
