#!/bin/bash
# setup-playground.sh — create a ready-to-use lingtai playground
#
# Usage: ./setup-playground.sh <project-dir> <minimax-api-key>
#
# Creates:
#   <project-dir>/.lingtai/orchestrator/init.json  — default agent config
#   <project-dir>/.lingtai/human/                   — TUI creates this on launch

set -e

PROJECT_DIR="${1:-.}"
API_KEY="${2:-}"

if [ -z "$API_KEY" ] && [ -z "$MINIMAX_API_KEY" ]; then
  echo "Usage: $0 <project-dir> <minimax-api-key>"
  echo "  or set MINIMAX_API_KEY environment variable"
  exit 1
fi

PROJECT_DIR="$(cd "$PROJECT_DIR" 2>/dev/null && pwd || mkdir -p "$PROJECT_DIR" && cd "$PROJECT_DIR" && pwd)"
ORCH_DIR="$PROJECT_DIR/.lingtai/orchestrator"

mkdir -p "$ORCH_DIR"

# Write init.json
cat > "$ORCH_DIR/init.json" << INITEOF
{
  "manifest": {
    "agent_name": "orchestrator",
    "language": "en",
    "llm": {
      "provider": "minimax",
      "model": "MiniMax-M2.7-highspeed",
      "api_key": ${API_KEY:+\"$API_KEY\"}${API_KEY:-null},
      "api_key_env": "MINIMAX_API_KEY",
      "base_url": null
    },
    "capabilities": {
      "file": {},
      "email": {},
      "web_search": {"provider": "minimax", "api_key_env": "MINIMAX_API_KEY"},
      "bash": {"yolo": true},
      "psyche": {},
      "library": {},
      "vision": {"provider": "minimax", "api_key_env": "MINIMAX_API_KEY"},
      "talk": {"provider": "minimax", "api_key_env": "MINIMAX_API_KEY"},
      "draw": {"provider": "minimax", "api_key_env": "MINIMAX_API_KEY"},
      "compose": {"provider": "minimax", "api_key_env": "MINIMAX_API_KEY"},
      "listen": {},
      "web_read": {},
      "avatar": {},
      "daemon": {}
    },
    "soul": {"delay": 30},
    "stamina": 3600,
    "context_limit": null,
    "molt_pressure": 0.8,
    "molt_prompt": "",
    "max_turns": 100,
    "admin": {"karma": true},
    "streaming": true
  },
  "principle": "You are the orchestrator agent. You manage tasks, delegate to avatars when appropriate, and communicate with the human operator via email.",
  "covenant": "You are a helpful orchestrator agent. Respond to messages from the human via email. You can use file operations, web search, bash commands, vision, speech, music, and more. Be concise and helpful.",
  "memory": "",
  "prompt": "Hello. I am ready to receive instructions via email."
}
INITEOF

echo "Playground created at: $PROJECT_DIR"
echo "  Orchestrator: $ORCH_DIR/init.json"
echo ""
echo "Next steps:"
echo "  1. lingtai-tui $PROJECT_DIR"
echo "  2. The TUI will ask for your MiniMax API key (first run)"
echo "  3. Tab to Agent view → press 'r' to launch the orchestrator"
echo "  4. Tab to Mail view → compose a message"
