# Policy Structure

The policy defines what commands a session can run and under what constraints.

## PolicyObject Schema

```json
{
  "allowed_commands": ["python3", "pip", "node"],
  "blocked_commands": ["curl", "wget", "nc", "ncat"],
  "mode_restrictions": {
    "auto": {"allowed_commands": ["python3"]},
    "supervised": {},
    "locked": {"allowed_commands": []}
  },
  "max_parallel": 4,
  "max_duration_seconds": 300,
  "network_policy": { "...": "see network-policy.md" },
  "filesystem_policy": { "...": "see filesystem-policy.md" }
}
```

## Fields

| Field | Type | Description |
|---|---|---|
| `allowed_commands` | `string[]` | Binary names that always proceed without approval |
| `blocked_commands` | `string[]` | Binary names that are always rejected |
| `mode_restrictions` | `map[mode]PolicyOverride` | Per-mode overrides to allowed/blocked lists |
| `max_parallel` | `int` | Maximum concurrent commands per session |
| `max_duration_seconds` | `int` | Hard timeout for any single command |
| `network_policy` | `NetworkPolicy` | Outbound/inbound network rules |
| `filesystem_policy` | `FilesystemPolicy` | Path-level read/write/create restrictions |

## Verdict Logic

Every command is evaluated to one of three verdicts:

| Verdict | Condition | Result |
|---|---|---|
| `allowed` | Binary is in `allowed_commands` (after mode overlay) | Runs immediately |
| `blocked` | Binary is in `blocked_commands` (after mode overlay) | Rejected with error |
| `needs_approval` | Binary is in neither list | Depends on session mode |

### Mode Behavior for `needs_approval`

| Mode | Action |
|---|---|
| `auto` | Command runs immediately |
| `supervised` | Command enters `pending_approval` queue |
| `locked` | Command is rejected |

## Evaluation Order

1. Check `blocked_commands` (mode-specific overlay first, then base). If matched, verdict is `blocked`.
2. Check `allowed_commands` (mode-specific overlay first, then base). If matched, verdict is `allowed`.
3. Otherwise, verdict is `needs_approval`.

## Dynamic Updates

Policy can be replaced at runtime via `set_policy` on the supervisor. Changes take effect for the next command; in-flight commands are not affected.
