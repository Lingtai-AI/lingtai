---
name: preset-skill-mimo
description: Official-source-led manual for the TUI `mimo` template.
version: 2.2.0
last_changed_at: "2026-08-09T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `mimo`

`mimoPreset()` (`tui/internal/preset/preset.go:1213-1238`) ships Xiaomi MiMo
model `mimo-v2.5` at `https://api.xiaomimimo.com/v1` with OpenAI
compatibility and `XIAOMI_API_KEY`. The manifest explicitly wires native
vision to that exact default model.

The TUI preset editor's `base_url` row offers three options
(`ProviderRegionURLs["mimo"]`):

- **MiMo** — `https://api.xiaomimimo.com/v1` (the template default).
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

MiMo implies no `api_key_env`, so saving an edited built-in on that row
yields a host-stamped numbered slot rather than overwriting the provider
default. **The slot is `MIMO_1_API_KEY`, not `XIAOMI_1_API_KEY`**:
`AutoEnvVarName` (`preset.go`) builds the prefix by uppercasing the PROVIDER
name (`mimo`), never from `ProviderDefaultEnv`. The `XIAOMI_` prefix appears
only on `ProviderDefaultEnv["mimo"] = "XIAOMI_API_KEY"`, which is the
template's shared slot — precisely the thing a numbered slot exists to
replace. Cycling *off* OpenCode Go restores the slot that was in place before
it was selected, so the choice is reversible.

## Template-specific settings

The model picker ships `mimo-v2.5` and its text-only sibling
`mimo-v2.5-pro`, and nothing else: per the TUI's model-curation rule
(`tui/CONTRACT.md`) only the latest two generations of a family are shipped,
so the older `mimo-v2-pro` / `mimo-v2-omni` ids and the retired V2 Flash IDs
are not selectable. Both shipped ids are served by Xiaomi's own endpoint
*and* by OpenCode Go, so the picker is valid on either `base_url` row — MiMo
ids are lowercase on both, and there is no uppercase form to avoid.

LingTai-side vision is wired to `mimo-v2.5` on Xiaomi's own endpoint only.
Switching to `mimo-v2.5-pro` or to the OpenCode Go row does not re-verify
that path, and `modelHasVision` records `mimo-v2.5-pro` as an explicit
`false` rather than leaving it undeclared.

Read the official [MiMo developer introduction](https://platform.xiaomimimo.com/llms.txt)
and [OpenAI-compatible API page](https://platform.xiaomimimo.com/docs/zh-CN/api/chat/openai-api)
on demand for current models, regions, and image-input rules. No official MiMo
vision MCP is established by the reviewed evidence. A direct-call failure
remains a direct-call failure; do not switch providers or auto-load/invoke an
MCP. Recheck `preset.go` and the preset editor for LingTai-owned model,
endpoint, env-var, and capability wiring.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
