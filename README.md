# noise-agent

Noise 节点监控采集端（Go 实现，无外部依赖），运行在目标 Linux VPS 上，采集硬件信息、GeoIP 地理位置、系统实时指标（CPU / 内存 / 磁盘 / 负载 / 实时网速 / 累计流量 / 系统运行时间）并执行 ICMP / TCP 延迟探测任务，上报至 `noise-server`。

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

## 运行机制

1. **启动同步**：调用 `GET /api/v1/agent/config` 拉取上报周期（`refresh`）、GeoIP 配置（`geoip`）、流量配置（`traffic`）与绑定的延迟监控任务（`tasks`）。
2. **硬件与 GeoIP 上报**：读取系统 OS、内核、架构、CPU 型号与核心数、物理内存、磁盘容量，并按服务端下发的 GeoIP 提供商（`ipinfo` / `ip-api` / `ip.sb`）查询出口 IP、国家地区代码与城市，调用 `POST /api/v1/agent/info` 上报。
3. **实时遥测心跳**：按 `refresh` 秒周期采集系统指标并调用 `POST /api/v1/agent/report` 上报，自动比对响应中的 `configVersion`，一旦后台修改配置或任务绑定即刻热更新。
4. **ICMP / TCP 延迟探测**：按每个监控任务的 `interval` 独立调度（5 秒超时、同任务不重叠），通过 `POST /api/v1/agent/latency-results` 批量上报。
