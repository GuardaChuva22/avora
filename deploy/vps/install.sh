#!/usr/bin/env bash
# Installs or updates Avora on a Linux server (Fedora/RHEL, Debian/Ubuntu,
# Arch or openSUSE with systemd). Run as root from the uploaded bundle:
#   sudo bash <bundle>/vps/install.sh <bundle>
# Idempotent: creates the avora user, directories, admin token, Postgres role
# and database, and the tailscale serve entry (when Tailscale is installed)
# only when they are missing. Never touches other services.
# AVORA_TIMEZONE (IANA name) sets the owner's clock on first install;
# default: the server's own zone.
set -euo pipefail

BUNDLE="${1:?bundle directory required}"
PORT=3190
TAILNET_PORT=8443

say() { echo "== $*"; }

# pkg_install installs packages with whatever manager the system has.
pkg_install() {
  if command -v dnf >/dev/null 2>&1; then dnf install -y -q "$@"
  elif command -v apt-get >/dev/null 2>&1; then DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "$@"
  elif command -v pacman >/dev/null 2>&1; then pacman -S --noconfirm --needed "$@"
  elif command -v zypper >/dev/null 2>&1; then zypper --non-interactive install "$@"
  else echo "no known package manager: install $* by hand" >&2; return 1
  fi
}

say "user and directories"
id avora >/dev/null 2>&1 || useradd --system --home-dir /var/lib/avora --shell /sbin/nologin avora
install -d -m 755 -o root -g root /opt/avora /opt/avora/bin
install -d -m 700 -o avora -g avora /var/lib/avora /var/lib/avora/runtime
install -d -m 750 -o root -g avora /etc/avora

if [ ! -f /etc/avora/avora.env ]; then
  say "admin token (new)"
  umask 027
  printf 'AVORA_ADMIN_TOKEN=%s\n' "$(openssl rand -hex 32)" > /etc/avora/avora.env
  chown root:avora /etc/avora/avora.env
  chmod 640 /etc/avora/avora.env
fi
if ! grep -q '^AVORA_TIMEZONE=' /etc/avora/avora.env; then
  TZ_NAME="${AVORA_TIMEZONE:-$(timedatectl show -p Timezone --value 2>/dev/null || echo UTC)}"
  say "time zone: $TZ_NAME"
  printf 'AVORA_TIMEZONE=%s\n' "$TZ_NAME" >> /etc/avora/avora.env
fi

if [ ! -f /etc/avora/vault.key ]; then
  say "vault key (new): seals the Plugins' credentials"
  umask 027
  openssl rand -hex 32 > /etc/avora/vault.key
  chown root:avora /etc/avora/vault.key
  chmod 640 /etc/avora/vault.key
fi

say "postgres"
if [ "$(sudo -u postgres psql -tAc "select 1 from pg_roles where rolname = 'avora'")" != "1" ]; then
  sudo -u postgres createuser avora
fi
if [ "$(sudo -u postgres psql -tAc "select 1 from pg_database where datname = 'avora'")" != "1" ]; then
  sudo -u postgres createdb -O avora avora
fi
if ! sudo -u avora psql -h /run/postgresql -d avora -tAc 'select 1' >/dev/null 2>&1; then
  echo "avora cannot reach its database over the local socket (check pg_hba peer auth)" >&2
  exit 1
fi
echo "  ok"

say "sandbox for executar (bubblewrap, pasta)"
if ! command -v bwrap >/dev/null 2>&1 || ! command -v pasta >/dev/null 2>&1; then
  pkg_install bubblewrap passt
fi
install -d -m 700 -o avora -g avora /var/lib/avora/work /var/lib/avora/work/agents
# A copy outside the SELinux pasta domain, which cannot be entered under
# the runtime's NoNewPrivileges.
install -m 755 /usr/bin/pasta /opt/avora/bin/pasta
restorecon /opt/avora/bin/pasta 2>/dev/null || true
bwrap --version

say "binaries and units"
install -m 755 "$BUNDLE/bin/avora-server" "$BUNDLE/bin/avora-runtime" /opt/avora/bin/
for helper in avora-pair avora-set-secret avora-chat avora-agent; do
  install -m 755 "$BUNDLE/vps/$helper" "/usr/local/bin/$helper"
done
for unit in avora-server.service avora-runtime.service; do
  install -m 644 "$BUNDLE/vps/$unit" /etc/systemd/system/$unit
done
systemctl daemon-reload
systemctl enable avora-server avora-runtime >/dev/null 2>&1

say "agent CLIs"
bash "$BUNDLE/vps/install-agents.sh"

say "server"
systemctl restart avora-server
for _ in $(seq 1 20); do curl -fsS "http://127.0.0.1:$PORT/healthz" >/dev/null 2>&1 && break; sleep 1; done
curl -fsS "http://127.0.0.1:$PORT/healthz"; echo

if command -v tailscale >/dev/null 2>&1; then
  say "tailscale serve :$TAILNET_PORT"
  if ! tailscale serve status 2>/dev/null | grep -q ":$TAILNET_PORT"; then
    tailscale serve --bg --https=$TAILNET_PORT "http://127.0.0.1:$PORT" >/dev/null
  fi
  tailscale serve status | grep -A1 ":$TAILNET_PORT" || true
else
  say "no Tailscale: Avora listens on 127.0.0.1:$PORT only"
  echo "  Put a TLS proxy in front of it (Caddy: reverse_proxy 127.0.0.1:$PORT) and use that https address in the app."
fi

say "runtime (VPS node)"
if [ ! -f /var/lib/avora/runtime/node.json ]; then
  CODE=$(/usr/local/bin/avora-pair vps vps | sed -n 's/.*"code":"\([A-Z0-9-]*\)".*/\1/p')
  [ -n "$CODE" ] || { echo "could not create a pairing code" >&2; exit 1; }
  umask 027
  printf 'AVORA_PAIRING_CODE=%s\n' "$CODE" > /etc/avora/runtime.env
  chown root:avora /etc/avora/runtime.env
  chmod 640 /etc/avora/runtime.env
  systemctl reset-failed avora-runtime 2>/dev/null || true
  systemctl restart avora-runtime
  for _ in $(seq 1 20); do [ -f /var/lib/avora/runtime/node.json ] && break; sleep 1; done
  rm -f /etc/avora/runtime.env
  [ -f /var/lib/avora/runtime/node.json ] || { echo "runtime did not pair; see journalctl -u avora-runtime" >&2; exit 1; }
  echo "  paired"
else
  systemctl reset-failed avora-runtime 2>/dev/null || true
  systemctl restart avora-runtime
fi
sleep 2
systemctl is-active avora-server avora-runtime
