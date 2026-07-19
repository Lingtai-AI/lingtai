---
name: preset-skill-nvidia
description: Official-source-led manual for the TUI `nvidia` template.
version: 2.0.0
last_changed_at: "2026-07-19T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# `nvidia`

`nvidiaPreset()` (`tui/internal/preset/preset.go:1106-1125`) ships exact
model `meta/llama-3.3-70b-instruct` at `https://integrate.api.nvidia.com/v1`
with OpenAI compatibility and `NVIDIA_API_KEY`. This default model is
text-only and the manifest has no `vision` capability.

## Template-specific settings

The same hosted gateway may offer other VLM slugs, but a clone selecting one
still needs an explicit `capabilities.vision` entry; changing only `llm.model`
does not wire a LingTai vision route. Read the official [NVIDIA API Catalog](https://build.nvidia.com/),
[model catalog](https://build.nvidia.com/models), and [NIM API reference](https://docs.api.nvidia.com/nim/reference/llm-apis)
on demand. No NVIDIA plan-level vision MCP is established. Do not switch
providers or auto-load/invoke an MCP. Recheck TUI source for the template
fields after changes. Never guess a key or copy the volatile catalog into this
manual.

## Operations

For base URL/API-compat/model/capability declaration shape versus
credentials, see `reference/operations/endpoint-capabilities/SKILL.md`.
