# ofs-gateway-agent

Oldtian's FRP Service 的内网跳板机 Agent。它向控制 API 自动注册、心跳、拉取隧道配置、
渲染 `frpc.toml`、管理 `frpc` 进程、做健康检查，并提供本地发布 API。

## 构建与运行

```bash
cp config.example.yaml config.yaml    # 设置 server_url + register_token
go build -o ofs-gateway-agent ./cmd/gateway-agent
./ofs-gateway-agent -config config.yaml
```

你需要在配置的 `frpc.bin_path` 处放一个 `frpc` 二进制
（从 [frp releases](https://github.com/fatedier/frp/releases) 下载）。

## 安装（Linux/macOS）

```bash
sudo ./install/install.sh ./ofs-gateway-agent
sudo systemctl start oldtians-frp-service      # Linux + systemd
journalctl -u oldtians-frp-service -f
```

Windows：见 `install/install.ps1`。

## 配置与状态

- 配置：[config.example.yaml](config.example.yaml)。`OFS_SERVER_URL` 和
  `OFS_REGISTER_TOKEN` 环境变量会覆盖 `server_url` / `register_token`。
- 状态：持久化到 `agent.state_path`（默认 `<work_dir>/state.yaml`），保存
  `gateway_id`、`gateway_secret`（权限 0600）以及已应用的配置版本。删除它会强制重新注册。

## 它做什么

| 循环 | 默认间隔 | 动作 |
| --- | --- | --- |
| 心跳 | 10s | POST 心跳，`need_sync` 时触发同步 |
| 同步 | 10s（+ 按需） | 拉取配置、渲染 `frpc.toml`、reload `frpc` |
| 健康检查 | 15s | 探测每个目标，上报结果 |
| 本地 API | — | `127.0.0.1:8899` 发布 + 状态 |

详细流程与维护命令：[../docs/AGENT.md](../docs/AGENT.md)。

## 本地 API

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `POST` | `/api/local/publish` | 发布一个内网服务（供 AI 部署使用） |
| `GET` | `/status` | Agent 状态（网关 id、配置版本、frpc 状态） |
| `GET` | `/healthz` | 存活探测 |

见 [../docs/AI_PUBLISH.md](../docs/AI_PUBLISH.md)。

## 包结构

| 包 | 职责 |
| --- | --- |
| `internal/config` | YAML 配置 + 持久化状态 |
| `internal/serverapi` | 控制 API 的类型化 HTTP 客户端 |
| `internal/register` | 首次启动注册 + 主机信息 / LAN IP |
| `internal/heartbeat` | 心跳循环 |
| `internal/sync` | 拉取配置 -> 渲染 -> reload |
| `internal/frpc` | `frpc.toml` 渲染 + 进程管理 |
| `internal/healthcheck` | 目标探测 + 上报 |
| `internal/publish` | 把发布转发到服务端 + 本地对账 |
| `internal/localapi` | 本地 HTTP API |
