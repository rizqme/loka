# Checkpoints API

Checkpoints capture session state as overlay-based snapshots. They form a directed acyclic graph (DAG) where each checkpoint references its parent.

## Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/sessions/:id/checkpoints` | Create checkpoint |
| `GET` | `/api/v1/sessions/:id/checkpoints` | List checkpoint DAG |
| `POST` | `/api/v1/sessions/:id/checkpoints/:cp_id/restore` | Restore to checkpoint |
| `DELETE` | `/api/v1/sessions/:id/checkpoints/:cp_id` | Delete checkpoint and subtree |
| `GET` | `/api/v1/sessions/:id/checkpoints/diff` | Diff two checkpoints |

## Create Checkpoint

```
POST /api/v1/sessions/ses_abc123/checkpoints
```

```json
{
  "type": "light",
  "label": "after-install"
}
```

| Type | Contents | Typical Size | Downtime |
|---|---|---|---|
| `light` | Overlay delta only | 1-50 MB | <100ms pause |
| `full` | Overlay + VM memory + device state | 50-600 MB | 200-500ms pause |

**Response** `201 Created`:

```json
{
  "id": "cp_001",
  "parent_id": null,
  "type": "light",
  "label": "after-install",
  "size_bytes": 4218880,
  "created_at": "2026-03-23T10:05:00Z"
}
```

## List Checkpoint DAG

```
GET /api/v1/sessions/ses_abc123/checkpoints
```

Returns nodes with `id`, `parent_id`, `type`, `label`, `size_bytes`, `created_at`. Clients reconstruct the tree from `parent_id` references.

## Restore

```
POST /api/v1/sessions/ses_abc123/checkpoints/cp_001/restore
```

The VM is paused, the overlay chain is rebuilt to the target checkpoint, and the VM resumes (or reboots for `light` checkpoints).

**Response** `200 OK`: returns the updated session object.

## Delete Subtree

```
DELETE /api/v1/sessions/ses_abc123/checkpoints/cp_001
```

Deletes the checkpoint and all its descendants. Returns `204 No Content`.

## Diff

```
GET /api/v1/sessions/ses_abc123/checkpoints/diff?from=cp_001&to=cp_002
```

```json
{
  "added": ["/app/model.pt"],
  "modified": ["/app/config.yaml"],
  "deleted": ["/tmp/cache.db"],
  "overlay_delta_bytes": 1048576
}
```

## Overlay Mechanics

Each checkpoint stores a frozen copy of the overlay ext4 image. Restoring replays the overlay chain: `base rootfs (RO) -> checkpoint overlay (RO) -> new writable overlay`. This keeps the base image untouched and enables branching -- multiple checkpoints can share the same parent.
