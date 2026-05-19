# Cascade Skill Vendoring + Sentinel-Ordering Patch

**Date:** 2026-05-18
**Repos touched:** `lingtai-skill` (upstream protocol) — proposed change
**Hosts affected:** `claude-code-plugin`, `codex-plugin`, plus any new host (Cascade/Windsurf, OpenCode, etc.)

## Two findings, one document

1. **Cascade has no plugin/marketplace system**, so the lingtai mailbox skill needs a different vendoring story than the `claude plugin add` / `./install.sh` paths. A minimal, host-agnostic recipe is below.
2. **A latent race in the canonical SKILL.md** — the "touch sentinel" step is listed *after* the outbox write. Fast orchestrators can reply with a folder mtime *older* than the sentinel, so `find -newer <sentinel>` silently misses real replies. The fix is to reorder the steps. Confirmed by end-to-end simulation against a mock orchestrator.

Both are small. The second is the one that should go upstream.

---

## Finding 1 — Cascade vendoring recipe

There is no equivalent of `claude plugin add` for Windsurf/Cascade. Cascade exposes two seams that together cover what the Claude Code SessionStart hook does:

| Need | Cascade primitive |
|------|-------------------|
| File the agent can read on demand | Any path the agent has FS access to. By convention: `~/.codeium/windsurf/skills/<name>/SKILL.md` |
| Auto-activation on a topic | `create_memory` with the relevant keywords in the body. The memory is surfaced into context when its semantic neighborhood is hit. |
| Project-scoped trigger (analog of `.lingtai/` detection in `hooks.json`) | A line inside the memory describing the FS check ("if `.lingtai/human/mailbox/` exists, load this skill") — Cascade applies that test naturally before acting. |

Recipe:

```bash
# 1. Drop the canonical skill into Cascade's skill directory
mkdir -p ~/.codeium/windsurf/skills/lingtai
curl -fsSL https://raw.githubusercontent.com/Lingtai-AI/lingtai-skill/main/skills/lingtai/SKILL.md \
  -o ~/.codeium/windsurf/skills/lingtai/SKILL.md

# 2. (Inside a Cascade session) create a trigger memory pointing at that file,
#    with keywords like "灵台 / LingTai / orchestrator / 邮件 / inbox / send mail".
#    See ~/.codeium/windsurf/skills/lingtai/SKILL.md for the cascade-host changes
#    that should be applied on top of the canonical text (mainly: via=cascade,
#    .last_read_cascade pointer, tool-mapping table for run_command vs read_file).
```

The cascade-host SKILL.md should differ from canonical in three small ways:

- `"via": "cascade"` instead of host-of-the-day
- Read tracker named `.last_read_cascade`
- A tool-mapping section that says "prefer `read_file`/`list_dir`/`find_by_name`/`write_to_file` over `run_command`; set `SafeToAutoRun: true` on idempotent local shell snippets (UUID gen, `find -newer`, heartbeat read); leave `false` for agent process launches".

Everything else — the protocol, the message JSON shape, the lifecycle commands — stays byte-identical to upstream.

When the upstream protocol changes (new field, new signal), refresh with `curl` again and re-apply the three deltas. The deltas should eventually become host-specific overlays in the canonical repo so `sync-from-canonical.sh` works for Cascade too.

**Status of this work:** the cascade-host SKILL.md is live at `~/.codeium/windsurf/skills/lingtai/SKILL.md` on the development machine and has been validated end-to-end against a mock orchestrator (full delivery, reply, sent/<uuid> proof, find-newer reply correlation, `.last_read_cascade` filtering, agent discovery, liveness, `.prompt` / `.inquiry` / `.suspend` signals).

---

## Finding 2 — Sentinel must be touched BEFORE the outbox write

### Bug

Current upstream `SKILL.md` (lingtai-skill@main, position 5 of the chunked render) describes the sentinel-and-monitor pattern this way:

> ```bash
> # At send time, immediately after writing the outbox JSON:
> SENT_AT_FILE=.lingtai/human/.send-marker-<uuid>
> touch "$SENT_AT_FILE"
> ```

The vendored `claude-code-plugin/skills/lingtai/SKILL.md` v0.4.2 mirrors that ordering in the **Steps** list:

> 1. Generate UUID and timestamp
> 2. Create the message JSON
> 3. Write it to `.lingtai/human/mailbox/outbox/<uuid>/message.json`
> 4. Check the recipient's heartbeat
> 5. Set up delivery + reply monitoring  (← sentinel is touched here)

The "monitor pattern" section then shows `touch "$SENT_AT_FILE"` happening after the outbox write.

### Why it breaks

The poller in `FilesystemMailService` claims an outbox message in a single tight loop (default ~0.5 s in the kernel; tighter in some mock/test harnesses). In the worst case, between `write_to_file(outbox/<uuid>/message.json)` and the user-side `touch sentinel`:

1. The orchestrator polls and finds the message.
2. It atomically renames `outbox/<uuid>/` → `sent/<uuid>/`.
3. It writes a copy into its own inbox.
4. Its LLM (or, in tests, an echo branch) drafts a reply and writes it to `human/mailbox/inbox/<reply-uuid>/message.json`.
5. *Then* the user-side script gets around to running `touch sentinel`.

The reply's folder mtime is now older than the sentinel's mtime. `find -newer "$SENT_AT_FILE"` returns nothing. The skill concludes "no reply yet" and the user is told the orchestrator is silent. The reply is sitting right there in the inbox.

This was caught in end-to-end simulation, not theory:

```
sentinel mtime: 1779107711.813685  (Mon May 18 20:35:11 2026)
inbox file:    1779107708.443841  (Mon May 18 20:35:08 2026)
                                   ^^ reply landed 3 seconds BEFORE sentinel
find -newer  → 0 matches
```

Real LLM-driven orchestrators take seconds to respond, so this race is rare with a human-typed message. But it surfaces immediately on:

- Mock/test orchestrators
- Auto-reply rules (e.g. acknowledgements, "thinking…" status pings)
- Retry queues being drained
- Any orchestrator that pre-composes replies from cached context

### Fix

Move the `touch sentinel` step to *before* the outbox write. The sentinel's mtime is then a hard lower bound: any subsequent inbox folder mtime is necessarily newer.

Proposed canonical text (replacing the current **Steps** block):

```markdown
### Steps

**Order matters.** The sentinel MUST be touched *before* the outbox message is
written. Fast orchestrators (or any race during heavy I/O) can otherwise reply
with a folder mtime older than the sentinel, breaking `find -newer` correlation.

1. Generate UUID and timestamp in one call:
   ```bash
   python3 -c "import uuid; from datetime import datetime, timezone; \
       print(uuid.uuid4()); \
       print(datetime.now(timezone.utc).strftime('%Y-%m-%dT%H:%M:%SZ'))"
   ```

2. **First**, touch the sentinel:
   ```bash
   touch .lingtai/human/.send-marker-<uuid>
   ```

3. Create the message JSON (template below) and write it to
   `.lingtai/human/mailbox/outbox/<uuid>/message.json`. Create the UUID
   directory with `mkdir -p` first.

4. Check the recipient's heartbeat. If the agent is dead, queueing still
   succeeds but nothing will pick it up — inform the user and offer to CPR
   the orchestrator.

5. Watch for delivery and replies (next section).
```

The "Delivery and Reply Monitoring" subsection further down already shows the right `find -newer` invocation; it doesn't need to move, just the ordering note above the snippet should be edited from "At send time, immediately after writing the outbox JSON" to "Before writing the outbox JSON".

### Validation

After the reorder, the same scenario produces:

```
sentinel mtime: 1779107802.316214
inbox file:    1779107810.121701  newer=True
find -newer  → 1 match  (the reply, correctly identified)
```

Tested with three back-to-back rounds: smoke ping, echo-ordering, and a third round confirming `.last_read_cascade` filters out older replies and only surfaces the new one.

### Where to land

- **Upstream first:** `Lingtai-AI/lingtai-skill` — edit `skills/lingtai/SKILL.md`, bump the inline version, push.
- **Re-sync hosts:**
  - `claude-code-plugin/skills/lingtai/SKILL.md` via `scripts/sync-from-canonical.sh`
  - `codex-plugin/<wherever-the-skill-lives>` (same script if mirrored)
  - `~/.codeium/windsurf/skills/lingtai/SKILL.md` — already patched locally with the same reordering; refresh after upstream catches up so the three deltas (via, last_read suffix, tool-mapping) re-apply cleanly.

No kernel change is needed. The protocol's wire format and atomic-rename semantics are unaffected; only the user-side recipe shifts by one line.

---

## Out of scope (deferred)

- Adopting Cascade-host overlays into `sync-from-canonical.sh` so the three deltas don't have to be re-applied by hand on every refresh. Probably worth doing once a second non-Claude-Code host (OpenCode? Cline?) is in the picture.
- Bundling a Cascade-flavored `SessionStart` analog — currently the trigger memory is created interactively. A `windsurf-skill` repo with an install script (analogous to `codex-plugin/install.sh`) could do this in one command if user volume justifies it.

---

## Bonus findings from end-to-end validation against a real agent network

After the mock-driven validation, the same skill was exercised against the live network at `~/workspace/trading_strategy_lab/.lingtai/` (orchestrator: `shouyi`, model `gpt-5.5`, 7 agents alive). Two things surfaced that the skill can't fix on its own but should be aware of.

### Finding A — "Trust but verify" caught a stuck orchestrator the heartbeat hid

The skill ships with a "Common pitfalls" warning that begins:

> An agent can heartbeat freshly and report state `active` while its LLM calls are silently failing (context overflow, rate limit, provider 5xx). Heartbeat ≠ progress.

Within 90 seconds of sending the test ping, the situation was exactly that:

- `shouyi/.agent.heartbeat` updated every ~0.5 s, age always < 1 s — fully alive by handshake rules.
- `shouyi/.agent.json` carried `state: "stuck"` (not `active`), which most callers don't look at.
- `shouyi/logs/agent.log` showed a hard failure mode: `LLM API not responding after 80s`, repeated 503s, and a recurring 400 on tool-schema validation.

The skill's prescription — read the log instead of trusting the heartbeat — fired correctly. Confirmation: **the warning text in SKILL.md is load-bearing, not boilerplate.** Cascade users (and any host's user) hitting silent-stall scenarios will not be misled by `is_alive() == True` as long as they follow the skill.

No SKILL.md change needed for this — the warning already exists. Just noting that it earned its keep on first real-world use.

### Finding B — Stale sentinels accumulate; protocol should self-clean

Walking the live `.lingtai/human/` directory found four orphan sentinels left over from previous host sessions:

```
.send-marker-00c66dbf-86b6-400b-9196-87f4b9c18e07
.send-marker-3b0b63be-7667-4b78-98ed-fd88c4db5088
.send-marker-4942fb42-5edf-49ee-af87-0a38ba271bac
.send-marker-562719ac-9bbf-4247-9605-88f47c9bedf3
```

Per the current SKILL.md text:

> After presenting to the user, delete the sentinel file and stop polling.

But that step relies on the host actually completing the read-and-acknowledge cycle. If the host's session dies mid-monitor, the user navigates away, or the reply never arrives, the sentinel is never cleaned up. Over months, they pile up.

This isn't a correctness bug — `find -newer .send-marker-<my-uuid>` always uses the *current* uuid's sentinel, so old sentinels don't poison anyone's polling. But the directory grows monotonically, and a pile of orphans makes it harder to spot a real in-flight send during debugging.

Two possible fixes, neither blocking:

1. **Best-effort cleanup at activation time.** When the skill activates in a project, scan `.lingtai/human/.send-marker-*`, drop any whose age > N hours (24h is generous; the longest legitimate wait for a reply is "the orchestrator was sleeping and the user came back tomorrow", well under 24h in practice). Note in the skill that this is housekeeping, not protocol.
2. **Move sentinels into a host-suffixed subdir.** `.lingtai/human/.markers/<host>/<uuid>` so each host owns its own pile, and `find -newer` still works because the whole tree is local. Cleaner for forensics.

Suggestion (1) is a one-liner, fits naturally in the "When to activate" or "Common pitfalls" section. (2) is a slightly larger restructuring; defer until the first multi-host setup that actually trips on the noise.

### Finding C — `in_reply_to` field unreliable, but UUID may show up in body

In the multi-hop validation run (the "守一统六" / hub-spoke test), the orchestrator's final summary mail back to human had:

```json
{
  "from": "shouyi",
  "subject": "Re: [测试] 守一统六——分身回报任务（请六分身回报齐备后再回复我）",
  "in_reply_to": null,
  "message": "human：\n\n六回报已齐，按原邮件要求汇总如下（本封为对原邮件 8d370d61-7761-4233-b8ac-a61937f4847e 的正式回复）：\n..."
}
```

Two observations:

1. **`in_reply_to` was null even though the message is unambiguously a reply.** The skill already says "fall back to `from == orchestrator && subject startswith 'Re: '`", and that worked — but the moment the user has multiple in-flight sends with similar subjects, even that fallback gets ambiguous.

2. **The orchestrator's LLM did write the original UUID, just into the message body instead of the metadata field.** The body contains `(本封为对原邮件 8d370d61-...-a61937f4847e 的正式回复)` verbatim. This is a stronger anchor than `subject startswith Re:` when present, and worth checking as a tertiary fallback.

Suggested correlation precedence in SKILL.md (replacing the current binary "use in_reply_to, else use subject"):

```python
# Strongest → weakest
def is_reply_to(my_uuid: str, m: dict) -> bool:
    if m.get("in_reply_to") == my_uuid:
        return True                                 # tier 1: metadata
    if my_uuid in (m.get("message") or ""):
        return True                                 # tier 2: body mentions uuid
    if (m.get("from") == expected_from and
        (m.get("subject") or "").startswith("Re: ")):
        return True                                 # tier 3: subject heuristic
    return False
```

Tier 2 is cheap (one substring check) and survives orchestrator implementations that don't bother filling `in_reply_to`. Recommend adding it to upstream SKILL.md as the standard reply-correlation routine; cascade-host SKILL.md will inherit on next sync.

### Finding D — Out-of-scope kernel bug surfaced

For the historical record only — not actionable from the skill: shouyi's stuck state traces to a kernel-level tool-schema mismatch with OpenAI strict mode:

```
[守一] AED attempt 1/3: Error code: 400 - {'error': {'message':
"Invalid schema for function 'psyche': schema must have type 'object' and not
have 'oneOf'/'anyOf'/'allOf'/'enum'/'not' at the top level.",
'type': 'invalid_request_error'}}
```

The `psyche` capability's tool descriptor uses one of the disallowed JSON-schema constructs at the top level, which gpt-5.5 (and any other strict-mode endpoint) rejects. Belongs in `lingtai-kernel`, not here. Filed against this skill only because it was the user-visible failure mode the skill correctly diagnosed via `logs/agent.log`.

### Finding E — Frontmatter YAML quoting bug

**Symptom (Cascade Customizations → Skills):**

> `failed to parse skill frontmatter: yaml: line 2: mapping values are not allowed in this context`

The skill listed but greyed out, marked with a warning icon, never auto-activates.

**Root cause.** The `description` value in canonical SKILL.md ended with `Opt-in: only activate on explicit user intent.` — a plain (unquoted) YAML scalar containing the literal sequence `<colon><space>`. To a YAML 1.2 parser, `Opt-in: only` looks like the start of a nested mapping inside what was supposed to be the parent mapping's `description` value, hence "mapping values are not allowed in this context". Cascade's frontmatter parser is strict on this; many other YAML parsers happen to accept it because of how implicit-mapping detection is implemented.

The error is reported as line 2 (the parser flags the mapping context, which it entered on line 2 with `name: lingtai`); the actual offending content is on the `description:` line.

**Fix (applied to Cascade host, version bumped `0.4.2-cascade.1` → `0.4.2-cascade.2`):**

```diff
 ---
 name: lingtai
-description: Interact with LingTai agents through the shared human mailbox. ... Opt-in: only activate on explicit user intent.
+description: "Interact with LingTai agents through the shared human mailbox. ... Opt-in — only activate on explicit user intent."
 version: 0.4.2-cascade.2
 host: cascade
-upstream: https://github.com/Lingtai-AI/lingtai-skill
+upstream: "https://github.com/Lingtai-AI/lingtai-skill"
 ---
```

Three defensive moves, deliberately layered:

1. Wrap the whole `description` value in double quotes — this alone resolves the parse error, since quoted scalars don't trigger implicit-mapping detection.
2. Replace `Opt-in: ` with `Opt-in — ` — so even if a future edit accidentally drops the quotes, the plain scalar form is still legal.
3. Quote `upstream` similarly. The URL `https://...` doesn't trip the same bug (no space after the colon), but defending here is free and means the entire frontmatter parses identically across plain-scalar-tolerant and plain-scalar-strict parsers.

Verified post-fix:

```
$ python -c "import yaml, pathlib; print(yaml.safe_load(open('.../SKILL.md').read().split('---', 2)[1]))"
{'name': 'lingtai', 'description': 'Interact with ... Opt-in — only ...',
 'version': '0.4.2-cascade.2', 'host': 'cascade',
 'upstream': 'https://github.com/Lingtai-AI/lingtai-skill'}
```

**Upstream recommendation.** In canonical `Lingtai-AI/lingtai-skill` SKILL.md, do the same two changes (quote `description`, replace `: ` punctuation with `—` or `;`). Every downstream host that vendors this file inherits the bug otherwise. Each host then bumps its own per-host suffix (`-cascade.N`, `-cc.N`, …) on next sync.

**General principle for skill authors.** Treat frontmatter `description:` as if it were YAML strict-mode: always quote, or always avoid `<colon><space>` inside the value. The cost of either rule is zero; the cost of failing in the wild is a silently-disabled skill that the user only notices because of an unobtrusive grey icon.
