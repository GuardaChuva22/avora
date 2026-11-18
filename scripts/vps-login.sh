#!/usr/bin/env bash
# Logs the server's agent CLIs into your subscription, interactively over SSH.
# The login is stored by the official CLI under /var/lib/avora and never
# passes through Avora.
#
#   ./scripts/vps-login.sh claude   # opens a link; paste the code back here
#   ./scripts/vps-login.sh codex    # device code: approve it on chatgpt.com
#   ./scripts/vps-login.sh status
#
# Target: AVORA_VPS_HOST, from the environment or .env.vps.
set -euo pipefail
WHAT="${1:?usage: vps-login.sh claude|codex|status}"
. "$(dirname "${BASH_SOURCE[0]}")/_vps.sh"

AS_AVORA="sudo -u avora -H env HOME=/var/lib/avora PATH=/var/lib/avora/.local/bin:/usr/local/bin:/usr/bin:/bin CLAUDE_CONFIG_DIR=/var/lib/avora/claude CODEX_HOME=/var/lib/avora/codex"
case "$WHAT" in
  claude) CMD="$AS_AVORA claude auth login" ;;
  codex) CMD="$AS_AVORA codex login --device-auth" ;;
  status) CMD="$AS_AVORA claude auth status; $AS_AVORA codex login status" ;;
  *) echo "usage: vps-login.sh claude|codex|status" >&2; exit 2 ;;
esac
vps_ssh_tty "$CMD"
