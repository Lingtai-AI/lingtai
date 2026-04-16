---
name: lingtai-wechat-setup
description: Configure the WeChat addon for this agent — read this when the human asks to set up WeChat.
version: 3.0.0
---

# WeChat Setup

You are helping the human connect this agent to WeChat via Tencent's iLink Bot API. Unlike other addons, WeChat uses **QR code login** — there are no static credentials to paste. Your job is to **walk the human through setup and execute the steps yourself** — do not just list instructions and ask the human to do everything.

## New Convention — Admin-Local `.secrets/`

**The WeChat config and credentials live inside your own working directory:**

```
.secrets/wechat.json        (config — plaintext JSON)
.secrets/credentials.json   (bot token — written by login command)
```

- These files are **yours alone** — other agents in the network do not read them.
- The `init.json` `addons.wechat.config` field is simply `.secrets/wechat.json` (no `../`, no absolute path).
- The config is plaintext JSON with no `*_env` indirection. One WeChat account per agent.

### Why

Addons are an admin-only responsibility. Avatars should never configure addons — the orchestrator owns them. Colocating the config with the orchestrator's working directory makes that ownership explicit.

## Legacy Path (back-compat only)

If your `init.json` already has `addons.wechat.config` pointing at `../.addons/wechat/config.json` and the old setup is working, **leave it alone**. The old path (and its companion `../.addons/wechat/credentials.json`) keeps functioning. Only use the new path when setting up WeChat for the first time, or when the human explicitly asks to migrate.

## Rules

- **Never edit `credentials.json` manually.** It is managed by the login command.
- **Config changes require refresh** — run `system(action="refresh")` yourself after any config change.
- **Status caveat:** after refresh, addon status may show `connected: false` even when working. Always verify by attempting actual operation — if it succeeds, the connection is fine.
- **If login fails** (QR expired, network error), retry the login command. Each attempt generates a fresh QR code.
- **The QR code expires in 5 minutes.** Tell the human to scan promptly.

## What You Need From the Human

1. **A WeChat account** on their phone (the one that will be connected to the agent).
2. **Physical access** to scan a QR code displayed in the terminal.
3. **Allowed users** (optional) — WeChat user IDs to restrict who can message the bot. If omitted, anyone can message.

## What You Do

### First-Time Setup

1. **Create the config file** at `.secrets/wechat.json` in your own working directory (create the `.secrets/` directory as needed):

   ```json
   {
     "base_url": "https://ilinkai.weixin.qq.com",
     "cdn_base_url": "https://novac2c.cdn.weixin.qq.com/c2c",
     "poll_interval": 1.0,
     "allowed_users": []
   }
   ```

   If the human provided specific allowed_users, include them as a list of WeChat user ID strings.

2. **Run the login command** to display the QR code. Pass the directory containing `wechat.json` — the login script writes `credentials.json` into that same directory:

   ```bash
   python -c "from lingtai.addons.wechat.login import cli_login; cli_login('.secrets')"
   ```

   This will:
   - Display a QR code in the terminal
   - Wait for the human to scan it with WeChat on their phone
   - Save the `bot_token` to `.secrets/credentials.json` on successful scan
   - Print "Connected as <user_id>" on success

3. **Wire it into `init.json`.** Read your `init.json`. If `addons.wechat` is missing, add it:
   ```json
   "addons": {
     "wechat": {
       "config": ".secrets/wechat.json"
     }
   }
   ```
   If `addons.wechat.config` already exists and points at a legacy path, see the "Legacy Path" section above — do **not** rewrite it unless the human asked you to migrate.

4. **Activate:** run `system(action="refresh")` to reload the addon config. Then verify the connection is working. Tell the human WeChat is configured.

### Re-Login (Session Expired)

WeChat sessions can expire. When this happens, the addon pauses and sends the human a notification mail. To re-login:

1. Run the same login command from step 2 above. If your setup is on the legacy path, pass `.lingtai/.addons/wechat` instead of `.secrets`.
2. Run `system(action="refresh")` to reload after successful login.

## Config Reference

See the example config at `.lingtai/.library/lingtai-wechat-setup/assets/config.json` for all available fields.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_url` | string | `https://ilinkai.weixin.qq.com` | iLink API endpoint |
| `cdn_base_url` | string | `https://novac2c.cdn.weixin.qq.com/c2c` | CDN for media uploads/downloads |
| `poll_interval` | float | `1.0` | Seconds between long-poll retries |
| `allowed_users` | string[] | `[]` | WeChat user IDs to accept. Empty = accept all. |
