# AI Publish

How an AI agent / Claude Code / Codex / deploy script publishes an internal service
and gets a public URL — without knowing any admin token.

## TL;DR

After your service is running on some LAN host/port, call the **local** agent API
on the jump host:

```bash
curl -X POST http://127.0.0.1:8899/api/local/publish \
  -H "Content-Type: application/json" \
  -d '{
    "name": "ai-demo-project",
    "type": "http",
    "target_host": "192.168.1.30",
    "target_port": 5173,
    "subdomain": "ai-demo-project",
    "ttl": "24h",
    "source": "ai-deploy"
  }'
```

Response:
```json
{
  "status": "ok",
  "tunnel_id": "72966094a717008bea329a071db55464",
  "public_url": "https://ai-demo-project.tunnel.oldtian.top",
  "target": "192.168.1.30:5173",
  "expires_at": "2026-06-23T21:45:30+08:00"
}
```

Open `public_url`. Done.

## Request fields

| Field | Required | Notes |
| --- | --- | --- |
| `target_host` | yes | LAN IP/hostname of the service (any reachable host) |
| `target_port` | yes | service port |
| `subdomain` | recommended | becomes `<subdomain>.<http_domain_suffix>`; falls back to `name` |
| `name` | optional | label; if `subdomain` is empty, used to derive it |
| `type` | optional | defaults to `http` (only `http` supported now) |
| `ttl` | optional | Go duration (`24h`, `90m`); defaults to server `default_ttl_hours` |
| `source` | optional | free-form tag, defaults to `manual`; use `ai-deploy` |

`gateway_id` is **not** required — the agent fills it from its own identity, and the
server scopes the publish to that gateway.

## How it works

```
your script ──POST /api/local/publish──> ofs-gateway-agent (127.0.0.1:8899)
   agent fills gateway_id, forwards ──POST /api/gateways/:id/publish──> control-api
      control-api creates the tunnel (subdomain collisions get a short suffix),
      bumps the gateway config version, returns public_url + expires_at
   agent triggers an immediate sync ──> regenerates frpc.toml ──> reloads frpc
```

So by the time you get `status: "ok"`, the tunnel exists on the server **and** frpc
has been reloaded locally.

## Getting the public URL

It is the `public_url` field. For HTTP it is `https://<subdomain>.<suffix>`. If your
subdomain collided, the actual subdomain has a `-xxxx` suffix — always read it back
from `public_url`, don't assume it equals your input.

## `pending_sync`

If the response is:
```json
{ "status": "pending_sync", "public_url": "https://...", "target": "..." }
```
the tunnel was created on the server, but the local `frpc` reload failed (e.g. frpc
not installed, config error). The tunnel **will** go live automatically on the next
heartbeat-driven sync (≤ ~10s) once the issue clears. To investigate:

```bash
curl http://127.0.0.1:8899/status                 # frpc_status should be "running"
journalctl -u oldtians-frp-service -n 50          # look for "sync:" / "[frpc]" errors
cat /etc/oldtians-frp-service/frpc.toml            # verify the proxy block exists
```

Common causes: `frpc` binary missing at `frpc.bin_path`, wrong `frp.auth_token`,
frps unreachable. See [TROUBLESHOOTING.md](TROUBLESHOOTING.md).

## Publish failed entirely

If you get an HTTP `502` with an `error` message, the forward to the control-api
failed. The body includes `upstream_code` / `upstream_http` from the server:
```json
{ "error": "domain \"x.tunnel.oldtian.top\" is already in use", "upstream_code": "conflict", "upstream_http": 409 }
```
Checklist:
- Agent registered? `curl http://127.0.0.1:8899/status` → `registered: true`.
- control-api reachable from the jump host? (`server_url` correct, network/VPN up).
- Valid `target_host`/`target_port`?

## Letting other LAN machines publish

By default the local API binds `127.0.0.1`. To let other hosts on the LAN call it,
set `agent.listen_addr: "0.0.0.0"` in the agent config and restart. Only do this on
a trusted network — the local API is unauthenticated by design.

## Removing a published service

Published tunnels expire automatically at `expires_at` (TTL). To remove one early,
delete it via the admin API (or the panel):
```http
DELETE /api/tunnels/:tunnel_id      (Authorization: Bearer <admin_token>)
```
After deletion the public URL stops working on the next sync.
