---
name: lingtai-trajectory-mining
description: >
  Mine LingTai event.jsonl / events.jsonl runtime traces with cheap
  models/daemons to extract operational pitfalls, repeated tool failures,
  latency patterns, context-pressure hazards, daemon lifecycle issues, and
  improvement candidates for LingTai itself. Use when the human asks to scan
  structured agent event logs for lessons or generate a low-cost improvement
  digest. Defaults to read-only evidence gathering and recommendation output;
  never creates issues, commits, PRs, config changes, schedules, or refreshes
  without explicit human authorization.
version: 0.3.0
tags: [reflection, trajectory, events, event-log, improvement, pitfalls, cheap-model, daemon, lingtai, observability]
---

# LingTai Event-Log Trajectory Mining

Purpose: turn LingTai's structured runtime event streams — especially `event.jsonl` / `events.jsonl` files — into actionable lessons for improving LingTai itself.

This is **not** a broad chat-history retrospective. Chat history, molt summaries, reports, and issues may provide context, but the primary signal is the event stream: tool calls/results, failures, durations, notification/wake behavior, context-pressure metadata, daemon lifecycle transitions, and other machine-readable traces.

---

## 1. When to Use / When Not to Use

**Use this skill when:**
- The human asks to mine, analyze, or audit LingTai event logs (`event.jsonl`, `events.jsonl`).
- The human says something like "最近轨迹", "look at my agent logs", "what went wrong last session", "scan for patterns", or "generate improvement candidates".
- You need to systematically extract operational pitfalls from large structured traces before writing a knowledge entry, skill, or issue draft.
- You want to build a cheap pre-pass before involving expensive models.

**Do not use this skill when:**
- The human just wants a quick summary of chat history without event-log grounding.
- The request is about code review, architecture analysis, or feature planning unrelated to runtime traces.
- You already have a specific, pre-identified bug and just need to fix it — skip the mining phase and go directly to debugging.
- The human wants to read mail or manage agents — use the relevant communication, avatar, or daemon manual instead.

---

## 2. Primary Inputs and Source Discovery

### 2.1 Discovery-First Principle

**Never assume fixed event field names.** LingTai event formats evolve. Always sample a few lines first to discover actual keys, then adapt your extraction.

### 2.2 Event-Log Source Discovery Patterns

Run these to discover candidate sources. Adjust the root to the actual `.lingtai/` tree for the current project.

```bash
# Find all event log files under the LingTai tree
find "$LINGTAI_ROOT" -type f \( -name "event.jsonl" -o -name "events.jsonl" -o -name "*.events.jsonl" \) \
  2>/dev/null | sort

# Daemon event logs specifically
find "$LINGTAI_ROOT/daemons" -type f -name "events.jsonl" 2>/dev/null | sort

# Check for spill/overflow sidecar files (names vary; probe common patterns)
find "$LINGTAI_ROOT" -type f \( -name "*spill*" -o -name "*overflow*" -o -name "*context_meta*" \) \
  2>/dev/null | sort

# MCP / notification channel event logs (names vary per deployment)
find "$LINGTAI_ROOT" -type f -name "*.jsonl" 2>/dev/null \
  | xargs grep -l '"type"' 2>/dev/null | sort

# Check sizes and mtimes for triage
find "$LINGTAI_ROOT" -type f -name "*.jsonl" -newer "$LINGTAI_ROOT/meta.json" \
  2>/dev/null -exec ls -lh {} \;
```

### 2.3 Key Source Families

| Family | Typical Path Pattern | Primary Signal |
|--------|---------------------|----------------|
| Agent event log | `<agent>/event.jsonl` or `<agent>/events.jsonl` | tool calls, tool results, errors, context pressure |
| Daemon event log | `daemons/em-*/events.jsonl` | task lifecycle, timeouts, exits |
| Notification/wake log | varies; grep for `notification` type events | mail wakeup latency, poll cadence |
| Spill / context metadata | varies; probe with find above | context size near limits |
| MCP channel log | varies | external service errors |

### 2.4 Schema Discovery (Do This First)

Before any analysis, sample keys from each source:

```bash
# Sample first 5 lines of a file to see actual keys
head -5 "$EVENT_FILE" | python3 -c "
import sys, json
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    try:
        obj = json.loads(line)
        print(sorted(obj.keys()))
    except Exception as e:
        print('NON-JSON:', repr(line[:80]))
"

# Get the union of all top-level keys in a file
python3 -c "
import sys, json, collections
keys = collections.Counter()
for line in open(sys.argv[1]):
    line = line.strip()
    if not line: continue
    try:
        obj = json.loads(line)
        if isinstance(obj, dict):
            keys.update(obj.keys())
    except: pass
for k, v in keys.most_common():
    print(v, k)
" "$EVENT_FILE"
```

Once you know the actual keys, extract timestamps, event types, tool names, statuses, and durations using those real field names.

---

## 3. Manifest Building

After discovery, build a manifest before any LLM review. The manifest is your contract for what you will and will not read.

```text
event_source_path | mtime | size_lines | time_range | top_event_types | why_included
```

Keep the manifest in memory (or a temp file) — do not persist private log paths to shared storage.

**Limits:**
- Default window: last 24 hours or current workstream. Never scan everything unless explicitly asked.
- Maximum lines to feed any single LLM call: 300 lines of redacted excerpts.
- If a file exceeds 5000 lines, use time-window or event-family slicing (see §5).

---

## 4. Mechanical First-Pass Metrics

Run cheap aggregations before any LLM call. These are free signal.

### 4.1 Event-Type Counts

```bash
python3 -c "
import sys, json, collections
counts = collections.Counter()
for line in open(sys.argv[1]):
    line = line.strip()
    if not line: continue
    try:
        obj = json.loads(line)
        # adapt field name after schema discovery
        etype = obj.get('type') or obj.get('event_type') or obj.get('kind') or 'unknown'
        counts[etype] += 1
    except: counts['__parse_error__'] += 1
for k, v in counts.most_common():
    print(v, k)
" "$EVENT_FILE"
```

### 4.2 Tool Call / Result Summary

```bash
python3 -c "
import sys, json, collections
tool_counts = collections.Counter()
error_counts = collections.Counter()
for line in open(sys.argv[1]):
    line = line.strip()
    if not line: continue
    try:
        obj = json.loads(line)
        etype = obj.get('type') or obj.get('event_type') or ''
        if 'tool' in etype.lower():
            tool = obj.get('tool') or obj.get('tool_name') or obj.get('name') or 'unknown'
            tool_counts[tool] += 1
            status = obj.get('status') or obj.get('exit_code') or obj.get('error')
            if status and str(status) not in ('0', 'ok', 'success', 'None', ''):
                error_counts[(tool, str(status)[:60])] += 1
    except: pass
print('--- tool call counts ---')
for k, v in tool_counts.most_common(20):
    print(v, k)
print('--- error clusters ---')
for (tool, status), v in error_counts.most_common(20):
    print(v, tool, status)
" "$EVENT_FILE"
```

### 4.3 Latency Measurements

After discovering the timestamp field name:

```bash
python3 -c "
import sys, json
ts_field = sys.argv[2]  # e.g. 'ts' or 'timestamp' or 'created_at'
events = []
for line in open(sys.argv[1]):
    line = line.strip()
    if not line: continue
    try:
        obj = json.loads(line)
        ts = obj.get(ts_field)
        etype = obj.get('type') or obj.get('event_type') or ''
        if ts:
            events.append((float(ts), etype, obj))
    except: pass
events.sort()
for i in range(1, len(events)):
    gap = events[i][0] - events[i-1][0]
    if gap > 10:  # seconds; adjust threshold
        print(f'GAP {gap:.1f}s between {events[i-1][1]} -> {events[i][1]} at {events[i-1][0]}')
" "$EVENT_FILE" "$TS_FIELD"
```

### 4.4 Context / Stamina Pressure

Look for events signaling high context usage or molt triggers:

```bash
grep -i -E '"(context|stamina|molt|pressure|overflow|spill|continuation)"' "$EVENT_FILE" \
  | head -50
```

### 4.5 Daemon Lifecycle

```bash
grep -i -E '"(start|finish|fail|timeout|cancel|reclaim|exit|dead)"' "$DAEMON_EVENTS_FILE" \
  | head -50
```

### 4.6 Spill / Tool-Result Overflow Detection

```bash
grep -i -E '"(spill|truncat|overflow|too_large|result_size|max_bytes)"' "$EVENT_FILE" \
  | head -30
```

### 4.7 Auth / Env Failures

```bash
grep -i -E '"(auth|token|credential|unauthorized|forbidden|env|missing.*key|wrong.*path)"' "$EVENT_FILE" \
  | python3 -c "
import sys
for line in sys.stdin:
    # Redact obvious secrets before printing
    import re
    line = re.sub(r'(token|key|secret|password|credential)[\":\s]+[^\",\s]{6,}',
                  r'\1: [REDACTED]', line, flags=re.IGNORECASE)
    print(line, end='')
" | head -30
```

---

## 5. Chunking Strategy for Large Event Logs

Never dump large private event logs into an LLM. Use one of these strategies:

### 5.1 Time-Window Slicing

Extract events from a specific time window:

```bash
python3 -c "
import sys, json
ts_field, start_ts, end_ts = sys.argv[2], float(sys.argv[3]), float(sys.argv[4])
for line in open(sys.argv[1]):
    line = line.strip()
    if not line: continue
    try:
        obj = json.loads(line)
        ts = obj.get(ts_field)
        if ts and start_ts <= float(ts) <= end_ts:
            print(line)
    except: pass
" "$EVENT_FILE" "$TS_FIELD" "$START_EPOCH" "$END_EPOCH"
```

### 5.2 Event-Family Slicing

Extract only events of a given type:

```bash
python3 -c "
import sys, json
target_types = set(sys.argv[2].split(','))
for line in open(sys.argv[1]):
    line = line.strip()
    if not line: continue
    try:
        obj = json.loads(line)
        etype = obj.get('type') or obj.get('event_type') or ''
        if etype in target_types:
            print(line)
    except: pass
" "$EVENT_FILE" "tool_result,error,timeout"
```

### 5.3 Anomaly-Window Excerpts

When a suspicious event is found at line N, extract ±30 lines for context:

```bash
awk "NR>=$((N-30)) && NR<=$((N+30))" "$EVENT_FILE"
```

### 5.4 Deduplication / Signature Hashing

Before sending repetitive errors to a cheap model, collapse duplicates:

```bash
python3 -c "
import sys, json, hashlib, collections
sigs = collections.Counter()
for line in open(sys.argv[1]):
    line = line.strip()
    if not line: continue
    try:
        obj = json.loads(line)
        # Build a signature from stable non-secret fields
        sig_parts = [
            obj.get('type') or obj.get('event_type') or '',
            obj.get('tool') or obj.get('tool_name') or '',
            str(obj.get('status') or obj.get('exit_code') or ''),
            (obj.get('error') or '')[:80],
        ]
        sig = hashlib.md5('|'.join(sig_parts).encode()).hexdigest()[:8]
        sigs[sig] += 1
    except: pass
for sig, count in sigs.most_common(30):
    print(count, sig)
"  "$EVENT_FILE"
```

---

## 6. Cheap Model / Daemon Strategy

### 6.1 Model Selection Priority

| Model / Preset | When to Use |
|----------------|-------------|
| DeepSeek Flash / DeepSeek-V3 cheap variant | Large-volume classification, error clustering, first-pass anomaly detection |
| MiniMax | Structured YAML extraction from moderate excerpts |
| Codex gpt5.3-like / tier:1 preset | Pattern matching over aggregated metrics |
| tier:2 preset | Moderate-complexity finding synthesis |
| Primary agent model (this session) | Shortlist triage, finding merging, confidence adjudication |
| Expensive model (Opus-class) | Only for ambiguous high-impact architecture/design findings |

**Default: never reach tier:3+ unless the human explicitly approves the budget.**

### 6.2 Daemon Task Structure

Spawn one daemon task per (source family × time window). Keep each task small:

- Input: redacted aggregate metrics + bounded excerpts (≤300 lines)
- Output: structured YAML only, using the finding schema in §8
- No side effects inside the daemon

Example daemon task description:

```text
Analyze these LingTai event-log excerpts (source: <family>, window: <time range>).
Extract durable runtime improvement candidates visible in the event data.
Focus on: tool failures, latency gaps, context pressure, daemon lifecycle, auth/env issues, observability gaps.
Do NOT quote secrets, tokens, or full message bodies. Redact paths if they contain usernames or private data.
Output ONLY a YAML list using this schema: [id, category, severity, confidence, event_evidence, pattern, impact, suggested_destination, suggested_next_step, side_effect_required].
Prefer 3-5 high-signal findings over a long list of weak ones.
```

### 6.3 Parallel Dispatch Strategy

When multiple source families or time windows exist, dispatch them in parallel:

```
daemon-1: agent event.jsonl — tool_call/tool_result family — last 24h
daemon-2: daemons/em-*/events.jsonl — lifecycle family — last 7d
daemon-3: notification/wake events — latency family — last 24h
daemon-4: context/spill events — pressure family — last 7d
```

Collect all results before primary-agent triage.

---

## 7. Prompt Templates for Cheap Daemons

### 7.1 Classifier Prompt

```
You are a runtime event log classifier for a multi-agent system called LingTai.
Below is a redacted aggregate summary of event-log metrics from a single source family and time window.
Classify the top patterns you see into the following categories:
  tool-failure, latency, context-pressure, daemon-lifecycle, auth-env, observability-gap, doc-gap, missing-skill, bug-candidate, process-improvement

For each category you identify, output one YAML block:
  category: <category>
  evidence_summary: <1-2 sentences citing event types, counts, or timing — no secrets>
  confidence: low | medium | high

METRICS:
{metrics_block}

Output ONLY valid YAML. No prose before or after.
```

### 7.2 Anomaly Summarizer Prompt

```
You are analyzing a bounded excerpt from a LingTai agent event log.
The excerpt is centered on a suspicious event. Surrounding lines are provided for context.
Your task: summarize the anomaly in terms of what failed, why it likely failed (based on event data only), and what the downstream impact was.

Rules:
- Do not quote tokens, credentials, or full message bodies.
- Reference events by their type, timestamp offset, and redacted field names.
- Output YAML only:
  anomaly_type: <one of: tool-failure | latency-spike | context-overflow | daemon-exit | auth-failure | unknown>
  timeline: <ordered list of key events in the excerpt>
  root_cause_hypothesis: <1 sentence, hedged>
  downstream_impact: <1 sentence>
  confidence: low | medium | high

EXCERPT (redacted):
{excerpt_block}
```

### 7.3 Observability-Gap Prompt

```
You are reviewing LingTai event-log summaries to identify what information is MISSING that would be needed to diagnose operational problems.
You have seen: {event_types_present}.
You did NOT see (or saw too rarely): {event_types_sparse}.

For each significant gap, output YAML:
  gap: <what is missing>
  why_needed: <what class of problem it would help diagnose>
  suggested_event: <what event type or field would close the gap>
  priority: low | medium | high

Output ONLY valid YAML. No prose.
```

### 7.4 Cross-Run Pattern Prompt

```
You are comparing event-log aggregate summaries from multiple LingTai sessions or agents.
Each summary is labeled with its source (agent name or daemon ID) and time window.
Identify patterns that repeat ACROSS multiple sources/sessions, not just within one.

For each cross-run pattern, output YAML:
  pattern_id: <short slug>
  description: <what repeats and where>
  sources_affected: [list of source labels]
  recurrence_count: <approximate>
  severity: low | medium | high
  confidence: low | medium | high

SUMMARIES:
{summaries_block}

Output ONLY valid YAML. No prose.
```

---

## 8. Finding Schema

Every finding, from any daemon or primary-agent review, must fit this schema:

```yaml
- id: short-stable-slug              # kebab-case, unique within the digest
  category: tool-failure | latency | context-pressure | daemon-lifecycle | auth-env | observability-gap | doc-gap | missing-skill | bug-candidate | process-improvement
  severity: low | medium | high
  confidence: low | medium | high
  event_evidence:
    - source: local path to event.jsonl/events.jsonl
      line_or_time: line number, Unix timestamp, ISO timestamp, or event id
      event_type: tool_call | tool_result | notification | daemon_state | context_pressure | other
      redacted: true | false      # whether the note below omits sensitive fields
      note: short redacted quote or paraphrase of the event content
  optional_context:                  # only after event_evidence is established
    - source: path, URL, or issue reference
      note: why this corroborates the event-log signal
  pattern: what repeated or what caused harm — describe in event terms
  impact: why it matters to LingTai, users, or agents
  suggested_destination: knowledge | skill | issue-draft | code-investigation | observability-improvement | no-action
  suggested_next_step: smallest concrete next action
  side_effect_required: none | human-approval-required
```

**Validation requirements before including a finding:**
- At least one `event_evidence` entry with a verifiable source path and line/time.
- `pattern` must describe something visible in event data, not inferred from chat history alone.
- Singleton events (happened once, low impact) → `severity: low`, or exclude entirely.
- `confidence: high` only if the same pattern appears in ≥3 distinct event occurrences or is corroborated by optional_context.

---

## 9. Privacy and Secret Rules

Apply these in order, before any LLM call:

1. **Redact tokens and credentials**: replace any value matching `(token|key|secret|password|credential|oauth)["\s:=]+[^\s",]{8,}` with `[REDACTED]`.
2. **Redact message bodies**: if an event field contains human-written message text, summarize rather than quote unless exact wording is necessary for the finding.
3. **Redact file paths containing usernames**: replace `/Users/<name>/` with `/Users/[USER]/`.
4. **Redact IP addresses and internal hostnames**: replace with `[HOST]`.
5. **Quote minimum evidence**: cite event type, timestamp/line range, and redacted field names. Do not dump entire event objects.
6. **No side effects without approval**: the output of this skill is a recommendation digest. Do not create files, issues, commits, PRs, scheduled jobs, or agent refreshes.

---

## 10. Validation Before Including a Finding

Before finalizing any finding:

1. **Re-read the source lines**: confirm the event path and line/time range are accurate.
2. **Reconcile timestamps**: if multiple events are involved, verify they form a plausible causal sequence.
3. **Check recurrence**: grep for similar events across the full time window; note count.
4. **Singleton rule**: a single occurrence of an error with no pattern context → downgrade to `severity: low` and `confidence: low` unless the single event had confirmed high impact (e.g., agent stopped functioning).
5. **Reject hallucinated fields**: if a daemon output references event fields that do not exist in the actual schema discovered in §2.2, discard or flag that finding.

### Confidence Rubric

| Evidence | Confidence |
|----------|-----------|
| ≥3 occurrences of the same event pattern, confirmed in source file | high |
| 2 occurrences OR 1 occurrence + corroborating optional_context | medium |
| 1 occurrence, no corroboration, no impact confirmed | low |
| Inferred from absence of events only | low |
| Daemon output references field not found in actual schema | reject |

---

## 11. Output Digest Template

Produce the digest in the agent's working language. Fields in brackets are placeholders.

```
# 轨迹挖掘摘要 / Trajectory Mining Digest
Generated: [ISO timestamp]
Sources scanned: [N files, total ~M events, time window]
Models used: [list of cheap models + primary agent]

---

## High-Signal Findings ([N])

[YAML block of top findings, severity: high or medium + high confidence]

---

## Quick Wins ([N])
Findings where suggested_destination is knowledge, skill, or observability-improvement
and side_effect_required is none.

[YAML block]

---

## Issue Candidates ([N])
Findings requiring human approval before action.

[YAML block with side_effect_required: human-approval-required]

---

## Observability Gaps ([N])
What was missing from the event logs that would help future diagnosis.

[YAML block, category: observability-gap]

---

## No-Action Observations ([N])
Low-confidence or low-impact findings, retained for reference.

[YAML block, severity: low or confidence: low]

---

## Evidence Appendix
[Table: finding_id | source_path | line_or_time | event_type | redacted_note]

---

## Recommended Next Steps
Choose one or more:
- [ ] Write/update skill: [skill name]
- [ ] Write knowledge entry: [topic]
- [ ] Draft issue for human review: [title]
- [ ] Code investigation: [component]
- [ ] Add observability: [event type / field]
- [ ] No action needed
```

---

## 12. Routing Next Actions

After producing the digest, route durable outputs as follows:

| Finding type | Destination | Action |
|---|---|---|
| Reusable operational pattern | `skill` | Propose skill update; wait for human approval |
| Private operational fact about this deployment | `knowledge` | Write knowledge entry (no secrets) |
| Active task / in-progress investigation | `pad` | Update pad with bounded note |
| LingTai bug or design issue | Issue draft | Use `lingtai-issue-report` skill if available; **ask human approval before filing** |
| Code change needed | Local worktree/patch | Propose; do not apply without approval |
| Configuration change | Propose in digest | **Do not apply without approval** |
| No clear action | `no-action` | Note in digest; move on |

---

## 13. Periodic Mode Guidance

If the human wants recurring event-log mining:

- **Do not set any scheduler without explicit approval.** Ask the human to confirm the cadence and scope first.
- Default cadence when approved: daily digest, not continuous monitoring.
- The scheduled job should only wake the agent with a bounded prompt; the agent performs the review.
- The digest should be silent (written to `pad.md` or a report file) unless `standing-rules.md` allows periodic check-in messages.

Suggested scheduled prompt body (for human approval before use):

```text
Run lingtai-trajectory-mining on recent event.jsonl/events.jsonl sources for the last 24h.
Produce a concise digest of high-signal runtime pitfalls and improvement candidates.
Do not create issues, commits, PRs, config changes, or scheduled jobs without explicit human approval.
Write the digest to: reports/trajectory-digest-YYYYMMDD.md
```

---

## 14. Concrete Example Findings

### Example A: Stale Claude Code OAuth Token

```yaml
- id: stale-claude-code-oauth-token
  category: auth-env
  severity: high
  confidence: high
  event_evidence:
    - source: .lingtai/daemons/em-<id>/events.jsonl
      line_or_time: "~line 847, ts 1716XXXXXX"
      event_type: tool_result
      redacted: true
      note: "claude CLI returned 'weekly limit reached'; subsequent tool_result showed success after env patch — CLAUDE_CODE_OAUTH_TOKEN was [REDACTED] in env"
  optional_context:
    - source: "GitHub: Lingtai-AI/lingtai#189"
      note: confirmed stale inherited env token failure mode; agents misdiagnosed quota exhaustion
  pattern: >
    Long-lived daemon inherits stale CLAUDE_CODE_OAUTH_TOKEN from parent env.
    After credential refresh, the env override prevents the new token from taking effect.
    Agents see 'weekly limit' errors and stop delegating heavy work.
  impact: Agents misdiagnose quota exhaustion; heavy work is not delegated; user sees silent failures.
  suggested_destination: code-investigation
  suggested_next_step: Strip CLAUDE_CODE_OAUTH_TOKEN in the claude-code daemon backend env; add smoke test that verifies auth after daemon start.
  side_effect_required: human-approval-required
```

### Example B: Telegram Post-Turn Responsiveness Delay

```yaml
- id: telegram-post-turn-poll-delay
  category: latency
  severity: high
  confidence: medium
  event_evidence:
    - source: .lingtai/<agent>/events.jsonl
      line_or_time: "ts window 1716XXXXXX – 1716XXXXXX+120"
      event_type: notification
      redacted: false
      note: "IDLE poll events fired 4 times before incoming Telegram message was picked up; gap ~90s post-turn"
  optional_context:
    - source: "GitHub: Lingtai-AI/lingtai-kernel#167"
      note: human-visible responsiveness regression report; corroborates 90s+ delay pattern
  pattern: >
    After a turn completes, the agent enters IDLE poll cadence.
    If the poll interval is long and a Telegram message arrives just after a poll,
    the next pick-up is delayed by a full poll interval.
    Observed gap: ~60–120s depending on context-overflow work running in parallel.
  impact: Human-visible silence in live chat; degrades real-time interaction quality.
  suggested_destination: issue-draft
  suggested_next_step: Investigate Telegram poll cadence separately from context-overflow work; consider event-driven wake for Telegram channel.
  side_effect_required: human-approval-required
```

### Example C: Tool-Result Spill / Context Pressure

```yaml
- id: tool-result-spill-context-pressure
  category: context-pressure
  severity: medium
  confidence: medium
  event_evidence:
    - source: .lingtai/<agent>/events.jsonl
      line_or_time: "lines ~1200–1250"
      event_type: tool_result
      redacted: true
      note: "tool_result event has result_size > threshold; spill metadata present; subsequent context_pressure event shows usage >85%"
  pattern: >
    Large tool results (file reads, bash output) push context usage past 85%.
    The spill event appears but the agent continues without triggering molt early enough,
    leading to a forced continuation mid-task.
  impact: Tasks are interrupted or produce incomplete output; user must re-prompt.
  suggested_destination: observability-improvement
  suggested_next_step: Verify that spill events are being routed to the molt trigger; check thresholds in context manager.
  side_effect_required: none
```

### Example D: Daemon Timeout on Long-Running Task

```yaml
- id: daemon-timeout-long-task
  category: daemon-lifecycle
  severity: medium
  confidence: high
  event_evidence:
    - source: .lingtai/daemons/em-<id>/events.jsonl
      line_or_time: "last 3 events, ts window 1716XXXXXX"
      event_type: daemon_state
      redacted: false
      note: "state transitions: running -> timeout -> cancelled; task ran 47m before timeout at 45m limit"
  pattern: >
    Daemon task exceeded the configured timeout (45m) by a small margin.
    The task was in a normal working state; the timeout was not due to a hang.
    No partial result was saved before cancellation.
  impact: Task result lost; agent must retry from scratch; no incremental output preserved.
  suggested_destination: knowledge
  suggested_next_step: Document that tasks expected to run >40m should checkpoint intermediate results; consider raising timeout or using subtask splitting.
  side_effect_required: none
```

---

## 15. Embedded Python Script for Event Summary Generation

Agents can copy and run this script locally to produce a JSON summary from one or more event log files. It is robust to unknown JSONL lines and redacts obvious secrets.

```python
#!/usr/bin/env python3
"""
lingtai_event_summary.py — generate a JSON aggregate summary from LingTai event logs.
Usage: python3 lingtai_event_summary.py <event.jsonl> [event2.jsonl ...]
Output: JSON to stdout. Safe to pipe to jq or save to a file.
Does not modify input files. Does not make network requests.
"""
import sys
import json
import re
import collections
import hashlib

SECRET_PATTERN = re.compile(
    r'(token|key|secret|password|credential|oauth|bearer)["\s:=]+[^\s",]{8,}',
    re.IGNORECASE
)
PATH_PATTERN = re.compile(r'/Users/[^/]+/')
IP_PATTERN = re.compile(r'\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b')

def redact(text: str) -> str:
    text = SECRET_PATTERN.sub(lambda m: m.group(0).split(m.group(0)[-1])[0] + '[REDACTED]', text)
    text = PATH_PATTERN.sub('/Users/[USER]/', text)
    text = IP_PATTERN.sub('[HOST]', text)
    return text

def sig(obj: dict) -> str:
    parts = [
        str(obj.get('type') or obj.get('event_type') or obj.get('kind') or ''),
        str(obj.get('tool') or obj.get('tool_name') or obj.get('name') or ''),
        str(obj.get('status') or obj.get('exit_code') or ''),
        redact(str(obj.get('error') or ''))[:60],
    ]
    return hashlib.md5('|'.join(parts).encode()).hexdigest()[:8]

def ts_field(obj: dict):
    for f in ('ts', 'timestamp', 'created_at', 'time', 't'):
        if f in obj:
            try:
                return float(obj[f])
            except (ValueError, TypeError):
                pass
    return None

def summarize(paths):
    result = {
        'sources': [],
        'total_events': 0,
        'parse_errors': 0,
        'event_type_counts': collections.Counter(),
        'tool_counts': collections.Counter(),
        'error_clusters': collections.Counter(),
        'sig_counts': collections.Counter(),
        'time_range': [None, None],
        'large_gaps': [],
        'schema_keys': collections.Counter(),
    }

    for path in paths:
        src = {'path': path, 'events': 0, 'errors': 0}
        prev_ts = None
        try:
            with open(path, 'r', errors='replace') as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        obj = json.loads(line)
                    except json.JSONDecodeError:
                        result['parse_errors'] += 1
                        src['errors'] += 1
                        continue
                    if not isinstance(obj, dict):
                        continue

                    result['total_events'] += 1
                    src['events'] += 1

                    # schema key discovery
                    for k in obj.keys():
                        result['schema_keys'][k] += 1

                    # event type
                    etype = obj.get('type') or obj.get('event_type') or obj.get('kind') or 'unknown'
                    result['event_type_counts'][etype] += 1

                    # tool info
                    tool = obj.get('tool') or obj.get('tool_name') or obj.get('name')
                    if tool:
                        result['tool_counts'][tool] += 1

                    # error clustering
                    err = obj.get('error') or (
                        str(obj.get('exit_code', '')) if obj.get('exit_code') not in (None, 0, '0', '') else None
                    )
                    if err:
                        cluster_key = redact(str(err)[:80])
                        result['error_clusters'][cluster_key] += 1

                    # dedup signature
                    result['sig_counts'][sig(obj)] += 1

                    # time range and gap detection
                    t = ts_field(obj)
                    if t is not None:
                        if result['time_range'][0] is None or t < result['time_range'][0]:
                            result['time_range'][0] = t
                        if result['time_range'][1] is None or t > result['time_range'][1]:
                            result['time_range'][1] = t
                        if prev_ts is not None:
                            gap = t - prev_ts
                            if gap > 30:
                                result['large_gaps'].append({
                                    'gap_seconds': round(gap, 1),
                                    'before_event_type': prev_etype,
                                    'after_event_type': etype,
                                    'at_ts': t,
                                })
                        prev_ts = t
                        prev_etype = etype

        except OSError as e:
            src['open_error'] = str(e)

        result['sources'].append(src)

    # Convert Counters to sorted lists for JSON serialization
    result['event_type_counts'] = result['event_type_counts'].most_common()
    result['tool_counts'] = result['tool_counts'].most_common(20)
    result['error_clusters'] = result['error_clusters'].most_common(20)
    result['sig_counts'] = result['sig_counts'].most_common(20)
    result['schema_keys'] = result['schema_keys'].most_common(30)
    result['large_gaps'] = sorted(result['large_gaps'], key=lambda x: -x['gap_seconds'])[:20]

    return result

if __name__ == '__main__':
    paths = sys.argv[1:]
    if not paths:
        print('Usage: python3 lingtai_event_summary.py <event.jsonl> [...]', file=sys.stderr)
        sys.exit(1)
    summary = summarize(paths)
    print(json.dumps(summary, indent=2, default=str))
```

Save this script to a temp path (e.g., `/tmp/lingtai_event_summary.py`) and run it against discovered event files. Feed the JSON output — not the raw event files — to cheap daemon prompts.

---

## 16. On-Demand Procedure (Step-by-Step)

1. **Clarify window and scope**
   - Default: recent event logs for the current agent/project plus daemon `events.jsonl` from the active workstream.
   - "最近轨迹" → last 24h or current active workstream.
   - Named subsystem (daemon, Telegram, Claude Code, context overflow) → filter events to that subsystem.

2. **Discover sources** (§2.2)
   Run discovery commands. Build manifest. Do not read beyond the manifest until step 3.

3. **Schema discovery** (§2.4)
   Sample keys from each source file before writing any extraction code.

4. **Mechanical first-pass** (§4)
   Run aggregation scripts. Capture output. Do not pass raw logs to any LLM yet.

5. **Chunk and redact** (§5, §9)
   Apply chunking strategy. Redact secrets and paths. Verify redaction before proceeding.

6. **Dispatch cheap daemon batch** (§6, §7)
   Send manifests + aggregates + bounded redacted excerpts to cheap models. One daemon per source family / time window. Collect results.

7. **Primary-agent triage**
   Merge daemon findings. Validate each against §10. Discard hallucinated fields. Adjust confidence per rubric.

8. **Produce digest** (§11)
   Render digest template. Include evidence appendix.

9. **Route outputs** (§12)
   Propose routing for each finding. Wait for human approval before any side effect.

10. **Stop**
    A good digest gives the human enough to choose: update skill, file issue, make patch, ignore, or schedule.

---

## 17. Good Stopping Point

Stop after producing the digest unless the human explicitly asks for an action. A good digest gives enough event evidence and next steps that the human can choose one of:

- "write/update the skill"
- "file the issue"
- "make the patch"
- "ignore for now"
- "schedule this daily/weekly"

Do not propose more than one of these as a default action. Present the options and wait.
