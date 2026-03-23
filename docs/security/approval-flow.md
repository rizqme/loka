# Approval Flow

In **ask** mode, every command is suspended until the agent explicitly approves or rejects it.

## How It Works

```
Agent runs command → Proxy: "not whitelisted" → Gate: SUSPEND
    ↓
Command is paused in-process, waiting for decision
    ↓
Agent calls approve or reject
    ↓
Approve → command resumes and runs
Reject  → command aborts with error
```

## Three Approval Scopes

| Scope | Meaning | API |
|-------|---------|-----|
| **once** | Approve this one execution only. Next time the same command runs, it needs approval again. | `{"scope": "once"}` |
| **command** | Approve this binary for the rest of the session. Future runs of the same command proceed without asking. | `{"scope": "command"}` |
| **always** | Permanently whitelist the command in the session's policy. Persisted across checkpoints. | `{"scope": "always"}` |

## SDK Usage

<!-- tabs:start -->

#### **Python**

```python
session = client.create_session(image="ubuntu:22.04", mode="ask")

# Run a command — it will suspend and wait for approval
ex = client.run(session.ID, "wget", ["http://example.com/data.csv"])
print(ex.Status)  # "pending_approval"

# Option 1: Approve once
client.approve_execution(session.ID, ex.ID, scope="once")

# Option 2: Approve this command for the session
client.approve_execution(session.ID, ex.ID, scope="command")

# Option 3: Permanently whitelist
client.approve_execution(session.ID, ex.ID, scope="always")

# Reject
client.reject_execution(session.ID, ex.ID, reason="not safe")
```

#### **TypeScript**

```typescript
const session = await loka.createSession({ image: 'ubuntu:22.04', mode: 'ask' });

const ex = await loka.run(session.ID, { command: 'wget', args: ['http://example.com/data.csv'] });
console.log(ex.Status); // "pending_approval"

// Approve once / command / always
await loka.approveExecution(session.ID, ex.ID, 'once');
await loka.approveExecution(session.ID, ex.ID, 'command');
await loka.approveExecution(session.ID, ex.ID, 'always');

// Reject
await loka.rejectExecution(session.ID, ex.ID, 'not safe');
```

<!-- tabs:end -->

## Manage the Whitelist Directly

You can also manage the command whitelist without going through the approval flow:

<!-- tabs:start -->

#### **Python**

```python
# View current whitelist
wl = client.whitelist(session.ID)
print(wl["allowed_commands"])  # ["python3", "ls", ...]
print(wl["blocked_commands"])  # ["rm", "dd"]

# Add commands
client.update_whitelist(session.ID, add=["wget", "curl"])

# Block commands
client.update_whitelist(session.ID, block=["nc", "nmap"])

# Remove from whitelist
client.update_whitelist(session.ID, remove=["wget"])
```

#### **TypeScript**

```typescript
const wl = await loka.getWhitelist(session.ID);
console.log(wl.allowed_commands);

await loka.updateWhitelist(session.ID, { add: ['wget', 'curl'] });
await loka.updateWhitelist(session.ID, { block: ['nc', 'nmap'] });
await loka.updateWhitelist(session.ID, { remove: ['wget'] });
```

<!-- tabs:end -->

## Streaming with Approval

When streaming, approval events appear in the stream:

<!-- tabs:start -->

#### **Python**

```python
for event in client.stream(session.ID, "wget", ["http://example.com"]):
    if event.event == "approval_required":
        print(f"Command {event.data['command']} needs approval")
        client.approve_execution(session.ID, event.data["command_id"], scope="command")
    if event.is_output:
        print(event.text, end="")
    if event.is_done:
        break
```

#### **TypeScript**

```typescript
for await (const event of loka.stream(session.ID, { command: 'wget', args: ['http://example.com'] })) {
  if (event.event === 'approval_required') {
    console.log(`${event.data.command} needs approval`);
    await loka.approveExecution(session.ID, event.data.command_id, 'command');
  }
  if (event.event === 'output') process.stdout.write(event.data.text);
  if (event.event === 'done') break;
}
```

<!-- tabs:end -->

## API Reference

### Approve

```
POST /api/v1/sessions/:id/exec/:execId/approve
{"scope": "once|command|always"}
```

### Reject

```
POST /api/v1/sessions/:id/exec/:execId/reject
{"reason": "optional explanation"}
```

### Get Whitelist

```
GET /api/v1/sessions/:id/whitelist
→ {"allowed_commands": [...], "blocked_commands": [...]}
```

### Update Whitelist

```
PUT /api/v1/sessions/:id/whitelist
{"add": ["wget"], "remove": ["curl"], "block": ["nc"]}
```
