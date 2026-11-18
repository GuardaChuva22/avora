# Shared by the scripts that talk to your server over SSH. Source it, then
# use vps_ssh / vps_ssh_tty. The target comes from the environment or from
# .env.vps at the repo root (gitignored; copy .env.vps.example):
#   AVORA_VPS_HOST  user@host of the server (required)
#   AVORA_VPS_KEY   private key file (optional; default: your ssh config/agent)
#   AVORA_TIMEZONE  forwarded to the installer on deploy (optional)
AVORA_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if [ -f "$AVORA_ROOT/.env.vps" ]; then
  # shellcheck disable=SC1091
  . "$AVORA_ROOT/.env.vps"
fi
[ -n "${AVORA_VPS_HOST:-}" ] || { echo "Set AVORA_VPS_HOST=user@host (or create .env.vps from .env.vps.example)." >&2; exit 1; }

VPS_SSH_OPTS=(-o StrictHostKeyChecking=accept-new)
if [ -n "${AVORA_VPS_KEY:-}" ]; then
  [ -f "$AVORA_VPS_KEY" ] || { echo "SSH key not found at $AVORA_VPS_KEY." >&2; exit 1; }
  # Keys copied from Windows keep loose permissions that OpenSSH refuses.
  VPS_KEY_TMP="$(mktemp)"
  trap 'rm -f "$VPS_KEY_TMP"' EXIT
  cp "$AVORA_VPS_KEY" "$VPS_KEY_TMP"
  chmod 600 "$VPS_KEY_TMP"
  VPS_SSH_OPTS+=(-i "$VPS_KEY_TMP")
fi

vps_ssh() { ssh "${VPS_SSH_OPTS[@]}" "$AVORA_VPS_HOST" "$@"; }
vps_ssh_tty() { ssh -t "${VPS_SSH_OPTS[@]}" "$AVORA_VPS_HOST" "$@"; }
