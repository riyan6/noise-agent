#!/usr/bin/env bash
set -euo pipefail

ENDPOINT="${NOISE_ENDPOINT:-}"
TOKEN="${NOISE_TOKEN:-}"
MONTH_ROTATE="${NOISE_MONTH_ROTATE:-1}"
DOWNLOAD_URL="${NOISE_AGENT_DOWNLOAD_URL:-}"
BASE_URL="${NOISE_AGENT_BASE_URL:-}"
GITHUB_REPO="${NOISE_GITHUB_REPO:-riyan6/noise-agent}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    -e|--endpoint)
      ENDPOINT="${2:-}"
      shift 2
      ;;
    -t|--token)
      TOKEN="${2:-}"
      shift 2
      ;;
    --month-rotate)
      MONTH_ROTATE="${2:-1}"
      shift 2
      ;;
    -u|--download-url)
      DOWNLOAD_URL="${2:-}"
      shift 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

if [[ -z "$ENDPOINT" || -z "$TOKEN" ]]; then
  echo "Error: both -e <endpoint> and -t <token> are required." >&2
  exit 1
fi

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "Error: install.sh only supports Linux systems with systemd." >&2
  exit 1
fi

ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  armv7l|armv7) ARCH="armv7" ;;
  i386|i686) ARCH="386" ;;
  *)
    echo "Error: unsupported architecture: $ARCH_RAW" >&2
    exit 1
    ;;
esac

BINARY_NAME="noise-agent-linux-${ARCH}"
INSTALL_BIN="/usr/local/bin/noise-agent"
CONFIG_DIR="/etc/noise-agent"
ENV_FILE="${CONFIG_DIR}/agent.env"
SERVICE_FILE="/etc/systemd/system/noise-agent.service"

fetch_file() {
  local url="$1"
  local dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --connect-timeout 10 --max-time 120 "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" --timeout=30 "$url"
  else
    echo "Error: neither curl nor wget is installed." >&2
    return 1
  fi
}

TMP_BIN="$(mktemp)"
trap 'rm -f "$TMP_BIN"' EXIT

DOWNLOADED=0
CANDIDATES=()
if [[ -n "$DOWNLOAD_URL" ]]; then
  CANDIDATES+=("$DOWNLOAD_URL")
fi
if [[ -n "$BASE_URL" ]]; then
  CANDIDATES+=("${BASE_URL%/}/${BINARY_NAME}")
fi
CANDIDATES+=(
  "${ENDPOINT%/}/agent/${BINARY_NAME}"
  "${ENDPOINT%/}/downloads/${BINARY_NAME}"
  "https://github.com/${GITHUB_REPO}/releases/latest/download/${BINARY_NAME}"
)

for url in "${CANDIDATES[@]}"; do
  echo "==> Downloading noise-agent from ${url} ..."
  if fetch_file "$url" "$TMP_BIN" && [[ -s "$TMP_BIN" ]]; then
    DOWNLOADED=1
    break
  fi
done

if [[ "$DOWNLOADED" -eq 0 ]]; then
  if [[ -f "./cmd/noise-agent/main.go" ]] && command -v go >/dev/null 2>&1; then
    echo "==> Building noise-agent from local source ..."
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$TMP_BIN" ./cmd/noise-agent
    DOWNLOADED=1
  else
    echo "Error: failed to download ${BINARY_NAME}. Set NOISE_AGENT_DOWNLOAD_URL or host ${BINARY_NAME} on your server/GitHub Releases." >&2
    exit 1
  fi
fi

install -m 0755 "$TMP_BIN" "$INSTALL_BIN"

mkdir -p "$CONFIG_DIR"
chmod 0700 "$CONFIG_DIR"
cat > "$ENV_FILE" <<EOF
NOISE_ENDPOINT=${ENDPOINT}
NOISE_TOKEN=${TOKEN}
NOISE_MONTH_ROTATE=${MONTH_ROTATE}
EOF
chmod 0600 "$ENV_FILE"

cat > "$SERVICE_FILE" <<'EOF'
[Unit]
Description=Noise Monitoring Agent
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/noise-agent/agent.env
ExecStart=/usr/local/bin/noise-agent -e ${NOISE_ENDPOINT} -t ${NOISE_TOKEN} --month-rotate ${NOISE_MONTH_ROTATE}
Restart=always
RestartSec=5
AmbientCapabilities=CAP_NET_RAW
CapabilityBoundingSet=CAP_NET_RAW

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable noise-agent.service
systemctl restart noise-agent.service

echo "==> noise-agent installed and started successfully."
systemctl --no-pager --full status noise-agent.service || true
