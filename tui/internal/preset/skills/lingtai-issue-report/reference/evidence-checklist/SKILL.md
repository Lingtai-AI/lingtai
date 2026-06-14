---
name: issue-report-evidence-checklist
description: Nested lingtai-issue-report reference for deciding when an observation is worth reporting, what evidence to collect, and how to keep secrets out of a report. Read this first, while the problem is fresh, before drafting the report body.
version: 1.0.0
---

# Issue report — evidence checklist

This is a nested `lingtai-issue-report` reference. It covers two things: *whether* an observation deserves a report, and *what evidence* to capture (without leaking secrets) before you draft one. Read it while the problem is still fresh — you can rarely reconstruct exact tool output later.

You operate inside the LingTai system continuously, hitting its skills, capabilities, and procedures as a real user. That makes you uniquely positioned to notice problems humans might miss — a doc URL that 404s, a capability that errors silently, a skill whose claims don't match what the API actually returns, a preset that ships a broken default, a procedure step that contradicts another. **When you notice something wrong, surface it.**

## When to invoke the issue-report protocol

You should reach for `lingtai-issue-report` whenever you spot any of:

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

## What makes a good report

You see far more than a human does inside the system. Use that:

- **Quote verbatim.** Tool outputs, error strings, doc snippets — copy them, don't summarize. Maintainers grep.
- **Show your work.** "I called X with args Y and got Z" beats "X seems broken."
- **Distinguish doc bug from code bug.** "The skill claims `mimo-v2-pro` supports vision but the API returns 400 on image input" — is that a doc bug (skill is wrong) or a code bug (API broke)? Note which you think and why.
- **Note what works.** If 3 of 4 modalities work and 1 doesn't, say so — narrows the maintainer's search.
- **Flag your version context.** If you know the kernel version, TUI version, active preset, repository commit, or recent migrations applied, include them. If you do not have an exact version surface, say what you do know rather than inventing one.

## Secret hygiene — keep credentials out of reports

Reports get filed publicly and maintainers grep them. Before you put anything into a report body, scrub it:

- **Never paste tokens, keys, or passwords.** If a `GH_TOKEN`, API key, or other secret appears in the tool output you want to quote, redact it (`GH_TOKEN=ghp_…redacted`) before it goes anywhere near the report.
- **Never echo a human-provided token** back into chat, a log, a file, or the issue body. A token the human handed you this session stays in the env of the single command that needs it (see `filing-flow`).
- **Scrub environment dumps.** Verbatim output from `env`, `gh auth status`, or a config file often carries secrets — strip them before quoting.
- **Watch for paths that reveal private data.** Redact home-directory usernames or private repo names only if they'd expose something the human wouldn't want public; otherwise keep paths verbatim so maintainers can navigate.
- **When in doubt, redact and say so.** "(redacted a token here)" is fine — it tells the maintainer something was there without leaking it.

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
