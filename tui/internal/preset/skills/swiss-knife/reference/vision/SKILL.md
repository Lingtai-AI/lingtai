---
name: vision
description: >
  Nested swiss-knife reference for image understanding — a decision tree that
  routes between three paths depending on what the agent has access to: (1) the
  built-in `vision` tool if the LLM provider supports image input, (2) the
  sibling `minimax-cli` reference if a usable MiniMax preset/key slot is
  available, or (3) a local Hugging Face VLM via the bundled
  `scripts/describe.py` if neither — offline, free, slow. Read this when you
  need to describe, OCR, or critique an image and you're not sure which path
  applies.
version: 1.0.0
tags: [vision, image, ocr, vlm, decision-tree, nested-skill]
---

# vision (router)

> Nested swiss-knife reference for image understanding. Three paths — pick the cheapest available.

## Decision Tree

```
Is `vision` tool in your tool list?
├── YES → use it directly, done.
└── NO  → does the user have a MiniMax preset/key slot?
         (read the sibling `../minimax-cli/SKILL.md`; scan ~/.lingtai-tui/presets/** without printing keys)
         ├── YES → use the `minimax-cli` reference for `mmx vision …` from the shell, or an already-registered MCP tool
         └── NO  → fall back to local VLM:
                  python3 <skill-path>/scripts/describe.py <image>
                  See reference/local-models.md.
```

## Path 1 — Built-in `vision` Tool

If your LLM provider supports image input (MiniMax, Gemini, Anthropic, OpenAI, Zhipu), the kernel exposes a `vision` tool directly. Cheapest, lowest latency, no extra setup.

`vision` not in your tool list? Your LLM provider doesn't support image input — fall through to Path 2 or 3.

## Path 2 — MiniMax via the sibling `minimax-cli` reference

For text-only LLMs (DeepSeek, OpenRouter text-only, Codex) **with** a usable MiniMax preset/key slot. Two routes can exist:

- **Shell** — `mmx vision …` via the official CLI. No MCP registration needed; just install + key. Best for ad-hoc one-shots in bash.
- **In-tool** — an already-registered MiniMax MCP vision tool. Best when the agent needs vision as a tool call inside a longer reasoning loop. MCP server registration is owned by `mcp-manual` (kernel `mcp` capability).

Read the sibling **`minimax-cli`** reference (`../minimax-cli/SKILL.md`) before using the shell route. It owns the canonical MiniMax credential discovery flow: scan TUI presets recursively, pick the declared slot, export it without printing the key, and match the preset region/base URL. Do not assume a bare `MINIMAX_API_KEY` is the only valid slot.

## Path 3 — Local VLM (offline, unlimited)

For agents that need image analysis without a vision-capable LLM **and** without a MiniMax key. Or for batch jobs / privacy-sensitive content / unlimited quota.

Run a Hugging Face vision-language model locally:

```bash
python3 <skill-path>/scripts/describe.py <image-path> [--prompt PROMPT] [--model qwen2-vl-2b|moondream2|qwen2-vl-7b]
```

First call downloads weights (2–15 GB depending on model), subsequent calls reuse the cache. The script auto-installs `transformers` + `torch` + model-specific deps via `lingtai.venv_resolve.ensure_package`.

Output is JSON on stdout: `{image, backend, prompt, response, elapsed_seconds}`. Errors → stderr, non-zero exit.

See [reference/local-models.md](reference/local-models.md) for model selection, hardware tradeoffs, prompt templates per model, batch patterns (load model once, loop many images), and failure modes.

## When NOT to use this skill

- Your LLM already has `vision` in its tool list — Path 1, no decision needed.
- You need to *generate* an image — use the sibling `minimax-cli` reference (`../minimax-cli/SKILL.md`, `mmx image …`).
- You need to *describe video frames* — extract frames with `ffmpeg` first, then loop this skill over them.

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
