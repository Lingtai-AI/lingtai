---
name: dev-guide-skill-stewardship
description: >
  Nested lingtai-dev-guide reference for turning one-off experience into durable
  skills: when to write a skill vs leave it as notes, the router-vs-nested-
  reference pattern, distilling experience (归一) and session journals into
  triggered/executable SKILL.md files, de-privatizing and parameterizing local
  paths and human-specific details, a lightweight pre-publish benchmark/checklist,
  grooming shared-library candidates, and PR-ready skill cleanup.
version: 1.0.1
last_changed_at: "2026-07-18T00:00:00Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Skill Stewardship

Nested lingtai-dev-guide reference. Read this after the top-level router sends you
here when deciding whether to write a skill, distilling experience into one, or
cleaning a skill up so it can ship in a PR.

This consolidates the skill-distillation meta-method (归一), skill audit/triage,
the experience-to-skill side of session-journal handoff, the skill-benchmark
checklist, and shared-library grooming. It governs *LingTai's* skill hygiene; for
generic authoring mechanics (frontmatter spec, trigger syntax, file layout)
cross-link to `skills-manual` rather than duplicating it here.

## Core principle

A skill turns a *repeated* experience into something triggered, executable,
verifiable, and maintainable. Write one when the technique would otherwise be
re-derived ad hoc across sessions or agents — and only after it is de-privatized
and lightly benchmarked. Skills shipping in this repo's preset tree must be
parameterized: no private paths, no human-specific details, no secrets.

## 1. When to write a skill

Write one when the technique was not intuitively obvious and you would reference
it again; when multiple agents/sessions keep re-implementing the same workflow
with divergent quality (convergent discovery is the strongest signal); or when
the pattern is broadly reusable rather than a one-off.

Do **not** write a skill for one-off solutions or single-incident narratives; for
standard practices already documented elsewhere (link instead); for
project-specific conventions (those belong in `CLAUDE.md`/repo docs); or for
mechanical constraints a test/regex could enforce (automate, don't document).

If it is real but not yet skill-worthy, leave it as a session-journal entry or a
shared-library candidate (§5) until the pattern repeats.

## 2. Routing vs nested-reference pattern

LingTai utility skills use progressive disclosure: a short **router** SKILL.md at
the root plus focused **nested references** under `reference/<topic>/SKILL.md`.

Read/context limits are a design signal, not a global hard-cap to keep open
forever. Do not encode an old tool threshold as a blanket character limit, and do
not paste dense material into the root just because a reader might need it later.
The root carries only the progressive-disclosure link; the related/reference file
carries the detail and loads on demand. For current mechanics (frontmatter,
layout, validator, limit-aware structure), load `skills-manual`.

- Keep the router compact: nested-reference catalog, routing table, cross-links.
  Detailed procedures live in the nested files.
- A nested reference is **parent-owned** — a drill-down of its parent skill, not a
  standalone top-level skill. Begin each nested file by identifying itself, e.g.
  "Nested lingtai-dev-guide reference."
- Adding a nested reference means updating the parent's catalog **and** routing
  table in the same commit, plus any test asserting the nested set (see
  `tui/internal/tui/skill_files_test.go`).

Prefer adding a nested reference to an existing router over creating a new
top-level skill when the topic drills down into an existing domain.

## 3. Distilling experience into a skill (归一)

The loop that turns scattered experience (chats, reports, code, daemon logs,
knowledge entries, session journals) into a maintainable `SKILL.md`:

1. **Trigger.** State the concrete situations that should load this skill — as
   the `description` "Use when…" triggers, in third person.
2. **Distill.** Extract the reusable technique, not the narrative. One excellent
   example beats five mediocre ones; drop session-specific storytelling.
3. **Validate.** Confirm any commands/computations actually run and produce the
   claimed result; fix or remove anything you cannot verify.
4. **Maintain.** Note what would make the skill stale (an API change, a renamed
   file) so a future edit knows what to check.

Session journals feed steps 1–2: an entry recording a reusable gotcha, decision,
or procedure is raw material for a skill. Promote it only when the pattern
recurs; until then it stays a journal/handoff note. (For the journal *format*,
defer to the kernel knowledge/psyche manuals — this reference only covers using
journals as skill seeds.)

## 4. De-private and parameterize before publishing

Anything shipped in the preset skill tree (or proposed for the shared library)
must be scrubbed. This list is the canonical de-privatization rule for the dev
guide; other references point here.

- **Paths:** `/Users/<name>/...` → `<your-lingtai-checkout>` or `~/.lingtai-tui/...`.
- **People:** named individuals → "the maintainer"; private project names → generic.
- **Secrets:** tokens/keys/passwords → `<REDACTED>` or `${ENV_VAR}` references.
- **Channels/IDs:** Telegram chat IDs, emails, recipient lists → parameterized or removed.
- **PR/issue numbers:** concrete numbers → examples only ("e.g. a small PR like #NNN").

Repo-relative paths and generic command examples are fine; only private,
machine-, or person-specific details must go.

## 5. Lightweight pre-publish benchmark / checklist

A quick pre-flight before a skill ships — a checklist, not a framework:

- [ ] YAML frontmatter parses; has `name` (letters/numbers/hyphens), a
      `description` that starts with "Use when…" and lists triggers only (no
      workflow summary), and for LingTai-maintained skills a `last_changed_at`
      ISO 8601 timestamp. For metadata-only backfills, initialize it from
      `git log -1 --format=%cI -- path/to/SKILL.md`; for substantive edits,
      update it in the same commit.
- [ ] Structure is scannable: overview/core principle, when-to-use, steps,
      related references. Root routers contain links, not bulk.
- [ ] Declared dependencies/cross-linked skills exist and are named correctly.
- [ ] Any commands/snippets were actually executed and produce the claimed output
      (this practice caught real API breakage).
- [ ] De-privatization (§4) is complete.
- [ ] For nested references: parent catalog + routing table + tests updated.

## 6. Shared-library candidate grooming

Not every reusable experience belongs in this repo's preset tree:

- **Repo dev-guide nested reference** — developer/operator methodology for working
  on LingTai itself.
- **Shared library candidate** — broadly reusable agent behaviour that is not
  LingTai-dev-specific; groom it (de-private, benchmark) and propose it for the
  shared library rather than the preset tree.
- **Project/custom skill** — domain-specific (heliophysics, nutrition, VC, etc.)
  or privacy-contaminated; keep it in the project, do not promote it.

When in doubt, prefer the smallest, most local home that still makes the skill
discoverable; promote later when convergent reuse is demonstrated.

## 7. PR-ready skill cleanup

Bundle skill changes into a reviewable PR with the router catalog/routing-table
edits in the same commit as the new/changed nested reference; updated tests
(`tui/internal/tui/skill_files_test.go` for nested-reference assertions) and a
passing focused run; and a self-contained HTML explainer (see
`reference/pr-review-deliverables/SKILL.md`).

## Related references

- `skills-manual` — generic skill authoring: frontmatter spec, trigger syntax,
  file layout, testing skills, limit-aware router/reference structure. Defer to it
  for mechanics; this reference covers LingTai-specific stewardship.
- `reference/pr-review-deliverables/SKILL.md` — review gates and the HTML
  explainer for a skill-changing PR.
- `reference/contributing/SKILL.md` — issue → worktree → PR → merge loop and the
  same-commit catalog/router/test rule.
