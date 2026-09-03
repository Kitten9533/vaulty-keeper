#!/bin/sh
# Entry point for the vaulty-keeper agent container.
#
# - Optionally installs agent CLIs listed in VAULTY_KEEPER_INSTALL_AGENTS
#   (space-separated npm package names, e.g.
#   "@openai/codex @anthropic-ai/claude-code opencode-ai"). Global installs go
#   to a user-local prefix so they work without root.
# - Then runs the command given to the container (default: a login shell).
set -e

export npm_config_prefix="$HOME/.npm-global"
export PATH="$HOME/.npm-global/bin:$PATH"

if [ -n "$VAULTY_KEEPER_INSTALL_AGENTS" ]; then
  echo ">> installing agent CLIs: $VAULTY_KEEPER_INSTALL_AGENTS"
  # shellcheck disable=SC2086
  npm install -g $VAULTY_KEEPER_INSTALL_AGENTS
fi

echo ">> vaulty-keeper agent container ready"
echo "   bridge:     ${VAULTY_KEEPER_BRIDGE_ADDR:-<unset>}"
echo "   token:      ${VAULTY_KEEPER_BRIDGE_TOKEN:+<set>}${VAULTY_KEEPER_BRIDGE_TOKEN:-<unset>}"
echo "   workspace:  /workspace"
echo "   read config via: vaulty-keeper remote list|get|compare (masked only)"

exec "$@"
