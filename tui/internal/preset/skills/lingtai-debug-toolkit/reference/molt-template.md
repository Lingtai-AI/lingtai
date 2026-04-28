# LingTai Molt Template — Reference Card

> Extracted from `lingtai-molt-template` skill (v1.0.0). A structured template for writing the molt summary before reincarnation.

## Core Principle

The summary is not a journal entry — it is an **operational briefing for your successor**. Every sentence should answer: *"What would I need to know to pick up exactly where I left off?"*

---

## Template Sections

Copy and fill ALL sections. If a section is empty, write "None" rather than omitting it.

### Section 1: Who I Am

- **Name**: [agent_name]
- **Address**: [address]
- **agent_id**: [permanent ID]
- **Parent**: [parent address] (karma: True/False)
- **Current lifetime**: Generation N
- **Role & specialty**: [one sentence on what you do]

### Section 2: Accomplishments

For each completed task:
- **Task name**: [name]
- **Output**: [file paths, codex IDs, or "verbal report to X"]
- **Key conclusion**: [1-2 sentences max]
- **Reported to**: [who knows about this? name + address]

### Section 3: Outstanding Tasks

For each incomplete task:
- **Task name**: [name]
- **Status**: [not started / in progress / awaiting reply]
- **Blocker**: [what's blocking completion]
- **Next step**: [concrete action]

### Section 4: Action Checklist (Required!)

**This is the most critical section.** List every specific action your successor should take, in priority order:

```
□ [Immediate] Notify [name]@[address]: [specific content]
□ [Immediate] Reply to [name]@[address] regarding [topic] pending reply
□ [Today] Build [skill name] skill
□ [Pending] Wait for [name]@[address] to approve [topic]
□ [Optional] [nice-to-have action]
```

Format: `□ [Priority] Action + Recipient + Specific content`

Priority levels:
- **Immediate**: Must do in first 5 minutes after waking
- **Today**: Should do in this session
- **Pending**: Blocked on someone else — follow up if no reply in [N] minutes
- **Optional**: Nice to have, skip if context is tight

### Section 5: Collaborators

For each person/agent you're working with:
- **[name]** @[address]: [role/capability] | [agent_id] | [relationship: parent/peer/sub]
- Note any pending interactions: "expecting reply re: [topic]" or "owes me [deliverable]"

### Section 6: Codex Cheat Sheet

List codex entries your successor should know about:
- **[id]** [title]: [one-line summary] → relevant for [task]

If your successor needs to load codex content into pad, specify:
```
Must load: codex(export, ids=[...]) → psyche(pad, edit, files=[...])
```

### Section 7: Key Paths

File paths your successor will need:
```
Reports: [absolute path]
Code: [absolute path]
Skills: [absolute path]
Configuration: [absolute path]
```

### Section 8: Lessons Learned

Numbered list. Each lesson should be actionable:
- ❌ "Don't make mistakes" → useless
- ✅ "Email address param for multiple recipients must be sent separately" → actionable

### Section 9: Context Status

- **Reason for reincarnation**: [context full / forced / scheduled]
- **Leftover items**: [anything you couldn't fit into codex/pad before molt]
- **Unsent messages**: [any drafts or intended messages you couldn't send]

---

## Verification Checklist

Before calling `psyche(context, molt)`, verify:

```
□ Every "Outstanding Tasks" item has a corresponding "Action Checklist" entry
□ Every "Action Checklist" entry specifies a recipient (who) and content (what)
□ Every collaborator has an address and agent_id listed
□ Every codex cheat sheet entry has an ID and purpose
□ Every key file path is an absolute path
□ Every lesson in "Lessons Learned" is actionable
□ All pending-reply contacts have been notified: "I am about to reincarnate; please wait for my successor to reply"
□ Codex is archived (codex entries up to date)
□ Pad is updated (pad reflects current state)
□ Lingtai is updated (lingtai reflects latest identity)
```

## Case Study: What Goes Wrong Without This Template

**comms-analyst's first molt** (2026-04-15):
- Discovered a critical erratum ("address splitting" was a misdiagnosis)
- Context was too full to send correction before molt
- Codex archived the knowledge, pad noted "need to send erratum"
- **But**: No action list specifying *who* to notify (tools-skills-analyst? parent? both?)
- **Result**: Post-molt, spent extra time figuring out who needed the correction
- **Root cause**: Summary said "what to do" but not "who to tell and how"

**Lesson learned**: "Outstanding Tasks" without an "Action Checklist" is like a map without directions.
