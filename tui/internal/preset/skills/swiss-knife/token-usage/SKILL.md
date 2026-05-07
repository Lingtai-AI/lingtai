---
name: token-usage
description: >
  Network-wide token usage and cost calculator using litellm's model pricing database.
  Reads token_ledger.jsonl from all agents, matches models to litellm pricing, and
  produces a per-agent and grand-total cost report. Supports any model litellm knows
  about (2700+ models) plus custom pricing overrides. Use when the human asks about
  token usage, cost, budget, or spending.
version: 2.0.0
tags: [python, cost, tokens, litellm, budget]
---

# Token Usage & Cost Calculator v2

Network-wide token cost analysis powered by [litellm](https://github.com/BerriAI/litellm)'s model pricing database (2700+ models).

## Quick Usage

Run the bundled script:

```bash
~/.lingtai-tui/runtime/venv/bin/python3 ~/.lingtai-tui/utilities/swiss-knife/token-usage/scripts/cost_report.py /path/to/.lingtai
```

Optional flags:
- `--json` — output as JSON instead of table
- `--by-model` — group by model instead of by agent
- `--since YYYY-MM-DD` — only count entries after this date
- `--top N` — show only top N agents (default: all)
- `--custom-pricing FILE` — load custom pricing overrides from JSON

## How It Works

1. Scans all `logs/token_ledger.jsonl` files under the `.lingtai/` network directory
2. For each entry, looks up the model in `litellm.model_cost` (2700+ models)
3. Falls back to OpenRouter API (`/api/v1/models`) for real-time pricing (368 models)
4. Falls back to custom pricing if the model isn't in either source
5. Calculates per-agent and grand-total costs
6. Reports cache hit rate, burn rate, and per-model breakdown

## Pricing Sources

**Primary:** litellm's `model_cost` dictionary — covers OpenAI, Anthropic, Google, Meta, Mistral, Xiaomi, and 100+ other providers.

**Secondary:** OpenRouter API — real-time pricing for 368 models, more up-to-date than litellm for some providers.

**Fallback:** Custom pricing in `scripts/custom_pricing.json` — for models not yet in litellm or OpenRouter (e.g., MiMo v2.5 Pro direct API).

**Override:** Pass `--custom-pricing FILE` to add project-specific pricing.

## Custom Pricing Format

```json
{
  "mimo-v2.5-pro": {
    "input_cost_per_token": 0.000001,
    "output_cost_per_token": 0.000003,
    "cache_read_input_token_cost": 0.0000002
  }
}
```

## Ledger Format

Each agent's `logs/token_ledger.jsonl` has one JSON object per LLM call:

```json
{
  "source": "main",
  "ts": "2026-05-06T19:31:40Z",
  "input": 53295,
  "output": 277,
  "thinking": 172,
  "cached": 12288,
  "model": "mimo-v2.5-pro",
  "endpoint": "https://api.xiaomimimo.com/v1"
}
```

## Dependencies

- `litellm` — required. Check with `python3 -c "import litellm"`. If missing, ask the human before installing:

  > token-usage needs `litellm` (~5MB) for model pricing. Install it? (`pip install litellm`)

  Install only after they say yes.
- Python 3.9+

## Tips

- Check after spawning multiple avatars — they each burn their own system prompt
- Cache hit rate is the key efficiency metric — aim for >90%
- Daemon tokens are tracked in `daemons/<run_id>/logs/token_ledger.jsonl`
- For per-session breakdown, use `--since` to filter by date
