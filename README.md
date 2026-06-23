# Oldtian's FRP Service

A small, personal intranet-penetration (内网穿透) control system built on top of
[frp](https://github.com/fatedier/frp). It does **not** reimplement tunneling —
it orchestrates `frps` / `frpc` and gives you a clean API to register gateways,
manage tunnels, publish internal services (including from AI deploy scripts), and
report health back to a future cloud dashboard.

> This project ships an **API only** — there is no bundled web console. A separate
> cloud visualization panel is expected to consume these APIs later.

## Components

| Name | Binary | Runs on | Role |
| --- | --- | --- | --- |
| Control API | `ofs-control-api` | Public server | REST API + SQLite state, tells agents what to run |
| Gateway Agent | `ofs-gateway-agent` | Internal jump host | Registers, heartbeats, generates `frpc.toml`, manages `frpc`, local publish API |
| frps | `frps` | Public server | frp server (the actual tunnel endpoint) |
| frpc | `frpc` | Internal jump host | frp client (managed by the agent) |

```
Browser ──HTTPS──> 1Panel OpenResty/Nginx ──> frps(vhost 8080) ──> frpc ──> 192.168.x.x:port
                         │
Agent/AI ──> ofs-control-api(8088) <── heartbeat/config/health ── ofs-gateway-agent ──manages──> frpc
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the full picture.

## Repository layout

```
oldtians-frp-service/
├── server/   # ofs-control-api (Go, Gin, SQLite)
├── agent/    # ofs-gateway-agent (Go, manages frpc)
├── deploy/   # docker-compose.yml, frps.toml, nginx.example.conf
├── docs/     # architecture, API, deployment, handoff, troubleshooting...
└── go.work   # Go workspace tying server + agent together
```

## Quick start (local dev)

Prerequisites: Go 1.25+.

### 1. Run the control-api

```bash
cd server
cp config.example.yaml config.yaml      # edit tokens
go run ./cmd/control-api -config config.yaml
# -> listening on 0.0.0.0:8088, SQLite auto-created under ./data
```

### 2. Run the gateway-agent (on the jump host)

```bash
cd agent
cp config.example.yaml config.yaml      # set server_url + register_token
# place an frpc binary at the configured bin_path
go run ./cmd/gateway-agent -config config.yaml
```

The agent auto-registers, starts heartbeating, generates `frpc.toml`, and starts
`frpc`. Its local publish API listens on `127.0.0.1:8899`.

### 3. Create a tunnel

```bash
# HTTP tunnel via the admin API
curl -X POST http://127.0.0.1:8088/api/tunnels \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"gateway_id":"home-gateway","name":"openlist","type":"http",
       "target_host":"192.168.1.10","target_port":5244,
       "domain":"openlist.tunnel.oldtian.top"}'
```

Or let an AI deploy script publish one locally:

```bash
curl -X POST http://127.0.0.1:8899/api/local/publish \
  -H "Content-Type: application/json" \
  -d '{"name":"ai-demo","type":"http","target_host":"192.168.1.30",
       "target_port":5173,"subdomain":"ai-demo","ttl":"24h","source":"ai-deploy"}'
```

## Production deployment

The public side runs via Docker Compose behind 1Panel's Nginx/OpenResty:

```bash
cd deploy
cp ../server/config.example.yaml control-api.config.yaml   # edit
# edit frps.toml (auth.token must match control-api frp.auth_token)
docker compose up -d
```

Full walkthrough: [docs/DEPLOYMENT_1PANEL.md](docs/DEPLOYMENT_1PANEL.md).

## Documentation

- [docs/HANDOFF.md](docs/HANDOFF.md) — start here if you are taking over the project
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)
- [docs/API.md](docs/API.md)
- [docs/DEPLOYMENT_1PANEL.md](docs/DEPLOYMENT_1PANEL.md)
- [docs/AGENT.md](docs/AGENT.md)
- [docs/PANEL_INTEGRATION.md](docs/PANEL_INTEGRATION.md)
- [docs/AI_PUBLISH.md](docs/AI_PUBLISH.md)
- [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md)
- [docs/CHANGELOG.md](docs/CHANGELOG.md)

## Scope (v1)

In scope: gateway registration, tunnels (HTTP + TCP), config versioning, AI
publish, health reporting, panel data APIs.

Out of scope (by design): web console, multi-user/RBAC, billing, multi-tenant
isolation, P2P/XTCP/STCP/SUDP, custom tunneling protocols.
