# ofs-control-api

The public-server control plane for Oldtian's FRP Service. Go + Gin + SQLite.

## Build & run

```bash
cp config.example.yaml config.yaml   # edit tokens + frp settings
go run ./cmd/control-api -config config.yaml
# or
go build -o ofs-control-api ./cmd/control-api
./ofs-control-api -config config.yaml
```

The SQLite database (and its parent directory) is created automatically on first
boot, and the schema is applied idempotently every start.

## Configuration

See [config.example.yaml](config.example.yaml). Every key can be overridden by an
`OFS_*` environment variable (handy for Docker secrets):

| Env | Overrides |
| --- | --- |
| `OFS_CONFIG` | config file path (CLI default `config.yaml`) |
| `OFS_LISTEN_ADDR` / `OFS_LISTEN_PORT` | `server.listen_addr` / `listen_port` |
| `OFS_PUBLIC_BASE_URL` | `server.public_base_url` |
| `OFS_DATABASE_PATH` | `database.path` |
| `OFS_ADMIN_TOKEN` | `auth.admin_token` |
| `OFS_REGISTER_TOKEN` | `auth.register_token` |
| `OFS_FRP_SERVER_ADDR` / `OFS_FRP_SERVER_PORT` | `frp.server_addr` / `server_port` |
| `OFS_FRP_AUTH_TOKEN` | `frp.auth_token` |
| `OFS_FRP_HTTP_DOMAIN_SUFFIX` | `frp.http_domain_suffix` |
| `OFS_DEBUG` | when set, enables Gin debug mode |

## Packages

| Package | Responsibility |
| --- | --- |
| `internal/config` | YAML load + env override + validation |
| `internal/database` | SQLite store, models, CRUD, schema (embedded) |
| `internal/httpx` | JSON error envelope, auth middleware, token helpers |
| `internal/eventlog` | structured writes to `event_logs` |
| `internal/gateway` | register / heartbeat / config + admin list / summary / events |
| `internal/tunnel` | tunnel Service (validation, port alloc) + management handlers |
| `internal/publish` | `/api/publish` + gateway-scoped publish |
| `internal/health` | health report + query handlers |

## API

Full reference: [../docs/API.md](../docs/API.md). Quick probe:

```bash
curl http://127.0.0.1:8088/healthz
```

## Docker

```bash
docker build -t oldtians-frp-service/ofs-control-api .
```

The image is CGO-free (pure-Go SQLite driver) and runs as a non-root user with a
built-in `/healthz` healthcheck. See [../deploy/docker-compose.yml](../deploy/docker-compose.yml).
