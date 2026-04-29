---
name: lingtai-issue-report
description: Protocol for reporting bugs, stale info, missing capabilities, or design issues you spot in any LingTai skill, capability, preset, or system behavior. You don't open GitHub issues yourself — you assemble a structured report, ask the human for permission, and guide them to file it.
version: 1.0.0
---

# Reporting LingTai Issues

You operate inside the LingTai system continuously, hitting its skills, capabilities, and procedures as a real user. That makes you uniquely positioned to notice problems humans might miss — a doc URL that 404s, a capability that errors silently, a skill whose claims don't match what the API actually returns, a preset that ships a broken default, a procedure step that contradicts another. **When you notice something wrong, surface it.** This skill is the protocol.

## When To Invoke

You should reach for this skill whenever you spot any of:

- **Stale documentation** — a skill claims a model/endpoint/feature that no longer exists or behaves differently than described
- **Broken URLs** — a doc link, console URL, or example URL returns 404 or the wrong page
- **Silent failure** — a capability accepts your call but returns nothing useful, or `setup` swallows an error and leaves you without a tool you should have
- **Wrong defaults** — a preset, capability config, or environment variable name in the docs doesn't match what users actually have
- **Missing capability** — you genuinely need a tool that doesn't exist (this is rarer than the others; check carefully that you haven't missed an existing one)
- **Procedure contradiction** — two skills or sections of `procedures.md` give incompatible guidance for the same situation
- **Reproducibly wrong output** — a model/tool returns clearly wrong answers in a way that isn't just a one-off hallucination (e.g. a vision model claims it can't see an image when image_url content is present)
- **Migration / rename gaps** — you encounter old names that the changelog skill doesn't document

You should **not** invoke this skill for:

- One-off LLM hallucinations or non-determinism (file a bug only if you can reproduce)
- Personal preference about wording or formatting in a doc — unless it's actually misleading
- Complaints about a model's quality on hard tasks (that's the model, not LingTai)
- Feature requests for things the system was never designed to do

## The Boundary — You Do Not File

**You never open GitHub issues yourself.** You don't have the credentials, and even if you did, the human is the accountable owner of what gets filed under their name. Your role is to:

1. Assemble a structured report
2. Send the report via `mail` to your **parent avatar** (if you're an avatar) AND to the **human**
3. Ask the human's permission before suggesting they file it
4. If the human consents, give them the formatted issue title and body and the URL to paste into

If the human declines, drop it. Don't nag, don't auto-retry on the next turn. Their call.

## The Report Template

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

Send via:
```
mail(action="send", address=<parent_or_human_address>, subject="[Issue Report] ...", message=<body>)
```

If you have multiple addressees (parent + human), send the same content twice — `mail` doesn't multicast.

## Asking The Human For Permission

After you've sent the report, follow up with a short turn that asks the human:

> "I noticed [one-sentence summary]. I sent a structured report to your inbox. If you'd like to file this as a GitHub issue, I can format it for you. The tracker is `https://github.com/Lingtai-AI/lingtai/issues`. Should I prep the title + body?"

Keep this short. The human reads the report you already sent if they want detail; the ask itself just confirms whether they want to escalate.

If they say **yes**:
- Format the report into a GitHub-flavored markdown issue body (preserve the section headers above; they render cleanly)
- Provide the title (your `Subject` line, minus the `[Issue Report]` prefix)
- Provide the URL: `https://github.com/Lingtai-AI/lingtai/issues/new`
- Tell them to paste; don't try to do it yourself

If they say **no** or are silent: stop. Don't bring it up again unless the same issue blocks you in a future turn.

## Which Repo

The umbrella issue tracker for end-user reports is **`Lingtai-AI/lingtai`** (the binary humans actually install). File there even if the underlying bug is in `lingtai-kernel`, a sibling skill repo, or a preset — the maintainers will route or transfer as needed.

If the human happens to know the issue is kernel-specific (e.g. they're a developer), they may prefer `https://github.com/Lingtai-AI/lingtai-kernel/issues`. Don't second-guess; let them choose.

## What Makes A Good Report

You see far more than a human does inside the system. Use that:

- **Quote verbatim.** Tool outputs, error strings, doc snippets — copy them, don't summarize. Maintainers grep.
- **Show your work.** "I called X with args Y and got Z" beats "X seems broken."
- **Distinguish doc bug from code bug.** "The skill claims `mimo-v2-pro` supports vision but the API returns 400 on image input" — is that a doc bug (skill is wrong) or a code bug (API broke)? Note which you think and why.
- **Note what works.** If 3 of 4 modalities work and 1 doesn't, say so — narrows the maintainer's search.
- **Flag your version context.** If you know the kernel version, TUI version, or recent migrations applied, include them. `system(action='show')` surfaces these.

## Self-Healing

This skill itself can have bugs. If the report template here is missing a section that you find yourself wanting, or if the GitHub URLs above 404 (the org may rename, repos may move), include a note in your report saying "the issue-report skill says X but Y is what I actually found" — and the maintainers will update this skill.

The canonical org is **`Lingtai-AI`** on GitHub. If `https://github.com/Lingtai-AI` itself 404s one day, the project has likely moved; ask the human where to file instead.
