---
name: dev-guide-pr-review-deliverables
description: >
  Nested lingtai-dev-guide reference for getting a PR review-ready: PR readiness
  gates, multi-model/daemon/Claude read-only review passes, self-contained local
  HTML explainers, PR body hygiene and gh pr edit troubleshooting,
  source-labeled deliverables with syntax/validation checks, and maintainer
  authorization boundaries for opening, editing, and merging PRs.
version: 1.1.0
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# PR Review & Deliverables

Nested lingtai-dev-guide reference. Read this after the top-level router sends
you here to prepare a PR for review, build its human-facing explainer, and run
review gates before asking for a merge.

It consolidates the PR-review-gate, HTML-explainer, deliverable-template, and
release-style-audit practices, covering *preparing and reviewing* PRs and their
human deliverables. Release publication is outside this bundled guide — use the
dedicated release owner available in the maintainer's environment.

## Core principle

A PR is review-ready only when (1) the diff is clean and validated, (2) a
self-contained HTML explainer tells the human the story, and (3) the change has
passed at least one independent review pass. Opening, editing, and merging PRs
are external side effects: do them only after an explicit maintainer
authorization for *that* side effect.

## 1. PR readiness gate

- [ ] Clean diff: `git diff --check` (no trailing whitespace / conflict markers).
- [ ] Targeted tests for the changed area pass; record commands and results.
- [ ] If you touched utility/dev-guide skills, the nested-reference tests and the
      root router catalog/routing table were updated in the **same commit**.
- [ ] Anatomy travels with code: any moved/renamed/split file cited by an
      `ANATOMY.md` is updated in the same commit.
- [ ] No secrets, tokens, or private absolute paths in the diff or the explainer.
- [ ] A self-contained HTML explainer exists (see §3).

Known repo pitfalls: `docs/stars/stars.csv` may use CRLF intentionally — do not
"normalize" it; and portal tests may need frontend assets built first
(`npm --prefix portal/web run build`).

## 2. Independent review passes (gates, not permission)

Run at least one independent, **read-only** review pass before merge. Strongest
first:

1. **Multi-model / daemon review.** Generate a diff prompt and have a read-only
   daemon (Claude Code / Codex) or a second model review correctness, scope, and
   regressions. Save the review as an artifact so it is auditable. Run API-backed
   reviewers with keys from the environment (`<API_KEY_ENV>`), never inline.
2. **Claude review gate.** A focused Claude Code review of the diff for
   correctness bugs and reuse/simplification cleanups.
3. **Self review against the readiness gate (§1).**

Treat each review as input, not a verdict: read the diff yourself (it is ground
truth, not the daemon's summary), classify each finding (confirmed defect, edge
risk, future refactor, doc/process note), and fix the smallest thing that
resolves a real defect. Do not expand into a broad refactor because nearby code
is related.

These reviews are **gates, not authorization to merge** — see §5.

## 3. Self-contained local HTML explainer

Every non-trivial PR ships one self-contained `.html` explainer for the human.
This is the canonical statement of the requirement; other dev-guide references
point here rather than restating it.

**Local-only by default.** Routine PR explainers are local output, not repo
artifacts. Write to an ignored location — `artifacts/pr<NUMBER>-<slug>-explainer.html`,
`reports/pr<NUMBER>-<slug>-explainer.html`, `tmp/<topic>-<date>.html`, or the
agent/worktree report workspace — and hand the human the absolute
`file://`-openable path in the short pointer message. Do **not** commit routine
explainers. Commit one only when the human explicitly asks, when it is a
release/long-term reference artifact, or when repo documentation links to it
deliberately; use `git add -f` for that exception and explain the reason in the PR.

**Naming and timing.** Name it `pr<NUMBER>-<slug>-explainer.html` once the PR
exists; pre-push use `<topic>-<date>.html` and rename locally after. Write it
**before** asking for review or merge, and refresh it when blockers or fixes
materially change the story.

**Required sections**, in order: TL;DR / conclusion-first, baseline,
what-was-done (with diff snippets), validation (commands + results),
risks/decisions, next steps, and a source index. Plain text/Markdown is reserved
for the short pointer message and conversational replies.

**The only exception** is a strictly one-line docs/chore PR where the human has
explicitly waived the report. Absent that waiver, write the HTML even for a small
fix; if a change is too small for a useful explainer, that is a signal to bundle
it with related work.

### Deliverable hygiene (any human-facing HTML)

- **Conclusion first**, then evidence. Label every nontrivial claim with its
  source (file path, command, commit SHA, PR/issue number).
- **De-private.** Repo-relative paths and generic placeholders
  (`<your-lingtai-checkout>`); redact tokens/keys/chat IDs as `<REDACTED>`.
- **Self-contained & valid.** Single file, inline CSS, no remote assets, no build
  step. Sanity-check the markup before handing it over so a malformed tag does
  not silently swallow content. For the HTML skeleton, MathJax setup, and the
  full validation checklist, read
  `~/.lingtai-tui/utilities/swiss-knife/reference/html-report/SKILL.md` (or load
  the `swiss-knife` skill and route to `html-report`) — it owns those mechanics.
  The PR-specific path, naming, and section rules above still govern.

## 4. PR body hygiene and `gh pr edit`

The PR body cites the issue, summarizes the change, and lists validation steps,
mirroring the explainer's TL;DR and validation without duplicating the whole
HTML. Keep body and explainer in sync when fixes land.

`gh pr edit` can emit GraphQL deprecation warnings on some `gh` versions — those
are warnings, not failures. If `gh pr edit --body-file` is noisy, re-run or set
the body via the web UI; treat it as a version-dependent troubleshooting note,
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
releases (tagging, publishing to PyPI/Homebrew, website release archive/blog),
use the maintainer environment's dedicated release owner; this reference grants
no publication authority.

## Related references

- `reference/contributing/SKILL.md` — issue → worktree → PR → merge loop and
  daemon decomposition.
- `reference/skill-stewardship/SKILL.md` — PR-ready cleanup when the change adds
  or edits a skill.
- `skills-manual` — generic skill authoring (when the deliverable is a skill).
