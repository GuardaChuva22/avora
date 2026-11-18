#!/usr/bin/env bash
# Builds Avora for your Linux server and installs or updates it there.
#
#   ./scripts/deploy-vps.sh
#
# Uploads only the two binaries and deploy/vps/. Secrets are generated on the
# server (/etc/avora) and never leave it. Target: AVORA_VPS_HOST and friends,
# from the environment or .env.vps (see scripts/_vps.sh).
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/_vps.sh"
ROOT="$AVORA_ROOT"
ARCH="${AVORA_VPS_ARCH:-arm64}"

GO="$(command -v go || true)"
[ -n "$GO" ] || GO="/c/Program Files/Go/bin/go"
[ -x "$GO" ] || { echo "Go not found" >&2; exit 1; }

STAGE="$(mktemp -d)"
trap 'rm -f "${VPS_KEY_TMP:-}"; rm -rf "$STAGE"' EXIT

echo "== build web app"
# The server embeds internal/webui/static, so the app must be built first.
(cd "$ROOT/web" && { [ -d node_modules ] || npm ci; } && npm run build)

echo "== build linux/$ARCH"
mkdir -p "$STAGE/bin"
for cmd in avora-server avora-runtime; do
  (cd "$ROOT" && GOOS=linux GOARCH="$ARCH" CGO_ENABLED=0 "$GO" build -trimpath -ldflags "-s -w" -o "$STAGE/bin/$cmd" "./cmd/$cmd")
done
cp -r "$ROOT/deploy/vps" "$STAGE/vps"

echo "== upload"
REMOTE=$(vps_ssh "mktemp -d /tmp/avora-deploy.XXXXXX")
tar -C "$STAGE" -cf - . | vps_ssh "tar -C $REMOTE -xmf -"

vps_ssh "sudo AVORA_TIMEZONE='${AVORA_TIMEZONE:-}' bash $REMOTE/vps/install.sh $REMOTE; status=\$?; rm -rf $REMOTE; exit \$status"
echo "Done. Logs on the server: sudo journalctl -u avora-server -u avora-runtime -f"
