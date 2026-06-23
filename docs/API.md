# API Reference — ofs-control-api

Base URL (production): `https://api.tunnel.oldtian.top`
Base URL (local dev): `http://127.0.0.1:8088`

All request/response bodies are JSON.

## Authentication

| Surface | Header | Token |
| --- | --- | --- |
| Gateway registration | — (token in body) | `register_token` |
| Gateway endpoints | `Authorization: Bearer <gateway_secret>` | per-gateway secret |
| Admin / panel endpoints | `Authorization: Bearer <admin_token>` | `admin_token` |

## Error format

Every error returns a consistent envelope:

```json
{ "error": { "code": "bad_request", "message": "human readable reason" } }
```

| HTTP | `code` | Meaning |
| --- | --- | --- |
| 400 | `bad_request` | invalid/missing fields, validation failed |
| 401 | `unauthorized` | missing/invalid token or gateway secret |
| 403 | `forbidden` | authenticated but not allowed |
| 404 | `not_found` | resource does not exist |
| 409 | `conflict` | name/domain/remote_port already in use, no free port |
| 500 | `internal_error` | unexpected server error |

## Health / liveness

### `GET /healthz` — no auth
```json
{ "status": "ok", "version": "0.1.0" }
```

---

## Gateway endpoints

### `POST /api/gateways/register`
Self-register a gateway. Auth: `register_token` in body.

Request:
```json
{
  "register_token": "change-me-register-token",
  "name": "home-gateway",
  "hostname": "mac-mini",
  "os": "darwin",
  "arch": "arm64",
  "agent_version": "0.1.0"
}
```
Response `201`:
```json
{
  "gateway_id": "home-gateway",
  "gateway_secret": "generated-secret",
  "server_url": "https://api.tunnel.oldtian.top"
}
```
Notes: `gateway_id` is the slugified `name`; a short suffix is appended if it
collides. `gateway_secret` is shown **once** (only its bcrypt hash is stored).

### `POST /api/gateways/:gateway_id/heartbeat`
Auth: gateway secret.

Request:
```json
{ "current_config_version": 1, "lan_ip": "192.168.1.2", "agent_version": "0.1.0", "frpc_status": "running" }
```
Response `200`:
```json
{ "status": "ok", "latest_config_version": 2, "need_sync": true }
```

### `GET /api/gateways/:gateway_id/config`
Auth: gateway secret. Returns the gateway's **enabled, non-expired** tunnels plus
frp server settings.

Response `200`:
```json
{
  "config_version": 2,
  "server_addr": "tunnel.oldtian.top",
  "server_port": 7000,
  "frp_auth_token": "change-me-frp-token",
  "tunnels": [
    { "name": "openlist", "type": "http", "target_host": "192.168.1.10", "target_port": 5244,
      "domain": "openlist.tunnel.oldtian.top", "enabled": true },
    { "name": "mac-ssh", "type": "tcp", "target_host": "192.168.1.20", "target_port": 22,
      "remote_port": 2222, "enabled": true }
  ]
}
```

### `POST /api/gateways/:gateway_id/health`
Auth: gateway secret. Upserts health results by tunnel name (within the gateway).

Request:
```json
{
  "checks": [
    { "tunnel_name": "openlist", "status": "healthy", "latency_ms": 12, "message": "tcp connect ok" },
    { "tunnel_name": "astrbot", "status": "unhealthy", "message": "connection refused" }
  ]
}
```
Response `200`:
```json
{ "status": "ok", "recorded": 2, "skipped": [] }
```
`skipped` lists `tunnel_name`s that did not match a tunnel. A transition into
`unhealthy` writes a `health.unhealthy` event.

### `POST /api/gateways/:gateway_id/publish`
Auth: gateway secret. Same semantics as `POST /api/publish` but `gateway_id` is
forced to the authenticated gateway (the agent uses this so it never needs the
admin token). See the publish section below.

---

## Admin / management endpoints

All require `Authorization: Bearer <admin_token>`.

### `POST /api/tunnels`
Create a tunnel.

Request:
```json
{
  "gateway_id": "home-gateway",
  "name": "openlist",
  "type": "http",
  "target_host": "192.168.1.10",
  "target_port": 5244,
  "domain": "openlist.tunnel.oldtian.top",
  "enabled": true,
  "source": "manual"
}
```
Rules:
- `type` is `http` or `tcp`.
- `http` requires a valid, unique `domain`.
- `tcp` requires `remote_port` **or** it is auto-allocated from `[tcp_port_min, tcp_port_max]`.
- `name` must match `^[A-Za-z0-9_-]+$` and be unique within the gateway.
- `target_host` must be a valid IP/hostname; `target_port` in 1–65535.
- On success, the gateway's config version is bumped and a `tunnel.create` event written.

Response `201`: the created tunnel as a [tunnel view](#tunnel-view).

### `GET /api/tunnels`
List tunnels. Query params: `gateway_id`, `enabled` (`true`/`false`/`1`/`0`).
Response `200`: `{ "tunnels": [ <tunnel view>, ... ] }`.

### `GET /api/gateways/:gateway_id/tunnels`
Same as above, scoped to one gateway.

### `PUT /api/tunnels/:id`
Partial update; any omitted field is unchanged.
```json
{ "enabled": false, "target_host": "192.168.1.11", "target_port": 5244, "domain": "...", "remote_port": 2223 }
```
Bumps config version, writes `tunnel.update`. Response `200`: the updated tunnel view.

### `DELETE /api/tunnels/:id`
Deletes the tunnel and its health row, bumps config version, writes `tunnel.delete`.
Response `200`: `{ "status": "ok", "deleted": "<id>" }`.

### `POST /api/publish`
AI-friendly publish (admin variant — `gateway_id` taken from the body).

Request:
```json
{
  "gateway_id": "home-gateway",
  "name": "ai-demo-project",
  "type": "http",
  "target_host": "192.168.1.30",
  "target_port": 5173,
  "subdomain": "ai-demo-project",
  "ttl": "24h",
  "source": "ai-deploy"
}
```
- Only `http` is supported in v1; `tcp` is reserved and returns `400`.
- `subdomain` → `domain = <subdomain>.<http_domain_suffix>`; collisions get a short
  random suffix.
- `ttl` is a Go duration (`24h`, `90m`); omitted → `default_ttl_hours`.
- `source` defaults to `manual`.

Response `201`:
```json
{
  "status": "ok",
  "tunnel_id": "xxxx",
  "public_url": "https://ai-demo-project.tunnel.oldtian.top",
  "target": "192.168.1.30:5173",
  "expires_at": "2026-06-23T12:00:00+08:00"
}
```

---

## Panel data endpoints

All require `admin_token`.

### `GET /api/dashboard/summary`
```json
{
  "gateway_count": 1, "gateway_online": 1,
  "tunnel_count": 3, "tunnel_enabled": 3,
  "healthy_count": 1, "unhealthy_count": 1
}
```
`gateway_online` is derived from `last_seen_at` freshness (60s window).

### `GET /api/gateways`
```json
{ "gateways": [ { "id": "home-gateway", "name": "home-gateway", "status": "online",
  "online": true, "latest_config_version": 2, "lan_ip": "192.168.1.2",
  "agent_version": "0.1.0", "last_seen_at": "...", "...": "..." } ] }
```
`secret_hash` is never returned.

### `GET /api/tunnels`, `GET /api/gateways/:gateway_id/tunnels`
See above — returns tunnel views including `public_url`, `target`, `health_status`.

### `GET /api/health`, `GET /api/gateways/:gateway_id/health`
```json
{ "health": [ { "tunnel_id": "...", "tunnel_name": "openlist", "gateway_id": "home-gateway",
  "status": "healthy", "latency_ms": 12, "message": "tcp connect ok", "checked_at": "..." } ] }
```

### `GET /api/events?limit=100`
```json
{ "events": [ { "id": "...", "gateway_id": "...", "tunnel_id": "...", "level": "info",
  "event_type": "tunnel.create", "message": "...", "created_at": "..." } ] }
```
`limit` default 100, max 1000.

---

## <a name="tunnel-view"></a>Tunnel view object

```json
{
  "id": "2668b482...",
  "gateway_id": "home-gateway",
  "name": "openlist",
  "type": "http",
  "target_host": "192.168.1.10",
  "target_port": 5244,
  "domain": "openlist.tunnel.oldtian.top",
  "remote_port": 0,
  "enabled": true,
  "auth_mode": "none",
  "ttl_seconds": 0,
  "expires_at": null,
  "source": "manual",
  "created_at": "2026-06-22T13:29:49Z",
  "updated_at": "2026-06-22T13:29:49Z",
  "public_url": "https://openlist.tunnel.oldtian.top",
  "target": "192.168.1.10:5244",
  "health_status": "healthy",
  "health": { "tunnel_id": "...", "status": "healthy", "latency_ms": 12, "message": "...", "checked_at": "..." }
}
```
For TCP tunnels `public_url` is `"<frp.server_addr>:<remote_port>"`.
`health` is omitted when no check has been recorded (`health_status` is then `"unknown"`).

## Event types

`gateway.register`, `tunnel.create`, `tunnel.update`, `tunnel.delete`,
`tunnel.expired`, `health.unhealthy`.
