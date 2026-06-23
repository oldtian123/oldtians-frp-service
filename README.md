# Oldtian's FRP Service

一个小巧的、面向个人使用的内网穿透控制系统，基于
[frp](https://github.com/fatedier/frp) 构建。它**不重新实现**穿透协议——而是在
`frps` / `frpc` 之上做编排，提供一套干净的 API 来注册网关、管理隧道、发布内网服务
（包括由 AI 部署脚本发布），并把健康状态上报给未来的云端面板。

> 本项目**只提供 API**，不附带 Web 控制台。后续会有独立的云端可视化面板来对接这些 API。

## 组件

| 名称 | 二进制 | 运行位置 | 职责 |
| --- | --- | --- | --- |
| 控制 API | `ofs-control-api` | 公网服务器 | REST API + SQLite 状态，告诉 Agent 该运行什么 |
| 网关 Agent | `ofs-gateway-agent` | 内网跳板机 | 注册、心跳、生成 `frpc.toml`、管理 `frpc`、本地发布 API |
| frps | `frps` | 公网服务器 | frp 服务端（真正的穿透端点） |
| frpc | `frpc` | 内网跳板机 | frp 客户端（由 Agent 管理） |

```
浏览器 ──HTTPS──> 1Panel OpenResty/Nginx ──> frps(vhost 8080) ──> frpc ──> 192.168.x.x:port
                         │
Agent/AI ──> ofs-control-api(8088) <── 心跳/配置/健康 ── ofs-gateway-agent ──管理──> frpc
```

完整架构见 [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md)。

## 仓库结构

```
oldtians-frp-service/
├── server/   # ofs-control-api（Go、Gin、SQLite）
├── agent/    # ofs-gateway-agent（Go，管理 frpc）
├── deploy/   # docker-compose.yml、frps.toml、nginx.example.conf
├── docs/     # 架构、API、部署、交接、故障排查……
└── go.work   # 将 server + agent 绑在一起的 Go workspace
```

## 快速开始（本地开发）

前置要求：Go 1.25+。

### 1. 运行控制 API

```bash
cd server
cp config.example.yaml config.yaml      # 编辑 token
go run ./cmd/control-api -config config.yaml
# -> 监听 0.0.0.0:8088，SQLite 自动在 ./data 下创建
```

### 2. 运行网关 Agent（在跳板机上）

```bash
cd agent
cp config.example.yaml config.yaml      # 设置 server_url + register_token
# 把 frpc 二进制放到配置里指定的 bin_path
go run ./cmd/gateway-agent -config config.yaml
```

Agent 会自动注册、开始心跳、生成 `frpc.toml` 并启动 `frpc`。它的本地发布 API
监听在 `127.0.0.1:8899`。

### 3. 创建一个隧道

```bash
# 通过管理 API 创建 HTTP 隧道
curl -X POST http://127.0.0.1:8088/api/tunnels \
  -H "Authorization: Bearer <admin_token>" \
  -H "Content-Type: application/json" \
  -d '{"gateway_id":"home-gateway","name":"openlist","type":"http",
       "target_host":"192.168.1.10","target_port":5244,
       "domain":"openlist.tunnel.oldtian.top"}'
```

或者让 AI 部署脚本在本地发布一个：

```bash
curl -X POST http://127.0.0.1:8899/api/local/publish \
  -H "Content-Type: application/json" \
  -d '{"name":"ai-demo","type":"http","target_host":"192.168.1.30",
       "target_port":5173,"subdomain":"ai-demo","ttl":"24h","source":"ai-deploy"}'
```

## 生产部署

公网侧通过 Docker Compose 部署，并跑在 1Panel 的 Nginx/OpenResty 之后：

```bash
cd deploy
cp ../server/config.example.yaml control-api.config.yaml   # 编辑
# 编辑 frps.toml（auth.token 必须与 control-api 的 frp.auth_token 一致）
docker compose up -d
```

完整流程：[docs/DEPLOYMENT_1PANEL.md](docs/DEPLOYMENT_1PANEL.md)。

## 文档

- [docs/HANDOFF.md](docs/HANDOFF.md) — 接手项目从这里开始
- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) — 架构
- [docs/API.md](docs/API.md) — API 参考
- [docs/DEPLOYMENT_1PANEL.md](docs/DEPLOYMENT_1PANEL.md) — 1Panel 部署
- [docs/AGENT.md](docs/AGENT.md) — Agent 说明
- [docs/PANEL_INTEGRATION.md](docs/PANEL_INTEGRATION.md) — 面板对接
- [docs/AI_PUBLISH.md](docs/AI_PUBLISH.md) — AI 发布
- [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) — 故障排查
- [docs/CHANGELOG.md](docs/CHANGELOG.md) — 变更记录

## 范围（v1）

包含：网关注册、隧道（HTTP + TCP）、配置版本管理、AI 发布、健康上报、面板数据 API。

刻意不做（按设计）：Web 控制台、多用户 / RBAC、计费、多租户隔离、
P2P/XTCP/STCP/SUDP、自研穿透协议。
