# ofs-gateway-agent

The internal jump-host agent for Oldtian's FRP Service. It self-registers with
the control-api, heartbeats, pulls tunnel config, renders `frpc.toml`, manages the
`frpc` process, runs health checks and serves a local publish API.

## Build & run

```bash
cp config.example.yaml config.yaml    # set server_url + register_token
go build -o ofs-gateway-agent ./cmd/gateway-agent
./ofs-gateway-agent -config config.yaml
```

You need an `frpc` binary on the host at the configured `frpc.bin_path`
(download from the [frp releases](https://github.com/fatedier/frp/releases)).

## Install (Linux/macOS)

```bash
sudo ./install/install.sh ./ofs-gateway-agent
sudo systemctl start oldtians-frp-service      # Linux + systemd
journalctl -u oldtians-frp-service -f
```

Windows: see `install/install.ps1`.

## Configuration & state

- Config: [config.example.yaml](config.example.yaml). `OFS_SERVER_URL` and
  `OFS_REGISTER_TOKEN` env vars override `server_url` / `register_token`.
- State: persisted to `agent.state_path` (default `<work_dir>/state.yaml`),
  holding `gateway_id`, `gateway_secret` (0600 perms) and the applied config
  version. Delete it to force a fresh registration.

## What it does

| Loop | Default interval | Action |
| --- | --- | --- |
| Heartbeat | 10s | POST heartbeat, trigger sync when `need_sync` |
| Sync | 10s (+ on-demand) | pull config, render `frpc.toml`, reload `frpc` |
| Health check | 15s | probe each target, report results |
| Local API | — | `127.0.0.1:8899` publish + status |

Detailed flows and maintenance commands: [../docs/AGENT.md](../docs/AGENT.md).

## Local API

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/api/local/publish` | publish an internal service (used by AI deploy) |
| `GET` | `/status` | agent status (gateway id, config version, frpc status) |
| `GET` | `/healthz` | liveness |

See [../docs/AI_PUBLISH.md](../docs/AI_PUBLISH.md).

## Packages

| Package | Responsibility |
| --- | --- |
| `internal/config` | YAML config + persistent state |
| `internal/serverapi` | typed HTTP client for control-api |
| `internal/register` | first-boot registration + host info / LAN IP |
| `internal/heartbeat` | heartbeat loop |
| `internal/sync` | config pull -> render -> reload |
| `internal/frpc` | `frpc.toml` rendering + process manager |
| `internal/healthcheck` | target probing + reporting |
| `internal/publish` | forward publish to server + reconcile |
| `internal/localapi` | local HTTP API |
