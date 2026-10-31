#!/usr/bin/env bash
set -euo pipefail

# Exercise the actual download loop without installing files, touching systemd, or using the network.
script="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/install.sh"
ENDPOINT="https://admin.example.test"
GITHUB_REPO="riyan6/noise-agent"
AGENT_VERSION="v0.2.0"
BINARY_NAME="noise-agent-linux-amd64"
DOWNLOAD_URL=""
BASE_URL=""
urls=()
fetch_file() {
  urls+=("$1")
  if [[ "$1" == "https://github.com/riyan6/noise-agent/releases/download/v0.2.0/noise-agent-linux-amd64" ]]; then
    printf '\177ELFtest-binary' > "$2"
  else
    printf '<!doctype html><html>SPA fallback</html>' > "$2"
  fi
}
source <(sed -n '/^TMP_BIN=/,/^install -m /{ /^install -m /d; p; }' "$script")
[[ "$DOWNLOADED" == 1 && "${#urls[@]}" == 3 ]]
[[ "$(head -c 4 "$TMP_BIN")" == $'\x7fELF' ]]
printf 'Installer rejects SPA HTML and selects the pinned v0.2.0 binary.\n'
