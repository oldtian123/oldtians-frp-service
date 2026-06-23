# Deployment on 1Panel (public server)

This guide deploys `frps` + `ofs-control-api` with Docker Compose and exposes them
through 1Panel's built-in Nginx/OpenResty.

## Prerequisites

- A public Linux server with 1Panel installed.
- Docker + Docker Compose (1Panel manages these).
- DNS: a wildcard `*.tunnel.oldtian.top` and `api.tunnel.oldtian.top` pointing at the
  server's public IP.
- A wildcard TLS certificate for `*.tunnel.oldtian.top` (1Panel can issue one via
  DNS-01 with your DNS provider).

## 1. Get the code & configure

```bash
git clone <your-repo> oldtians-frp-service
cd oldtians-frp-service/deploy

# control-api config
cp ../server/config.example.yaml control-api.config.yaml
```

Edit `control-api.config.yaml`:
- `server.public_base_url`: `https://api.tunnel.oldtian.top`
- `auth.admin_token`, `auth.register_token`: strong random strings.
- `frp.server_addr`: `tunnel.oldtian.top` (public DNS to this server).
- `frp.auth_token`: a strong random string.
- `frp.http_domain_suffix`: `tunnel.oldtian.top`.

Edit `frps.toml`:
- `auth.token`: **must equal** `control-api.config.yaml` → `frp.auth_token`.
- `webServer.password`: change it.

> Tip: instead of putting secrets in the file, inject them via `environment:` in
> `docker-compose.yml` (`OFS_ADMIN_TOKEN`, `OFS_REGISTER_TOKEN`, `OFS_FRP_AUTH_TOKEN`).

## 2. Start the stack

```bash
docker compose up -d
docker compose ps
curl http://127.0.0.1:8088/healthz      # {"status":"ok",...}
```

Ports after start:
- `7000` — **public**, frpc connects here.
- `127.0.0.1:8080` — frps vhost HTTP (Nginx only).
- `127.0.0.1:8088` — control-api (Nginx only).
- `127.0.0.1:7500` — frps dashboard (optional).

## 3. Firewall

Open **7000/tcp** to the internet (frpc control connection) plus each TCP tunnel
`remotePort` you allocate (default range `22000–22999`, or specific ports like
`2222`). Do **not** expose 8080/8088/7500 publicly — they sit behind Nginx /
localhost.

In 1Panel: Host → Firewall → add the rules. Also adjust your cloud provider's
security group.

## 4. Nginx / OpenResty (wildcard) via 1Panel

Create two sites in 1Panel (Websites → Create, type "Reverse proxy") or paste the
blocks from [`../deploy/nginx.example.conf`](../deploy/nginx.example.conf):

1. **`*.tunnel.oldtian.top`** → `http://127.0.0.1:8080`
   - Enable the wildcard SSL cert.
   - Forward the `Host` header unchanged (frps routes HTTP tunnels by Host).
   - Enable WebSocket (Upgrade/Connection headers).
2. **`api.tunnel.oldtian.top`** → `http://127.0.0.1:8088`
   - Normal SSL cert (or wildcard).

Adding a new HTTP tunnel later requires **no Nginx change** — the wildcard handles
every subdomain.

## 5. SSL

- Wildcard certificate `*.tunnel.oldtian.top` requires DNS-01 validation. In 1Panel:
  Certificates → apply, choose DNS account, domain `*.tunnel.oldtian.top` (add the
  apex/`api` SAN if needed). Bind the cert to the sites above.
- 1Panel auto-renews.

## 6. Verify

```bash
# control-api through Nginx
curl https://api.tunnel.oldtian.top/healthz

# register a gateway (from anywhere)
curl -X POST https://api.tunnel.oldtian.top/api/gateways/register \
  -H 'Content-Type: application/json' \
  -d '{"register_token":"<register_token>","name":"home-gateway"}'
```

Then start the agent on the jump host (see [AGENT.md](AGENT.md)), create a tunnel,
and open `https://<subdomain>.tunnel.oldtian.top`.

## 7. Updating

```bash
cd deploy
git pull
docker compose build ofs-control-api
docker compose up -d
```

The SQLite DB persists in the `control_api_data` Docker volume; the schema migrates
automatically on start.

## 8. Backups

Back up the `control_api_data` volume (the `.db`, `.db-wal`, `.db-shm` files) and
your `control-api.config.yaml` + `frps.toml`.
