#!/bin/sh
# Entry point for the ai-tools agent container.
#
# - Optionally installs agent CLIs listed in AI_TOOLS_INSTALL_AGENTS
#   (space-separated npm package names, e.g.
#   "@openai/codex @anthropic-ai/claude-code opencode-ai"). Global installs go
#   to a user-local prefix so they work without root.
# - Then runs the command given to the container (default: a login shell).
set -e

export npm_config_prefix="$HOME/.npm-global"
export PATH="$HOME/.npm-global/bin:$PATH"

if [ -n "$AI_TOOLS_INSTALL_AGENTS" ]; then
  echo ">> installing agent CLIs: $AI_TOOLS_INSTALL_AGENTS"
  # shellcheck disable=SC2086
  npm install -g $AI_TOOLS_INSTALL_AGENTS
fi

echo ">> ai-tools agent container ready"
echo "   bridge:     ${AI_TOOLS_BRIDGE_ADDR:-<unset>}"
echo "   token:      ${AI_TOOLS_BRIDGE_TOKEN:+<set>}${AI_TOOLS_BRIDGE_TOKEN:-<unset>}"
echo "   workspace:  /workspace"
echo "   read config via: ai-tools remote list|get|compare (masked only)"

exec "$@"
