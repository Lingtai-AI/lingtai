---
name: headless-bot
description: >
  Use when creating a fresh headless LingTai project, including the bundled
  Telegram-only provisioning helper.
version: 1.0.0
last_changed_at: "2026-10-12T18:16:58Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Headless Bot Project

Nested lingtai-bot-guide reference, promoted with its script from Swiss Knife.
Read [initiation](../initiation/SKILL.md) first. This creates a normal project,
then wires a bridge; it is not a new kernel Project API.

## Inputs and supported creation

`PROJECT_DIR` must not contain `.lingtai/`; choose `PRESET`, `AGENT_NAME` (a safe
single directory segment), and `LANGUAGE` (`en`, `zh`, `wen`). Inspect the installed
CLI help before adapting this supported invocation:

```bash
lingtai-tui spawn "$PROJECT_DIR" \
  --preset "$PRESET" --agent-name "$AGENT_NAME" --language "$LANGUAGE"
```

It creates **and launches** the agent with the `plain` recipe. Parse its JSON
`agent_dir`, PID and readiness fields; do not guess the agent path. `ready` is a
bounded heartbeat observation, not a guarantee of model or channel readiness.
A nonzero exit/partial creation is recovery state, not permission to delete or
retry on the same directory. Source: [headless anatomy](../../../../../headless/ANATOMY.md).

## Preferred Telegram helper

Resolve this skill's root from the installed skills catalog, then use
`reference/headless-bot/scripts/create_telegram_bot_project.py` relative to it
(or `scripts/create_telegram_bot_project.py` relative to this reference). The
source checkout contains the same relative subtree. No private `.library` path
or obsolete Swiss Knife alias is required. The script has **only standard-library
imports**, no sibling-module or asset dependencies; invoke via Python rather than
assuming the extracted file is executable.

In Bash, collect the token without placing it in argv/history:

```bash
read -rsp 'Telegram bot token: ' TELEGRAM_BOT_TOKEN; export TELEGRAM_BOT_TOKEN; echo
python3 "$BOT_GUIDE_ROOT/reference/headless-bot/scripts/create_telegram_bot_project.py" \
  --project-dir "$PROJECT_DIR" --preset "$PRESET" \
  --agent-name "$AGENT_NAME" --language "$LANGUAGE" \
  --allowed-users "$ALLOWED_USERS"
unset TELEGRAM_BOT_TOKEN
```

`ALLOWED_USERS` must contain the explicitly approved comma-separated numeric user
IDs. Use a secure local prompt/environment mechanism for other shells; do not
paste tokens into transcripts. An optional `--preset-policy-from <source-init>`
copies only the source's `manifest.preset` policy; use it only after the owner
approves every allowed preset. Never copy saved preset JSON or credentials.

The helper runs spawn, writes `.secrets/telegram.json` (`0600`, directory `0700`),
merges `addons` and `mcp.telegram` and uses the current module:

```json
"args": ["-m", "lingtai.mcp_servers.telegram"]
```

It references that sidecar through `LINGTAI_TELEGRAM_CONFIG`, optionally copies
preset policy, and touches `.refresh`. Its `configured` output is **not** an
end-to-end PASS. The helper chooses the standard per-user managed runtime Python
(with Windows `Scripts/python.exe`); for a nonstandard install, inspect/adapt the
activation using the curated owner before use.

## Limits and manual adaptation

The helper implements **Telegram only**. [Channels](../channels/SKILL.md) routes
all other supported bridges to their exact current owners, without duplicating
secret schemas. For manual Telegram setup, load `mcp-manual` →
`reference/curated-addons.md` and the Telegram README; preserve unrelated entries.

The script is a fresh-project provisioner, not a reconciler, dry-run, rollback,
or transaction. It starts the agent **before** writing channel config and can
leave a running/partially configured project if a later step fails. Its secret
writer truncates the target sidecar; its policy copy replaces that policy block.
The spawn guard must hold: never repurpose it for an existing agent, foreign
sidecar or directory, or concurrent bot setup. Recover by inventory/inspection,
not a second spawn. Redaction is token-pattern based, not proof all subprocess
output is safe; review sanitized output before sharing.

## Verification and secret incident response

Follow [operations](../operations/SKILL.md) plus the curated owner's **Telegram
setup/readiness checklist**: one intended agent/child/account, allowed-user fresh
inbound message and same-channel reply, plus approved access-denial validation.
`getMe` alone proves only Bot API reachability. Do not run a competing `getUpdates`
poller while the listener owns polling. Never print token-bearing exception URLs.

Keep `.secrets`, `.env`, tokens, IDs and raw logs out of Git/issues/reports.
Do not omit the allowlist to make testing easier. If a token appears in logs,
Git history, chat, argv or captures, revoke/rotate it with the human through
BotFather immediately; do not spread the leaked value in the incident receipt.
