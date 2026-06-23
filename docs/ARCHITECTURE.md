# Architecture

Oldtian's FRP Service is a thin control plane around [frp](https://github.com/fatedier/frp).
It never touches packet forwarding itself — `frps`/`frpc` do that. The system's job
is to decide *what* tunnels should exist and to keep the internal `frpc` configured
and running.

## Components

### Public server
- **1Panel** — manages the host, Nginx/OpenResty, SSL certs, firewall.
- **OpenResty / Nginx (1Panel built-in)** — terminates HTTPS and reverse-proxies:
  - `*.tunnel.oldtian.top` → `127.0.0.1:8080` (frps vhost HTTP port)
  - `api.tunnel.oldtian.top` → `127.0.0.1:8088` (ofs-control-api)
- **frps** — the frp server. Public `bindPort 7000` for frpc; `vhostHTTPPort 8080`
  bound to localhost for Nginx; TCP `remotePort`s exposed for TCP tunnels.
- **ofs-control-api** — the REST control plane. Stores state in SQLite, answers
  agent register/heartbeat/config calls, serves admin + panel APIs.
- **SQLite** — single-file database, persisted on a Docker volume.

### Internal jump host (one dedicated machine)
- **ofs-gateway-agent** — registers with the control-api, heartbeats, pulls config,
  renders `frpc.toml`, manages the `frpc` process, runs health checks, and serves a
  local publish API.
- **frpc** — the frp client, launched and reloaded by the agent.
- The jump host does **not** need to host the business services. `frpc` forwards to
  any reachable LAN address (e.g. `192.168.1.10:5244`, `192.168.1.40:22`).

## Request flows

### HTTP service (scenario 1)
```
Browser ──HTTPS──> OpenResty/Nginx ──http://127.0.0.1:8080──> frps (vhost, routes by Host)
        ──> frpc ──> 192.168.1.10:5244 (e.g. openlist)
```
Adding a new HTTP tunnel needs **no Nginx change**: the wildcard server block
forwards every `*.tunnel.oldtian.top` host to frps, which routes by the `Host`
header to the matching proxy's `customDomains`.

### TCP service (scenario 2)
```
Client ──tcp──> server.oldtian.top:2222 ──> frps (remotePort 2222) ──> frpc ──> 192.168.1.20:22
```

### AI publish (scenario 3)
```
AI deploy script ──POST /api/local/publish──> ofs-gateway-agent
   └─ forwards ──POST /api/gateways/:id/publish──> ofs-control-api (creates tunnel, bumps version)
   └─ triggers immediate sync ──> renders frpc.toml ──> reloads frpc ──> tunnel live
```

## Control loop (how config propagates)

```
admin/publish creates|updates|deletes a tunnel
        │
        ▼
control-api bumps config_versions.version  (the "desired" version)
        │
        ▼  (agent heartbeat every ~10s)
agent sends current_config_version ──> server replies latest_config_version + need_sync
        │ need_sync == true
        ▼
agent GET /config ──> renders frpc.toml ──> frpc reload ──> persists applied version
```

Two version numbers exist:
- `config_versions.version` — the latest **desired** version on the server.
- `gateways.current_config_version` — the version the agent reports as **applied**
  (updated on each heartbeat).

`need_sync = latest != current`. The agent only marks a version applied after a
successful `frpc` reload, so a failed reload keeps retrying.

## Trust & auth boundaries

| Caller | Credential | Scope |
| --- | --- | --- |
| Agent registration | `register_token` (body) | create a gateway |
| Agent (heartbeat/config/health/publish) | `gateway_secret` (Bearer) | only its own gateway |
| Admin / panel | `admin_token` (Bearer) | everything |

- `gateway_secret` is generated server-side, returned **once**, and stored only as a
  bcrypt hash. The agent persists it locally in `state.yaml` (0600).
- The agent never needs the `admin_token`: it publishes through a gateway-scoped
  endpoint authenticated with its own secret.

## Technology

- Go (Gin) for both binaries; pure-Go SQLite driver (`modernc.org/sqlite`, no CGO).
- YAML config; SQLite storage; Docker Compose for the public side.
- frp is an external dependency — versions are decoupled from this project.
