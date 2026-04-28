# Avatar Network Governance — Reference Card

> Extracted from `avatar-network-governance` skill (v1.0.0). Patterns for running a healthy avatar network: health checks, CPR, delegation, workspace organization, and batch reporting.

---

## Core Rules

1. **Deposit before email** — all avatar work goes to the shared workspace BEFORE sending reports
2. **One email per task** — batch everything, send once
3. **Master doc corrections** — fix the file first, then one summary email to the human
4. **Avatar health check** — scan all avatar directories every watchdog cycle
5. **Organize, don't do it yourself** — delegate to avatars; only do what avatars can't

---

## Health Monitoring

On every cycle, scan all avatar directories:

```bash
for dir in .lingtai/mvelli-*; do
  grep '"state"' "$dir/system/system.md"
done
```

States to watch for:
- `"state": "idle"` — healthy, working or waiting
- `"state": "suspended"` — dead, need CPR
- Non-responsive — try `cpr` on their address

System suspends idle avatars after ~5 minutes of inactivity. CPR revives them.

## CPR Protocol

```
system(action="cpr", address="/path/to/avatar-dir")
```

After CPR, send them a status message to wake them up.

## Watchdog Schedule

Set up a recurring email ping to keep the network alive:

```python
email(action="send", schedule={
  "action": "create",
  "address": "self",
  "interval": 600,  # 10 minutes
  "count": 99
})
```

## Avatar Role Documentation

Create an `avatar-roles.md` in the workspace for each avatar with:
- Name and purpose
- Current status (done/active/blocked)
- Output file(s)
- Next steps

Update this when roles change.

---

## Delegation Patterns

| Task | Who |
|------|-----|
| Biography / institutional history | biographer |
| Bibliography / arXiv scraping | papers |
| Physics deep-reading | deep-reader |
| Collaboration network analysis | network |
| Master synthesis | synthesizer |

**Principle**: Spawn new avatars for new domains. One task, one avatar. Better to spawn than to overload a single agent.

---

## Workspace Organization

```
.lingtai/workspace/
  master_complete.md       # master synthesis
  biography.md             # biographical research
  publications_full.md     # full bibliography
  deep_reading_notes.md    # paper analysis
  collaboration_network.md # co-author map
  papers/                  # PDF archive
```

---

## Avatar-to-Avatar Communication

- Network → deep-reader: share missing papers, collaborators
- Deep-reader → synthesizer: share thematic findings
- All → workspace: deposit first, then report

**Key rule**: Avatars should communicate via email to the parent agent, who batches and routes. Avoid direct avatar-to-avatar channels — the parent maintains the full picture.

---

## Generalizing Beyond mvelli

While the original skill was built for the mvelli research network, the patterns generalize:

1. **Health monitoring loop** applies to any avatar fleet — scan `system.md` for state, CPR the suspended ones.
2. **One-task-one-avatar** scales — spawn per domain, not per subtask.
3. **Deposit-before-report** prevents lost work — files on disk survive crashes; email drafts don't.
4. **Batch reporting** reduces noise — the human and parent don't need per-paragraph updates.
5. **Role docs** (`avatar-roles.md`) act as a lightweight knowledge base for the fleet's current state.
