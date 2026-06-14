---
name: issue-report-report-template
description: Nested lingtai-issue-report reference for the report skeleton — subject/title format, the structured body sections (what's wrong, where, reproduction, expected vs actual, severity, suggested fix), and how to send it via LingTai email or the human's active channel. Read this when you are ready to assemble the report.
version: 1.0.0
---

# Issue report — report template

This is a nested `lingtai-issue-report` reference. It is the canonical structure for an issue report: the subject/title, the body sections, and how to deliver it through the appropriate communication channel before any filing decision.

## The report template

Send the report as a mail message with a clear subject and a structured body. Use this skeleton:

```
Subject: [Issue Report] <one-line summary>

## What's wrong
<concise statement of the problem — one paragraph>

## Where
- Component: <skill name / capability name / preset name / procedure section>
- File or URL (if known): <path or URL>

## Reproduction
<exact steps you took, exact tool calls, exact responses you got. Include
verbatim error messages, status codes, or contradictory text.>

## What you expected
<what the docs/skill led you to expect>

## What actually happened
<what you observed instead>

## Severity
<one of: blocking | major | minor | cosmetic>
- blocking — agents cannot complete the affected workflow at all
- major — a documented feature is broken or absent; workaround exists but costs time
- minor — incorrect detail; doesn't break workflows but misleads new agents
- cosmetic — typo, formatting, broken link in a doc

## Suggested fix (optional)
<if you have a concrete suggestion, include it. otherwise omit this section.>
```

Keep the section headers verbatim — they double as the GitHub-flavored markdown issue body later, and GFM renders them cleanly. The title used for filing is your `Subject` line **minus the `[Issue Report]` prefix**.

## Send it through the right channel first

Before any filing decision, send the report so there is a durable record and your parent/human can see it. Use the channel they are actually using:

```python
# Internal LingTai email / peer mail
email(action="send", address=<parent_or_human_address>, subject="[Issue Report] ...", message=<body>)

# If the human is currently on Telegram/another chat channel, send or reply there instead.
# Always follow the surrounding agent's channel discipline.
```

Send the report to your **parent avatar** (if you're an avatar) AND to the **human**. If you have multiple addressees (parent + human), send the same content to each appropriate channel explicitly.

## Which repo

The umbrella issue tracker for end-user reports is **`Lingtai-AI/lingtai`** (the binary humans actually install). File there even if the underlying bug is in `lingtai-kernel`, a sibling skill repo, or a preset — the maintainers will route or transfer as needed.

If the human happens to know the issue is kernel-specific (e.g. they're a developer), they may prefer `https://github.com/Lingtai-AI/lingtai-kernel/issues`. Don't second-guess; let them choose.

The canonical org is **`Lingtai-AI`** on GitHub. If `https://github.com/Lingtai-AI` itself 404s one day, the project has likely moved; ask the human where to file instead.

## Self-healing

This skill itself can have bugs. If the report template here is missing a section that you find yourself wanting, or if the GitHub URLs in this reference 404 (the org may rename, repos may move), include a note in your report saying "the issue-report skill says X but Y is what I actually found" — and the maintainers will update this skill.

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
