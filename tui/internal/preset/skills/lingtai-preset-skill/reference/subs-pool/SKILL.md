---
name: preset-skill-subs-pool
description: "Use when an agent should spread Codex requests across several ChatGPT subscriptions through the external subs-pool proxy."
version: 1.0.0
last_changed_at: "2026-09-29T00:00:00Z"
related_files:
  - tui/internal/preset/preset.go
  - tui/internal/tui/preset_editor.go
  - tui/internal/preset/skills/lingtai-preset-skill/reference/custom/SKILL.md
  - tui/internal/preset/skills/lingtai-preset-skill/reference/codex/SKILL.md
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# subs-pool (external Codex account pool)

[subs-pool](https://github.com/Lingtai-AI/subs-pool) is a standalone Codex
subscription pool: a local Responses proxy, a CLI, and a Textual frontend. It
owns multi-account pooling, weights, and quota. It replaces the retired
built-in `codex-pool` template/provider and `~/.lingtai-tui/codex-auth-pool.json`;
the TUI and kernel no longer manage pooled accounts. It is not a
`BuiltinPresets()` template.

## When to use

Use it when one agent should balance across several ChatGPT/Codex accounts.
For a single account, use the `codex` preset (`reference/codex/SKILL.md`).

## Config shape

LingTai reaches the pool as a plain OpenAI-compatible Responses endpoint: a
saved preset cloned from the `custom` template with this `manifest.llm`:

```json
"llm": {
  "provider": "custom",
  "api_compat": "openai",
  "wire_api": "responses",
  "base_url": "http://127.0.0.1:<port>/v1",
  "api_key_env": "SUBS_POOL_API_KEY",
  "model": "gpt-5.6-sol"
}
```

- `base_url` is the subs-pool serve address plus `/v1`.
- `api_key_env` names the env var holding the subs-pool **local access key**
  (the key the proxy was started with), never a ChatGPT token.
- `model` is a Codex model the pooled accounts serve.

In the preset editor this is the `custom` template with `wire_api` set to
`responses`, `base_url` and `model` filled in, and the local key pasted as the
API key.

## Install and run

Follow the subs-pool repository README to install it, add accounts, and start
the proxy. The TUI does not install, start, or supervise it; if the proxy is
down, requests fail like any unreachable endpoint, so diagnose from subs-pool.
An agent that still names `codex-pool` or `templates/codex-pool.json` must be
repointed to a preset like the one above (or to `codex`).

## Operations

For save, activation/refresh, and troubleshooting, use the shared children
under `reference/operations/`; custom Responses fields are described in
`reference/custom/SKILL.md`.
