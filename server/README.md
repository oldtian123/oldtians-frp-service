# ofs-control-api

Oldtian's FRP Service 的公网服务端控制面。Go + Gin + SQLite。

## 构建与运行

```bash
cp config.example.yaml config.yaml   # 编辑 token + frp 设置
go run ./cmd/control-api -config config.yaml
# 或
go build -o ofs-control-api ./cmd/control-api
./ofs-control-api -config config.yaml
```

SQLite 数据库（及其父目录）会在首次启动时自动创建，建表语句每次启动都会幂等执行。

## 配置

见 [config.example.yaml](config.example.yaml)。每个配置项都可以用 `OFS_*` 环境变量覆盖
（方便配合 Docker secrets）：

| 环境变量 | 覆盖的配置项 |
| --- | --- |
| `OFS_CONFIG` | 配置文件路径（命令行默认 `config.yaml`） |
| `OFS_LISTEN_ADDR` / `OFS_LISTEN_PORT` | `server.listen_addr` / `listen_port` |
| `OFS_PUBLIC_BASE_URL` | `server.public_base_url` |
| `OFS_DATABASE_PATH` | `database.path` |
| `OFS_ADMIN_TOKEN` | `auth.admin_token` |
| `OFS_REGISTER_TOKEN` | `auth.register_token` |
| `OFS_FRP_SERVER_ADDR` / `OFS_FRP_SERVER_PORT` | `frp.server_addr` / `server_port` |
| `OFS_FRP_AUTH_TOKEN` | `frp.auth_token` |
| `OFS_FRP_HTTP_DOMAIN_SUFFIX` | `frp.http_domain_suffix` |
| `OFS_DEBUG` | 设置后启用 Gin 调试模式 |

## 包结构

| 包 | 职责 |
| --- | --- |
| `internal/config` | YAML 加载 + 环境变量覆盖 + 校验 |
| `internal/database` | SQLite 存储、模型、CRUD、内嵌 schema |
| `internal/httpx` | JSON 错误信封、鉴权中间件、token 工具 |
| `internal/eventlog` | 向 `event_logs` 写结构化事件 |
| `internal/gateway` | 注册 / 心跳 / 配置 + 管理端 列表 / 汇总 / 事件 |
| `internal/tunnel` | 隧道 Service（校验、端口分配）+ 管理处理器 |
| `internal/publish` | `/api/publish` + 网关作用域发布 |
| `internal/health` | 健康上报 + 查询处理器 |

## API

完整参考：[../docs/API.md](../docs/API.md)。快速探活：

```bash
curl http://127.0.0.1:8088/healthz
```

## Docker

```bash
docker build -t oldtians-frp-service/ofs-control-api .
```

镜像无需 CGO（纯 Go SQLite 驱动），以非 root 用户运行，并内置 `/healthz` 健康检查。
见 [../deploy/docker-compose.yml](../deploy/docker-compose.yml)。
