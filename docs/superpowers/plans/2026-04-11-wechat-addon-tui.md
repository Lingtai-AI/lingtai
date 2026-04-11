# WeChat Addon (TUI Side) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add WeChat as a fourth addon option in the LingTai TUI, including the setup skill, config template, and i18n strings.

**Architecture:** The TUI addon system is convention-driven — adding `"wechat"` to `AllAddons` and creating the skill/template files is sufficient. No code changes needed in addon display, init.json generation, or Python import verification since those all work generically off addon names.

**Tech Stack:** Go (Bubble Tea TUI), embedded assets via `//go:embed`

**Spec:** `docs/superpowers/specs/2026-04-11-wechat-addon-design.md`

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `tui/internal/tui/presets.go` | Modify | Add `"wechat"` to `AllAddons` |
| `tui/internal/preset/skills/lingtai-wechat-setup/SKILL.md` | Create | Setup skill for WeChat addon |
| `tui/internal/preset/skills/lingtai-wechat-setup/assets/config.json` | Create | Example config reference |
| `tui/internal/preset/templates/wechat.jsonc` | Create | Config template with comments |
| `tui/i18n/en.json` | Modify | Add `firstrun.addon_desc.wechat`, update `palette.addon` |
| `tui/i18n/zh.json` | Modify | Add `firstrun.addon_desc.wechat`, update `palette.addon` |
| `tui/i18n/wen.json` | Modify | Add `firstrun.addon_desc.wechat`, update `palette.addon` |

---

### Task 1: Register WeChat in AllAddons

**Files:**
- Modify: `tui/internal/tui/presets.go:16`

- [ ] **Step 1: Add `"wechat"` to `AllAddons`**

In `tui/internal/tui/presets.go`, change line 16 from:

```go
var AllAddons = []string{"imap", "telegram", "feishu"}
```

to:

```go
var AllAddons = []string{"imap", "telegram", "feishu", "wechat"}
```

- [ ] **Step 2: Verify build**

Run: `cd tui && make build`
Expected: Build succeeds (no new code references `"wechat"` yet — it's just a data entry).

- [ ] **Step 3: Commit**

```bash
git add tui/internal/tui/presets.go
git commit -m "feat(addon): register wechat in AllAddons"
```

---

### Task 2: Create WeChat setup skill

**Files:**
- Create: `tui/internal/preset/skills/lingtai-wechat-setup/SKILL.md`
- Create: `tui/internal/preset/skills/lingtai-wechat-setup/assets/config.json`

- [ ] **Step 1: Create the example config**

Create `tui/internal/preset/skills/lingtai-wechat-setup/assets/config.json`:

```json
{
  "base_url": "https://ilinkai.weixin.qq.com",
  "cdn_base_url": "https://novac2c.cdn.weixin.qq.com/c2c",
  "poll_interval": 1.0,
  "allowed_users": []
}
```

- [ ] **Step 2: Create the setup skill**

Create `tui/internal/preset/skills/lingtai-wechat-setup/SKILL.md`:

```markdown
---
name: lingtai-wechat-setup
description: Configure the WeChat addon for this agent — read this when the human asks to set up WeChat.
version: 1.0.0
---

# WeChat Setup

You are helping the human connect this agent to WeChat via Tencent's iLink Bot API. Unlike other addons, WeChat uses **QR code login** — there are no static credentials to paste.

## Fixed-by-Convention Path

**The WeChat config file lives at a single fixed location, shared by all agents in this project:**

` ` `
.lingtai/.addons/wechat/config.json   (relative to project root)
` ` `

- **Do not try to change this path.** The TUI and the kernel both expect it exactly here.
- The file is shared across all agents in the same project.
- One WeChat account per project.
- From your agent's working directory, the relative path in `init.json` is `../.addons/wechat/config.json`. You should not need to edit `init.json`.

## Credentials

WeChat does NOT use static API keys. Instead, a `bot_token` is obtained by scanning a QR code with the WeChat mobile app. The token is stored separately from config:

` ` `
.lingtai/.addons/wechat/credentials.json   (mode 0600, machine-managed)
` ` `

You do NOT manually create this file. The login command creates it.

## What You Need From the Human

1. **A WeChat account** on their phone (the one that will be connected to the agent).
2. **Physical access** to scan a QR code displayed in the terminal.
3. **Allowed users** (optional) — WeChat user IDs to restrict who can message the bot. If omitted, anyone can message.

## What You Do

### First-Time Setup

1. **Create the config file** at `.lingtai/.addons/wechat/config.json` relative to the project root (create directories as needed):

   ` ` `json
   {
     "base_url": "https://ilinkai.weixin.qq.com",
     "cdn_base_url": "https://novac2c.cdn.weixin.qq.com/c2c",
     "poll_interval": 1.0,
     "allowed_users": []
   }
   ` ` `

   If the human provided specific allowed_users, include them as a list of WeChat user ID strings.

2. **Run the login command** to display the QR code:

   ` ` `bash
   python -c "from lingtai.addons.wechat.login import cli_login; cli_login('.lingtai/.addons/wechat')"
   ` ` `

   This will:
   - Display a QR code in the terminal
   - Wait for the human to scan it with WeChat on their phone
   - Save the `bot_token` to `credentials.json` on successful scan
   - Print "Connected as <user_id>" on success

3. **Tell the human** to run `/refresh` in the TUI to activate the WeChat addon.

### Re-Login (Session Expired)

WeChat sessions can expire. When this happens, the addon pauses and sends the human a notification mail. To re-login:

1. Run the same login command from step 2 above.
2. Tell the human to run `/refresh` after successful login.

## Rules

- **Never edit `credentials.json` manually.** It is managed by the login command.
- **Config changes require `/refresh`** to take effect.
- **If login fails** (QR expired, network error), retry the login command. Each attempt generates a fresh QR code.
- **The QR code expires in 5 minutes.** Tell the human to scan promptly.

## Config Reference

See the example config at `.lingtai/.skills/lingtai-wechat-setup/assets/config.json` for all available fields.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `base_url` | string | `https://ilinkai.weixin.qq.com` | iLink API endpoint |
| `cdn_base_url` | string | `https://novac2c.cdn.weixin.qq.com/c2c` | CDN for media uploads/downloads |
| `poll_interval` | float | `1.0` | Seconds between long-poll retries |
| `allowed_users` | string[] | `[]` | WeChat user IDs to accept. Empty = accept all. |
```

**Note:** The triple backticks in the SKILL.md above are written as `` ` ` ` `` for escaping in this plan. In the actual file, use real triple backticks (` ``` `).

- [ ] **Step 3: Verify build**

Run: `cd tui && make build`
Expected: Build succeeds. The skill files are embedded via `//go:embed all:skills` in `preset.go` — no Go code changes needed.

- [ ] **Step 4: Commit**

```bash
git add tui/internal/preset/skills/lingtai-wechat-setup/
git commit -m "feat(addon): add wechat setup skill and example config"
```

---

### Task 3: Create config template

**Files:**
- Create: `tui/internal/preset/templates/wechat.jsonc`

- [ ] **Step 1: Create the JSONC template**

Create `tui/internal/preset/templates/wechat.jsonc`:

```jsonc
// =============================================================
// WeChat — wechat.json
// =============================================================
// Point to this file from init.json:
//   "addons": { "wechat": { "config": "path/to/this/config.json" } }
//
// Setup:
//   1. Create this config file at .lingtai/.addons/wechat/config.json
//   2. Run the login command to scan a QR code with your phone:
//      python -c "from lingtai.addons.wechat.login import cli_login; cli_login('.lingtai/.addons/wechat')"
//   3. Run /refresh in the TUI to activate
//
// Unlike other addons, WeChat uses QR-code login — no static
// credentials needed. The bot_token is saved automatically to
// .lingtai/.addons/wechat/credentials.json after scanning.
//
// =============================================================
{
  // iLink Bot API base URL (Tencent's official gateway).
  // Override only for testing or if Tencent changes the endpoint.
  "base_url": "https://ilinkai.weixin.qq.com",

  // CDN base URL for media uploads and downloads.
  "cdn_base_url": "https://novac2c.cdn.weixin.qq.com/c2c",

  // Seconds between long-poll retries.
  // The server-side long-poll timeout is 35s; this controls retry spacing.
  "poll_interval": 1.0,

  // Optional: restrict to specific WeChat user IDs.
  // Omit or leave empty for open access (anyone can message).
  // "allowed_users": ["wxid_abc123@im.wechat"]
}
```

- [ ] **Step 2: Commit**

```bash
git add tui/internal/preset/templates/wechat.jsonc
git commit -m "feat(addon): add wechat config template"
```

---

### Task 4: Add i18n strings

**Files:**
- Modify: `tui/i18n/en.json:73,143`
- Modify: `tui/i18n/zh.json:73,138`
- Modify: `tui/i18n/wen.json:73,138`

- [ ] **Step 1: Update en.json**

Add after line 73 (`firstrun.addon_desc.feishu`):

```json
  "firstrun.addon_desc.wechat": "Talk to the agent from WeChat.\nScan a QR code with your phone to connect — no API keys needed. Supports text, images, voice, video, and files.",
```

Update `palette.addon` (line 143) from:

```json
  "palette.addon": "View addon configs (IMAP, Telegram, Feishu)",
```

to:

```json
  "palette.addon": "View addon configs (IMAP, Telegram, Feishu, WeChat)",
```

- [ ] **Step 2: Update zh.json**

Add after line 73 (`firstrun.addon_desc.feishu`):

```json
  "firstrun.addon_desc.wechat": "在微信上与 Agent 对话。\n用手机扫描二维码即可连接，无需 API 密钥。支持文字、图片、语音、视频和文件。",
```

Update `palette.addon` (line 138) from:

```json
  "palette.addon": "查看扩展配置（IMAP、Telegram、飞书）",
```

to:

```json
  "palette.addon": "查看扩展配置（IMAP、Telegram、飞书、微信）",
```

- [ ] **Step 3: Update wen.json**

Add after line 73 (`firstrun.addon_desc.feishu`):

```json
  "firstrun.addon_desc.wechat": "于微信与器灵对话。\n以手机扫码即通，不须密钥。通文字、图画、语音、影像与文牍。",
```

Update `palette.addon` (line 138) from:

```json
  "palette.addon": "查看扩展配置（IMAP、Telegram、飞书）",
```

to:

```json
  "palette.addon": "查看扩展配置（IMAP、Telegram、飞书、微信）",
```

- [ ] **Step 4: Verify build**

Run: `cd tui && make build`
Expected: Build succeeds.

- [ ] **Step 5: Commit**

```bash
git add tui/i18n/en.json tui/i18n/zh.json tui/i18n/wen.json
git commit -m "feat(addon): add wechat i18n strings"
```

---

### Task 5: Integration test

- [ ] **Step 1: Run existing tests**

Run: `cd tui && go test ./...`
Expected: All tests pass. The existing `TestEnsureDefault_CreatesBuiltinPresets` may fail (pre-existing issue unrelated to this change).

- [ ] **Step 2: Manual verification**

Verify the skill is embedded correctly:

```bash
cd tui && go run . 2>&1 | head -1  # just check it launches
```

Or check the embed includes the new files:

```bash
grep -r "wechat" tui/internal/preset/skills/
# Should find SKILL.md and assets/config.json
```

- [ ] **Step 3: Final commit (if any fixups needed)**

```bash
git add -A
git commit -m "fix(addon): fixups from integration test"
```
