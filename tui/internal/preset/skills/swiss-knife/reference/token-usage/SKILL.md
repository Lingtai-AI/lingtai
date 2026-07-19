---
name: token-usage
description: >
  Nested swiss-knife reference for token usage, cost, cache, and tool-call/API-call reports.
  Use for model cost reports, cache rates, budget/burn analysis, and tools-per-API-call trends across LingTai logs.
version: 2.1.0
tags: [python, cost, tokens, litellm, budget, tools, agents]
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Token Usage, Cost, and Agentic-Intensity Reports v2

Network-wide token cost analysis powered by [litellm](https://github.com/BerriAI/litellm)'s model pricing database (2700+ models), plus runtime-event trend analysis for tool calls per API call.

## Quick Usage

Run the bundled cost script:

```bash
~/.lingtai-tui/runtime/venv/bin/python3 ~/.lingtai-tui/utilities/swiss-knife/reference/token-usage/scripts/cost_report.py /path/to/.lingtai
```

Cost-report optional flags:
- `--json` — output as JSON instead of table
- `--by-model` — group by model instead of by agent
- `--since YYYY-MM-DD` — only count entries after this date
- `--top N` — show only top N agents (default: all)
- `--custom-pricing FILE` — load custom pricing overrides from JSON



### Tool-call/API-call trend

Use this when the human asks for “tool calls per API call”, “tools per API call”, “agentic intensity”, or how tool-heavy the local LingTai network has been over time. The script reads LingTai `logs/events.jsonl` files, not `token_ledger.jsonl`, because it needs runtime event IDs.

```bash
~/.lingtai-tui/runtime/venv/bin/python3 ~/.lingtai-tui/utilities/swiss-knife/reference/token-usage/scripts/tool_calls_per_api_call_trend.py \
  /path/to/project/.lingtai \
  --days 5 \
  --timezone America/Los_Angeles \
  --model gpt-5.5
```

Optional flags:
- `--out-prefix PREFIX` — write `PREFIX.md`, `PREFIX.json`, and `PREFIX.csv`
- `--model MODEL` — also emit a model-specific table for an exact model name; repeatable
- `--include-daemons` — include daemon event logs instead of excluding them

Metric definitions:
- **API calls:** unique successful LLM response events (`type == "llm_response"`), deduplicated by `(events log path, api_call_id)`.
- **Tool calls:** unique model-requested top-level tool calls (`type == "tool_call_received"`), deduplicated by `(events log path, api_call_id, tool_call_id)` and assigned to the local-day bin of the producing API response.
- **Tool calls per API call:** `tool_calls / api_calls`.
- **Tool-using API-call rate:** fraction of API calls that produced at least one tool call.

Treat the metric as behavioral intensity, not token volume: a higher value means each model API call is asking the runtime to do more tool work on average.

## How It Works

`cost_report.py` scans every `logs/token_ledger.jsonl` under `.lingtai/`, prices
each entry via litellm `model_cost` → OpenRouter → custom-pricing fallback (see
below), and reports per-agent/grand-total cost plus cache-hit and burn rates.
`tool_calls_per_api_call_trend.py` instead scans `logs/events.jsonl`, counts
`llm_response` / `tool_call_received` events, and writes Markdown/JSON/CSV daily
trend artifacts.

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

- `cost_report.py` requires `litellm`. Check with `python3 -c "import litellm"`. If missing, ask the human before installing:

  > token-usage needs `litellm` (~5MB) for model pricing. Install it? (`pip install litellm`)

  Install only after they say yes.
- `tool_calls_per_api_call_trend.py` uses only the Python standard library.
- Python 3.9+

## Tips

- Check after spawning multiple avatars — they each burn their own system prompt
- Cache hit rate is the key efficiency metric — aim for >90%
- Tool calls per API call is a useful agentic-intensity metric: values above 1 mean the average model response asks for more than one tool call
- Daemon tokens are tracked in `daemons/<run_id>/logs/token_ledger.jsonl`
- For per-session breakdown, use `--since` to filter by date

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
