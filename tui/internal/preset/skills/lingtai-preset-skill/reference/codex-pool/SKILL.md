---
name: preset-skill-codex-pool
description: Official-source-led manual for the TUI `codex-pool` template.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `codex-pool`

`codexPoolPreset()` ships provider `codex-pool`, exact model `gpt-5.6-sol`,
`https://chatgpt.com/backend-api/codex`, `thinking: xhigh`, and an empty
`api_key_env` because it selects from local ChatGPT OAuth token files. The
manifest declares `vision` parity with `codex`; pooling changes account
selection, not the model or endpoint.

That manifest parity requires the companion kernel pool-vision dispatch support.
Kernel versions before the pool-vision fix can silently skip this capability
because `codex-pool` was absent from the vision-provider allow-list. Do not
claim current-main runtime parity before that companion support is merged and
verified. No separate official vision MCP is established; a native failure must
remain visible rather than falling back to generic OpenAI, guessing credentials,
or auto-loading an MCP.

Read the official [Codex authentication](https://developers.openai.com/codex/auth)
and [Codex models](https://developers.openai.com/codex/models) pages on demand.
Verify the template’s model, endpoint, and capability fields in TUI source;
never document pool membership or OAuth token contents here.
