#!/usr/bin/env python3
"""Network-wide token usage & cost calculator powered by litellm.

Usage:
    python3 cost_report.py /path/to/.lingtai [--json] [--by-model] [--since DATE] [--top N]
"""

import argparse
import json
import os
import sys
from collections import defaultdict
from datetime import datetime
from pathlib import Path

# ---------------------------------------------------------------------------
# Pricing lookup
# ---------------------------------------------------------------------------

# Custom pricing for models not yet in litellm.
# Override with --custom-pricing FILE or edit scripts/custom_pricing.json.
CUSTOM_PRICING = {
    "mimo-v2.5-pro": {
        "input_cost_per_token": 1.00 / 1_000_000,       # $1.00/M input
        "output_cost_per_token": 3.00 / 1_000_000,      # $3.00/M output
        "cache_read_input_token_cost": 0.20 / 1_000_000, # $0.20/M cached
    },
    "mimo-v2.5-lite": {
        "input_cost_per_token": 0.20 / 1_000_000,
        "output_cost_per_token": 0.60 / 1_000_000,
        "cache_read_input_token_cost": 0.04 / 1_000_000,
    },
}


def load_openrouter_pricing():
    """Fetch model pricing from OpenRouter API. Returns dict keyed by model slug."""
    import urllib.request
    pricing = {}
    try:
        url = "https://openrouter.ai/api/v1/models"
        req = urllib.request.Request(url, headers={"User-Agent": "lingtai-token-usage/2.0"})
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read())
        for model in data.get("data", []):
            model_id = model.get("id", "")  # e.g. "openai/gpt-4o"
            pricing_info = model.get("pricing", {})
            if not pricing_info:
                continue
            prompt_price = float(pricing_info.get("prompt", 0))
            completion_price = float(pricing_info.get("completion", 0))
            pricing[model_id] = {
                "input_cost_per_token": prompt_price,
                "output_cost_per_token": completion_price,
                "cache_read_input_token_cost": 0,  # OpenRouter doesn't expose cache pricing
                "source": "openrouter",
            }
    except Exception as e:
        print(f"Warning: Could not fetch OpenRouter pricing: {e}", file=sys.stderr)
    return pricing


def load_litellm_cost():
    """Load litellm's model_cost dictionary. Returns empty dict on failure."""
    try:
        import litellm
        return litellm.model_cost
    except ImportError:
        print("Warning: litellm not installed. Using custom pricing only.", file=sys.stderr)
        return {}


def load_custom_pricing(path=None):
    """Load custom pricing overrides from a JSON file."""
    pricing = dict(CUSTOM_PRICING)
    if path and os.path.exists(path):
        with open(path) as f:
            pricing.update(json.load(f))
    # Also try the bundled custom_pricing.json
    bundled = os.path.join(os.path.dirname(__file__), "custom_pricing.json")
    if os.path.exists(bundled):
        with open(bundled) as f:
            pricing.update(json.load(f))
    return pricing


def find_model_pricing(model_name, litellm_cost, custom_pricing, openrouter_pricing=None):
    """Find pricing for a model: OpenRouter → litellm → custom → fuzzy match."""
    if not model_name:
        return None
    if openrouter_pricing is None:
        openrouter_pricing = {}

    # Normalize: strip provider prefix (e.g., "openai/gpt-4" -> "gpt-4")
    normalized = model_name.split("/")[-1] if "/" in model_name else model_name

    # 1. Exact match in OpenRouter
    for key in [model_name, normalized]:
        if key in openrouter_pricing:
            entry = openrouter_pricing[key]
            return {"input": entry["input_cost_per_token"], "output": entry["output_cost_per_token"],
                    "cached": entry.get("cache_read_input_token_cost", 0), "source": "openrouter"}

    # 2. Exact match in litellm
    for key in [model_name, normalized]:
        if key in litellm_cost:
            entry = litellm_cost[key]
            return {"input": entry.get("input_cost_per_token", 0), "output": entry.get("output_cost_per_token", 0),
                    "cached": entry.get("cache_read_input_token_cost", entry.get("input_cost_per_token_cache_hit", 0)),
                    "source": "litellm"}

    # 3. Exact match in custom pricing
    for key in [model_name, normalized]:
        if key in custom_pricing:
            entry = custom_pricing[key]
            return {"input": entry.get("input_cost_per_token", 0), "output": entry.get("output_cost_per_token", 0),
                    "cached": entry.get("cache_read_input_token_cost", 0), "source": "custom"}

    # 4. Fuzzy: suffix match in OpenRouter
    for key in openrouter_pricing:
        if key.endswith("/" + normalized) or key.endswith("/" + model_name):
            entry = openrouter_pricing[key]
            return {"input": entry["input_cost_per_token"], "output": entry["output_cost_per_token"],
                    "cached": entry.get("cache_read_input_token_cost", 0), "source": f"openrouter:{key}"}

    # 5. Fuzzy: suffix match in litellm
    for key in litellm_cost:
        if key.endswith("/" + normalized) or key.endswith("/" + model_name):
            entry = litellm_cost[key]
            return {"input": entry.get("input_cost_per_token", 0), "output": entry.get("output_cost_per_token", 0),
                    "cached": entry.get("cache_read_input_token_cost", entry.get("input_cost_per_token_cache_hit", 0)),
                    "source": f"litellm:{key}"}

    return None


# ---------------------------------------------------------------------------
# Ledger parsing
# ---------------------------------------------------------------------------

def parse_ledger(path, since=None):
    """Parse a token_ledger.jsonl file, optionally filtering by date."""
    entries = []
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    d = json.loads(line)
                    if since:
                        ts = d.get("ts", "")
                        if ts and ts[:10] < since:
                            continue
                    entries.append(d)
                except json.JSONDecodeError:
                    continue
    except (OSError, IOError):
        pass
    return entries


def aggregate_entries(entries):
    """Aggregate token entries into totals."""
    total_in = 0
    total_out = 0
    total_cached = 0
    total_think = 0
    by_model = defaultdict(lambda: {"input": 0, "output": 0, "cached": 0, "thinking": 0, "calls": 0})

    for d in entries:
        inp = d.get("input", 0)
        out = d.get("output", 0)
        cached = d.get("cached", 0)
        think = d.get("thinking", 0)
        model = d.get("model", "unknown")

        total_in += inp
        total_out += out
        total_cached += cached
        total_think += think

        m = by_model[model]
        m["input"] += inp
        m["output"] += out
        m["cached"] += cached
        m["thinking"] += think
        m["calls"] += 1

    return {
        "input": total_in,
        "output": total_out,
        "cached": total_cached,
        "thinking": total_think,
        "calls": len(entries),
        "by_model": dict(by_model),
    }


def calculate_cost(agg, pricing_fn):
    """Calculate cost from aggregated tokens using a pricing function."""
    cost = 0.0
    cost_breakdown = {}
    unmapped_models = []

    for model, stats in agg["by_model"].items():
        p = pricing_fn(model)
        if p is None:
            unmapped_models.append(model)
            # Use zero pricing for unmapped models
            p = {"input": 0, "output": 0, "cached": 0, "source": "unmapped"}

        miss_input = stats["input"] - stats["cached"]
        model_cost = (
            miss_input * p["input"]
            + stats["cached"] * p["cached"]
            + (stats["output"] + stats["thinking"]) * p["output"]
        )
        cost += model_cost
        cost_breakdown[model] = {
            "cost": model_cost,
            "pricing_source": p.get("source", "unknown"),
            "stats": stats,
        }

    return cost, cost_breakdown, unmapped_models


# ---------------------------------------------------------------------------
# Report formatting
# ---------------------------------------------------------------------------

def format_tokens(n):
    if n >= 1_000_000:
        return f"{n/1_000_000:.1f}M"
    elif n >= 1_000:
        return f"{n/1_000:.1f}K"
    return str(n)


def print_table_report(results, grand_total, grand_cost, unmapped, openrouter_n=0, litellm_n=0):
    """Print a human-readable table report."""
    print("=" * 80)
    print("  TOKEN USAGE & COST REPORT")
    print(f"  Pricing: OpenRouter ({openrouter_n}) + litellm ({litellm_n}) models")
    print("=" * 80)
    print()
    print(f"{'Agent':<28} {'Calls':>6} {'Input':>10} {'Output':>10} {'Cache%':>7} {'Cost':>10}")
    print("-" * 80)

    # Sort by cost descending
    sorted_results = sorted(results, key=lambda x: x["cost"], reverse=True)

    for r in sorted_results:
        cache_pct = f"{r['cached']/r['input']*100:.0f}%" if r["input"] > 0 else "N/A"
        print(f"{r['name']:<28} {r['calls']:>6} {format_tokens(r['input']):>10} "
              f"{format_tokens(r['output']):>10} {cache_pct:>7} ${r['cost']:>8.2f}")

    print("-" * 80)

    cache_pct = f"{grand_total['cached']/grand_total['input']*100:.1f}%" if grand_total["input"] > 0 else "N/A"
    print(f"{'TOTAL':<28} {grand_total['calls']:>6} {format_tokens(grand_total['input']):>10} "
          f"{format_tokens(grand_total['output']):>10} {cache_pct:>7} ${grand_cost:>8.2f}")

    if unmapped:
        print()
        print(f"⚠️  Models without pricing ({len(unmapped)}):")
        for m in sorted(set(unmapped)):
            print(f"    {m}")
        print("    (cost shown as $0.00 — add to custom_pricing.json to track)")

    print()
    print(f"  Total tokens: {format_tokens(grand_total['input'] + grand_total['output'] + grand_total['thinking'])}")
    print(f"  Total calls:  {grand_total['calls']}")
    print(f"  Cache hit:    {cache_pct}")
    print(f"  Est. cost:    ${grand_cost:.2f}")


def print_model_report(results):
    """Print a per-model breakdown across all agents."""
    model_totals = defaultdict(lambda: {"input": 0, "output": 0, "cached": 0, "thinking": 0, "calls": 0, "agents": set()})

    for r in results:
        for model, stats in r["by_model"].items():
            m = model_totals[model]
            m["input"] += stats["input"]
            m["output"] += stats["output"]
            m["cached"] += stats["cached"]
            m["thinking"] += stats["thinking"]
            m["calls"] += stats["calls"]
            m["agents"].add(r["name"])

    print()
    print("PER-MODEL BREAKDOWN")
    print("-" * 80)
    print(f"{'Model':<30} {'Calls':>6} {'Tokens':>10} {'Agents':>7}")
    print("-" * 80)

    for model, stats in sorted(model_totals.items(), key=lambda x: x[1]["calls"], reverse=True):
        total = stats["input"] + stats["output"] + stats["thinking"]
        print(f"{model:<30} {stats['calls']:>6} {format_tokens(total):>10} {len(stats['agents']):>7}")


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(description="Network-wide token cost report")
    parser.add_argument("network_dir", help="Path to .lingtai/ network directory")
    parser.add_argument("--json", action="store_true", help="Output as JSON")
    parser.add_argument("--by-model", action="store_true", help="Show per-model breakdown")
    parser.add_argument("--since", help="Only count entries after this date (YYYY-MM-DD)")
    parser.add_argument("--top", type=int, help="Show only top N agents")
    parser.add_argument("--custom-pricing", help="Path to custom pricing JSON file")
    args = parser.parse_args()

    network_dir = Path(args.network_dir)
    if not network_dir.exists():
        print(f"Error: {network_dir} does not exist", file=sys.stderr)
        sys.exit(1)

    # Load pricing
    openrouter_pricing = load_openrouter_pricing()
    litellm_cost = load_litellm_cost()
    custom_pricing = load_custom_pricing(args.custom_pricing)
    pricing_fn = lambda model: find_model_pricing(model, litellm_cost, custom_pricing, openrouter_pricing)

    # Scan all agents
    results = []
    all_unmapped = []
    grand_total = {"input": 0, "output": 0, "cached": 0, "thinking": 0, "calls": 0}
    grand_cost = 0.0

    for agent_dir in sorted(network_dir.iterdir()):
        if not agent_dir.is_dir():
            continue
        ledger_path = agent_dir / "logs" / "token_ledger.jsonl"
        if not ledger_path.exists():
            continue

        entries = parse_ledger(ledger_path, args.since)
        if not entries:
            continue

        agg = aggregate_entries(entries)
        cost, cost_breakdown, unmapped = calculate_cost(agg, pricing_fn)
        all_unmapped.extend(unmapped)

        results.append({
            "name": agent_dir.name,
            "calls": agg["calls"],
            "input": agg["input"],
            "output": agg["output"],
            "cached": agg["cached"],
            "thinking": agg["thinking"],
            "cost": cost,
            "by_model": agg["by_model"],
            "cost_breakdown": cost_breakdown,
        })

        grand_total["input"] += agg["input"]
        grand_total["output"] += agg["output"]
        grand_total["cached"] += agg["cached"]
        grand_total["thinking"] += agg["thinking"]
        grand_total["calls"] += agg["calls"]
        grand_cost += cost

    # Apply --top
    if args.top:
        results = sorted(results, key=lambda x: x["cost"], reverse=True)[:args.top]

    # Output
    if args.json:
        output = {
            "agents": results,
            "total": grand_total,
            "total_cost": grand_cost,
            "unmapped_models": sorted(set(all_unmapped)),
            "pricing_sources": {"openrouter": len(openrouter_pricing), "litellm": len(litellm_cost), "custom": len(custom_pricing)},
        }
        print(json.dumps(output, indent=2, default=str))
    else:
        print_table_report(results, grand_total, grand_cost, all_unmapped,
                          openrouter_n=len(openrouter_pricing), litellm_n=len(litellm_cost))
        if args.by_model:
            print_model_report(results)


if __name__ == "__main__":
    main()
