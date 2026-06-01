---
name: headless-telegram-bot
description: >
  Create a fresh LingTai project for a Telegram bot without opening the TUI:
  spawn the project with `lingtai-tui spawn`, write the Telegram MCP sidecar
  secret, activate `addons` and `mcp.telegram` in init.json, then refresh or
  relaunch and verify the bot safely. Use when the human asks to provision or
  automate a LingTai Telegram bot project.
version: 1.0.0
tags: [utilities, telegram, mcp, headless, bootstrap]
---

# Headless Telegram Bot Project

This utility creates a new LingTai project from the supported headless entrypoint, then applies the Telegram MCP wiring that the bot needs. Do not fabricate `.lingtai/` by hand: always start from `lingtai-tui spawn`.

## Inputs

- `PROJECT_DIR`: new project directory. It must not already contain `.lingtai/`.
- `PRESET`: saved or template preset name, for example `minimax`.
- `AGENT_NAME`: agent directory/name to create under `.lingtai/`.
- `LANGUAGE`: one of `en`, `zh`, or `wen`.
- `TELEGRAM_BOT_TOKEN`: bot token from `@BotFather`. Keep it in the environment or a local prompt, never in shell history, logs, git, or docs.
- `ALLOWED_USERS`: comma-separated Telegram numeric user IDs allowed to talk to the bot.

## Preferred Helper

Run the bundled helper from an installed utility bundle:

```bash
read -rsp 'Telegram bot token: ' TELEGRAM_BOT_TOKEN; export TELEGRAM_BOT_TOKEN; echo
python3 ~/.lingtai-tui/utilities/swiss-knife/reference/headless-telegram-bot/scripts/create_telegram_bot_project.py \
  --project-dir /path/to/new-project \
  --preset minimax \
  --agent-name my-telegram-agent \
  --language en \
  --allowed-users 123456789
```

Or run it from a source checkout:

```bash
read -rsp 'Telegram bot token: ' TELEGRAM_BOT_TOKEN; export TELEGRAM_BOT_TOKEN; echo
python3 tui/internal/preset/skills/swiss-knife/reference/headless-telegram-bot/scripts/create_telegram_bot_project.py \
  --project-dir /path/to/new-project \
  --preset minimax \
  --agent-name my-telegram-agent \
  --language en \
  --allowed-users 123456789
```

The helper:

1. Runs `lingtai-tui spawn <dir> --preset <name> --agent-name <name> --language <code>`.
2. Writes `<agent_dir>/.secrets/telegram.json` with mode `0600`.
3. Adds `telegram` to top-level `addons`.
4. Adds top-level `mcp.telegram` using the local LingTai runtime Python and `LINGTAI_TELEGRAM_CONFIG=.secrets/telegram.json`.
5. Touches `<agent_dir>/.refresh` so a running agent reloads the updated config. If refresh is not enough for the installed runtime, relaunch the agent.

The helper intentionally reads `TELEGRAM_BOT_TOKEN` from the environment instead of a command-line flag, because argv can be captured by shell history and process monitors. Its output redacts token-like values.

## Manual Workflow

Use this when you need to inspect or adapt each step. Replace placeholders, but never paste a real token into committed files or chat transcripts.

```bash
lingtai-tui spawn "$PROJECT_DIR" \
  --preset "$PRESET" \
  --agent-name "$AGENT_NAME" \
  --language "$LANGUAGE"
```

The command prints JSON containing `agent_dir`. Treat that as the only supported source for the agent path.

Create the sidecar secret:

```json
{
  "accounts": [
    {
      "alias": "main",
      "bot_token": "<redacted-telegram-bot-token>",
      "allowed_users": [123456789]
    }
  ],
  "poll_interval": 1.0
}
```

Save it as:

```text
<agent_dir>/.secrets/telegram.json
```

Then set restrictive permissions:

```bash
chmod 700 "<agent_dir>/.secrets"
chmod 600 "<agent_dir>/.secrets/telegram.json"
```

Patch `<agent_dir>/init.json` so these top-level keys exist:

```json
{
  "addons": ["telegram"],
  "mcp": {
    "telegram": {
      "type": "stdio",
      "command": "~/.lingtai-tui/runtime/venv/bin/python",
      "args": ["-m", "lingtai_telegram"],
      "env": {
        "LINGTAI_TELEGRAM_CONFIG": ".secrets/telegram.json"
      }
    }
  }
}
```

If `addons` or `mcp` already exists, merge instead of replacing unrelated entries. The `command` should point to the local LingTai runtime Python. On unusual installs, resolve it with:

```bash
python3 - <<'PY'
from pathlib import Path
print(Path.home() / ".lingtai-tui" / "runtime" / "venv" / "bin" / "python")
PY
```

## Refresh Or Relaunch

After writing `init.json` and `.secrets/telegram.json`, refresh the running agent:

```bash
touch "<agent_dir>/.refresh"
```

If the Telegram listener does not appear in logs within a minute, stop and relaunch the agent from the project:

```bash
lingtai-tui list "$PROJECT_DIR"
cd "$PROJECT_DIR"
lingtai-tui
```

Use the local process controls available in your environment if you need a hard restart. Do not start a second copy of the same agent without checking `lingtai-tui list` first.

## Verification Checklist

Run these checks before handing the bot to a user:

- `lingtai-tui list "$PROJECT_DIR"` shows the new agent and no duplicate copy.
- Bot API `getMe` succeeds. Redact the token in commands and logs:

```bash
python3 - <<'PY'
import json, os, urllib.request
token = os.environ["TELEGRAM_BOT_TOKEN"]
with urllib.request.urlopen(f"https://api.telegram.org/bot{token}/getMe", timeout=15) as r:
    data = json.load(r)
print(json.dumps({"ok": data.get("ok"), "result": data.get("result", {})}, indent=2))
print("token: <redacted>")
PY
```

- The agent log shows the Telegram MCP/listener starting, without printing the bot token.
- Slash commands still work in the TUI, especially `/refresh`, `/doctor`, and a simple chat prompt.
- From an allowed Telegram account, send `/start` to the bot and confirm the agent responds.
- From a non-allowed account, confirm access is denied or ignored.

## Security Rules

- Never commit `.secrets/telegram.json`, `.env`, screenshots, terminal captures, or issue comments containing a real bot token.
- Prefer `allowed_users`; an omitted allowlist makes the bot reachable by anyone who discovers it.
- Rotate the token in `@BotFather` immediately if it appears in logs, git history, chat, crash reports, or process arguments.
- Use placeholders such as `<redacted-telegram-bot-token>` in examples. Do not use realistic token-shaped dummy strings.
- Keep generated secret files mode `0600` and directories mode `0700` on shared machines.
