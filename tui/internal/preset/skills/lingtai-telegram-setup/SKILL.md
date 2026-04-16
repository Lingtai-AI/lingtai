---
name: lingtai-telegram-setup
description: Configure the Telegram bot addon for this agent — read this when the human asks to set up a Telegram bot.
version: 3.0.0
---

# Telegram Bot Setup

You are helping the human set up a Telegram bot for this agent. Your job is to **create the config file yourself** — do not just list the steps and ask the human to do it.

## New Convention — Admin-Local `.secrets/`

**The Telegram config lives inside your own working directory:**

```
.secrets/telegram.json   (relative to your working directory)
```

- This file is **yours alone** — other agents in the network do not read it.
- The `init.json` `addons.telegram.config` field is simply `.secrets/telegram.json` (no `../`, no absolute path).
- The bot token goes **directly in the JSON** as plaintext. No `*_env` indirection. No `.env` file involved. The config is self-contained.
- Telegram supports one bot per config file. If the human needs multiple bots, ask them to clarify — that's not supported by a single config.

### Why

Addons are an admin-only responsibility. Avatars should never configure addons — the orchestrator owns them. Colocating the config with the orchestrator's working directory makes that ownership explicit.

## Legacy Path (back-compat only)

If your `init.json` already has `addons.telegram.config` pointing at `../.addons/telegram/config.json` and the old setup is working, **leave it alone**. The old path keeps functioning. Only create setup at the new path when the human is setting up Telegram for the first time, or explicitly asks to migrate.

## Rules

- **Bot token is plaintext in the JSON.** Do not use `bot_token_env`. The `.env` file is only for LLM API keys and other non-addon secrets.
- **Activation:** after creating or editing `.secrets/telegram.json`, run `system(action="refresh")` yourself to reload. Do not ask the human to refresh for you.
- **Troubleshooting:** if the addon fails to load, check that `.secrets/telegram.json` exists, is valid JSON, and that your `init.json` `addons.telegram.config` points at `.secrets/telegram.json`. Report back to the human with the specific problem.
- **Status caveat:** after refresh, addon status may show `connected: false` even when working. Always verify by attempting actual operation — if it succeeds, the connection is fine.

## What You Need From the Human

Ask the human for:
1. **Bot token** — from @BotFather on Telegram (`/newbot` → follow prompts → copy token)
2. **Allowed users** (optional) — Telegram user IDs allowed to message the bot. If omitted, anyone can message.
   - To find a user ID: have them message the bot first, the ID appears in the `from` field.

## What You Do

Once you have the bot token:

1. **Create the config file** at `.secrets/telegram.json` in your own working directory:
   ```json
   {
     "bot_token": "<the token they gave you>",
     "allowed_users": [123456789],
     "poll_interval": 1.0
   }
   ```
   - If no allowed_users requested, omit the field entirely (open access).

2. **Wire it into `init.json`.** Read your `init.json`. If `addons.telegram` is missing, add it:
   ```json
   "addons": {
     "telegram": {
       "config": ".secrets/telegram.json"
     }
   }
   ```
   If `addons.telegram.config` already exists and points at a legacy path (e.g. `../.addons/telegram/config.json`), see the "Legacy Path" section above — do **not** rewrite it unless the human asked you to migrate.

3. **Activate:** run `system(action="refresh")` to reload the addon config. Then verify the bot is responding. Tell the human Telegram is configured.

## Config Reference

See the example config at `.lingtai/.library/lingtai-telegram-setup/assets/config.json` for a full reference of all available fields.
