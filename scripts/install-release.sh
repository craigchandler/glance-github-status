#!/bin/sh
set -eu

REPO="${GITHUB_REPOSITORY:-craigchandler/glance-github-status}"
VERSION="${VERSION:-latest}"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT INT TERM

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) printf 'Unsupported architecture: %s\n' "$(uname -m)" >&2; exit 1 ;;
esac

if [ "$(uname -s)" != "Linux" ]; then
  printf 'This installer currently supports Linux only.\n' >&2
  exit 1
fi

if [ "$VERSION" = "latest" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
  [ -n "$VERSION" ] || { printf 'Could not determine latest release.\n' >&2; exit 1; }
fi

ASSET="github-status_${VERSION}_linux_${ARCH}"
BASE="https://github.com/$REPO/releases/download/$VERSION"

curl -fL "$BASE/$ASSET" -o "$TMPDIR/github-status"
curl -fL "$BASE/$ASSET.sha256" -o "$TMPDIR/$ASSET.sha256"
(
  cd "$TMPDIR"
  sed "s|$ASSET|github-status|" "$ASSET.sha256" | sha256sum -c -
)
chmod 0755 "$TMPDIR/github-status"

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
"$SCRIPT_DIR/install-systemd.sh" "$TMPDIR/github-status"
