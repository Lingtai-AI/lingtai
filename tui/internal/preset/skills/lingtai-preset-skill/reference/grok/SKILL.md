---
name: preset-skill-grok
description: Official-source-led manual for the TUI `grok` template.
version: 1.0.0
last_changed_at: "2026-08-09T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `grok`

`grokPreset()` in `tui/internal/preset/preset.go` ships provider
`grok`, model `grok-4.5`, `api_compat: openai`, and the OpenCode Go endpoint
`https://opencode.ai/zen/go/v1` with `OPENCODE_GO_API_KEY`. The manifest has
no `vision` capability: the Go endpoint's image-input mapping for `grok-4.5`
is not pinned, so this manual records no direct image route and no Grok
vision MCP.

Unlike every other provider template, this one's **default** endpoint is
OpenCode Go. The TUI ships no native xAI route — no `api.x.ai` endpoint and
model pairing has been verified here — so Grok is reached through the
OpenCode Go subscription or not at all.

The TUI preset editor's `base_url` row offers two options
(`ProviderRegionURLs["grok"]`):

- **OpenCode Go** — `https://opencode.ai/zen/go/v1` (the template default).
  This row implies a credential: selecting it sets `api_key_env` to
  `OPENCODE_GO_API_KEY`, the same shared OpenCode Go account the `deepseek`,
  `zhipu`, `minimax`, `kimi`, and `mimo` templates use on their own OpenCode
  Go rows. Saving an edited built-in **keeps** that slot rather than minting
  a numbered one, so a user who already configured OpenCode Go for another
  provider needs no second copy of the key.
- **Custom** — free-typed endpoint (an xAI key against your own gateway, a
  proxy, a relay). Selecting it clears `base_url` so Enter opens an inline
  edit. Custom implies no `api_key_env`, and the editor deliberately leaves
  the current slot alone on this row — your endpoint keeps whatever
  credential it actually uses. **Because grok's default row is OpenCode Go,
  arriving on Custom leaves the preset pointing at `OPENCODE_GO_API_KEY`.**
  If your endpoint takes an xAI key instead, change `api_key_env` in the
  preset JSON (`GROK_API_KEY` is the conventional name). A saved edited
  built-in gets `stampAutoEnvVar`'s numbered slot instead —
  `GROK_1_API_KEY`, the prefix being the uppercased provider name, not the
  default slot name.

Because the template default row declares its own `Env`, `grok` is the one
provider where `grokPreset()`'s declared `api_key_env` (`OPENCODE_GO_API_KEY`)
deliberately differs from `ProviderDefaultEnv["grok"]` (`GROK_API_KEY`).
`GROK_API_KEY` is the provider-generic fallback name — it is not the shipped
template's slot, and it is not applied by base_url cycling. Keeping the two
different is what makes the editor treat the OpenCode Go slot as a shared
cross-provider account worth preserving across a save, rather than as this
template's own slot to be replaced by a numbered one.

**Switching an existing preset's provider *to* grok adopts
`OPENCODE_GO_API_KEY`, not `GROK_API_KEY`.** A provider switch resets
`base_url` to the new provider's first region row, and for grok that row *is*
OpenCode Go, so the row's own declared `Env` wins over the provider-generic
default: the credential slot always matches the endpoint you land on. (Every
other provider's first row declares either no `Env` or the same name as its
`ProviderDefaultEnv` — deepseek — so only grok's adopted slot changes.) If
you then cycle `base_url` to
**Custom** and your endpoint takes an xAI key, set `api_key_env` yourself —
the editor leaves the slot alone on the Custom row by design.

## Template-specific settings

Grok ids are lowercase, and there is no uppercase form to avoid. The picker
ships `grok-4.5` only — that is the id the OpenCode Go model list serves,
and per the TUI's model-curation rule
(`tui/CONTRACT.md`) only the latest two generations of a family may ship, so
no older Grok generation is offered. Read the official
[OpenCode Go docs](https://opencode.ai/docs/go/) for the current served-model
list and the [xAI API docs](https://docs.x.ai/) for model-level capability
facts; treat neither as evidence that a given id resolves on the other's
endpoint. For an image request, report that this preset's shipped wiring is
text-only. Do not guess credentials, switch providers, or auto-load/invoke an
MCP. Verify the provider/model/endpoint/env-var fields in TUI source after a
template change.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
