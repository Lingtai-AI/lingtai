# Telegram Bot Setup

## Overview
Telegram bot lets you interact with users via the Telegram Bot API.

## Setup Steps

1. **Create a bot** via Telegram:
   - Message @BotFather on Telegram → `/newbot` → follow the prompts → copy the bot token

2. **Add the token to the .env file** (the path is in your init.json under `env_file`):
   ```
   TELEGRAM_BOT_TOKEN=<your-token>
   ```

3. **Edit the config file** at this location:
   ```
   ~/.lingtai-tui/addons/telegram/example/config.json
   ```
   Or copy it to a new location and edit there. Fill in:
   - `bot_token_env`: `"TELEGRAM_BOT_TOKEN"` (matches the .env variable)
   - `allowed_users`: list of Telegram user IDs allowed to message the bot (optional — omit for open access)
   - To find a user ID: have them message the bot, then check the `from` field in incoming messages

4. **Tell the human** the config file is ready and they should:
   - Use `/addon` in the TUI to enter the config file path
   - Then use `/refresh` to activate the Telegram tools

## Config Template
See: `~/.lingtai-tui/addons/telegram/example/config.json`
