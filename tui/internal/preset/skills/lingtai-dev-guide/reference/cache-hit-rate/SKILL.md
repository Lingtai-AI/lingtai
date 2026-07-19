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
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Cache Hit Rate

Nested `lingtai-dev-guide` reference. Read this after the top-level router sends
you here when you need to know **how well prompt caching has been working
recently** for one or more LingTai agents, grounded in the token ledger rather
than guessed.

This pairs with `reference/runtime-self-check/SKILL.md` §6: when a cache/affinity
fix "should be live," the token ledger is the observable that proves it. This
reference is the *measurement*; runtime-self-check is the
*did-the-object-rebuild* diagnosis.

## Core principle

A **read-only metric**. It only reads append-only `logs/token_ledger.jsonl`
files; it never writes, rotates, or mutates runtime state. Report rates without
pasting private absolute paths into human-facing deliverables — the ledger holds
no secrets, but its parent paths can be private, so generalize to
`~/.lingtai-tui/...` or `<project>/.lingtai/<agent>/`.

## Data source: the token ledger

Single source of truth: `logs/token_ledger.jsonl`, one JSON object per LLM call,
written after every call by `lingtai/kernel/token_ledger.py`
(`append_token_entry`). Required fields:

| Field | Meaning |
|---|---|
| `ts` | Call time, UTC, `%Y-%m-%dT%H:%M:%SZ` (always `Z`/UTC). |
| `input` | **Total** prompt/input tokens for the call. Already **includes** the cached portion. For the Anthropic/Claude adapters this is `raw_input + cache_read + cache_write`. |
| `output` | Output tokens. |
| `thinking` | Reasoning/thinking tokens. |
| `cached` | Cache-**read** input tokens served from the provider prompt cache. A **subset of `input`**. |
| `model`, `endpoint` | Attribution (which model / base_url produced the tokens). |

Optional tags on some entries: `source` (`main`, `soul`, `tc_wake`, `daemon`),
and for daemon-attributed rows `em_id` / `run_id` / `api_call_id` / `codex_*`.

The kernel normalizes every provider's usage into these same fields before
writing, so the metric is **provider-agnostic** (verified across `gpt-5.5`,
`mimo-v2.5-pro`, `deepseek-v4-pro`, and the Anthropic adapters). Key invariant,
confirmed in the adapters (`lingtai/llm/anthropic/adapter.py`,
`lingtai/llm/claude_code/adapter.py`) and empirically over a full ledger:
`0 <= cached <= input`, so the hit rate is always in `[0, 1]`.

## Formula

For a window `[now - W, now]`, over all entries whose `ts` lies in it:

```
hit_rate(W) = sum(cached) / sum(input)
```

- **Denominator** `sum(input)` already includes cached tokens, making this a
  token-weighted rate (a 100k-token call counts more than a 1k-token call) —
  which is what "how much of the prompt volume came from cache" means.
- **Windows** are a rolling lookback from *now* in UTC; defaults `1h`, `5h`,
  `1d`, `3d`. An entry is in the window iff `now - W <= ts <= now`. Ledger
  timestamps are UTC and "now" is computed in UTC — no local timezone involved.
- **Zero denominator** (no calls, or only zero-input rows) reports `n/a`, never a
  divide-by-zero.
- **Missing/garbage rows** — not valid JSON, no parseable `ts`, or non-numeric
  `input`/`cached` — are skipped and counted under `skipped`, so silent data loss
  is visible.

**Caveat on `cached`.** Native streaming/non-streaming adapters set
`cached = cache_read` only; cache *writes* are billed into `input` but not
counted as cached. CLI-backed daemon runs
(`lingtai/tools/daemon/run_dir.py`) instead document `cached` as
`cache_read + cache_creation`, because the CLI backend only exposes an aggregate.
So CLI-backed entries lean slightly optimistic (first-write tokens read as
"cached"). The `cached <= input` bound still holds — just don't over-interpret
sub-percent differences on daemon CLI traffic.

## The double-count hazard (important)

Daemon LLM calls are written to **two** ledgers: the daemon's own
`daemons/<run>/logs/token_ledger.jsonl` **and** the parent agent's
`logs/token_ledger.jsonl` (tagged `source="daemon"` + `em_id`/`run_id`). Naively
globbing `**/logs/token_ledger.jsonl` under a project **double-counts** every
daemon token.

The rule this reference and the script follow:

- **Agent workdir** — read only `<workdir>/logs/token_ledger.jsonl`; it already
  contains the daemon-tagged rows.
- **Project root** — read only each direct child
  `<root>/<agent>/logs/token_ledger.jsonl`; never recurse into `daemons/`.

## Script: `scripts/cache_hit_rate.py`

Deterministic, read-only, **standard library only**. Accepts an agent workdir, a
project root, or a single ledger file.

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
one source tag), `--now ISO` (override the clock for deterministic runs),
`--json`, `--help`.

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
window (e.g. one active session in the last hour).

Exit codes: `0` success (including empty windows); `1` no ledger found under the
path; `2` bad argument (missing path, bad `--now`, bad `--windows`).

### One-liner without the script

For a single window against one explicit ledger path:

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

It takes one explicit path, so it cannot trip the double-count hazard — but it
does not report `skipped` rows. Prefer the script for anything you will report.

## Troubleshooting

- **`no token_ledger.jsonl found`** — the path is neither an agent workdir
  (`logs/token_ledger.jsonl`) nor a project root with child agents. Point at the
  agent dir (e.g. `.lingtai/codex`), the `.lingtai/` root, or the ledger file.
- **All windows `n/a`** — no input tokens in those windows: idle agent, or
  `--now` predates the activity. Widen the window, drop `--source`, or check `ts`
  ranges with `head -1` / `tail -1`.
- **Rate looks too high on daemon traffic** — the CLI `cache_creation` caveat
  above.
- **`skipped` is non-zero** — corrupt/rotated lines or pre-schema rows. A handful
  is normal (e.g. a partially written final line); a large fraction suggests
  schema drift.
- **Schema drift** — if a future kernel renames `input`/`cached`/`ts`, this
  reference and the script must be updated in the same spirit as the "anatomy
  travels with code" rule. Re-read `lingtai/kernel/token_ledger.py` and the
  active provider adapter to re-confirm field semantics before trusting numbers.
- **Project-root totals equal one agent's** — expected when only one agent has
  recent activity; idle colleagues contribute zero.

## Validating after a change

Sanity-check the script against a known answer with a fixture and a pinned clock:

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
  as proof a cache/affinity fix took effect after `refresh`.
- `reference/debug-troubleshoot/SKILL.md` — broader runtime diagnostics when a
  low/zero hit rate points at a misbehaving session rather than a metric.
- `reference/architecture/SKILL.md` — where runtime state (including
  `.lingtai/<agent>/logs/`) lives.
