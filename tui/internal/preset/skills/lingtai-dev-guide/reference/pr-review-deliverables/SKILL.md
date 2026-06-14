---
name: dev-guide-pr-review-deliverables
description: >
  Nested lingtai-dev-guide reference for getting a PR review-ready: PR readiness
  gates, multi-model/daemon/Claude read-only review passes, self-contained local
  HTML explainers, PR body hygiene and gh pr edit troubleshooting,
  source-labeled deliverables with syntax/validation checks, and maintainer
  authorization boundaries for opening, editing, and merging PRs.
version: 1.0.0
---

# PR Review & Deliverables

Nested lingtai-dev-guide reference. Read this after the top-level router sends
you here to prepare a PR for review, build its human-facing explainer, and run
review gates before asking for a merge.

This consolidates the PR-review-gate, HTML-explainer, deliverable-template, and
release-style-audit practices. It covers *preparing and reviewing* PRs and their
human deliverables; for release-specific publishing (tags, PyPI, Homebrew,
website release log/blog) cross-link to `reference/release-workflow/SKILL.md`
instead of duplicating it here.

## Core principle

A PR is review-ready only when (1) the diff is clean and validated, (2) a
self-contained HTML explainer tells the human the story, and (3) the change has
passed at least one independent review pass. Opening, editing, and merging PRs
are external side effects: do them only after an explicit maintainer
authorization for *that* side effect.

## 1. PR readiness gate

Before requesting review, confirm:

- [ ] Clean diff: `git diff --check` (no trailing whitespace / conflict markers).
- [ ] Targeted tests for the changed area pass; record commands and results.
- [ ] If you touched utility/dev-guide skills, the nested-reference tests and the
      root router catalog/routing table were updated in the **same commit**.
- [ ] Anatomy travels with code: any moved/renamed/split file cited by an
      `ANATOMY.md` is updated in the same commit.
- [ ] No secrets, tokens, or private absolute paths in the diff or the explainer.
- [ ] A self-contained HTML explainer exists under `reports/` (see §3).

Known repo pitfalls to not trip over:

- `tui/internal/tui/stars.csv` may use CRLF intentionally — do not "normalize" it.
- Portal tests may need frontend assets built first (`npm --prefix portal/web run build`).

## 2. Independent review passes (gates, not permission)

Run at least one independent, **read-only** review pass before merge. Options,
strongest first:

1. **Multi-model / daemon review.** Generate a diff prompt and have a read-only
   daemon (Claude Code / Codex) or a second model review correctness, scope, and
   regressions. Save the review as an artifact under `reports/` so it is
   auditable. Run API-backed reviewers with keys from the environment
   (`<API_KEY_ENV>`), never inline.
2. **Claude review gate.** A focused Claude Code review of the diff for
   correctness bugs and reuse/simplification cleanups.
3. **Self review against the readiness gate (§1).**

Treat each review as input, not a verdict: read the diff yourself (it is ground
truth, not the daemon's summary), classify each finding (confirmed defect, edge
risk, future refactor, doc/process note), and fix the smallest thing that
resolves a real defect. Do not expand into a broad refactor because nearby code
is related.

These reviews are **gates, not authorization to merge.** Passing review does not
imply permission to merge — see §5.

## 3. Self-contained local HTML explainer

Every non-trivial PR ships one self-contained `.html` explainer for the human,
under `reports/` at the worktree root. Requirements:

- Single file, inline CSS, **no remote assets, no build step** — open via `file://`.
- Name it `pr<NUMBER>-<slug>-explainer.html` once the PR exists; pre-push use
  `<topic>-<date>.html` and rename after.
- Write it **before** asking for review/merge; hand the human its absolute path
  in the short pointer message; update it in the same PR when fixes materially
  change the story.

Required sections: TL;DR / conclusion-first, baseline, what-was-done (with diff
snippets), validation (commands + results), risks/decisions, next steps, and a
source index. Plain text/Markdown is reserved for the short pointer message and
conversational replies.

The only exception is a strictly one-line docs/chore PR where the human has
explicitly waived the report. Absent that waiver, write the HTML even for a small
fix; if a change is too small for a useful explainer, that is a signal to bundle
it with related work.

### Deliverable hygiene (applies to any human-facing HTML)

- **Conclusion first**, then evidence. Label every nontrivial claim with its
  source (file path, command, commit SHA, PR/issue number).
- **Self-contained & valid.** No external fetches. Sanity-check the markup before
  handing it over — e.g. an HTMLParser pass or a quick browser/`file://` open —
  so a malformed tag does not silently swallow content.
- **De-private.** Use repo-relative paths and generic placeholders
  (`<your-lingtai-checkout>`); redact tokens/keys/chat IDs as `<REDACTED>`.

## 4. PR body hygiene and `gh pr edit`

- The PR body cites the issue, summarizes the change, and lists validation steps;
  it should mirror the explainer's TL;DR and validation without duplicating the
  whole HTML.
- Keep the PR body and the explainer in sync when fixes land.
- `gh pr edit` can emit GraphQL deprecation warnings on some `gh` versions; these
  are warnings, not failures. If `gh pr edit --body-file` is noisy, re-run or set
  the body via the web UI — treat it as a version-dependent troubleshooting note,
  not a blocker.

## 5. Maintainer authorization boundaries

Do not, without an explicit imperative authorization for that specific side
effect:

- open or merge PRs, push commits, or force-push;
- file/close issues, close/delete branches or other resources;
- edit a PR body on the remote.

Generating diffs, running read-only reviews, and writing local explainers are
always allowed. When approval wording is ambiguous, show the maintainer the final
candidate refs/diff and ask before mutating GitHub state. For consequential
releases (tagging, publishing to PyPI/Homebrew, website release log/blog), the
publishing boundaries and authorization rules live in
`reference/release-workflow/SKILL.md` — follow that reference rather than
re-deriving release permissions here.

## Related references

- `reference/contributing/SKILL.md` — issue → worktree → PR → merge loop, daemon
  decomposition, and the canonical HTML-explainer requirement.
- `reference/release-workflow/SKILL.md` — release-specific gates, publishing
  boundaries, and the release blog/style-audit workflow.
- `reference/skill-stewardship/SKILL.md` — PR-ready cleanup when the change adds
  or edits a skill.
- `skills-manual` — generic skill authoring (when the deliverable is a skill).
