---
name: preset-skill-openai
description: "Use when configuring or revising the built-in openai (OpenAI-compatible) TUI preset, including pointing it at another vendor's OpenAI-compatible API or an account pool such as sub2api / subs-pool."
version: 1.0.0
last_changed_at: "2026-09-29T00:00:00Z"
related_files:
  - tui/internal/preset/preset.go
  - tui/internal/tui/preset_editor.go
  - tui/internal/tui/doctor.go
  - tui/internal/tui/SKILL.md
  - tui/CONTRACT.md
  - tui/internal/preset/revision.go
  - tui/internal/headless/preset_revision.go
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# openai preset (OpenAI-compatible)

Use this child for the built-in `openai` template (display "OpenAI-compatible")
and for any saved preset whose provider is `openai`. openaiPreset in
tui/internal/preset/preset.go ships provider `openai`, an empty model,
`api_key_env` `OPENAI_API_KEY`, `base_url` null, and `wire_api`
`chat_completions`, with web_search on DuckDuckGo, vision inheriting the
agent's own provider, and the skills default.

## Template-specific settings

- **`base_url`** — optional. Empty means the official OpenAI API
  (`https://api.openai.com/v1`). Set it to any OpenAI-compatible endpoint:
  another vendor's compatible API, a local server, a relay, or an account
  pool. The value is SDK-style and carries its own version path (for example
  `http://127.0.0.1:8080/v1`); requests go to `{base_url}/chat/completions`
  or `{base_url}/responses`.
- **`wire_api`** — `chat_completions` (the default, written explicitly) or
  `responses`. With `responses`, `responses_transport` may select `http`
  (the default, omitted) or `websocket`.
- **`api_key_env`** — the env-var name holding the key (default
  `OPENAI_API_KEY`). Editing and saving the template stamps a fresh
  per-preset slot (`OPENAI_1_API_KEY`, `OPENAI_2_API_KEY`, ...) so two
  presets never overwrite each other's key.
- **`model`** — free text. There is no universal model: read the configured
  endpoint's own catalog (an authenticated `GET {base_url}/models` when it
  implements it, otherwise its documentation) and record the exact served ID.
  Save refuses an empty model.
- **`thinking`** — standard reasoning effort: `none`, `minimal`, `low`,
  `medium`, `high`, `xhigh`, or omitted (the kernel default). It is sent as
  reasoning effort on either wire.
- **`service_tier`** — `normal` (omitted) or `fast` (sent as `priority`).
  Whether an endpoint honors it is the endpoint's business.

Other vendors are reached here, not through vendor-named providers: pick
`openai`, set `base_url` to the vendor's OpenAI-compatible endpoint, paste
that vendor's key, and type the vendor's model ID. A vendor that exposes an
Anthropic-compatible endpoint instead uses `reference/anthropic/SKILL.md`.

## Subscriptions and account pools (sub2api, subs-pool)

A subscription is not an API key. To use subscriptions (for example several
ChatGPT/Codex accounts) behind one endpoint, run an external pool/proxy such
as sub2api or [subs-pool](https://github.com/Lingtai-AI/subs-pool) and point
an `openai` preset at it. The TUI and kernel do not install, start, or
supervise the pool, and there is no built-in pool provider (`codex-pool` is
retired). A pool that serves Codex models speaks the Responses wire:

```json
"llm": {
  "provider": "openai",
  "wire_api": "responses",
  "base_url": "http://127.0.0.1:<port>/v1",
  "api_key_env": "SUBS_POOL_API_KEY",
  "model": "gpt-5.6-sol"
}
```

- `base_url` is the pool's serve address plus `/v1`.
- `api_key_env` names the env var holding the pool's **local access key**
  (the key the proxy was started with), never a ChatGPT token.
- `model` is a model the pooled accounts serve.

In the preset editor: open the `openai` template, set `wire_api` to
`responses`, fill in `base_url` and `model`, and paste the pool's local
access key on the API-key row. Install, account management, and quota live
in the pool's own README; if the proxy is down, requests fail like any
unreachable endpoint. For a single ChatGPT account, use the `codex` preset
(`reference/codex/SKILL.md`) instead.

## TUI surfaces to revise

Start at openaiPreset in tui/internal/preset/preset.go. In
tui/internal/tui/preset_editor.go the provider row cycles the four families
(`editorProviders`); `openai` has no `providerModels` entry, so the model
row is free text; base_url is free text with the official endpoint shown
when empty; `wireAPIOptions`, `responsesTransportOptions`,
`levelThinkingOptions`, and `serviceTierOptions` drive the other rows, and
`normalizeLLMForCommit` writes `wire_api` explicitly and drops stale
transport/thinking/service-tier values and the retired `api_compat` field.
`/doctor` probes this family with `GET {base_url}/models` (Bearer) and, on
the Chat Completions wire, one `max_tokens=1` completion
(`probeLLM` in tui/internal/tui/doctor.go).

## Reviewed deterministic revision

Prepare an evidence-bound manifest and explicit input, then run
lingtai-tui presets revise --manifest PATH --input PATH --mode dry-run|check|apply
[--output-dir PATH]. Review the JSON plan, use dry-run/check before apply, and
apply only to a new explicit output directory. revision.go validates hashes,
route bindings, and evidence and preserves unowned bytes; wire_api and
responses_transport changes are eligible only for `openai` Responses targets.

Maintenance: If the relevant TUI preset/page is revised, check whether this sub-skill also needs revision and, if so, include it in the same PR.

## Operations

For save, endpoint/capability, availability, activation/refresh, or
troubleshooting, use the five shared operation children under
`reference/operations/`; this child owns the OpenAI-compatible family facts,
so inspect the actual saved manifest for user-owned endpoint, model,
credential, and capability facts.
