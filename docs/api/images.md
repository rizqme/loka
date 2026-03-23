# Images API

## Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/images/pull` | Pull and convert a Docker image |
| `GET` | `/api/v1/images` | List available images |
| `GET` | `/api/v1/images/:id` | Get image details |
| `DELETE` | `/api/v1/images/:id` | Delete image |

## Pull Image

```
POST /api/v1/images/pull
```

```json
{
  "reference": "python:3.11-slim",
  "registry_auth": {
    "username": "user",
    "password": "token"
  }
}
```

**Response** `202 Accepted`:

```json
{
  "id": "img_abc123",
  "reference": "python:3.11-slim",
  "status": "pulling",
  "created_at": "2026-03-23T10:00:00Z"
}
```

Poll `GET /api/v1/images/img_abc123` until `status` is `ready` or `failed`.

## Pull Flow

```
docker pull python:3.11-slim
        |
        v
docker export --> flat tarball
        |
        v
mkfs.ext4 -L rootfs image.ext4
mount + extract tar into image
        |
        v
inject /usr/bin/supervisor (static binary, ~5 MB)
inject /etc/supervisor.conf (default policy)
        |
        v
umount, sha256sum --> content-addressed storage
```

The final `rootfs.ext4` is stored once and mounted read-only by all sessions using that image.

## List Images

```
GET /api/v1/images?limit=20&offset=0
```

```json
{
  "items": [
    {
      "id": "img_abc123",
      "reference": "python:3.11-slim",
      "digest": "sha256:a1b2c3...",
      "size_bytes": 157286400,
      "status": "ready",
      "created_at": "2026-03-23T10:00:00Z"
    }
  ],
  "total": 5
}
```

## Delete Image

```
DELETE /api/v1/images/img_abc123
```

Fails if any active session references the image. Returns `204 No Content` on success.
