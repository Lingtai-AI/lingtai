---
name: dev-guide-debug-troubleshoot
description: >
  Nested lingtai-dev-guide reference for diagnosing LingTai failures: agent process state, OOM/crashes, avatar spawn issues, post-molt memory loss, mail delivery, scheduled messages, tool timeouts, and escalation.
version: 1.0.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# LingTai Debug & Troubleshoot Reference

Nested lingtai-dev-guide reference. Read this after the top-level router sends you here.

> **Read the `lingtai-kernel-anatomy` skill first.** It owns the concepts — lifecycle states, memory layers, mail protocol, tool surface — that every section below assumes. This document is only the diagnosis-and-recovery procedure; the per-section concept map is at the end.

## Quick Diagnosis Decision Tree

```
Problem?
├── Process issues?
│   ├── Peer unresponsive → §1.1
│   ├── Peer OOM / crashed → §1.2
│   └── Cannot spawn avatar → §1.3
├── Memory issues?
│   ├── Post-molt amnesia → §2.1
│   ├── Codex entries missing → §2.2
│   ├── Pad not loaded → §2.3
│   └── Molt imminent, critical operations incomplete → §2.4
├── Communication issues?
│   ├── Pigeon not delivered → §3.1
│   ├── Pigeon bounced "No agent at X" → §3.2
│   └── Scheduled pigeon not firing → §3.3
└── Tool issues?
    ├── Tool timeout → §4.1
    ├── Tool not found → §4.2
    └── Tool output truncated → §4.3
```

## 1. Process Issues

### 1.1 Peer Unresponsive

Pigeons go unanswered; the peer is in contacts but silent. Possible states: busy (long LLM turn), stuck (LLM timeout/upstream error), asleep (energy depleted or lulled), suspended (process dead), or wrong address.

Diagnose in order — `system(show)` (your own health) → `email(contacts)` (verify the address) → `email(send, address=<peer>, message="ping")` → the heartbeat:

```bash
ls -la <work-dir>/.lingtai/<peer>/.agent.heartbeat
cat <work-dir>/.lingtai/<peer>/.agent.heartbeat
```

Fresh (< 5 min) = busy, just wait. Stale (> 5 min) = stuck or crashed. No file = probably no agent at that address. For a whole network at once, use the sweep in §5.

**Action.** With karma: `system(interrupt, address=<peer>)` for a stuck LLM turn, `system(cpr, address=<peer>)` to revive a suspended agent. Without karma: report to parent with evidence (heartbeat timestamp, last contact time).

**Pitfalls.** Repeated probe emails waste resources and cannot wake a suspended process; CPR without nirvana privileges fails silently. Asleep ≠ suspended — asleep agents wake on email, suspended ones need CPR first. Check the heartbeat before choosing to wait, interrupt, or CPR.

### 1.2 Peer OOM / Crashed

Heartbeat stops abruptly while the working directory survives. Usual causes: host memory exhausted (OS OOM killer), LLM upstream unresponsive past the process timeout, an uncaught Python exception, or a full disk.

```bash
# Comprehensive health check for an agent
peer_dir="<work-dir>/.lingtai/<peer>"
echo "=== Process ==="
ls -la "$peer_dir/.agent.heartbeat" 2>/dev/null || echo "No heartbeat"
echo "=== Disk ==="
df -h "$peer_dir" | tail -1
echo "=== Recent logs ==="
tail -30 "$peer_dir/logs/"*.log 2>/dev/null || echo "No logs"
echo "=== OOM scan ==="
grep -il "oom\|killed\|memory" "$peer_dir/logs/"*.log 2>/dev/null || echo "No OOM indicators"
```

**Action.** With karma, `system(cpr, address=<peer>)` revives it — then check context usage immediately, because reviving into a near-full context crashes again. After an OOM, prioritize the context window and attachment file sizes; ignoring disk space leaves the root cause live and the issue recurs.

### 1.3 Cannot Spawn Avatar

`avatar(spawn)` errors, or the new process never appears under delegates. Causes: name collision, unwritable working directory, insufficient disk, or a malformed init.json.

```bash
# List all current avatars (name collisions, quantity limits)
cat <work-dir>/.lingtai/delegates/ledger.jsonl | python3 -c "
import sys, json
for line in sys.stdin:
    entry = json.loads(line.strip())
    print(f\"{entry.get('name', '?')}: {entry.get('status', '?')}\")
"

# Directory writable?
touch <work-dir>/.lingtai/delegates/.test && rm <work-dir>/.lingtai/delegates/.test

# Disk space
df -h <work-dir>
```

If those pass, compare the avatar's init.json against the parent's to validate the format.

**Pitfalls.** Special characters in avatar names (slashes, spaces, leading dots) make spawn fail silently, as do names over 64 characters. Check the ledger first to avoid a collision; use only letters, digits, underscores, and hyphens.

## 2. Memory Issues

### 2.1 Post-Molt Amnesia

After a molt you don't know what you were doing, and pad/lingtai are empty or partial. (A vanished conversation history is normal — that is what molt does.) Causes: durable layers not updated before molting, a system-forced molt that left no summary, or appended files exceeding the 100K token limit and failing to load.

Recover in this order:

```
psyche(pad, load)                                    # 1. reload working notes
codex(filter)                                        # 2. browse archived knowledge
psyche(lingtai, load)                                # 3. reload identity
email(check)                                         # 4. mail that arrived during the molt
codex(export, ids=[...]) → psyche(pad, edit, files=[<paths>])   # 5. rebuild an empty pad
```

If the molt was system-forced (no summary), the activity log is the only trail:

```bash
tail -200 <work-dir>/.lingtai/<name>/logs/events.jsonl
grep "molt" <work-dir>/.lingtai/<name>/logs/events.jsonl | tail -5
```

**Prevention.** Follow the fixed pre-molt checklist — codex → pad edit → lingtai update → molt summary. Start preparing past 70% context and act immediately on a level-1 warning. Never treat conversation history as storage; it is all lost. Self-email survives molts, so mail yourself anything critical and unfinished.

### 2.2 Codex Entries Missing

An entry you remember creating no longer appears in `codex(filter)`. Causes: submission failed silently, `consolidate` merged it into another entry (originals are deleted), manual deletion, or a lost export file.

```bash
find <work-dir> -name "*.codex.*" -mtime -1                                  # export files
grep "codex" <work-dir>/.lingtai/<name>/logs/events.jsonl | tail -20         # operation records
```

Also re-run `codex(filter)` and look for the content under a different title.

**Pitfalls.** Originals do not survive `consolidate` — export critical entries first. Confirm every submit succeeded; a network error can fail silently.

### 2.3 Pad Not Loaded

The system prompt has no pad content and working notes are gone. Causes: `pad.md` is empty, total appended file size exceeds the 100K token limit, or a loading error.

```
psyche(pad, load)
```

```bash
cat <work-dir>/.lingtai/<name>/system/pad.md      # does it have content?
du -sh <work-dir>/.lingtai/<name>/system/          # appended size, if it does
```

If the pad is genuinely empty, rebuild it: `codex(export, ids=[...]) → psyche(pad, edit, files=[<paths>])`.

**Pitfalls.** Appending too many large files breaks loading at the 100K limit. Review the appended list periodically — `psyche(pad, append)` with no `files` parameter prints it — and check pad files *before* molting, not after.

### 2.4 Molt Imminent, Critical Operations Incomplete

Context warnings, difficulty recalling earlier conversation, sluggish tool calls. Spend what remains in priority order:

| Priority | Action | Description |
|----------|--------|-------------|
| 🔴 P0 | Send critical notifications | Unreplied important emails, key findings, corrections |
| 🟡 P1 | Archive to codex | Key findings, decisions, corrections |
| 🟡 P1 | Update pad | Current status, pending items, collaborators |
| 🟢 P2 | Update lingtai | Identity changes, new skills |
| 🔵 P3 | Write molt summary | Final step — last words for your successor |

Self-email survives molt, so use it for critical unfinished items. If you can only do one thing, write the most detailed molt summary possible.

**Pitfalls.** Do not start long operations (file analysis, web search) past 80% context — overflow is guaranteed. Ignoring system warnings ends in a forced molt with no summary.

## 3. Communication Issues

### 3.1 Pigeon Not Delivered

Sent successfully, but the recipient never got it. Causes: address format error (an internal address containing `@`), `send` used where `reply` was needed, a misspelled recipient directory, or a suspended recipient (mail is delivered but never processed).

```
email(check, folder=sent)     # confirm it actually went out
```

```bash
ls -la <work-dir>/.lingtai/<recipient>/mailbox/inbox/    # does their inbox exist?
```

Address format: `human`, `researcher`, `some-peer` (bare path) are correct; `human@example.com` is **not** an internal address — the `@` routes it through the IMAP channel instead of the LingTai pigeon.

**Pitfalls.** Use `reply` for incoming messages and `send` only for new conversations; `send` on a reply can route into the wrong address space. Repeatedly mailing a suspended recipient just piles up unprocessed mail.

### 3.2 Pigeon Bounced "No agent at X"

`email(send)` returns "No agent at X". Either X contains `@` (wrong channel — switch to the IMAP tool), or X is a bare path with no agent behind it: renamed/migrated, nirvana'd (permanently deleted), or mid-molt and briefly unavailable.

```bash
cat <work-dir>/.lingtai/delegates/ledger.jsonl    # renamed, migrated, or gone?
```

Ask the parent or peers whether the agent was nirvana'd. If it was just molting, wait a few seconds and retry.

**Pitfalls.** "No agent" is not proof of deletion — it is often temporary. Determine the address type first, then pick the channel.

### 3.3 Scheduled Pigeon Not Firing

A schedule stops sending at the expected interval. Causes: paused/cancelled, count exhausted, or interval/count set wrong.

```
email(schedule={action: "list"})                                  # status: paused / active / exhausted
email(schedule={action: "reactivate", schedule_id: "<id>"})       # if paused
email(schedule={action: "cancel", schedule_id: "<id>"})           # if parameters are wrong:
email(schedule={action: "create", interval: N, count: M}, address=..., message=...)
```

**Pitfalls.** Omitting `count` can make a schedule fire once and stop; cancelling without recreating loses the task. List immediately after creating to confirm the parameters.

## 4. Tool Issues

### 4.1 Tool Timeout

A tool call hangs or times out. Causes: I/O-intensive work (`bash`, `web_search`) exceeding the default timeout, an unavailable external API, an oversized file, or host resource shortage.

```
bash(command="...", timeout=120)                       # raise the bash timeout
read(file_path="...", offset=1, limit=100)             # chunk large reads

# Best pattern for long output: redirect to a file, then read in chunks
bash(command="long-running-command > /tmp/output.txt 2>&1", timeout=300)
read(file_path="/tmp/output.txt", offset=1, limit=100)
```

For web operations, test connectivity with a simple query first; for systemic timeouts, check host load. `vision` is compute-intensive while `bash`/`web_search` are I/O-intensive — they fail differently.

**Pitfalls.** The default 30-second bash timeout guarantees failure on long tasks; single-call reads of large files guarantee truncation.

See also `web_search(action="manual")` / the `web-search-manual` skill.

### 4.2 Tool Not Found

A tool returns "not available", or a newly installed MCP tool is invisible. Causes: the MCP server was not refreshed, the capability is missing from init.json, or `servers.json` is malformed.

```
system(show)        # 1. current capability list
system(refresh)     # 2. after installing an MCP server or editing init.json
system(show)        # 3. confirm
```

```bash
cat <work-dir>/.lingtai/<name>/mcp/servers.json
```

**Pitfalls.** Nothing takes effect without a refresh — after install *and* after any init.json change. See `mcp-manual` for MCP configuration (kernel `mcp` capability).

### 4.3 Tool Output Truncated

Output is incomplete or ends in a truncation marker — the file exceeded the single-return limit, grep hit `max_matches`, or an email preview was clipped.

| Tool | Solution |
|------|----------|
| `read` | Use `offset`/`limit` to read in chunks |
| `bash` | Redirect output to a file: `command > /tmp/out.txt 2>&1` |
| `grep` | Reduce `max_matches` or narrow the glob scope |
| `email(check)` | Use `filter.truncate=0` for full text, or `email(read)` for a single message |

**Pitfalls.** Never assume output is complete — silent truncation drops exactly the detail you needed. Re-reading the same large file wastes context; write it out once, then read what you need.

## 5. Health Checks

### One-Click Network Diagnosis

```bash
# Check heartbeats for all agents
for dir in <network-dir>/.lingtai/*/; do
  name=$(basename "$dir")
  hb="$dir/.agent.heartbeat"
  if [ -f "$hb" ]; then
    age=$(( $(date +%s) - $(stat -f %m "$hb" 2>/dev/null || stat -c %Y "$hb") ))
    if [ "$age" -lt 300 ]; then
      echo "✅ $name: alive (${age}s ago)"
    else
      echo "⚠️  $name: stale heartbeat (${age}s ago)"
    fi
  else
    echo "❌ $name: no heartbeat"
  fi
done

# Check disk space
df -h <network-dir>

# Check inbox sizes
for dir in <network-dir>/.lingtai/*/mailbox/inbox/; do
  name=$(echo "$dir" | sed 's|.*\.lingtai/\(.*\)/mailbox/.*|\1|')
  count=$(ls "$dir" 2>/dev/null | wc -l)
  if [ "$count" -gt 50 ]; then
    echo "⚠️  $name: inbox has $count messages (possible overflow)"
  fi
done
```

✅ = healthy · ⚠️ = warning, needs attention · ❌ = error, requires immediate action.

## 6. Escalation Protocol

When you cannot resolve a problem on your own:

1. **Gather evidence**: heartbeat timestamps, log excerpts, error messages.
2. **Report to parent**: `email(send, address=<parent>)` with a subject prefixed `[Issue]`.
3. **Include**: what happened, what was attempted, what was expected, and the relevant file paths.
4. **If the parent is also unresponsive**: check whether other peers are alive — the issue may be network-wide.
5. **Never** send repeated probe emails to a seemingly unresponsive peer; escalate upward instead.

## Concept references

Each section's underlying concepts live in `lingtai-kernel-anatomy`:

| Section | Anatomy topic |
|---|---|
| §1.1 | five lifecycle states; avatar management |
| §1.2 | process model; molt operations |
| §1.3 | avatar / network topology |
| §2.1 | five-layer accumulation; molt operations; codex |
| §2.2 | codex / memory system |
| §2.3 | psyche / molt protocol |
| §2.4 | warning levels; molt operations |
| §3.1, §3.3 | mail protocol |
| §3.2 | mail protocol; network topology |
| §4.1, §4.3 | bash / read / grep tools |
| §4.2 | see `mcp-manual` (kernel `mcp` capability) |

## Appendix: Five Lifecycle States Quick Reference

| State | Mind (LLM) | Body (Heartbeat/Listener) | Typical Trigger |
|-------|------------|---------------------------|-----------------|
| ACTIVE | Working | Running | Processing messages or turns |
| IDLE | Waiting | Running | Between turns; heartbeat is current |
| STUCK | Error | Running | LLM timeout / upstream error |
| ASLEEP (dormant) | Paused | Running | `system(sleep)` / `system(lull)` / energy depleted |
| SUSPENDED (dead) | Off | Off | `.suspend` file / SIGINT / crash / `system(suspend)` |

**Key distinction**: ASLEEP agents still have a running body and can be woken by email; SUSPENDED agents have a dead process and require CPR before they can process mail.
