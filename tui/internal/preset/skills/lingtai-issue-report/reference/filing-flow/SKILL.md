---
name: issue-report-filing-flow
description: Nested lingtai-issue-report reference for the filing decision — the always-required human consent boundary, the read-only gh probe, Path A (direct gh filing) and Path B (paste-ready handoff), token hygiene, and proactive surfacing. Read this once the report is drafted and you are deciding how it gets filed.
version: 1.0.0
---

# Issue report — filing flow

This is a nested `lingtai-issue-report` reference. It governs everything after the report is drafted: getting human consent, detecting whether `gh` can file directly, the two filing paths, and surfacing the issue proactively.

## The boundary — permission required, always

**You never open a GitHub issue without an explicit "yes" from the human.** The human is the accountable owner of what gets filed under their name. Even if `gh` is authenticated and you have a shell, the per-issue consent is non-negotiable. Your role is to:

1. Assemble a structured report (see the `report-template` reference)
2. Send the report through the appropriate channel to your **parent avatar** (if you're an avatar) AND to the **human**
3. Check whether `gh` is available and authenticated (see "Filing path" below)
4. Ask the human's permission, naming the path you'd take (direct `gh` filing vs. paste-into-browser)
5. Only then act — file it via `gh` if they say yes, or hand them the title/body if they prefer to file manually

If the human declines, drop it. Don't nag, don't auto-retry on the next turn. Their call.

## Filing path — detect `gh` first

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

### Path A — direct filing via `gh` (preferred when available)

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
- The `--repo` value defaults to `Lingtai-AI/lingtai`. If the human asked for the kernel tracker (`Lingtai-AI/lingtai-kernel`) or names another repo, use what they said — do not silently override. For repo-choice details, see "Which repo" in the `report-template` reference.
- After `gh issue create` returns, it prints the issue URL on stdout. Quote that URL back to the human so they can verify.
- If the command errors (network blip, repo permission, rate limit, 401), tell the human exactly what `gh` said and offer Path B as fallback. Do not retry silently. On 401, the token may be expired — don't keep retrying with it.
- **Token hygiene** (Path A with `GH_TOKEN`): keep the token in the env of the single `gh` command, never echo it back in chat or logs, never write it to a file, never include it in the issue body or commit message. Delete the body tempfile after filing.

If they say **paste it myself**, use Path B even though `gh` is available.

### Path B — hand off title + body for manual filing (always works)

When `gh` is unusable, or when the human prefers manual filing, the ask is:

> "I noticed [one-sentence summary]. I sent a structured report to your inbox. If you'd like to file this as a GitHub issue, I can format it for you. The tracker is `https://github.com/Lingtai-AI/lingtai/issues`. Should I prep the title + body?"

If they say **yes**:
- Format the report into a GitHub-flavored markdown issue body (preserve the section headers; they render cleanly)
- Provide the title (your `Subject` line, minus the `[Issue Report]` prefix)
- Provide the URL: `https://github.com/Lingtai-AI/lingtai/issues/new`
- Tell them to paste; don't try to do it yourself

If they say **no** or are silent: stop. Don't bring it up again unless the same issue blocks you in a future turn.

## Proactive surfacing — don't wait to be asked

When you identify an issue that meets the "When to invoke" criteria (see the `evidence-checklist` reference), **surface it immediately** — don't wait for the human to ask you to file a report. The correct sequence is:

1. You diagnose the problem (as part of your normal work)
2. You apply a workaround or fix (if possible)
3. **You proactively tell the human:** "I ran into [brief summary]. Want me to file a GitHub issue about it?"
4. If they say yes → follow the filing path above
5. If they say no → drop it

The human should never have to guess that you found a bug. **Your job is not just to fix — it's to surface.**

Signs you should proactively offer:
- You spent more than 2 turns diagnosing an unexpected error
- The workaround you used is not documented anywhere
- The bug would affect other agents or users, not just you
- You discovered the fix requires a restart, manual file edit, or other non-obvious step
- The issue contradicts what the documentation claims

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
