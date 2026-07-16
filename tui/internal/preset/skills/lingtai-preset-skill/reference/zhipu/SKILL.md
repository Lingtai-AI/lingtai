---
name: preset-skill-zhipu
description: Official-source-led manual for the TUI `zhipu` template.
version: 1.0.0
last_changed_at: "2026-07-15T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `zhipu`

`zhipuPreset()` ships provider `zhipu` with exact model `GLM-5.2`,
`ZHIPU_API_KEY`, and the OpenAI-compatible regional endpoints
`https://open.bigmodel.cn/api/coding/paas/v4` (CN) and
`https://api.z.ai/api/coding/paas/v4` (INTL). The manifest keeps `web_search`
and `skills` and intentionally has no built-in `vision` key: GLM-5.2 is
text-in/text-out, not a native image route.

The separate official [Z.AI Vision MCP](https://docs.z.ai/devpack/mcp/vision-mcp-server)
uses package `@z_ai/mcp-server` and the distinct backing model GLM-4.6V. It is a
local MCP that an operator must manually install and register; it is not
TUI-wired and is not a direct-model fallback. The preset reads `ZHIPU_API_KEY`,
while the MCP server reads `Z_AI_API_KEY`. Set the MCP’s regional `Z_AI_MODE`
from the matching docs/region (`ZHIPU` for mainland or `ZAI` for international;
the mainland docs list both values). Official guidance says Vision
Understanding is available on Lite/Pro/Max, but consumes the separate rolling
5-hour prompt pool rather than the monthly MCP quota used by search/reader/zread.
Do not auto-install, register, or invoke it.

Recheck TUI source for the shipped model, regional URL, env-var, and capability
wiring. Never guess a key.
