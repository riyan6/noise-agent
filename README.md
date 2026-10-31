# noise-agent

Noise 节点监控采集端（Go >=1.23），运行在 Linux VPS 上。v0.2.0 使用 WebSocket 向 noise-admin 的 Cloudflare Worker 上报实时指标，执行 ICMP/TCP 延迟任务；不创建本地数据库。

## 构建跨平台二进制

在部署前可构建 Linux 二进制并托管（例如放在静态服务器、Nginx `/agent/` 目录或 GitHub Releases）：

```powershell
# Linux amd64
$env:GOOS = "linux"; $env:GOARCH = "amd64"; $env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o dist/noise-agent-linux-amd64 ./cmd/noise-agent

# Linux arm64
$env:GOOS = "linux"; $env:GOARCH = "arm64"; $env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o dist/noise-agent-linux-arm64 ./cmd/noise-agent
```

## 一键安装（由 `noise-admin` 生成）

```bash
wget -qO- -- 'https://example.com/install.sh' | sudo bash -s -- -e 'https://api.example.com' -t '<NODE_TOKEN>' --month-rotate 1
```

可通过环境变量自定义二进制下载源：
- `NOISE_AGENT_DOWNLOAD_URL`：指定完整二进制下载地址
- `NOISE_AGENT_BASE_URL`：指定二进制所在目录前缀（自动拼接 `/noise-agent-linux-<arch>`）
- `NOISE_GITHUB_REPO`：指定 GitHub Releases 仓库（默认 `riyan6/noise-agent`）
- `NOISE_AGENT_VERSION`：指定 Release 标签（默认 `v0.2.0`，避免意外下载不兼容的旧版本）

使用默认下载源前，先推送新版 Agent 并等待 GitHub Actions 发布 `v0.2.0`。安装脚本检查 ELF 文件头，不会把 Worker 的 SPA 网页误装成程序。

## 运行机制

1. **启动同步**：以 Bearer Token 建立 `/api/v1/agent/connect` WebSocket，接收上报周期、GeoIP、流量和延迟任务配置；后续配置直接通过此连接下发。
2. **硬件与 GeoIP 上报**：读取系统 OS、内核、架构、CPU 型号与核心数、物理内存、磁盘容量，并按服务端下发的 GeoIP 提供商（`ipinfo` / `ip-api` / `ip.sb`）查询出口 IP、国家地区代码与城市，调用 `POST /api/v1/agent/info` 上报。
3. **实时指标**：默认每 3 秒采集，通过 WebSocket 发送；采集与网络收发解耦，断网仍继续采集。
4. **ICMP/TCP 延迟**：每个任务独立按至少 30 秒周期调度，默认 60 秒、超时 5 秒、同任务不重叠。结果附在实时消息中，减少请求数。
5. **历史归档**：五分钟原始采样与延迟结果合并为归档；仅在 D1 提交并返回 `archiveAck` 后移出内存重试队列。已完成的窗口最多保留约十分钟用于重试，过期自动丢弃，不会无限占用内存。进程重启会丢失未归档的数据。
6. **重连**：指数退避并加入随机抖动，普通故障最大约一分钟，鉴权失败约五分钟重试；没有周期性 HTTP 指标回退路径。

v0.2.0 需要新的 Cloudflare 后端，不能直接连接旧 Go server。迁移顺序和 D1 数据转换说明见 noise-admin 仓库 README。原 `-e/-t/--month-rotate` 安装参数保留；月度流量以云端累计检查点为准，三天历史清理不会清空月度累计。
