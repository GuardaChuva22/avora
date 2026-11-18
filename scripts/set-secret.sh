#!/usr/bin/env bash
# Stores a secret on the server without showing it on screen, in history or
# in process lists. The value travels only through the SSH connection's stdin.
#
#   ./scripts/set-secret.sh AVORA_NATIVE_API_KEY
#
# Target: AVORA_VPS_HOST, from the environment or .env.vps.
set -euo pipefail
NAME="${1:?usage: set-secret.sh NAME}"
. "$(dirname "${BASH_SOURCE[0]}")/_vps.sh"

read -rsp "Valor de $NAME (não aparece na tela): " VALUE
echo
[ -n "$VALUE" ] || { echo "vazio, nada gravado" >&2; exit 1; }
printf '%s\n' "$VALUE" | vps_ssh "sudo /usr/local/bin/avora-set-secret $NAME"
unset VALUE
