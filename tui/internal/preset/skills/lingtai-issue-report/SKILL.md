---
name: lingtai-issue-report
description: Protocol router for reporting bugs, stale info, missing capabilities, or design issues you spot in any LingTai skill, capability, preset, or system behavior. Enter through this router, then load the one nested reference you need — evidence-checklist (when to report, what evidence to collect, secret hygiene), report-template (the report body/title structure), or filing-flow (human consent, the gh CLI path, and the paste-ready fallback). You always assemble a structured report and ask the human for permission before filing.
version: 1.4.0
---

# Reporting LingTai Issues

This is a reference router. You operate inside the LingTai system continuously, hitting its skills, capabilities, and procedures as a real user — so you are uniquely positioned to notice problems humans miss. **When you notice something wrong, surface it.** This skill is the protocol. Enter through this router, pick the one nested reference that matches where you are in the report lifecycle, and read that leaf for the full procedure.

## The non-negotiables (read before anything else)

Two rules hold across every path and every leaf:

1. **Human consent is required, always.** You never open a GitHub issue without an explicit "yes" from the human. The human is the accountable owner of what gets filed under their name. Even if `gh` is authenticated and you have a shell, per-issue consent is non-negotiable. If they decline, drop it — no nagging, no auto-retry.
2. **Secrets never enter a report.** No tokens, keys, or passwords in the body, in chat, in logs, or in files. A human-provided `GH_TOKEN` stays in the env of the single command that needs it. Redact before you quote.

The nested references elaborate these; they never weaken them.

## Nested reference catalog

```yaml
- name: issue-report-evidence-checklist
  location: reference/evidence-checklist/SKILL.md
  description: When an observation is worth reporting (and when it isn't), what evidence to capture verbatim, and how to keep secrets out of the report.
- name: issue-report-report-template
  location: reference/report-template/SKILL.md
  description: The report skeleton — subject/title, the structured body sections, sending it via mail to your parent and the human, and which repo to target.
- name: issue-report-filing-flow
  location: reference/filing-flow/SKILL.md
  description: The filing decision — human consent boundary, the read-only gh probe, Path A (direct gh filing) and Path B (paste-ready handoff), token hygiene, and proactive surfacing.
```

## Routing table

| Need | Read |
|---|---|
| Decide whether to report, gather evidence, or scrub secrets | `reference/evidence-checklist/SKILL.md` |
| Assemble the report body/title and send it via mail | `reference/report-template/SKILL.md` |
| Get consent and file it (gh CLI or paste-ready handoff) | `reference/filing-flow/SKILL.md` |

## How to use this router

1. **Just noticed something?** → `evidence-checklist` — confirm it's report-worthy and capture the evidence before it's gone.
2. **Ready to write it up?** → `report-template` — fill in the structured body and mail it to your parent and the human.
3. **Time to file?** → `filing-flow` — probe `gh`, ask the human's permission, then file via Path A or hand off via Path B.

You will usually move through all three in order, but read one leaf at a time — don't pull the whole protocol into context at once.

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
