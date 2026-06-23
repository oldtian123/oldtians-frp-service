# HANDOFF — Oldtian's FRP Service

Read this first if you are an engineer or AI agent taking over the project. It is
the single source of truth for *what exists, why, and where*.

## 1. Project goal

A personal intranet-penetration control system on top of frp. It orchestrates
`frps`/`frpc`, exposing an API to register gateways, manage HTTP/TCP tunnels,
publish internal services (including from AI deploy scripts), and report health.
**API-only** — a separate cloud panel will consume these APIs later.

## 2. Boundaries (explicitly NOT in v1)

Web console, multi-user/RBAC, billing, multi-tenant isolation, P2P/XTCP/STCP/SUDP,
custom tunneling protocols, deep access auditing, SSO. We do **not** fork or
replace `frpc`/`frps`.

## 3. Current architecture

See [ARCHITECTURE.md](ARCHITECTURE.md). In short:

- Public server: 1Panel + Nginx/OpenResty + `frps` + `ofs-control-api` + SQLite (Docker Compose).
- Internal jump host: `ofs-gateway-agent` + `frpc`.
- The agent pulls config from the control-api and keeps `frpc` configured/running.

## 4. Module responsibilities

### server/ (`ofs-control-api`)
| Path | Responsibility |
| --- | --- |
| `cmd/control-api/main.go` | wiring, router, TTL janitor, graceful shutdown |
| `internal/config` | YAML load, `OFS_*` env overrides, validation, ensure DB dir |
| `internal/database` | SQLite store, models, CRUD, embedded schema, ID gen |
| `internal/httpx` | JSON error envelope, admin/gateway auth middleware, bcrypt helpers |
| `internal/eventlog` | structured writes to `event_logs` |
| `internal/gateway` | register, heartbeat, config; admin list/summary/events |
| `internal/tunnel` | tunnel `Service` (validation, port alloc, version bump) + handlers |
| `internal/publish` | `/api/publish` (admin) and gateway-scoped publish |
| `internal/health` | health report + query |

### agent/ (`ofs-gateway-agent`)
| Path | Responsibility |
| --- | --- |
| `cmd/gateway-agent/main.go` | orchestration: register → sync → loops → local API |
| `internal/config` | YAML config + persistent `state.yaml` |
| `internal/serverapi` | typed HTTP client for control-api |
| `internal/register` | first-boot registration, host info, LAN IP |
| `internal/heartbeat` | heartbeat loop, triggers sync |
| `internal/sync` | pull config → render `frpc.toml` → reload `frpc` |
| `internal/frpc` | TOML rendering + child-process manager |
| `internal/healthcheck` | probe targets, report results |
| `internal/publish` | forward publish to server, reconcile locally |
| `internal/localapi` | local HTTP API (publish/status/healthz) |

> Note: `internal/httpx` (server) and `internal/serverapi` (agent) are supporting
> packages not in the original spec sketch; they keep HTTP plumbing and the API
> client cleanly separated.

## 5. Deployment

Public side via Docker Compose behind 1Panel Nginx — see
[DEPLOYMENT_1PANEL.md](DEPLOYMENT_1PANEL.md). Agent via `install/install.sh`
(systemd) — see [AGENT.md](AGENT.md).

## 6. Run commands

```bash
# server (dev)
cd server && go run ./cmd/control-api -config config.yaml
# agent (dev)
cd agent && go run ./cmd/gateway-agent -config config.yaml
# build binaries
cd server && go build -o ofs-control-api ./cmd/control-api
cd agent  && go build -o ofs-gateway-agent ./cmd/gateway-agent
# public stack
cd deploy && docker compose up -d
```

Both modules are tied by `go.work`. When running module-scoped commands
(`go build ./...`, `go vet ./...`) run them **inside** `server/` or `agent/`.

## 7. Configuration files

| File | Used by | Notes |
| --- | --- | --- |
| `server/config.example.yaml` | control-api | copy to `config.yaml`; `OFS_*` env overrides |
| `agent/config.example.yaml` | agent | copy to `config.yaml`; `OFS_SERVER_URL`/`OFS_REGISTER_TOKEN` overrides |
| `agent` `state.yaml` | agent | generated; holds `gateway_id`, `gateway_secret`, applied version (0600) |
| `deploy/frps.toml` | frps | `auth.token` MUST equal control-api `frp.auth_token` |
| `deploy/docker-compose.yml` | public stack | compose project `oldtians-frp-service` |
| `deploy/nginx.example.conf` | 1Panel Nginx | wildcard + api reverse proxy |

**Any new config key must be added to the relevant `config.example.yaml` and to
this table.** Tokens are never logged.

## 8. API overview

Full reference in [API.md](API.md). Surfaces:
- Gateway: `register`, `heartbeat`, `config`, `health`, gateway-scoped `publish`.
- Admin: tunnel CRUD, `publish`.
- Panel: `dashboard/summary`, `gateways`, `tunnels`, `health`, `events`.

## 9. Database tables (SQLite)

Schema is embedded at `server/internal/database/schema.sql` (mirror in
`server/migrations/0001_init.sql`) and applied idempotently on every boot.

| Table | Purpose | Key columns |
| --- | --- | --- |
| `gateways` | registered jump hosts | `id` (slug), `secret_hash` (bcrypt), `current_config_version`, `last_seen_at` |
| `tunnels` | proxy definitions | `type`, `target_host/port`, `domain`, `remote_port`, `enabled`, `expires_at`, `source` |
| `config_versions` | latest **desired** version per gateway | `gateway_id`, `version` |
| `health_checks` | latest probe per tunnel | `tunnel_id`, `status`, `latency_ms`, `checked_at` |
| `event_logs` | audit/activity | `level`, `event_type`, `message`, `created_at` |

Conventions:
- `created_at`/`updated_at` written automatically; timestamps stored as RFC3339 UTC text.
- Every tunnel create/update/delete (and TTL expiry) **bumps the gateway's
  `config_versions.version`**. This is the contract that makes agents re-sync.
- Important operations write `event_logs`.

## 10. frpc.toml generation logic

Implemented in `agent/internal/frpc/render.go`. Input: `server_addr`, `server_port`,
`frp_auth_token`, and the enabled tunnels from `GET /config`. Output is a stable,
sorted-by-name TOML:

```toml
serverAddr = "tunnel.oldtian.top"
serverPort = 7000

auth.method = "token"
auth.token  = "xxx"

[[proxies]]
name = "openlist"
type = "http"
localIP = "192.168.1.10"
localPort = 5244
customDomains = ["openlist.tunnel.oldtian.top"]

[[proxies]]
name = "mac-ssh"
type = "tcp"
localIP = "192.168.1.20"
localPort = 22
remotePort = 2222
```

Rules: skip `enabled=false`; sanitize proxy names to `[A-Za-z0-9_-]`; validate
host/port/domain and skip (with a logged warning) anything invalid; `localIP` may be
any LAN address, not necessarily the agent host. Changes here must be mirrored in
[AGENT.md](AGENT.md).

## 11. Done

- control-api: config, SQLite auto-migrate, gateway register/heartbeat/config,
  tunnel CRUD + version bump, publish (http), health report + queries, dashboard
  summary, events, TTL janitor, graceful shutdown, Dockerfile.
- agent: config + state, auto-register, heartbeat, sync, `frpc.toml` render, frpc
  process manager (start/reload/restart/status), health checks, local publish API,
  install scripts + systemd unit.
- Verified end-to-end locally (register → heartbeat → tunnel create → config pull →
  publish → health report; agent registers, renders correct `frpc.toml`).

## 12. Not done / known limitations

- TCP **publish** is reserved (`/api/publish` returns 400 for `type=tcp`); TCP
  tunnels are fully supported via `POST /api/tunnels`.
- `frpc` "reload" is implemented as a process restart (kill + start), not frp hot
  reload. The interface is isolated in `frpc.Manager` for a future swap.
- The agent owns `frpc` as a child process — run the agent under systemd so the
  child is reaped cleanly. A stray external `frpc` is not auto-detected.
- No background "mark offline" job; online status is derived from `last_seen_at`.
- No pagination on list endpoints beyond `events?limit=`.

## 13. Maintenance notes

- Keep `frps` `auth.token` == control-api `frp.auth_token` == the token agents pull.
- Secrets: rotate by updating config + restarting; gateways must re-register if their
  secret is lost (delete `state.yaml`).
- SQLite runs in WAL mode with a single writer connection; back up the `.db` (+ `-wal`).
- When changing the schema: edit **both** `schema.sql` and `migrations/0001_init.sql`,
  and update §9 here.

## 14. Future extension points

- frp hot reload via the frpc admin API (replace `frpc.Manager.reloadLocked`).
- TCP publish (auto-allocate remote port — logic already exists in `tunnel.Service`).
- Auth modes per tunnel (`tunnels.auth_mode` column already present).
- Public IP capture, gateway tags/labels, soft-delete, event pagination.
- The cloud panel (see [PANEL_INTEGRATION.md](PANEL_INTEGRATION.md)).
