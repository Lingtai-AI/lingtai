---
name: dev-guide-cache-hit-rate
description: >
  Nested lingtai-dev-guide reference for computing the recent prompt-cache hit
  rate from LingTai token ledgers over rolling windows (default 1h / 5h / 1d /
  3d). Explains the provider-agnostic input/cached fields in
  logs/token_ledger.jsonl, the exact formula (sum(cached)/sum(input) per
  window), timestamp/timezone handling, the daemon double-count hazard, and
  ships a read-only stdlib script (scripts/cache_hit_rate.py) that aggregates an
  agent workdir, a project root, or a single ledger file. Use when asked how
  effective prompt caching has been recently, or to diagnose a cache-hit-rate
  drop after a refresh/affinity/cache-key change.
version: 1.0.1
last_changed_at: "2026-07-11T14:20:00-07:00"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Cache Hit Rate

Nested `lingtai-dev-guide` reference. Read this after the top-level router sends
you here when you need to know **how well prompt caching has been working
recently** for one or more LingTai agents — and want a number you can trust,
grounded in the token ledger rather than guessed.

This pairs with `reference/runtime-self-check/SKILL.md` §6: when a cache/affinity
fix "should be live," the token ledger is the observable that proves it. This
reference is the *measurement*; runtime-self-check is the *did-the-object-rebuild*
diagnosis.

## Core principle

This is a **read-only metric**. It only reads append-only
`logs/token_ledger.jsonl` files; it never writes, rotates, or mutates runtime
state. Report rates; do not paste private absolute paths or secrets into
human-facing deliverables (the ledger itself contains no secrets, but its parent
paths can be private — generalize to `~/.lingtai-tui/...` or
`<project>/.lingtai/<agent>/`).

## Data source: the token ledger

Single source of truth: `logs/token_ledger.jsonl`, one JSON object per LLM call,
written after every call by `lingtai/kernel/token_ledger.py`
(`append_token_entry`). Every entry carries these required fields:

| Field | Meaning |
|---|---|
| `ts` | Call time, UTC, `%Y-%m-%dT%H:%M:%SZ` (always `Z`/UTC). |
| `input` | **Total** prompt/input tokens for the call. Already **includes** the cached portion. For the Anthropic/Claude adapters this is `raw_input + cache_read + cache_write`. |
| `output` | Output tokens. |
| `thinking` | Reasoning/thinking tokens. |
| `cached` | Cache-**read** input tokens served from the provider prompt cache. A **subset of `input`**. |
| `model`, `endpoint` | Attribution (which model / base_url produced the tokens). |

Optional tags appear on some entries: `source` (`main`, `soul`, `tc_wake`,
`daemon`), and for daemon-attributed rows `em_id` / `run_id` / `api_call_id` /
`codex_*`.

The kernel normalizes every provider's usage into these same fields before
writing, so the metric below is **provider-agnostic** (verified across
`gpt-5.5`, `mimo-v2.5-pro`, `deepseek-v4-pro`, and the Anthropic adapters).

Key invariant, confirmed in the adapters (`lingtai/llm/anthropic/adapter.py`,
`lingtai/llm/claude_code/adapter.py`) and empirically over a full ledger:
`0 <= cached <= input`. So the hit rate is always in `[0, 1]`.

## Formula

For a time window `[now - W, now]`, over all entries whose `ts` lies in the
window:

```
hit_rate(W) = sum(cached) / sum(input)
```

- **Denominator:** `sum(input)` — total input tokens, which already includes
  cached tokens. This is a token-weighted rate (a 100k-token call counts more
  than a 1k-token call), which is what you want for "how much of the prompt
  volume came from cache."
- **Numerator:** `sum(cached)` — cache-read tokens.
- **Windows:** rolling lookback from *now* (UTC). Defaults: `1h`, `5h`, `1d`,
  `3d`. An entry is in the window iff `now - W <= ts <= now`.
- **Timezone:** ledger timestamps are UTC; "now" is computed in UTC. No local
  timezone is involved.
- **Zero denominator:** if a window has no input tokens (no calls, or only
  zero-input rows), `hit_rate` is reported as `n/a`, never a divide-by-zero.
- **Missing/garbage rows:** lines that are not valid JSON, lack a parseable
  `ts`, or lack numeric `input`/`cached` are skipped and counted under
  `skipped` so silent data loss is visible.

### Caveat: what `cached` includes

The native streaming/non-streaming adapters set `cached = cache_read` only
(cache *writes* are billed into `input` but are **not** counted as cached). One
path differs: CLI-backed daemon runs (`lingtai/tools/daemon/run_dir.py`) document
`cached` as `cache_read + cache_creation` because the CLI backend only exposes an
aggregate. So for CLI-backed entries the rate leans slightly optimistic (it
treats first-write tokens as "cached"). The `cached <= input` bound still holds,
so the number is still meaningful; just don't over-interpret sub-percent
differences on daemon CLI traffic.

## The double-count hazard (important)

Daemon LLM calls are written to **two** ledgers: the daemon's own
`daemons/<run>/logs/token_ledger.jsonl` **and** the parent agent's
`logs/token_ledger.jsonl` (tagged with `source="daemon"` + `em_id`/`run_id`).
If you naively glob `**/logs/token_ledger.jsonl` under a project you will
**double-count** every daemon token.

The rule this reference (and the script) follow:

- For an **agent workdir**, read only `<workdir>/logs/token_ledger.jsonl`. It
  already contains the daemon-tagged rows.
- For a **project root**, read only each direct child
  `<root>/<agent>/logs/token_ledger.jsonl`; never recurse into `daemons/`.

## Script: `scripts/cache_hit_rate.py`

Deterministic, read-only, **standard library only** (no third-party deps).
Accepts an agent workdir, a project root, or a single ledger file.

```bash
SCRIPT=~/.lingtai-tui/utilities/lingtai-dev-guide/reference/cache-hit-rate/scripts/cache_hit_rate.py
PY="$HOME/.lingtai-tui/runtime/venv/bin/python"   # or any python3.11+

# Current agent workdir (run from e.g. <project>/.lingtai/codex)
"$PY" "$SCRIPT" .

# A specific agent workdir
"$PY" "$SCRIPT" <project>/.lingtai/codex

# A whole project root: each agent's ledger, aggregated (daemons not double-counted)
"$PY" "$SCRIPT" <project>/.lingtai

# A single ledger file, custom windows, JSON output
"$PY" "$SCRIPT" logs/token_ledger.jsonl --windows 1h 6h 1d --json

# Only main-chat turns; pin the clock for a reproducible result
"$PY" "$SCRIPT" . --source main --now 2026-06-22T01:00:00Z
```

Flags: `--windows` (`<int><s|m|h|d|w>`, e.g. `90m 1d 1w`), `--source` (filter to
one source tag), `--now ISO` (override the clock for deterministic/testing
runs), `--json`, `--help`.

Example text output:

```
 window    calls           input          cached  hit_rate
----------------------------------------------------------
     1h       43       3,933,316       1,627,648     41.4%
     5h       43       3,933,316       1,627,648     41.4%
     1d       43       3,933,316       1,627,648     41.4%
     3d       43       3,933,316       1,627,648     41.4%
```

Equal rows across windows just mean all recent activity fell inside the smallest
window (e.g. a single active session in the last hour).

Exit codes: `0` success (including empty windows); `1` no ledger found under the
path; `2` bad argument (missing path, bad `--now`, bad `--windows`).

### One-liner without the script

If you only need a single window and don't want to invoke the script:

```bash
PY="$HOME/.lingtai-tui/runtime/venv/bin/python"
"$PY" - logs/token_ledger.jsonl 5 <<'PY'
import json, sys
from datetime import datetime, timedelta, timezone
path, hours = sys.argv[1], float(sys.argv[2])
cut = datetime.now(timezone.utc) - timedelta(hours=hours)
inp = cac = 0
for line in open(path):
    line = line.strip()
    if not line: continue
    try: d = json.loads(line)
    except ValueError: continue
    ts = d.get("ts","")
    try: t = datetime.fromisoformat(ts.replace("Z","+00:00"))
    except ValueError: continue
    if t >= cut:
        inp += d.get("input",0); cac += d.get("cached",0)
print(f"{cac}/{inp} = {100*cac/inp:.1f}%" if inp else "n/a (no input in window)")
PY
```

## Troubleshooting

- **`no token_ledger.jsonl found`** — you pointed at a directory that is neither
  an agent workdir (`logs/token_ledger.jsonl`) nor a project root with child
  agents. Point at the agent dir (e.g. `.lingtai/codex`) or the `.lingtai/` root,
  or pass the ledger file path directly.
- **All windows show `n/a`** — no input tokens in those windows. Either the agent
  has been idle, or your `--now` predates the activity. Widen the window, drop
  `--source`, or check `ts` ranges with `head -1` / `tail -1` on the ledger.
- **Rate looks too high on daemon traffic** — see the CLI `cache_creation`
  caveat above; CLI-backed `cached` can include first-write tokens.
- **`skipped` count is non-zero** — corrupt/rotated lines or pre-schema rows.
  A handful is normal (e.g. a partially written final line). A large fraction
  suggests schema drift — confirm the ledger still matches
  `lingtai/kernel/token_ledger.py` before trusting the numbers.
- **Schema drift** — if a future kernel renames `input`/`cached`/`ts`, this
  reference and the script must be updated in the same spirit as the
  "anatomy travels with code" rule. Re-read `lingtai/kernel/token_ledger.py`
  and the active provider adapter to re-confirm field semantics.
- **Project-root totals equal one agent's** — expected when only one agent has
  recent activity; idle colleagues contribute zero to recent windows.

## Validating after a change

To sanity-check the script against a known answer, build a tiny fixture and pin
`--now`:

```bash
T=$(mktemp -d); mkdir -p "$T/a/logs"
printf '%s\n' \
 '{"source":"main","ts":"2026-06-22T00:50:00Z","input":1000,"output":1,"thinking":0,"cached":800}' \
 '{"source":"main","ts":"2026-06-21T22:00:00Z","input":1000,"output":1,"thinking":0,"cached":500}' \
 > "$T/a/logs/token_ledger.jsonl"
"$PY" "$SCRIPT" "$T/a" --now 2026-06-22T01:00:00Z   # 1h -> 80.0%, 5h -> 65.0%
rm -rf "$T"
```

## Related references

- `reference/runtime-self-check/SKILL.md` — §6 live-object lifecycle: the ledger
  as proof a cache/affinity fix actually took effect after `refresh`.
- `reference/debug-troubleshoot/SKILL.md` — broader runtime diagnostics when a
  low/zero hit rate points at a misbehaving session rather than a metric.
- `reference/architecture/SKILL.md` — where runtime state (including
  `.lingtai/<agent>/logs/`) lives.
