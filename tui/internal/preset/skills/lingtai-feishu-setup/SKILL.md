---
name: lingtai-feishu-setup
description: Configure the Feishu (Lark) bot addon for this agent — read this when the human asks to set up a Feishu bot.
version: 3.0.0
---

# Feishu (Lark) Bot Setup

You are helping the human set up a Feishu bot for this agent. Your job is to **create the config file yourself** — do not just list the steps and ask the human to do it.

## New Convention — Admin-Local `.secrets/`

**The Feishu config lives inside your own working directory:**

```
.secrets/feishu.json   (relative to your working directory)
```

- This file is **yours alone** — other agents in the network do not read it.
- The `init.json` `addons.feishu.config` field is simply `.secrets/feishu.json` (no `../`, no absolute path).
- Secrets go **directly in the JSON** as plaintext. No `*_env` indirection. No `.env` file involved. The config is self-contained.
- Feishu supports one bot per config file. If the human needs multiple bots, ask them to clarify — that's not supported by a single config.

### Why

Addons are an admin-only responsibility. Avatars should never configure addons — the orchestrator owns them. Colocating the config with the orchestrator's working directory makes that ownership explicit.

## Legacy Path (back-compat only)

If your `init.json` already has `addons.feishu.config` pointing at `../.addons/feishu/config.json` and the old setup is working, **leave it alone**. The old path keeps functioning. Only create setup at the new path when the human is setting up Feishu for the first time, or explicitly asks to migrate.

## Rules

- **Secrets are plaintext in the JSON.** Do not use `app_id_env` or `app_secret_env`. The `.env` file is only for LLM API keys and other non-addon secrets.
- **Activation:** after creating or editing `.secrets/feishu.json`, run `system(action="refresh")` yourself to reload. Do not ask the human to refresh for you.
- **Troubleshooting:** if the addon fails to load, check that `.secrets/feishu.json` exists, is valid JSON, and that your `init.json` `addons.feishu.config` points at `.secrets/feishu.json`. Report back to the human with the specific problem.
- **Status caveat:** after refresh, addon status may show `connected: false` even when working. Always verify by attempting actual operation — if it succeeds, the connection is fine.

## What You Need From the Human

Ask the human for:
1. **App ID** — from Feishu Open Platform Developer Console → Credentials (App ID, starts with `cli_`)
2. **App Secret** — from the same Credentials page
3. **Allowed users** (optional) — Feishu open_ids of users allowed to message the bot. If omitted, anyone can message.
   - To find a user's open_id: ask them to open the bot in Feishu — the open_id is visible in the developer console's contact directory.

## What You Do

Once you have the App ID and App Secret:

1. **Create the config file** at `.secrets/feishu.json` in your own working directory:
   ```json
   {
     "app_id": "cli_xxxxxxxxxxxxxxxxxxxxxxxx",
     "app_secret": "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",
     "allowed_users": ["ou_xxxxxxxxxxxxxxxx", "ou_yyyyyyyyyyyyyyyy"]
   }
   ```
   - If no allowed_users requested, omit the field entirely (open access).

2. **Wire it into `init.json`.** Read your `init.json`. If `addons.feishu` is missing, add it:
   ```json
   "addons": {
     "feishu": {
       "config": ".secrets/feishu.json"
     }
   }
   ```
   If `addons.feishu.config` already exists and points at a legacy path (e.g. `../.addons/feishu/config.json`), see the "Legacy Path" section above — do **not** rewrite it unless the human asked you to migrate.

3. **Activate:** run `system(action="refresh")` to reload the addon config. Then verify the bot is responding. Tell the human Feishu is configured.

## Feishu Bot Setup (Platform Side)

The Feishu addon uses a **long WebSocket connection** (via lark-oapi SDK) to receive events in real time — no polling, no webhooks needed.

Setup on Feishu Open Platform:
1. Go to https://open.feishu.cn/app
2. Create an enterprise app (or use existing)
3. **Enable Bot capability** — this is required for the bot to receive messages
4. In **Event Subscriptions**, choose **"Use long connection to receive events"** (no URL needed)
5. Subscribe to the event: `im.message.receive_v1` (receive messages)
6. In **Permissions**, add: `im:message` (read and send messages)

## Config Reference

See the example config at `~/.lingtai-tui/utilities/lingtai-feishu-setup/assets/config.json` for a full reference of all available fields.
