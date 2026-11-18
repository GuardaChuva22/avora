#!/usr/bin/env bash
# Copies the Avora admin token from your server straight to the Windows
# clipboard, so you can paste it into the app's login without it ever
# showing on screen or landing in shell history.
#
#   bash scripts/admin-token.sh
#
# Target: AVORA_VPS_HOST, from the environment or .env.vps.
set -euo pipefail
command -v clip.exe >/dev/null || { echo "clip.exe not found (run this on Windows)." >&2; exit 1; }
. "$(dirname "${BASH_SOURCE[0]}")/_vps.sh"

# The token goes server -> pipe -> clipboard; it is never echoed or stored here.
vps_ssh "sudo sed -n 's/^AVORA_ADMIN_TOKEN=//p' /etc/avora/avora.env" | tr -d '\r\n' | clip.exe

echo "Token copied to the clipboard. Paste it into the Avora login (Ctrl+V)."
