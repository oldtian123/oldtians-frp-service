# Changelog

All notable changes to Oldtian's FRP Service are documented here.

## 2026-06-22

### Added
- Initial MVP of the whole system.
- **control-api (`ofs-control-api`)**
  - YAML config with `OFS_*` env overrides, startup validation, auto-created DB dir.
  - SQLite store with embedded schema, auto-migrated on boot; tables `gateways`,
    `tunnels`, `config_versions`, `health_checks`, `event_logs`.
  - Gateway registration (slugified id, bcrypt-hashed secret, register-token auth).
  - Gateway heartbeat (online tracking, `need_sync` via config versions).
  - Gateway config pull (enabled, non-expired tunnels + frp server settings).
  - Tunnel CRUD with validation, TCP remote-port auto-allocation, uniqueness checks,
    and automatic config-version bump on every change.
  - Publish API: `POST /api/publish` (admin) + gateway-scoped
    `POST /api/gateways/:id/publish` (agent), HTTP only, subdomain collision suffixing,
    TTL handling.
  - Health report ingestion + panel queries; dashboard summary; events query.
  - TTL janitor that disables expired tunnels and bumps config version.
  - Consistent JSON error envelope, admin/gateway auth middleware, graceful shutdown,
    CGO-free Dockerfile with healthcheck.
- **gateway-agent (`ofs-gateway-agent`)**
  - YAML config + persistent `state.yaml` (0600).
  - Auto-registration with capped-backoff retries; host info + LAN IP detection.
  - Heartbeat loop that triggers config sync on `need_sync`.
  - Config sync: pull → render `frpc.toml` → validate → reload frpc → persist version.
  - `frpc.toml` renderer (stable, sorted, sanitized) supporting http + tcp proxies.
  - frpc child-process manager (start/reload/restart/status) with output capture.
  - Health-check loop (HTTP GET / TCP connect) reporting back to the server.
  - Local publish API (`127.0.0.1:8899`) that forwards to the server and reconciles
    locally, returning `pending_sync` when the local reload fails.
  - Install scripts (`install.sh`, `install.ps1`) + systemd unit.
- **deploy**: `docker-compose.yml` (project `oldtians-frp-service`), `frps.toml`,
  `nginx.example.conf` (wildcard + api reverse proxy).
- **docs**: HANDOFF, ARCHITECTURE, API, DEPLOYMENT_1PANEL, AGENT, PANEL_INTEGRATION,
  AI_PUBLISH, TROUBLESHOOTING, CHANGELOG.

### Notes
- TCP **publish** via `/api/publish` is intentionally deferred (returns 400); TCP
  tunnels are fully supported through `POST /api/tunnels`.
- frpc "reload" is a process restart, not frp hot reload — the interface is isolated
  for a future swap.
- Verified end-to-end locally on Windows (Go 1.26): register → heartbeat → tunnel
  create (http + tcp) → config pull → publish → health report, and the agent
  registering + rendering a correct `frpc.toml`.
- Still unsupported by design: STCP / SUDP / P2P / XTCP, web console, multi-user.
