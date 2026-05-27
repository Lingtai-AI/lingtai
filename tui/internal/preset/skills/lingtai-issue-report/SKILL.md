---
name: lingtai-issue-report
description: Protocol for reporting bugs, stale info, missing capabilities, or design issues you spot in any LingTai skill, capability, preset, or system behavior. You assemble a structured report, ask the human for permission, then either file it directly via the `gh` CLI (if `gh auth` is present OR the human provided a `GH_TOKEN`, and the human consents) or hand them a formatted title + body to paste into the issue tracker.
version: 1.3.0
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
- **Migration / rename gaps** — you encounter old names that `lingtai-kernel-anatomy`'s `reference/changelog.md` doesn't document

You should **not** invoke this skill for:

- One-off LLM hallucinations or non-determinism (file a bug only if you can reproduce)
- Personal preference about wording or formatting in a doc — unless it's actually misleading
- Complaints about a model's quality on hard tasks (that's the model, not LingTai)
- Feature requests for things the system was never designed to do

## The Boundary — Permission Required, Always

**You never open a GitHub issue without an explicit "yes" from the human.** The human is the accountable owner of what gets filed under their name. Even if `gh` is authenticated and you have a shell, the per-issue consent is non-negotiable. Your role is to:

1. Assemble a structured report
2. Send the report via `mail` to your **parent avatar** (if you're an avatar) AND to the **human**
3. Check whether `gh` is available and authenticated (see "Filing Path" below)
4. Ask the human's permission, naming the path you'd take (direct `gh` filing vs. paste-into-browser)
5. Only then act — file it via `gh` if they say yes, or hand them the title/body if they prefer to file manually

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
imap(action="send", address=<parent_or_human_address>, subject="[Issue Report] ...", message=<body>)
```

If you have multiple addressees (parent + human), send the same content twice — `imap` doesn't multicast.

## Filing Path — Detect `gh` First

Before you ask the human for permission, run a quick read-only probe to see whether the GitHub CLI is installed AND there's a way to authenticate. There are two acceptable auth sources — either is enough to make Path A available:

1. **Existing `gh auth`** — the host already has a logged-in account.
2. **A `GH_TOKEN` the human provided this session** — they pasted a personal access token into chat (or it's already in your shell env). `gh` reads `GH_TOKEN` from the environment per-invocation, so no `gh auth login` is needed.

Probe:

```bash
# Is gh installed?
command -v gh

# Is gh already authenticated?
gh auth status 2>&1
```

Interpret the result:

- **`gh` is installed AND (`gh auth status` exits 0 with a logged-in account, OR the human has handed you a `GH_TOKEN` this session)** → Path A is available.
- **`gh` is missing, or neither auth source is present** → Path A is not available. Fall through to Path B.

Do **not** run `gh issue create` during the probe. Do **not** echo, log, or commit the token. The probe is read-only; the actual filing happens only after the human says yes.

### Path A — Direct filing via `gh` (preferred when available)

If the probe succeeded, your permission ask becomes one of:

> "I noticed [one-sentence summary]. I sent a structured report to your inbox. Your `gh` CLI is authenticated — with your OK, I can file this directly to `Lingtai-AI/lingtai` as a GitHub issue. Want me to file it, or would you rather paste it yourself?"

…or, when you're using the token they handed you:

> "I noticed [one-sentence summary]. I sent a structured report to your inbox. I can file this directly to `Lingtai-AI/lingtai` using the `GH_TOKEN` you provided — the token stays in the env of this one command, never logged. Want me to file it, or would you rather paste it yourself?"

If they say **file it** (or equivalent — "yes", "go ahead", "do it"):

```bash
# When relying on existing gh auth:
gh issue create \
  --repo Lingtai-AI/lingtai \
  --title "<your Subject line, minus the [Issue Report] prefix>" \
  --body-file <path-to-a-tempfile-with-the-rendered-body>

# When using a human-provided token (inline env, single command):
GH_TOKEN=$TOKEN gh issue create \
  --repo Lingtai-AI/lingtai \
  --title "<your Subject line, minus the [Issue Report] prefix>" \
  --body-file <path-to-a-tempfile-with-the-rendered-body>
```

Notes on the `gh` invocation:
- Write the body to a tempfile (e.g. `/tmp/lingtai-issue-<timestamp>.md`) and pass `--body-file`. Avoid `--body "<long string>"` — shell quoting eats backticks, code fences, and newlines.
- Preserve the report's section headers verbatim; GFM renders them cleanly.
- The `--repo` value defaults to `Lingtai-AI/lingtai`. If the human asked for the kernel tracker (see "Which Repo" below) or names another repo, use what they said — do not silently override.
- After `gh issue create` returns, it prints the issue URL on stdout. Quote that URL back to the human so they can verify.
- If the command errors (network blip, repo permission, rate limit, 401), tell the human exactly what `gh` said and offer Path B as fallback. Do not retry silently. On 401, the token may be expired — don't keep retrying with it.
- **Token hygiene** (Path A with `GH_TOKEN`): keep the token in the env of the single `gh` command, never echo it back in chat or logs, never write it to a file, never include it in the issue body or commit message. Delete the body tempfile after filing.

If they say **paste it myself**, use Path B even though `gh` is available.

### Path B — Hand off title + body for manual filing (always works)

When `gh` is unusable, or when the human prefers manual filing, the ask is:

> "I noticed [one-sentence summary]. I sent a structured report to your inbox. If you'd like to file this as a GitHub issue, I can format it for you. The tracker is `https://github.com/Lingtai-AI/lingtai/issues`. Should I prep the title + body?"

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

## Proactive Surfacing — Don't Wait To Be Asked

When you identify an issue that meets the "When To Invoke" criteria, **surface it immediately** — don't wait for the human to ask you to file a report. The correct sequence is:

1. You diagnose the problem (as part of your normal work)
2. You apply a workaround or fix (if possible)
3. **You proactively tell the human:** "I ran into [brief summary]. Want me to file a GitHub issue about it?"
4. If they say yes → follow the Filing Path below
5. If they say no → drop it

The human should never have to guess that you found a bug. **Your job is not just to fix — it's to surface.**

Signs you should proactively offer:
- You spent more than 2 turns diagnosing an unexpected error
- The workaround you used is not documented anywhere
- The bug would affect other agents or users, not just you
- You discovered the fix requires a restart, manual file edit, or other non-obvious step
- The issue contradicts what the documentation claims
