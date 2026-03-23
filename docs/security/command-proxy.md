# Command Proxy

The command proxy is a **binary gate**, not a code parser. It controls which programs can be spawned, not what source code they contain.

## Controls

### Binary Whitelist

Only binaries on the allow-list can be invoked. The proxy resolves the full path and checks against the list before spawning.

```json
{
  "allowed_binaries": [
    "/usr/bin/python3",
    "/usr/local/bin/pip",
    "/usr/bin/node"
  ]
}
```

### Environment Variable Filtering

The proxy strips dangerous environment variables before passing them to the child process.

| Blocked Variable | Reason |
|---|---|
| `LD_PRELOAD` | Shared library injection |
| `LD_LIBRARY_PATH` | Library search path hijack |
| `PYTHONPATH` | Python module injection |
| `NODE_OPTIONS` | Node.js runtime flag injection |
| `PERL5LIB` | Perl module injection |
| `RUBYLIB` | Ruby module injection |
| `CLASSPATH` | Java class injection |
| `BASH_ENV` | Shell startup injection |

### PATH Restriction

`PATH` is overwritten to a fixed value:

```
PATH=/usr/local/bin:/usr/bin:/bin
```

This prevents discovery of binaries outside approved directories.

### I/O Limits

| Limit | Default | Description |
|---|---|---|
| `max_stdout_bytes` | 10 MB | Truncates stdout beyond this limit |
| `max_stderr_bytes` | 10 MB | Truncates stderr beyond this limit |
| `max_open_files` | 256 | `RLIMIT_NOFILE` |
| `max_file_size_bytes` | 100 MB | `RLIMIT_FSIZE` |

### Resource Limits

| Limit | Default | Description |
|---|---|---|
| `max_pids` | 64 | Prevents fork bombs |
| `max_memory_bytes` | 256 MB | Per-command memory cap via cgroup |
| `cpu_shares` | 256 | Relative CPU weight |

## Audit Log

Every command invocation is logged:

```json
{
  "ts": "2026-03-23T10:01:00.123Z",
  "session_id": "ses_abc123",
  "command_id": "cmd_xyz789",
  "binary": "/usr/bin/python3",
  "args": ["-c", "print('hello')"],
  "verdict": "allowed",
  "exit_code": 0,
  "duration_ms": 120
}
```

Logs are written to `/var/log/supervisor/audit.jsonl` inside the VM and streamed to the host via vsock.
