---
name: daily-reflection
description: Daily self-examination across all agents — scan logs, find errors, summarize lessons learned, track progress. Use when you want to review today's work, find issues before they compound, or generate a daily improvement report.
version: 1.0.0
---

# Daily Reflection

> *"The unexamined agent is not worth running."*

This skill automates daily self-examination across all agents in a Lingtai project. It scans event logs and agent logs for errors, anomalies, and patterns, then produces a structured report with health summaries, lessons learned, and action items. Run it at the end of each day or the start of the next.

---

## When to Use

- End of a work day — review what happened, catch problems before they compound
- Start of a new day — understand the state of the network before planning work
- After a major incident — audit all agents for collateral damage
- Periodically — weekly health checks, trend analysis

---

## The Reflection Loop

```
1. Discover all agents
2. For each agent: read logs, parse events, check health
3. Aggregate findings across agents
4. Classify by severity
5. Extract lessons and patterns
6. Generate structured report
7. (Optional) File GitHub issues for critical problems
8. Write report to dated file
```

---

## Step 1: Discover All Agents

Find the project root's `.lingtai/` directory and enumerate all agent directories.

```bash
# Find the project .lingtai/ directory
PROJECT_LINGTAI="$(git -C "$(pwd)" rev-parse --show-toplevel 2>/dev/null)/.lingtai"

# If not in a git repo, walk up from cwd
if [ ! -d "$PROJECT_LINGTAI" ]; then
  dir="$(pwd)"
  while [ "$dir" != "/" ]; do
    if [ -d "$dir/.lingtai" ]; then
      PROJECT_LINGTAI="$dir/.lingtai"
      break
    fi
    dir="$(dirname "$dir")"
  done
fi

# List all agent directories (those containing .agent.json)
for d in "$PROJECT_LINGTAI"/*/; do
  if [ -f "$d/.agent.json" ]; then
    echo "$d"
  fi
done
```

**Skip these directories** — they are not agents:
- `.git/`
- `.library_shared/`
- `.tui-asset/`
- `human/` (pseudo-agent, no logs)

**Identify each agent by reading** `.agent.json`:
- `agent_name` — the agent's name
- `state` — current state: `active`, `idle`, `asleep`, `suspended`
- `capabilities` — what tools the agent has

---

## Step 2: Determine the Time Window

The reflection covers a specific day. Default to today (UTC).

```bash
# Today's date boundaries (UTC)
TODAY=$(date -u +%Y-%m-%d)
DAY_START_EPOCH=$(date -u -j -f "%Y-%m-%d %H:%M:%S" "$TODAY 00:00:00" +%s 2>/dev/null || date -u -d "$TODAY 00:00:00" +%s)
DAY_END_EPOCH=$((DAY_START_EPOCH + 86400))
```

For `events.jsonl`, filter by the `ts` field (Unix epoch float).
For `agent.log`, filter by the timestamp prefix (`YYYY-MM-DD HH:MM:SS` or `HH:MM:SS`).

---

## Step 3: Scan Agent Logs

For each agent, scan two log sources.

### 3a. Parse `events.jsonl`

Location: `<agent-dir>/logs/events.jsonl`

Each line is a JSON object with at minimum: `type`, `agent_name`, `ts`.

**Error event types to look for:**

| Event Type Pattern | Severity | What It Means |
|---|---|---|
| `error`, `*_error`, `*_failed` | HIGH | Something broke |
| `capability_skipped` | MEDIUM | A capability failed to load |
| `heartbeat_timeout`, `heartbeat_failed` | HIGH | Agent may be unresponsive |
| `mail_bounce`, `mail_error` | MEDIUM | Communication failure |
| `mcp_error`, `mcp_*_failed` | MEDIUM | MCP tool integration broken |
| `daemon_error`, `daemon_crash` | HIGH | Background process died |
| `molt_*` | LOW | Session reset (track frequency) |
| `api_error`, `llm_error`, `glm_error` | HIGH | LLM/API call failed |
| `tool_error`, `tool_call_failed` | MEDIUM | A tool invocation failed |
| `refresh_complete` with missing capabilities | LOW | Capabilities didn't all load |

```bash
# Extract error/warning events for today from one agent
AGENT_DIR="<agent-dir>"
EVENTS="$AGENT_DIR/logs/events.jsonl"

if [ -f "$EVENTS" ]; then
  # Filter to today's events and find errors
  cat "$EVENTS" | while IFS= read -r line; do
    ts=$(echo "$line" | jq -r '.ts // empty')
    if [ -n "$ts" ]; then
      ts_int=${ts%.*}
      if [ "$ts_int" -ge "$DAY_START_EPOCH" ] && [ "$ts_int" -lt "$DAY_END_EPOCH" ]; then
        type=$(echo "$line" | jq -r '.type // empty')
        case "$type" in
          *error*|*failed*|*crash*|*bounce*|*timeout*|*skipped*)
            echo "$line"
            ;;
        esac
      fi
    fi
  done
fi
```

**Better approach — use `jq` directly:**

```bash
jq -c "select(.ts >= $DAY_START_EPOCH and .ts < $DAY_END_EPOCH) | select(.type | test(\"error|failed|crash|bounce|timeout|skipped\"))" "$EVENTS"
```

### 3b. Parse `agent.log`

Location: `<agent-dir>/logs/agent.log`

Format varies but common patterns:

```
HH:MM:SS LEVEL component: [agent] message
YYYY-MM-DD HH:MM:SS,mmm LEVEL component message
```

**Grep patterns for problems:**

```bash
AGENT_LOG="$AGENT_DIR/logs/agent.log"

if [ -f "$AGENT_LOG" ]; then
  # Filter today's entries and find errors/warnings
  grep -E "^$TODAY|ERROR|CRITICAL|WARNING|Traceback|Exception|failed|timed out" "$AGENT_LOG" | \
    grep -i -E "error|warning|critical|exception|failed|timeout|refused|denied|crash|abort|fatal"
fi
```

**Key patterns to match (regex):**

```
ERROR                          — any error-level log
CRITICAL                       — critical failures
WARNING.*(?:skip|fail|timeout) — warnings with actionable content
Traceback                      — Python tracebacks
Exception                      — Exception messages
refused|denied|unauthorized    — auth/permission failures
timed? out                     — timeouts
ECONNREFUSED|ECONNRESET        — network failures
429|rate.limit                 — rate limiting
OOM|out of memory              — resource exhaustion
```

### 3c. Check `token_ledger.jsonl`

Location: `<agent-dir>/logs/token_ledger.jsonl`

Look for abnormal token usage — signs of runaway loops or inefficiency.

```bash
TOKEN_LOG="$AGENT_DIR/logs/token_ledger.jsonl"

if [ -f "$TOKEN_LOG" ]; then
  # Sum today's token usage
  jq -s "[.[] | select(.ts >= \"${TODAY}T00:00:00Z\" and .ts < \"${TOMORROW}T00:00:00Z\")] | {
    total_input: (map(.input) | add // 0),
    total_output: (map(.output) | add // 0),
    total_thinking: (map(.thinking) | add // 0),
    api_calls: length,
    models: (map(.model) | unique)
  }" "$TOKEN_LOG"
fi
```

**Red flags:**
- `api_calls` > 200 in a day for a single agent
- `total_input` > 5,000,000 tokens (possible context loop)
- `total_output` > 500,000 tokens (possible generation loop)
- High `input` with near-zero `cached` (cache misses — inefficient prompting)

### 3d. Check Agent Health Indicators

```bash
# Check heartbeat freshness
HEARTBEAT="$AGENT_DIR/.agent.heartbeat"
if [ -f "$HEARTBEAT" ]; then
  HEARTBEAT_EPOCH=$(cat "$HEARTBEAT")
  NOW_EPOCH=$(date +%s)
  STALE_SECONDS=$((NOW_EPOCH - ${HEARTBEAT_EPOCH%.*}))
  if [ "$STALE_SECONDS" -gt 7200 ]; then
    echo "WARNING: Heartbeat stale by ${STALE_SECONDS}s"
  fi
fi

# Check agent state
STATE=$(jq -r '.state' "$AGENT_DIR/.agent.json")

# Check stamina
STAMINA_LEFT=$(jq -r '.runtime.stamina_left // empty' "$AGENT_DIR/.status.json" 2>/dev/null)
STAMINA_MAX=$(jq -r '.runtime.stamina // empty' "$AGENT_DIR/.status.json" 2>/dev/null)
```

---

## Step 4: Classify Findings

Assign severity levels to each finding:

| Severity | Criteria | Action |
|---|---|---|
| **CRITICAL** | Agent crashed, daemon died, data loss risk, security issue | File GitHub issue, notify human immediately |
| **HIGH** | Repeated errors, API failures blocking work, heartbeat dead | File GitHub issue, include in action items |
| **MEDIUM** | Capability skipped, mail bounces, occasional timeouts | Note in report, monitor tomorrow |
| **LOW** | Warnings, configuration nits, high token usage | Note in report for awareness |
| **INFO** | Normal operations, successful completions, metrics | Summary statistics only |

**Escalation rules:**
- 3+ occurrences of the same MEDIUM error in a day → promote to HIGH
- Any CRITICAL finding → must have an action item
- Any HIGH finding that persists for 2+ days → promote to CRITICAL

---

## Step 5: Extract Lessons and Patterns

After scanning all agents, look for cross-cutting themes:

1. **Recurring errors** — same error across multiple agents = systemic issue
2. **Communication failures** — mail bounces between specific agent pairs
3. **Resource patterns** — which agents consumed the most tokens and why
4. **Capability gaps** — capabilities that repeatedly fail to load
5. **Timing patterns** — errors clustered at specific times (rate limits? API outages?)

Ask yourself:
- What went wrong that could have been prevented?
- What worked well that should be replicated?
- What new patterns or techniques did agents discover?
- Are there agents that should be reconfigured, merged, or retired?

---

## Step 6: Generate the Report

Write a Markdown report with this structure:

```markdown
# Daily Reflection — YYYY-MM-DD

**Generated by:** <your agent name>
**Time window:** YYYY-MM-DD 00:00 UTC — 23:59 UTC
**Agents scanned:** N

---

## Network Health Summary

| Agent | State | Heartbeat | Errors | Warnings | Tokens Used |
|-------|-------|-----------|--------|----------|-------------|
| agent-1 | active | fresh | 0 | 2 | 45,230 |
| agent-2 | asleep | stale (4h) | 3 | 1 | 120,500 |
| ... | ... | ... | ... | ... | ... |

**Overall health:** GOOD / DEGRADED / CRITICAL

---

## Errors Found

### CRITICAL
- **[agent-name]** Description of critical error
  - Source: `events.jsonl` line / `agent.log` timestamp
  - First seen: HH:MM UTC
  - Occurrences: N
  - Impact: What broke or is at risk

### HIGH
- ...

### MEDIUM
- ...

### LOW
- ...

---

## Lessons Learned

1. **Pattern to avoid:** [description]
   - What happened: ...
   - Root cause: ...
   - Prevention: ...

2. **Pattern to replicate:** [description]
   - What worked: ...
   - Why it worked: ...
   - Where to apply next: ...

---

## Discoveries and Improvements

- [Agent] discovered [technique/workaround/insight]
- [Agent] improved [process] by [method]
- New skill/tool/pattern emerged: [description]

---

## Action Items for Tomorrow

- [ ] [CRITICAL] Fix [issue] in [agent] — blocks [work]
- [ ] [HIGH] Investigate [pattern] across [agents]
- [ ] [MEDIUM] Reconfigure [agent] to [change]
- [ ] [LOW] Monitor [metric] for [agent]

---

## Token Usage Summary

| Agent | Input | Output | Thinking | Cached | API Calls | Model |
|-------|-------|--------|----------|--------|-----------|-------|
| ... | ... | ... | ... | ... | ... | ... |
| **Total** | **X** | **Y** | **Z** | **W** | **N** | — |
```

---

## Step 7: File GitHub Issues (Optional)

For CRITICAL and HIGH findings, optionally create GitHub issues:

```bash
# Only for CRITICAL or HIGH findings that need tracking
gh issue create \
  --title "[daily-reflection] CRITICAL: <short description>" \
  --body "$(cat <<'EOF'
## Daily Reflection Finding

**Date:** YYYY-MM-DD
**Agent:** <agent-name>
**Severity:** CRITICAL

### Description
<what happened>

### Evidence
<log excerpts, event data>

### Impact
<what is broken or at risk>

### Suggested Fix
<what should be done>

---
*Auto-generated by daily-reflection skill*
EOF
)" \
  --label "daily-reflection,bug"
```

**Rules for filing issues:**
- Only file for CRITICAL or HIGH severity findings
- Check for existing open issues with the same label first: `gh issue list --label daily-reflection --state open`
- Don't file duplicate issues — add a comment to existing ones instead
- Include enough context that someone can act on the issue without reading the full report

---

## Step 8: Write the Report

Save the report to a dated file in the agent's working directory:

```bash
# Write to the agent's directory
REPORT_DIR="$AGENT_DIR/reflections"
mkdir -p "$REPORT_DIR"
REPORT_FILE="$REPORT_DIR/reflection-$TODAY.md"

# Write the report
cat > "$REPORT_FILE" <<'REPORT'
<generated report content>
REPORT

echo "Report written to: $REPORT_FILE"
```

Also update the agent's `pad.md` with a summary:

```markdown
## Latest Reflection (YYYY-MM-DD)

- **Health:** GOOD/DEGRADED/CRITICAL
- **Errors:** N critical, N high, N medium
- **Key lesson:** <one-liner>
- **Top action:** <most important thing to do next>
- Full report: reflections/reflection-YYYY-MM-DD.md
```

---

## Complete Procedure (Copy-Paste Checklist)

When invoking this skill, follow these steps in order:

1. **Set date range.** Determine today's date. Compute epoch boundaries for the day.

2. **Find all agents.** Walk the project's `.lingtai/` directory. For each subdirectory containing `.agent.json`, record the agent name, state, and capabilities.

3. **For each agent, collect data:**
   - Read `.agent.json` for state, capabilities, stamina
   - Read `.status.json` for runtime stats and token counts
   - Read `.agent.heartbeat` for liveness
   - Scan `logs/events.jsonl` for today's error/warning events using `jq` or line-by-line parsing
   - Scan `logs/agent.log` for today's ERROR/WARNING/CRITICAL lines using `grep`
   - Scan `logs/token_ledger.jsonl` for today's token usage
   - Check `mailbox/inbox/` and `mailbox/outbox/` for bounced or stuck messages

4. **Aggregate and classify.** Group findings by severity. Count error types. Identify cross-agent patterns.

5. **Write the report.** Use the template from Step 6. Fill in all sections. Be specific — include timestamps, counts, and log excerpts.

6. **File issues if needed.** For CRITICAL/HIGH findings, check for existing issues, then create or comment.

7. **Save the report.** Write to `reflections/reflection-YYYY-MM-DD.md`. Update `pad.md` with the summary.

---

## Anti-Patterns

- **Skimming logs** — Don't just grep for "error". Read context around errors to understand causation.
- **False positives** — A `capability_skipped` for an unused capability is noise, not a finding. Filter by what the agent actually needs.
- **Ignoring idle agents** — An agent that's been `asleep` for 3 days with a stale heartbeat might be stuck, not resting.
- **Reporting without action items** — Every finding above LOW should have a concrete next step.
- **Filing too many issues** — Only file GitHub issues for problems that need tracking beyond tomorrow. Most findings belong in the report only.
- **Ignoring successes** — The "Discoveries and Improvements" section is not optional. Agents learn. Capture what they learned.

---

## Worked Example

**Scenario:** You run daily-reflection at the end of May 10, 2026.

1. You find 38 agents under `.lingtai/`. 12 are active, 8 asleep, 18 idle/suspended.

2. Scanning `mimo-1/logs/events.jsonl`, you find:
   ```json
   {"type": "mcp_error", "ts": 1778571600.5, "agent_name": "mimo-1", "error": "Feishu config not found"}
   ```
   This is MEDIUM — Feishu MCP failed to load but the agent can function without it.

3. Scanning `telegram-patcher/logs/agent.log`, you find:
   ```
   2026-05-10 14:23:11,456 ERROR telegram: Message too long (4200 chars), truncated
   2026-05-10 14:23:11,457 ERROR telegram: Message too long (5100 chars), truncated
   2026-05-10 15:01:33,890 ERROR telegram: Message too long (4800 chars), truncated
   ```
   3 occurrences of the same error → promote from MEDIUM to HIGH. The agent repeatedly generates messages exceeding Telegram's 4096-char limit.

4. Scanning `xhelio-maintainer/logs/token_ledger.jsonl`, you find 340,000 input tokens with 0 cached tokens — pure cache misses. This agent's prompting strategy is inefficient. MEDIUM.

5. You notice `researcher-past-selves` has a heartbeat from 2 days ago but state is `active`. HIGH — the agent may be stuck.

6. You write the report to `reflections/reflection-2026-05-10.md` and update `pad.md`.

7. You file a GitHub issue for the stuck `researcher-past-selves` agent since it may need manual intervention (CPR or restart).

---

## Customization

Agents can customize this skill by adjusting thresholds:

- **Heartbeat staleness threshold:** Default 7200s (2h). Increase for agents with long sleep cycles.
- **Token usage alert threshold:** Default 5M input tokens/day. Adjust per agent workload.
- **API call alert threshold:** Default 200 calls/day. Adjust for high-throughput agents.
- **Issue filing:** Can be disabled entirely by skipping Step 7.
- **Report location:** Default `reflections/`. Can be changed to any directory.
- **Pad update:** Can be disabled if the agent manages its pad differently.
