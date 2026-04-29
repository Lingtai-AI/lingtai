---
name: vision
description: >
  Decision-tree skill for image understanding. Routes between three paths
  depending on what the agent has access to: (1) the built-in `vision` tool
  if the LLM provider supports image input, (2) the `minimax-token-plan` skill
  if a MiniMax coding-plan key is available, or (3) a local Hugging Face
  VLM via the bundled `scripts/describe.py` if neither — offline, free,
  slow. Read this when you need to describe, OCR, or critique an image
  and you're not sure which path applies.
version: 1.0.0
---

# vision (router)

> Three paths to image understanding. Pick the cheapest available.

## Decision Tree

```
Is `vision` tool in your tool list?
├── YES → use it directly, done.
└── NO  → does the user have a MiniMax coding-plan key?
         (check ~/.lingtai-tui/.env for MINIMAX_API_KEY)
         ├── YES → see `minimax-token-plan` skill (use `understand_image` MCP tool)
         └── NO  → fall back to local VLM:
                  bash python <skill-path>/scripts/describe.py <image>
                  See reference/local-models.md.
```

## Path 1 — Built-in `vision` Tool

If your LLM provider supports image input (MiniMax, Gemini, Anthropic, OpenAI, Zhipu), the kernel exposes a `vision` tool directly. Cheapest, lowest latency, no extra setup.

`vision` not in your tool list? Your LLM provider doesn't support image input — fall through to Path 2 or 3.

## Path 2 — MiniMax via `minimax-token-plan` Skill

For text-only LLMs (DeepSeek, OpenRouter text-only, Codex) **with** a MiniMax coding-plan key. The MiniMax `understand_image` MCP tool gives you Claude-3-Haiku-class vision for ~$0.001/image.

Read the **`minimax-token-plan`** skill — it covers credential sourcing, region selection, and points at the live docs for tool parameters. MCP server registration itself is owned by `lingtai-mcp`.

## Path 3 — Local VLM (offline, unlimited)

For agents that need image analysis without a vision-capable LLM **and** without a MiniMax key. Or for batch jobs / privacy-sensitive content / unlimited quota.

Run a Hugging Face vision-language model locally:

```bash
python <skill-path>/scripts/describe.py <image-path> [--prompt PROMPT] [--model qwen2-vl-2b|moondream2|qwen2-vl-7b]
```

First call downloads weights (2–15 GB depending on model), subsequent calls reuse the cache. The script auto-installs `transformers` + `torch` + model-specific deps via `lingtai.venv_resolve.ensure_package`.

Output is JSON on stdout: `{image, backend, prompt, response, elapsed_seconds}`. Errors → stderr, non-zero exit.

See [reference/local-models.md](reference/local-models.md) for model selection, hardware tradeoffs, prompt templates per model, batch patterns (load model once, loop many images), and failure modes.

## When NOT to use this skill

- Your LLM already has `vision` in its tool list — Path 1, no decision needed.
- You need to *generate* an image — use `minimax-token-plan` (`text_to_image`).
- You need to *describe video frames* — extract frames with `ffmpeg` first, then loop this skill over them.
