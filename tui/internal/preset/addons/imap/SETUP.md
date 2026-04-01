# IMAP Email Setup

## Overview
IMAP email lets you send and receive real emails via IMAP/SMTP.

## Setup Steps

1. **Choose an email account** for this agent (e.g., a dedicated Gmail address).

2. **Create an App Password** (regular passwords do NOT work):
   - **Gmail**: Enable 2FA at myaccount.google.com/security → Go to myaccount.google.com/apppasswords → Create a new app password → Copy the 16-char password
   - **Outlook**: Enable 2FA at account.microsoft.com/security → Security → App passwords → Create

3. **Add the password to the .env file** (the path is in your init.json under `env_file`):
   ```
   IMAP_PASSWORD=xxxx xxxx xxxx xxxx
   ```

4. **Edit the config file** at this location:
   ```
   ~/.lingtai-tui/addons/imap/example/config.json
   ```
   Or copy it to a new location and edit there. Fill in:
   - `email_address`: the agent's email address
   - `email_password_env`: `"IMAP_PASSWORD"` (matches the .env variable)
   - `imap_host`: `"imap.gmail.com"` for Gmail, `"imap.outlook.com"` for Outlook
   - `smtp_host`: `"smtp.gmail.com"` for Gmail, `"smtp.outlook.com"` for Outlook
   - `allowed_senders`: list of email addresses allowed to message this agent (optional — omit for open access)

5. **Tell the human** the config file is ready and they should:
   - Use `/addon` in the TUI to enter the config file path
   - Then use `/refresh` to activate the IMAP tools

## Config Template
See: `~/.lingtai-tui/addons/imap/example/config.json`
