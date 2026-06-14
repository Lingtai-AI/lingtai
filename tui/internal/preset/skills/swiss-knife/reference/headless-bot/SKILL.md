---
name: headless-bot
description: >
  Nested swiss-knife reference for creating and operating a headless LingTai bot
  project without opening the TUI. Start from `lingtai-tui spawn`, then apply the
  relevant addon/MCP wiring, keep secrets in sidecar files, copy preset policy by
  referencing existing presets instead of copying preset JSON files, refresh or
  relaunch, and verify the bot safely. The bundled helper currently implements
  the Telegram MCP case.
version: 1.1.0
tags: [utilities, bot, telegram, mcp, headless, bootstrap]
---

# Headless Bot Project

Use this reference when a human asks for a LingTai-backed bot that should run as
its own project without stepping through the interactive TUI. The pattern is
channel-agnostic:

1. Create a normal LingTai project with `lingtai-tui spawn`.
2. Write channel secrets into sidecar files under the agent directory.
3. Activate the required addon/MCP entries in `init.json`.
4. Copy or construct the agent's **preset policy** (`manifest.preset`) by
   referencing the intended saved/template presets.
5. Refresh or relaunch the agent and verify the channel end-to-end.

Do not fabricate `.lingtai/` by hand. The supported bootstrap entrypoint remains
`lingtai-tui spawn`. The helper bundled here currently provisions Telegram bots;
future helpers for other chat surfaces should live under this same generic
reference rather than creating channel-specific top-level skill names.

## Inputs

Common inputs:

- `PROJECT_DIR`: new project directory. It must not already contain `.lingtai/`.
- `PRESET`: saved or template preset name for the initial `lingtai-tui spawn`.
- `AGENT_NAME`: agent directory/name to create under `.lingtai/`.
- `LANGUAGE`: one of `en`, `zh`, or `wen`.
- Optional source agent `init.json` if the new bot should inherit the same
  `manifest.preset` policy as an existing orchestrator.

Telegram-specific inputs:

- `TELEGRAM_BOT_TOKEN`: bot token from `@BotFather`. Keep it in the environment
  or a local prompt, never in shell history, logs, git, issues, or docs.
- `ALLOWED_USERS`: comma-separated Telegram numeric user IDs allowed to talk to
  the bot.

## Preferred Telegram Helper

Run the bundled helper from an installed utility bundle:

```bash
read -rsp 'Telegram bot token: ' TELEGRAM_BOT_TOKEN; export TELEGRAM_BOT_TOKEN; echo
python3 ~/.lingtai-tui/utilities/swiss-knife/reference/headless-bot/scripts/create_telegram_bot_project.py \
  --project-dir /path/to/new-project \
  --preset minimax \
  --agent-name my-bot-agent \
  --language en \
  --allowed-users 123456789 \
  --preset-policy-from /path/to/source-agent/init.json
```

Or run it from a source checkout:

```bash
read -rsp 'Telegram bot token: ' TELEGRAM_BOT_TOKEN; export TELEGRAM_BOT_TOKEN; echo
python3 tui/internal/preset/skills/swiss-knife/reference/headless-bot/scripts/create_telegram_bot_project.py \
  --project-dir /path/to/new-project \
  --preset minimax \
  --agent-name my-bot-agent \
  --language en \
  --allowed-users 123456789 \
  --preset-policy-from /path/to/source-agent/init.json
```

The helper:

1. Runs `lingtai-tui spawn <dir> --preset <name> --agent-name <name> --language <code>`.
2. Writes `<agent_dir>/.secrets/telegram.json` with mode `0600`.
3. Adds `telegram` to top-level `addons`.
4. Adds top-level `mcp.telegram` using the local LingTai runtime Python and
   `LINGTAI_TELEGRAM_CONFIG=.secrets/telegram.json`.
5. Optionally copies `manifest.preset` from `--preset-policy-from` into the new
   agent's `init.json` without copying any preset JSON files.
6. Touches `<agent_dir>/.refresh` so a running agent reloads the updated config.
   If refresh is not enough for the installed runtime, relaunch or CPR the agent.

The helper intentionally reads `TELEGRAM_BOT_TOKEN` from the environment instead
of a command-line flag, because argv can be captured by shell history and process
monitors. Its output redacts token-like values. The `--preset-policy-from` flag
is optional; use it when the bot should inherit an existing agent's preset access
policy.

## Preset Policy: Copy References, Not Preset Files

Headless bots often need access to the same model/preset choices as an existing
orchestrator. Do **not** grant that access by manually copying saved preset JSON
files into the new project's `.lingtai/presets/saved/` directory. That creates
stale, duplicated configuration and can diverge from the user's real saved
presets.

Instead, patch the new agent's `init.json` so its `manifest.preset` block points
to the intended saved or template presets. If the bot should mirror an existing
agent, copy only that source agent's `manifest.preset` policy block:

```bash
python3 - <<'PY'
import json
from pathlib import Path

source_init = Path('/path/to/source-agent/init.json')
target_init = Path('/path/to/new-project/.lingtai/<agent-name>/init.json')

source = json.loads(source_init.read_text())
target = json.loads(target_init.read_text())

preset_policy = source.get('manifest', {}).get('preset')
if not isinstance(preset_policy, dict):
    raise SystemExit(f'{source_init} has no manifest.preset policy object')

target.setdefault('manifest', {})['preset'] = preset_policy
target_init.write_text(json.dumps(target, indent=2, ensure_ascii=False) + '\n')
PY
```

Expected shape:

```json
{
  "manifest": {
    "preset": {
      "active": "~/.lingtai-tui/presets/saved/codex.json",
      "default": "~/.lingtai-tui/presets/saved/codex.json",
      "allowed": [
        "~/.lingtai-tui/presets/saved/codex.json",
        "~/.lingtai-tui/presets/saved/minimax_cn.json"
      ]
    }
  }
}
```

Keep this separate from secrets and MCP credentials. After changing the preset
policy, refresh or restart the agent and confirm the active preset in
`.agent.json` or the runtime logs.

## Manual Telegram Workflow

Use this when you need to inspect or adapt each step. Replace placeholders, but
never paste a real token into committed files or chat transcripts.

```bash
lingtai-tui spawn "$PROJECT_DIR" \
  --preset "$PRESET" \
  --agent-name "$AGENT_NAME" \
  --language "$LANGUAGE"
```

The command prints JSON containing `agent_dir`. Treat that as the only supported
source for the agent path.

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

If `addons` or `mcp` already exists, merge instead of replacing unrelated entries.
The `command` should point to the local LingTai runtime Python. On unusual
installs, resolve it with:

```bash
python3 - <<'PY'
from pathlib import Path
print(Path.home() / ".lingtai-tui" / "runtime" / "venv" / "bin" / "python")
PY
```

## Refresh Or Relaunch

After writing `init.json` and channel secret files, refresh the running agent:

```bash
touch "<agent_dir>/.refresh"
```

If the listener does not appear in logs within a minute, inspect process state
before starting anything else:

```bash
lingtai-tui list "$PROJECT_DIR"
```

If the agent is stuck in a relaunch/dead loop, use the runtime lifecycle controls
available in your environment (for example suspend/CPR from an orchestrator) or
relaunch from the project. Do not start a second copy of the same agent without
checking `lingtai-tui list` first.

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

- The agent log shows the Telegram MCP/listener starting, without printing the
  bot token.
- The runtime sees the intended preset policy; check `init.json`, `.agent.json`,
  or the logs after refresh/relaunch.
- Slash commands still work in the TUI, especially `/refresh`, `/doctor`, and a
  simple chat prompt.
- From an allowed Telegram account, send `/start` to the bot and confirm the
  agent responds.
- From a non-allowed account, confirm access is denied or ignored.

## Security Rules

- Never commit `.secrets/telegram.json`, `.env`, screenshots, terminal captures,
  issue comments, or summaries containing a real bot token.
- Prefer `allowed_users`; an omitted allowlist makes the bot reachable by anyone
  who discovers it.
- Rotate the token in `@BotFather` immediately if it appears in logs, git
  history, chat, crash reports, or process arguments.
- Use placeholders such as `<redacted-telegram-bot-token>` in examples. Do not use
  realistic token-shaped dummy strings.
- Keep generated secret files mode `0600` and directories mode `0700` on shared
  machines.
