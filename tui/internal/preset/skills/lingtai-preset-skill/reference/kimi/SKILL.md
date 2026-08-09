---
name: preset-skill-kimi
description: Official-source-led manual for the TUI `kimi` template.
version: 2.2.0
last_changed_at: "2026-08-09T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `kimi`

`kimiPreset()` (`tui/internal/preset/preset.go:1280-1292`) ships Kimi Code
model `kimi-for-coding` at the exact OpenAI-compatible endpoint
`https://api.kimi.com/coding/v1` with `KIMI_CODE_API_KEY`. The manifest has
no built-in `vision` capability, so this shipped preset remains unwired for
LingTai-side vision.

The TUI preset editor's `base_url` row offers three options
(`ProviderRegionURLs["kimi"]`):

- **Kimi Code** — `https://api.kimi.com/coding/v1` (the template default).
- **OpenCode Go** — `https://opencode.ai/zen/go/v1`. This is the only row
  that implies a credential: selecting it sets `api_key_env` to
  `OPENCODE_GO_API_KEY` (the shared OpenCode Go account) and saving keeps
  that slot instead of minting a numbered one.
- **Custom** — free-typed endpoint (a proxy, relay, or self-hosted gateway).
  Selecting it clears `base_url` so Enter opens an inline edit. Custom
  implies no `api_key_env`, and the editor deliberately leaves the current
  slot alone on this row — your endpoint keeps whatever credential it
  actually uses. If you arrived here from the OpenCode Go row the preset is
  still pointing at `OPENCODE_GO_API_KEY`; change it in the preset JSON if
  your endpoint takes a different key. A saved edited built-in gets
  `stampAutoEnvVar`'s numbered slot instead, because an empty `base_url` is
  not a row-declared cross-provider account.

Kimi Code implies no `api_key_env`, so saving an edited built-in on that row
yields a host-stamped numbered slot rather than overwriting the provider
default. The slot name is derived from the PROVIDER name, not from
`ProviderDefaultEnv`, so it is `KIMI_1_API_KEY` — not `KIMI_CODE_1_API_KEY`
(`AutoEnvVarName`, `preset.go`). Cycling *off* OpenCode Go restores the slot
that was in place before it was selected, so the choice is reversible.

The model row is FREE TEXT for kimi — there is deliberately no entry in the
editor's `providerModels` picker (`tui/internal/tui/preset_editor.go`).
Moonshot's catalog is wider than any list the TUI can verify, and a picker
would let one `→` overwrite a typed id with the first list entry. Type the id
you want:

- Native Kimi Code row: the subscription model `kimi-for-coding` (the
  template default).
- OpenCode Go row: LOWERCASE K-series ids, currently `kimi-k3` and
  `kimi-k2.7-code`. Only these two generations are current; older K2.x ids
  may have been retired on the Go endpoint. Verify against the OpenCode Go
  model list before relying on one.

## Template-specific settings

Current Kimi Code/K2.7 evidence describes native text, image, and video input,
but the literal `kimi-for-coding` endpoint mapping was not mechanically pinned
by the reviewed provider model table. Treat that mapping as conditional rather
than claiming provider-wide text-only or unconditional direct support. Read the
official [Kimi Code docs](https://www.kimi.com/code/docs/en/), [K2.7 Code
quickstart](https://platform.kimi.com/docs/guide/kimi-k2-7-code-quickstart), and
[provider configuration](https://www.kimi.com/code/docs/en/kimi-code-cli/configuration/providers.html)
when the model or endpoint changes. This manual makes no automatic fallback or
MCP claim; do not guess credentials or silently switch providers. Verify
current wiring in `preset.go`.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
