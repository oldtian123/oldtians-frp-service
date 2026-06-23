# Gateway Agent (ofs-gateway-agent)

The agent runs on the internal jump host. It owns `frpc` and keeps it configured
and running according to the control-api.

## Install

```bash
# build (on a machine with Go) or download a prebuilt binary
cd agent && go build -o ofs-gateway-agent ./cmd/gateway-agent

# install (Linux/macOS, as root)
sudo ./install/install.sh ./ofs-gateway-agent
# -> /usr/local/bin/ofs-gateway-agent
# -> /etc/oldtians-frp-service/config.yaml (from the example; EDIT IT)
# -> systemd unit oldtians-frp-service.service (Linux)
```

Place an `frpc` binary at the configured `frpc.bin_path` (default
`/usr/local/bin/frpc`), downloaded from the
[frp releases](https://github.com/fatedier/frp/releases).

Start it:
```bash
sudo systemctl start oldtians-frp-service
journalctl -u oldtians-frp-service -f
```

macOS / no systemd: run it directly or under launchd:
```bash
/usr/local/bin/ofs-gateway-agent -config /etc/oldtians-frp-service/config.yaml
```

## Configuration file

`/etc/oldtians-frp-service/config.yaml` (see `agent/config.example.yaml`):

```yaml
server_url: "https://api.tunnel.oldtian.top"   # OFS_SERVER_URL
register_token: "change-me-register-token"     # OFS_REGISTER_TOKEN
gateway:
  name: "home-gateway"
agent:
  listen_addr: "127.0.0.1"   # 0.0.0.0 to let other LAN hosts call publish
  listen_port: 8899
  heartbeat_interval_seconds: 10
  sync_interval_seconds: 10
  health_check_interval_seconds: 15
  state_path: "/etc/oldtians-frp-service/state.yaml"
frpc:
  bin_path: "/usr/local/bin/frpc"
  config_path: "/etc/oldtians-frp-service/frpc.toml"
  work_dir: "/etc/oldtians-frp-service"
```

## State file

`state.yaml` (mode 0600), written by the agent:

```yaml
gateway_id: "home-gateway"
gateway_secret: "generated-secret"
current_config_version: 1
```

- Created on first successful registration.
- `gateway_secret` is the only copy you have — back it up or be ready to
  re-register. Deleting `state.yaml` forces a fresh registration (a new
  `gateway_id` if the name collides).

## Registration flow

1. On boot, if `state.yaml` has credentials, reuse them.
2. Otherwise gather `hostname`, `os` (`runtime.GOOS`), `arch` (`runtime.GOARCH`),
   LAN IP, and `POST /api/gateways/register` with the `register_token`.
3. Save `gateway_id` + `gateway_secret` to `state.yaml`.
4. Retries with capped exponential backoff (2s → 30s) until it succeeds.

## Heartbeat flow

- Every `heartbeat_interval_seconds`, `POST /heartbeat` with
  `current_config_version`, `lan_ip`, `agent_version`, `frpc_status`.
- If the response has `need_sync: true`, an out-of-band sync is triggered.
- Network failures are logged and retried; the loop never exits.

## Config sync flow

1. `GET /config`, store the tunnel snapshot (used by health checks).
2. If `config_version` is unchanged **and** `frpc` is running → no-op.
3. Else render `frpc.toml`, write it to a temp file, validate
   (`frpc verify -c` when supported), atomically replace the live file, and reload.
4. On success, persist the applied `config_version` to `state.yaml`.
5. On failure, log it and keep the old applied version so the next heartbeat retries.

## frpc reload logic

Implemented in `agent/internal/frpc/process.go`:
- The agent runs `frpc -c <config_path>` as a **child process**.
- "Reload" = stop the child (SIGKILL, wait up to 5s) then start a new one with the
  new config. This is intentionally simple and reliable.
- `EnsureRunning` starts frpc if it died; `Status()` (`running`/`stopped`) feeds the
  heartbeat.
- The interface is isolated so it can later become frp hot-reload without touching
  callers.

> Because the agent owns `frpc`, run the agent under systemd (or launchd) so the
> child is reaped on restart. If you previously ran `frpc` by hand, stop it first to
> avoid duplicate proxies registering with frps.

## frpc.toml generation

Stable, sorted-by-name TOML; one `[[proxies]]` block per enabled tunnel. HTTP uses
`customDomains`; TCP uses `remotePort`. Names are sanitized to `[A-Za-z0-9_-]`.
`localIP` can be any LAN address. See [HANDOFF.md §10](HANDOFF.md) for an example.

## Health checks

Every `health_check_interval_seconds`, probe each enabled tunnel's
`target_host:target_port`:
- `http` → HTTP `GET /` (any response = reachable; falls back to TCP connect).
- `tcp` → TCP connect.
Latency is measured; results `POST /health`. Runs in its own goroutine so it never
blocks heartbeat/sync.

## Logs & maintenance

```bash
# logs (systemd)
journalctl -u oldtians-frp-service -f

# agent status (local)
curl http://127.0.0.1:8899/status

# the live frpc config
cat /etc/oldtians-frp-service/frpc.toml

# force re-registration
sudo systemctl stop oldtians-frp-service
sudo rm /etc/oldtians-frp-service/state.yaml
sudo systemctl start oldtians-frp-service

# restart
sudo systemctl restart oldtians-frp-service
```

The agent process and `frpc` both log to stdout (captured by journald). `frpc` lines
are prefixed `[frpc]`.
