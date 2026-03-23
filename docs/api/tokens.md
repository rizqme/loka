# Tokens API

Tokens authenticate self-managed workers during registration with the control plane.

## Endpoints

| Method | Path | Description |
|---|---|---|
| `POST` | `/api/v1/tokens` | Create a registration token |
| `GET` | `/api/v1/tokens` | List tokens |
| `DELETE` | `/api/v1/tokens/:id` | Revoke a token |

## Create Token

```
POST /api/v1/tokens
```

```json
{
  "label": "on-prem-cluster",
  "expires_in": "720h",
  "max_uses": 10
}
```

**Response** `201 Created`:

```json
{
  "id": "tok_abc123",
  "token": "loka_t_xxxxxxxxxxxxxxxxxxxxxxxxxxxx",
  "label": "on-prem-cluster",
  "max_uses": 10,
  "uses": 0,
  "expires_at": "2026-04-22T10:00:00Z",
  "created_at": "2026-03-23T10:00:00Z"
}
```

The `token` value is returned only at creation time. Store it securely.

## List Tokens

```
GET /api/v1/tokens
```

```json
{
  "items": [
    {
      "id": "tok_abc123",
      "label": "on-prem-cluster",
      "max_uses": 10,
      "uses": 3,
      "expires_at": "2026-04-22T10:00:00Z",
      "created_at": "2026-03-23T10:00:00Z"
    }
  ]
}
```

The `token` secret is not included in list responses.

## Revoke Token

```
DELETE /api/v1/tokens/tok_abc123
```

**Response** `204 No Content`. Already-registered workers remain connected. Only future registration attempts with this token are rejected.

## Usage

Use the token when starting a self-managed worker:

```bash
loka worker \
  --control-plane https://cp.example.com \
  --token loka_t_xxxxxxxxxxxxxxxxxxxxxxxxxxxx \
  --labels env=production,rack=r42
```
