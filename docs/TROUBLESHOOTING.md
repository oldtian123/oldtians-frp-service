# Troubleshooting

General tools:
```bash
curl http://127.0.0.1:8088/healthz                 # control-api alive
curl http://127.0.0.1:8899/status                  # agent status
journalctl -u oldtians-frp-service -f              # agent + frpc logs
docker compose logs -f ofs-control-api             # server logs
docker compose logs -f frps                        # frps logs
cat /etc/oldtians-frp-service/frpc.toml            # live frpc config
```

---

## 1. gateway-agent cannot register

Symptoms: agent log shows `register: failed (...), retrying`.

- **Wrong `register_token`** → control-api returns 401 `invalid register token`.
  Make `agent` `register_token` == server `auth.register_token`.
- **Wrong `server_url` / network** → `request ...: dial tcp ...`. Verify the agent
  can reach the control-api (`curl <server_url>/healthz` from the jump host). Check
  VPN/firewall and that `api.tunnel.oldtian.top` resolves + Nginx is up.
- **TLS errors** → ensure the cert chain for `api.tunnel.oldtian.top` is valid.
- After fixing, the agent retries automatically (backoff up to 30s).

## 2. Gateway shows offline

`online: false` in `GET /api/gateways`, or `gateway_online` is 0 in summary.

- Online is derived from `last_seen_at` within 60s. If heartbeats stopped, the agent
  is down or can't reach the server.
- Check the agent is running (`systemctl status oldtians-frp-service`) and its log
  for `heartbeat: failed`.
- Confirm the `gateway_secret` is still valid; if `state.yaml` was wiped/rotated,
  re-register (see AGENT.md).

## 3. frpc cannot connect to frps

`[frpc]` log lines show `login to server failed` / `connection refused`.

- **Port 7000 not open** publicly → open it in the firewall + cloud security group.
- **Token mismatch** → frps `auth.token` must equal the `frp_auth_token` agents pull
  (which equals control-api `frp.auth_token`). Fix and restart frps + re-sync agent.
- **Wrong `server_addr`/`server_port`** in the pulled config → check control-api
  `frp.server_addr`/`frp.server_port`.
- Verify frps is up: `docker compose ps`, `docker compose logs frps`.

## 4. frpc.toml generation errors

- Agent log shows `sync: render warning: skip proxy ...` → that tunnel had an invalid
  host/port/domain and was skipped. Fix the tunnel via the admin API.
- `sync: frpc reload failed: frpc config validation failed: ...` → `frpc verify`
  rejected the file. Inspect `/etc/oldtians-frp-service/frpc.toml`. Usually a bad
  custom domain or duplicate proxy name. Validation runs before the live file is
  replaced, so the previous good config stays in place.
- Names are sanitized to `[A-Za-z0-9_-]`; duplicates after sanitizing are skipped.

## 5. HTTP domain not reachable

`https://<sub>.tunnel.oldtian.top` fails.

Work through the chain:
1. **DNS** → `<sub>.tunnel.oldtian.top` resolves to the server (wildcard A record).
2. **Nginx** → the wildcard site is up with valid SSL; it proxies to `127.0.0.1:8080`
   and forwards the `Host` header.
3. **frps vhost** → `vhostHTTPPort = 8080`; `docker compose logs frps`.
4. **Tunnel exists + enabled** → `GET /api/tunnels`; `domain` matches the hostname.
5. **Agent synced** → `current_config_version == latest_config_version`; the proxy is
   in `frpc.toml`.
6. **Target up** → `health_status` is `healthy`; the service answers on the LAN.

## 6. 1Panel Nginx reverse proxy fails

- 502/504 → frps not listening on `127.0.0.1:8080`, or the container port isn't
  published to localhost. Check `docker compose ps` shows `127.0.0.1:8080->8080`.
- Wrong host routing → make sure Nginx passes `Host $host` (frps routes by Host). Do
  not override it to a fixed value.
- WebSocket apps break → add `Upgrade`/`Connection "upgrade"` headers (see
  `deploy/nginx.example.conf`).

## 7. TCP tunnel cannot connect

`server.oldtian.top:2222` refuses.

- **`remotePort` not open** in the firewall/security group → open it publicly.
- **DNS** → `server.oldtian.top` (or whatever name) points at the frps host.
- **Proxy present?** → `frpc.toml` has the `tcp` block with the right `remotePort`;
  agent synced.
- **Target reachable** → the jump host can reach `target_host:target_port`
  (e.g. `nc -vz 192.168.1.20 22`).
- Port already taken on frps → frps log will complain; pick a different `remote_port`.

## 8. publish returns ok but the URL is dead

- If `status` was `pending_sync`, the local reload failed — see
  [AI_PUBLISH.md](AI_PUBLISH.md) and §4 above. The tunnel goes live once frpc reloads.
- If `status` was `ok` but still dead, walk §5 (HTTP) — most often DNS/Nginx/SSL or
  the target service itself is down (`health_status`).
- Check the tunnel didn't already **expire** (TTL): `GET /api/tunnels` → `enabled`
  may be `false` with a past `expires_at` (the janitor disables expired tunnels).

## 9. health check reports unhealthy

- The agent could not reach `target_host:target_port` from the jump host.
- `message` tells you why (`connection refused`, `context deadline exceeded`, etc.).
- Verify the target service is up and the jump host has a network route to it
  (`nc -vz <host> <port>`).
- HTTP probes count any HTTP response as healthy; a service that only speaks raw TCP
  on an "http" tunnel still passes via the TCP-connect fallback.

## 10. config_version changed but agent didn't sync

- The agent only marks a version applied **after a successful frpc reload**. If
  reloads keep failing (frpc missing / config invalid), `current_config_version`
  stays behind on purpose. Fix the underlying frpc issue (§3/§4).
- Confirm heartbeats are flowing (§2). Each heartbeat with `need_sync` triggers a
  sync; the periodic sync loop also retries every `sync_interval_seconds`.
- Check `frpc.bin_path` exists and is executable.
- As a last resort, restart the agent: `systemctl restart oldtians-frp-service`.

---

## SQLite notes

- "database is locked" under heavy concurrent writes is mitigated by WAL + a single
  writer connection. If you see it, you are likely running two control-api instances
  against the same file — don't.
- Back up `*.db`, `*.db-wal`, `*.db-shm` together.
