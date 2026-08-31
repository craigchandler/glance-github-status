#!/bin/sh
set -eu

BIN="${1:-./github-status}"
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PROJECT_DIR=$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)

if [ ! -f "$BIN" ]; then
  printf 'Binary not found: %s\n' "$BIN" >&2
  exit 1
fi

id githubstatus >/dev/null 2>&1 || useradd --system --home /var/lib/github-status --shell /usr/bin/nologin githubstatus
install -d -m 0755 /var/lib/github-status
install -m 0755 "$BIN" /usr/local/bin/github-status
install -m 0644 "$PROJECT_DIR/systemd/github-status.service" /etc/systemd/system/github-status.service

if [ ! -f /etc/github-status.env ]; then
  cat > /etc/github-status.env <<'EOT'
GITHUB_TOKEN=replace_me
LISTEN_ADDR=127.0.0.1:8794
REFRESH_INTERVAL=5m
HTTP_TIMEOUT=15s
CONFIG_FILE=/etc/github-status.json
EOT
  chmod 600 /etc/github-status.env
fi

if [ ! -f /etc/github-status.json ]; then
  install -m 0644 "$PROJECT_DIR/config.example.json" /etc/github-status.json
fi

systemctl daemon-reload
printf '%s\n' 'Installed.'
printf '%s\n' 'Edit /etc/github-status.env and /etc/github-status.json, then run:'
printf '%s\n' '  sudo systemctl enable --now github-status'
