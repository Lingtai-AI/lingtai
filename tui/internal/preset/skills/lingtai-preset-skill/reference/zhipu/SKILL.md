---
name: preset-skill-zhipu
description: Official-source-led manual for the TUI `zhipu` template.
version: 2.2.0
last_changed_at: "2026-08-09T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `zhipu`

`zhipuPreset()` in `tui/internal/preset/preset.go` ships provider
`zhipu` with exact model `GLM-5.2`, `ZHIPU_API_KEY`, and the
OpenAI-compatible regional endpoints
`https://open.bigmodel.cn/api/coding/paas/v4` (CN) and
`https://api.z.ai/api/coding/paas/v4` (INTL). The manifest keeps `web_search`
and `skills` and intentionally has no built-in `vision` key: GLM-5.2 is
text-in/text-out, not a native image route.

The TUI preset editor's `base_url` row offers three options
(`ProviderRegionURLs["zhipu"]`):

- **CN** — `https://open.bigmodel.cn/api/coding/paas/v4` (the template
  default).
- **INTL** — `https://api.z.ai/api/coding/paas/v4`.
- **OpenCode Go** — `https://opencode.ai/zen/go/v1`. This is the only row
  that implies a credential: selecting it sets `api_key_env` to
  `OPENCODE_GO_API_KEY` (the shared OpenCode Go account) and saving keeps
  that slot instead of minting a region-suffixed one. Its model ids are
  LOWERCASE (`glm-5.2`, `glm-5.1`) — an uppercase id is rejected with
  `Model GLM-5.2 is not supported`, even though the native CN/INTL rows take
  the uppercase catalog names. `glm-5.2` is the verified datum (uppercase
  rejected, lowercase accepted); `glm-5.1` is inferred from the native
  catalog's `GLM-5.1` by the same lowercasing rule and has not been
  separately confirmed on the Go endpoint. There is no `glm-5`: the catalog
  has no standalone `GLM-5`, so no lowercase alias for one is shipped.

CN and INTL imply no `api_key_env`, so cycling between them preserves
whatever slot the host stamped (`ZHIPU_CN_1_API_KEY`,
`ZHIPU_INTL_1_API_KEY`). Cycling *off* OpenCode Go restores the slot that
was in place before it was selected, so the choice is reversible.

The model picker holds two mutually exclusive id sets in a single cycle —
`GLM-5.2`, `GLM-5.1` for the native rows and `glm-5.2`, `glm-5.1` for
OpenCode Go — and the model row is not coupled to the selected `base_url`
row, so it is possible to pair an id with an endpoint that rejects it. Match
the case to the row you are on. Per the TUI's model-curation rule
(`tui/CONTRACT.md`) only the latest two generations ship, so `GLM-5-Turbo`,
`GLM-4.7`, and `GLM-4.5-Air` are no longer selectable.

## Template-specific settings

The separate official [Z.AI Vision MCP](https://docs.z.ai/devpack/mcp/vision-mcp-server)
uses package `@z_ai/mcp-server` and the distinct backing model GLM-4.6V. It is a
local MCP that an operator must manually install and register; it is not
TUI-wired and is not a direct-model fallback. The preset reads `ZHIPU_API_KEY`,
while the MCP server reads `Z_AI_API_KEY`. Set the MCP’s regional `Z_AI_MODE`
from the matching docs/region (`ZHIPU` for mainland or `ZAI` for international;
the mainland docs list both values). Official guidance says Vision
Understanding is available on Lite/Pro/Max, but consumes the separate rolling
5-hour prompt pool rather than the monthly MCP quota used by search/reader/zread.
Do not auto-install, register, or invoke it. Recheck TUI source for the shipped
model, regional URL, env-var, and capability wiring. Never guess a key.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
