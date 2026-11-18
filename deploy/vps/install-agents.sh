#!/usr/bin/env bash
# Installs the official agent CLIs for the avora user (run as root by
# install.sh). Idempotent: skips what is already there. Both CLIs keep their
# login inside /var/lib/avora and update themselves from their vendors.
#   Claude Code: official installer (claude.ai/install.sh)
#   Codex:       official npm package @openai/codex
set -euo pipefail

HOME_DIR=/var/lib/avora
as_avora() { sudo -u avora -H env HOME="$HOME_DIR" PATH="$HOME_DIR/.local/bin:/usr/local/bin:/usr/bin:/bin" bash -c "$1"; }

install -d -m 700 -o avora -g avora "$HOME_DIR/work" "$HOME_DIR/claude" "$HOME_DIR/codex" "$HOME_DIR/.local"

if [ ! -x "$HOME_DIR/.local/bin/claude" ]; then
  echo "  installing Claude Code"
  as_avora 'curl -fsSL https://claude.ai/install.sh | bash' >/dev/null
fi
echo "  claude $(as_avora "$HOME_DIR/.local/bin/claude --version")"

if [ ! -x "$HOME_DIR/.local/bin/codex" ]; then
  echo "  installing Codex"
  # sudo's secure_path omits /usr/local/bin, where Node.js lives here.
  [ -x /usr/local/bin/npm ] || command -v npm >/dev/null || { echo "npm not found; Codex needs Node.js" >&2; exit 1; }
  as_avora "npm install -g --silent --no-fund --no-audit --prefix $HOME_DIR/.local @openai/codex@latest" >/dev/null
fi
as_avora "$HOME_DIR/.local/bin/codex --version" >/dev/null || { echo "codex install failed" >&2; exit 1; }
echo "  $(as_avora "$HOME_DIR/.local/bin/codex --version")"
