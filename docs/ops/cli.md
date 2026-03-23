# CLI Reference

```
loka [global flags] <command> <subcommand> [flags]
```

## Global Flags

| Flag | Default | Description |
|---|---|---|
| `--api-url` | `http://localhost:8080` | Control plane URL |
| `--token` | `$LOKA_TOKEN` | API authentication token |
| `--output` | `table` | Output format: `table`, `json`, `yaml` |
| `--timeout` | `30s` | Request timeout |
| `--verbose` | `false` | Enable debug logging |

## session

| Command | Description |
|---|---|
| `loka session create --image <img> [--vcpus N] [--memory N] [--mode M]` | Create session |
| `loka session list [--status S] [--limit N]` | List sessions |
| `loka session get <id>` | Show session details |
| `loka session destroy <id>` | Destroy session |
| `loka session pause <id>` | Pause session |
| `loka session resume <id>` | Resume session |
| `loka session mode <id> <auto\|supervised\|locked>` | Change mode |

## run

| Command | Description |
|---|---|
| `loka run <session-id> -- <command> [args...]` | Run single command |
| `loka run <session-id> --parallel <file.json>` | Run batch from JSON file |
| `loka run <session-id> -- <cmd> --wait` | Run and block until completion |

## checkpoint

| Command | Description |
|---|---|
| `loka checkpoint create <session-id> [--type light\|full] [--label L]` | Create checkpoint |
| `loka checkpoint list <session-id>` | List checkpoints |
| `loka checkpoint tree <session-id>` | Show checkpoint DAG as tree |
| `loka checkpoint restore <session-id> <checkpoint-id>` | Restore checkpoint |
| `loka checkpoint diff <session-id> <cp1> <cp2>` | Diff two checkpoints |

## worker

| Command | Description |
|---|---|
| `loka worker list` | List workers |
| `loka worker get <id>` | Show worker details |
| `loka worker drain <id>` | Drain worker |
| `loka worker undrain <id>` | Undrain worker |
| `loka worker remove <id>` | Remove worker |
| `loka worker label <id> key=value [...]` | Set labels |
| `loka worker top` | Live resource utilization view |

## provider

| Command | Description |
|---|---|
| `loka provider list` | List configured providers |
| `loka provider provision <key> --count N [--instance-type T]` | Provision nodes |
| `loka provider status <key>` | Show provider status |

## token

| Command | Description |
|---|---|
| `loka token create [--label L] [--expires-in D] [--max-uses N]` | Create token |
| `loka token list` | List tokens |
| `loka token revoke <id>` | Revoke token |

## image

| Command | Description |
|---|---|
| `loka image pull <reference>` | Pull and convert Docker image |
| `loka image list` | List available images |

## Utility

| Command | Description |
|---|---|
| `loka status` | Show control plane status and summary |
| `loka version` | Print client and server versions |
