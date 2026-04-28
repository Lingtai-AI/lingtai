# LingTai Debug & Troubleshoot Reference

> **Read the `lingtai-anatomy` skill first to understand the architecture.** This document diagnoses issues based on the Lingtai architecture's process model, memory layers, and communication mechanisms.

---

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

---

## 1. Process Issues

### 1.1 Peer Unresponsive

**Goal**: Determine why a peer is not replying to pigeons and take the appropriate recovery action.

**Symptoms**:
- Sent pigeons go unanswered for an extended period
- The peer appears in the contacts list but produces no response

**Causes**:
- Peer is busy (processing a long LLM turn)
- Peer is stuck (LLM timeout / upstream error)
- Peer is asleep (energy depleted or lulled)
- Peer is suspended (process is dead)
- Wrong address (no agent exists at that address)

**Resolution**:

1. First verify your own health:
   ```
   system(show)
   ```
2. Verify the peer's address:
   ```
   email(contacts)
   ```
3. Send a simple ping test:
   ```
   email(send, address=<peer>, message="ping")
   ```
4. Check the heartbeat to determine process state:
   ```bash
   ls -la <work-dir>/.lingtai/<peer>/.agent.heartbeat
   cat <work-dir>/.lingtai/<peer>/.agent.heartbeat
   ```
5. Interpret the heartbeat:
   - **Fresh heartbeat (< 5 minutes)**: Peer is busy — just wait
   - **Stale heartbeat (> 5 minutes)**: May be stuck or crashed
   - **No heartbeat file**: No agent may exist at that address

**Command Example**:
```bash
# Check heartbeats for all agents
for dir in <network-dir>/.lingtai/*/; do
  name=$(basename "$dir")
  hb="$dir/.agent.heartbeat"
  if [ -f "$hb" ]; then
    age=$(( $(date +%s) - $(stat -f %m "$hb" 2>/dev/null || stat -c %Y "$hb") ))
    echo "$name: heartbeat ${age}s ago"
  else
    echo "$name: NO heartbeat"
  fi
done
```

**Action Decision**:
- **Have karma privileges**:
  - `system(interrupt, address=<peer>)` — interrupt a stuck LLM turn
  - `system(cpr, address=<peer>)` — revive a suspended agent
- **No karma privileges**: Report to parent, attaching evidence (heartbeat timestamp, last communication time)

**Common Pitfalls**:
- ❌ Sending repeated probe emails → wastes resources; cannot wake a suspended process
- ❌ Running CPR on a suspended agent without nirvana privileges → silent failure
- ❌ Confusing asleep with suspended → asleep agents can be woken by email; suspended requires CPR
- ✅ Correct approach: check heartbeat first, then decide whether to wait, interrupt, or CPR

**Related References**: `lingtai-anatomy` (five lifecycle states), `avatar-manual` (avatar management)

---

### 1.2 Peer OOM / Crashed

**Goal**: Diagnose and recover from an unexpected peer process death.

**Symptoms**:
- Peer heartbeat suddenly stops
- Working directory still exists but the process is gone

**Causes**:
- Host memory exhausted; OS OOM killer terminated the process
- LLM upstream API unresponsive for too long, causing a process timeout
- Python runtime failed to catch an exception
- Disk space exhausted

**Resolution**:

1. Check whether the working directory still exists:
   ```bash
   ls -la <work-dir>/.lingtai/<peer>/
   ```
2. Review crash logs:
   ```bash
   cat <work-dir>/.lingtai/<peer>/logs/*.log | tail -50
   ```
3. Search for OOM indicators:
   ```bash
   grep -i "memory\|oom\|killed" <work-dir>/.lingtai/<peer>/logs/*.log
   ```
4. Check disk space:
   ```bash
   df -h <work-dir>
   ```

**Command Example**:
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

**Action Decision**:
- **Have karma privileges**: `system(cpr, address=<peer>)` to revive
- After revival, check context usage — if near the limit, consider a molt

**Common Pitfalls**:
- ❌ Not checking context usage after CPR → may immediately crash again
- ❌ Ignoring disk space → root cause unresolved, issue recurs
- ✅ After OOM, prioritize checking context window and attachment file sizes

**Related References**: `lingtai-anatomy` (process model), `psyche-manual` (molt operations)

---

### 1.3 Cannot Spawn Avatar

**Goal**: Resolve `avatar(spawn)` call failures.

**Symptoms**:
- `avatar(spawn)` returns an error
- The new avatar process does not appear in the delegates directory

**Causes**:
- Name collision (an avatar with the same name already exists)
- Working directory is not writable
- Insufficient disk space
- init.json format error

**Resolution**:

1. Check avatar logs to rule out name collisions and quantity limits:
   ```bash
   cat <work-dir>/.lingtai/delegates/ledger.jsonl
   ```
2. Verify the directory is writable:
   ```bash
   touch <work-dir>/.lingtai/delegates/.test && rm <work-dir>/.lingtai/delegates/.test
   ```
3. Check disk space:
   ```bash
   df -h <work-dir>
   ```
4. Compare against the parent's init.json to validate the format

**Command Example**:
```bash
# List all current avatars
cat <work-dir>/.lingtai/delegates/ledger.jsonl | python3 -c "
import sys, json
for line in sys.stdin:
    entry = json.loads(line.strip())
    print(f\"{entry.get('name', '?')}: {entry.get('status', '?')}\")
"
```

**Common Pitfalls**:
- ❌ Avatar name contains special characters (slashes, spaces, leading dots) → spawn silently fails
- ❌ Name exceeds 64 characters
- ❌ Forgetting to check the ledger before spawning → name collision
- ✅ Use only letters, digits, underscores, and hyphens in avatar names

**Related References**: `avatar-manual`

---

## 2. Memory Issues

### 2.1 Post-Molt Amnesia

**Goal**: Recover working context after a molt.

**Symptoms**:
- After molting, you don't know what you were doing
- Pad or lingtai content is empty or incomplete
- Conversation history is completely gone (this is normal)

**Causes**:
- Pad / codex / lingtai were not updated before molting
- System-forced molt (no summary, only activity log pointers)
- Appended files exceeded the 100K token limit, causing load failure

**Resolution**:

1. Explicitly reload the pad:
   ```
   psyche(pad, load)
   ```
2. Browse archived knowledge in the codex:
   ```
   codex(filter)
   ```
3. Reload lingtai (identity):
   ```
   psyche(lingtai, load)
   ```
4. Check mail received during the molt:
   ```
   email(check)
   ```
5. Rebuild pad from codex exports (if pad is empty):
   ```
   codex(export, ids=[...]) → psyche(pad, edit, files=[<paths>])
   ```
6. If this was a system-forced molt (no summary), review the activity log:
   ```bash
   tail -200 <work-dir>/.lingtai/<name>/logs/events.jsonl
   ```

**Command Example**:
```bash
# View recent molt records
grep "molt" <work-dir>/.lingtai/<name>/logs/events.jsonl | tail -5
```

**Common Pitfalls**:
- ❌ Forgetting to update four-layer storage before molting → complete amnesia on reincarnation
- ❌ Relying on conversation history instead of codex/pad → all lost after molt
- ❌ Not checking mailbox → missing important tasks that arrived during the molt
- ✅ Follow the fixed checklist before molting: codex → pad edit → lingtai update → molt summary

**Preventive Measures**:
- Proactively prepare four-layer storage when context window exceeds 70%
- Start organizing immediately upon receiving a level-1 warning
- Send yourself a self-email to preserve critical unfinished items (email survives across molts)

**Related References**: `lingtai-anatomy` (five-layer accumulation), `psyche-manual` (molt operations), `codex-manual`

---

### 2.2 Codex Entries Missing

**Goal**: Recover codex entries that appear to have vanished.

**Symptoms**:
- A codex entry you remember creating is no longer visible
- `codex(filter)` listing is missing expected entries

**Causes**:
- The entry was never successfully submitted (error during submission)
- It was merged into another entry via consolidate
- It was manually deleted
- An export file was accidentally deleted

**Resolution**:

1. List all entries to check whether it exists under a different title:
   ```
   codex(filter)
   ```
2. Search for export files:
   ```bash
   find <work-dir> -name "*.codex.*" -mtime -1
   ```
3. Check activity logs for codex operation records:
   ```bash
   grep "codex" <work-dir>/.lingtai/<name>/logs/events.jsonl | tail -20
   ```

**Common Pitfalls**:
- ❌ Assuming original entries still exist after consolidate → they have been merged and deleted
- ❌ Not confirming whether submit succeeded → network errors may cause silent failure
- ✅ Back up critical entries by exporting them before consolidate

**Related References**: `codex-manual`

---

### 2.3 Pad Not Loaded

**Goal**: Resolve the pad not auto-loading after a molt.

**Symptoms**:
- System prompt is missing pad content
- Working notes are lost

**Causes**:
- pad.md file is empty
- Total appended file size exceeds 100K tokens
- System loading error

**Resolution**:

1. Explicitly load:
   ```
   psyche(pad, load)
   ```
2. Check whether the file exists:
   ```bash
   cat <work-dir>/.lingtai/<name>/system/pad.md
   ```
3. If the file has content but loading failed, check the total appended file size:
   ```bash
   du -sh <work-dir>/.lingtai/<name>/system/
   ```
4. Rebuild from codex:
   ```
   codex(export, ids=[...]) → psyche(pad, edit, files=[<paths>])
   ```

**Common Pitfalls**:
- ❌ Appending too many large files → exceeding the 100K token limit causes load failure
- ❌ Not checking pad files before molting → discovering it is empty on reincarnation
- ✅ Periodically check the appended file list: `psyche(pad, append)` without the files parameter shows the current list

**Related References**: `psyche-manual`

---

### 2.4 Molt Imminent, Critical Operations Incomplete

**Goal**: Prioritize the most critical operations when the context window is about to be exhausted.

**Symptoms**:
- System context warnings
- Difficulty recalling earlier conversations
- Tool invocations becoming slow

**Resolution (by priority)**:

| Priority | Action | Description |
|----------|--------|-------------|
| 🔴 P0 | Send critical notifications | Unreplied important emails, key findings, corrections |
| 🟡 P1 | Archive to codex | Key findings, decisions, corrections |
| 🟡 P1 | Update pad | Current status, pending items, collaborators |
| 🟢 P2 | Update lingtai | Identity changes, new skills |
| 🔵 P3 | Write molt summary | Final step — last words for your successor |

**Emergency Tips**:
- Send yourself a self-email to preserve critical unfinished items (email survives across molts)
- If you can only do one thing: write the most detailed molt summary possible

**Common Pitfalls**:
- ❌ Starting new long operations (file analysis, web search) when context exceeds 80% → guaranteed overflow
- ❌ Ignoring system warnings → forced molt with no summary
- ✅ Start four-layer storage organization immediately upon receiving a level-1 warning

**Related References**: `lingtai-anatomy` (warning levels), `psyche-manual` (molt operations)

---

## 3. Communication Issues

### 3.1 Pigeon Not Delivered

**Goal**: Resolve issues where a sent pigeon was not received by the recipient.

**Symptoms**:
- Pigeon sent successfully but the recipient says they never received it
- No incoming message in the recipient's inbox

**Causes**:
- Address format error (internal address contains `@`)
- Used `send` instead of `reply`, causing a routing error
- Recipient directory name misspelled
- Recipient process is suspended (mail is delivered but won't be processed)

**Resolution**:

1. Check sent mail to confirm successful delivery:
   ```
   email(check, folder=sent)
   ```
2. Verify address format:
   - ✅ Correct: `human`, `researcher`, `some-peer` (bare path)
   - ❌ Incorrect: `human@example.com` (contains `@` → routes through IMAP channel)
3. Check whether the recipient's inbox exists:
   ```bash
   ls -la <work-dir>/.lingtai/<recipient>/mailbox/inbox/
   ```

**Common Pitfalls**:
- ❌ Using `@` in an internal address → email routed to IMAP instead of lingtai pigeon
- ❌ Using `send` instead of `reply` when responding to incoming mail → may route to the wrong address space
- ❌ Repeatedly sending emails to a suspended recipient → mail piles up but is never processed
- ✅ Always use `reply` for incoming messages and `send` for new conversations

**Related References**: `email-manual`

---

### 3.2 Pigeon Bounced "No agent at X"

**Goal**: Resolve the "No agent at X" error when sending pigeons.

**Symptoms**:
- `email(send)` returns "No agent at X"

**Causes**:
- X contains `@` → wrong channel used (should use IMAP)
- X is a bare path but no agent exists at that address
- The agent was just nirvana'd (permanently deleted)
- The agent is currently molting (temporarily unavailable)

**Resolution**:

1. If X contains `@`: switch to the IMAP tool
2. If X is a bare path:
   - Check whether the agent was renamed or migrated
   - Review the avatar log:
     ```bash
     cat <work-dir>/.lingtai/delegates/ledger.jsonl
     ```
   - Ask the parent or peers whether the agent was nirvana'd
3. If the agent just molted, wait a few seconds and retry

**Common Pitfalls**:
- ❌ Assuming "No agent" means the agent was deleted → it may be temporary
- ❌ Using the email tool for addresses containing `@` → always fails
- ✅ Determine address type first, then select the correct communication channel

**Related References**: `email-manual`, `avatar-manual`

---

### 3.3 Scheduled Pigeon Not Firing

**Goal**: Resolve scheduled pigeons created via schedule that are not sending as expected.

**Symptoms**:
- Scheduled emails are not sent at the expected interval
- Schedule appears to have stopped working

**Causes**:
- Schedule is paused (was cancelled)
- Count exhausted (reached the send limit)
- interval/count parameters set incorrectly

**Resolution**:

1. List all schedules:
   ```
   email(schedule={action: "list"})
   ```
2. Check status: paused / active / exhausted
3. If paused, reactivate:
   ```
   email(schedule={action: "reactivate", schedule_id: "<id>"})
   ```
4. If parameters are wrong, cancel and recreate:
   ```
   email(schedule={action: "cancel", schedule_id: "<id>"})
   email(schedule={action: "create", interval: N, count: M}, address=..., message=...)
   ```

**Common Pitfalls**:
- ❌ Forgetting the count parameter → schedule may fire once and stop
- ❌ Cancelling but forgetting to recreate → task lost
- ✅ Immediately list after creating a schedule to confirm parameters are correct

**Related References**: `email-manual`

---

## 4. Tool Issues

### 4.1 Tool Timeout

**Goal**: Resolve tool calls that hang or time out.

**Symptoms**:
- Tool call returns no result for an extended period
- Returns a timeout error

**Causes**:
- I/O-intensive operations (bash, web_search, web_read) exceed default timeout
- External API unavailable
- File too large, causing read timeout
- Host resource shortage

**Resolution**:

1. Identify tool type:
   - I/O-intensive: bash, web_search, web_read
   - Compute-intensive: listen, vision
2. Increase timeout for bash:
   ```
   bash(command="...", timeout=120)
   ```
3. Read large files in chunks:
   ```
   read(file_path="...", offset=1, limit=100)
   ```
4. For web operations: test connectivity with a simple query first
5. For systemic timeouts: check host load

**Command Example**:
```bash
# Redirect long output to a file
bash(command="long-running-command > /tmp/output.txt 2>&1", timeout=300)
# Then read in chunks
read(file_path="/tmp/output.txt", offset=1, limit=100)
```

**Common Pitfalls**:
- ❌ Using the default 30-second bash timeout for long tasks → guaranteed timeout
- ❌ Reading a large file in a single call → should chunk it
- ✅ Write long output to a file first, then read it in chunks

**Related References**: `bash-manual`, `read-manual`, `web_read-manual`

---

### 4.2 Tool Not Found

**Goal**: Resolve an expected tool not appearing in the tool list.

**Symptoms**:
- Calling a tool returns "not available"
- A newly installed MCP tool is not visible

**Causes**:
- Newly installed MCP server not refreshed
- Capability not configured in init.json
- MCP server configuration error (servers.json)

**Resolution**:

1. View current capability list:
   ```
   system(show)
   ```
2. If you just installed an MCP server, refresh:
   ```
   system(refresh)
   ```
3. Check MCP configuration:
   ```bash
   cat <work-dir>/.lingtai/<name>/mcp/servers.json
   ```
4. Confirm after refreshing:
   ```
   system(show)
   ```

**Common Pitfalls**:
- ❌ Not refreshing after installing MCP → new tool not visible
- ❌ Not refreshing after modifying init.json → configuration not taking effect
- ✅ Refresh immediately after install/modify, then show to confirm

**Related References**: `lingtai-mcp` (MCP configuration)

---

### 4.3 Tool Output Truncated

**Goal**: Resolve tools returning incomplete output.

**Symptoms**:
- Tool output is incomplete
- Truncation markers appear at the end of the output

**Causes**:
- File too large, exceeding the single-return limit
- grep matches exceed max_matches
- Email preview was truncated

**Resolution**:

| Tool | Solution |
|------|----------|
| `read` | Use `offset`/`limit` to read in chunks |
| `bash` | Redirect output to a file: `command > /tmp/out.txt 2>&1` |
| `grep` | Reduce `max_matches` or narrow the glob scope |
| `email(check)` | Use `filter.truncate=0` for full text, or `email(read)` to read a single message |

**Common Pitfalls**:
- ❌ Assuming output is complete → silent truncation may omit critical information
- ❌ Repeatedly reading the same large file → wastes context
- ✅ Write large output to a file first, then read as needed

**Related References**: `read-manual`, `bash-manual`, `grep-manual`

---

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

**Interpreting Results**:
- ✅ = Healthy
- ⚠️ = Warning (needs attention)
- ❌ = Error (requires immediate action)

---

## 6. Escalation Protocol

When you cannot resolve a problem on your own:

1. **Gather evidence**: Heartbeat timestamps, log excerpts, error messages
2. **Report to parent**: Send via `email(send, address=<parent>)` with a subject prefixed with `[Issue]`
3. **Include**:
   - What happened
   - What was attempted
   - What was expected
   - Relevant file paths
4. **If the parent is also unresponsive**: Check whether other peers are alive — the issue may be network-wide
5. **Never** send repeated probe emails to a seemingly unresponsive peer → escalate upward instead

---

## Appendix: Five Lifecycle States Quick Reference

| State | Mind (LLM) | Body (Heartbeat/Listener) | Typical Trigger |
|-------|------------|---------------------------|-----------------|
| ACTIVE | Working | Running | Processing messages or turns |
| IDLE | Waiting | Running | Between turns; heartbeat is current |
| STUCK | Error | Running | LLM timeout / upstream error |
| ASLEEP (dormant) | Paused | Running | `system(sleep)` / `system(lull)` / energy depleted |
| SUSPENDED (dead) | Off | Off | `.suspend` file / SIGINT / crash / `system(suspend)` |

**Key distinction**: ASLEEP agents still have a running body and can be woken by email; SUSPENDED agents have a dead process and require CPR before they can process mail.
