# CLI Reference

The CLI is just `loka`. It manages sessions, runs commands, and deploys infrastructure.

```
loka [command] [subcommand] [flags]
```

## Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--server, -s` | `http://localhost:8080` | Control plane address |
| `--token, -t` | | Auth token |
| `--output, -o` | `table` | Output format: `table`, `json` |

## Commands

### Deploy

```bash
loka deploy local                              # Run locally
loka deploy aws --region us-east-1 --workers 3 # Deploy to AWS
loka deploy gcp --zone us-central1-a           # Deploy to GCP
loka deploy azure --region eastus              # Deploy to Azure
loka deploy digitalocean --region nyc1         # Deploy to DigitalOcean
loka deploy ovh                                # Deploy to OVH
loka deploy status                             # Show deployment status
loka deploy destroy                            # Tear down
```

### Sessions

```bash
loka session create --image python:3.12-slim --mode execute
loka session list
loka session get <id>
loka session destroy <id>
loka session pause <id>
loka session resume <id>
loka session mode <id> ask
```

### Commands

```bash
loka exec <session-id> -- echo "hello"
loka exec <session-id> -- python3 script.py
loka exec <session-id> --parallel --cmd "echo A" --cmd "echo B"
loka exec <session-id> --wait=false -- sleep 60
```

### Checkpoints

```bash
loka checkpoint create <session-id> --label "before-change"
loka checkpoint list <session-id>
loka checkpoint tree <session-id>
loka checkpoint restore <session-id> <checkpoint-id>
loka checkpoint diff <session-id> <cp-a> <cp-b>
```

### Workers

```bash
loka worker list
loka worker get <id>
loka worker drain <id>
loka worker undrain <id>
loka worker remove <id>
loka worker label <id> gpu=true region=us-east
loka worker top
```

### Images

```bash
loka image pull ubuntu:22.04
loka image list
```

### Tokens

```bash
loka token create --name "my-server" --expires 86400
loka token list
loka token revoke <id>
```

### Whitelist

```bash
loka session whitelist <session-id>                          # View
loka session whitelist <session-id> --add wget,curl          # Add
loka session whitelist <session-id> --block rm,dd            # Block
```

### Other

```bash
loka status                     # System overview
loka version                    # Version info
loka completion bash            # Shell completions
```
