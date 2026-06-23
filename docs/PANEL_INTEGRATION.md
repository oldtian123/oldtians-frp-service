# Panel Integration

Guide for building the future cloud visualization panel against ofs-control-api.
This project ships no UI; the panel is a separate app that calls these APIs.

## Auth

All panel calls use the admin token:
```
Authorization: Bearer <admin_token>
```
Keep the token server-side in the panel backend; never ship it to a browser.

## Endpoints the panel should call

| Purpose | Endpoint |
| --- | --- |
| Overview cards | `GET /api/dashboard/summary` |
| Gateways list | `GET /api/gateways` |
| Tunnels (all / per gateway) | `GET /api/tunnels` · `GET /api/gateways/:id/tunnels` |
| Health | `GET /api/health` · `GET /api/gateways/:id/health` |
| Activity feed | `GET /api/events?limit=100` |
| Create tunnel | `POST /api/tunnels` |
| Enable/disable / edit tunnel | `PUT /api/tunnels/:id` |
| Delete tunnel | `DELETE /api/tunnels/:id` |
| Publish (HTTP) | `POST /api/publish` |

Full request/response details: [API.md](API.md).

## Data structures

### summary (`GET /api/dashboard/summary`)
```json
{ "gateway_count": 1, "gateway_online": 1, "tunnel_count": 3,
  "tunnel_enabled": 3, "healthy_count": 1, "unhealthy_count": 1 }
```

### gateway (`GET /api/gateways` → `gateways[]`)
```json
{ "id": "home-gateway", "name": "home-gateway", "status": "online", "online": true,
  "hostname": "mac-mini", "os": "darwin", "arch": "arm64", "lan_ip": "192.168.1.2",
  "public_ip": "", "agent_version": "0.1.0", "current_config_version": 2,
  "latest_config_version": 2, "last_seen_at": "2026-06-22T13:29:48Z",
  "created_at": "...", "updated_at": "..." }
```
- `online` is the authoritative liveness flag (derived from `last_seen_at`, 60s
  window). Prefer it over `status`.
- `current_config_version` < `latest_config_version` means the agent is mid-sync.

### tunnel view (`GET /api/tunnels` → `tunnels[]`)
```json
{ "id": "...", "gateway_id": "home-gateway", "name": "openlist", "type": "http",
  "target_host": "192.168.1.10", "target_port": 5244,
  "domain": "openlist.tunnel.oldtian.top", "remote_port": 0, "enabled": true,
  "source": "manual", "expires_at": null,
  "public_url": "https://openlist.tunnel.oldtian.top", "target": "192.168.1.10:5244",
  "health_status": "healthy", "health": { "...": "..." } }
```
- `public_url`: HTTP → `https://<domain>`; TCP → `<frp.server_addr>:<remote_port>`.
- `health_status`: `healthy` | `unhealthy` | `unknown`.
- `source`: `manual` | `ai-deploy` | custom.

### health (`GET /api/health` → `health[]`)
```json
{ "tunnel_id": "...", "tunnel_name": "openlist", "gateway_id": "home-gateway",
  "status": "healthy", "latency_ms": 12, "message": "tcp connect ok",
  "checked_at": "2026-06-22T13:29:49Z" }
```

### event (`GET /api/events` → `events[]`)
```json
{ "id": "...", "gateway_id": "home-gateway", "tunnel_id": "...", "level": "info",
  "event_type": "tunnel.create", "message": "tunnel \"openlist\" (http) created",
  "created_at": "2026-06-22T13:29:49Z" }
```
`level`: `info` | `warn` | `error`.

## Recommended panel pages

1. **Dashboard** — summary cards (gateways online, tunnels enabled, unhealthy
   count) + recent events feed.
2. **Gateways** — table with online dot, last seen, agent version, config-version
   drift indicator.
3. **Tunnels** — table grouped by gateway: name, type, target, `public_url` (link),
   health badge, enable/disable toggle, edit, delete. A "Create tunnel" form.
4. **Health** — latest probe per tunnel with latency + message.
5. **Events** — paginated activity log (use `?limit=`).
6. **Publish** — a quick form mapping to `POST /api/publish`.

## One-click enable/disable a tunnel

Use a partial update — do **not** delete/recreate:
```http
PUT /api/tunnels/:id
{ "enabled": false }
```
This bumps the config version; the owning gateway disables the proxy on its next
sync (≤ ~10s). Re-enable with `{ "enabled": true }`.

## Polling guidance

- Summary / gateways: every 5–10s is plenty (heartbeats are ~10s).
- Treat a gateway as offline when `online == false`.
- Health and events change slowly; poll on view or every 15–30s.

## Notes / gotchas

- List endpoints are unpaginated (except `events?limit=`); fine for personal scale.
- Creating a tunnel does not guarantee it is live yet — watch the gateway's
  `current_config_version` catch up to `latest_config_version`, and `health_status`.
- TCP publish via `POST /api/publish` is not available in v1 (returns 400); create
  TCP tunnels via `POST /api/tunnels`.
