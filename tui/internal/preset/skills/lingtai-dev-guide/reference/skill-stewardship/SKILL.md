---
name: dev-guide-skill-stewardship
description: >
  Nested lingtai-dev-guide reference for turning one-off experience into durable
  skills: when to write a skill vs leave it as notes, the router-vs-nested-
  reference pattern, distilling experience (归一) and session journals into
  triggered/executable SKILL.md files, de-privatizing and parameterizing local
  paths and human-specific details, a lightweight pre-publish benchmark/checklist,
  grooming shared-library candidates, and PR-ready skill cleanup.
version: 1.0.0
---

# Skill Stewardship

Nested lingtai-dev-guide reference. Read this after the top-level router sends you
here when you are deciding whether to write a skill, distilling experience into
one, or cleaning a skill up so it can ship in a PR.

This consolidates the skill-distillation meta-method (归一), the skill audit/
triage practice, the experience-to-skill side of session-journal handoff, the
lightweight skill-benchmark checklist, and shared-library candidate grooming. It
governs *LingTai's* skill hygiene; for generic skill-authoring mechanics
(frontmatter spec, trigger syntax, file layout) cross-link to `skills-manual`
rather than duplicating it here.

## Core principle

A skill is the standard way to turn a *repeated* experience into something
triggered, executable, verifiable, and maintainable. Write one when the
technique would otherwise be re-derived ad hoc across sessions or agents — and
only after it is de-privatized and lightly benchmarked. Skills that ship in this
repo's preset tree must be parameterized: no private paths, no human-specific
details, no secrets.

## 1. When to write a skill

Write a skill when:

- the technique was not intuitively obvious and you would reference it again;
- multiple agents/sessions keep re-implementing the same workflow with divergent
  quality (convergent discovery is the strongest signal — see the skill audit);
- the pattern is broadly reusable, not a one-off or project-specific convention.

Do **not** write a skill for:

- one-off solutions or single-incident narratives;
- standard practices already documented elsewhere (link instead);
- project-specific conventions — those belong in `CLAUDE.md`/repo docs;
- mechanical constraints enforceable by a test/regex — automate, don't document.

If it is real but not yet skill-worthy, leave it as a session-journal entry or a
shared-library candidate (§5) until the pattern repeats.

## 2. Routing vs nested-reference pattern

LingTai utility skills use progressive disclosure: a short **router** SKILL.md at
the root plus focused **nested references** under `reference/<topic>/SKILL.md`.

- Keep the router compact: a nested-reference catalog, a routing table, and
  cross-links. Detailed procedures live in the nested files, not the router.
- A nested reference is **parent-owned** — it is a drill-down of its parent
  skill, not a standalone top-level skill. Begin each nested file by identifying
  itself, e.g. "Nested lingtai-dev-guide reference."
- When you add a nested reference, update the parent's catalog **and** routing
  table in the same commit, and update any test that asserts the nested set (see
  `tui/internal/tui/skill_files_test.go`).

Prefer adding a nested reference to an existing router over creating a new
top-level skill when the topic is a drill-down of an existing domain.

## 3. Distilling experience into a skill (归一)

The distillation loop turns scattered experience (chats, reports, code, daemon
logs, knowledge entries, session journals) into a maintainable `SKILL.md`:

1. **Trigger.** State the concrete situations that should load this skill — write
   them as the `description` "Use when…" triggers, in third person.
2. **Distill.** Extract the reusable technique, not the narrative. One excellent
   example beats five mediocre ones; drop session-specific storytelling.
3. **Validate.** Confirm any commands/computations actually run and produce the
   claimed result; fix or remove anything you cannot verify.
4. **Maintain.** Note what would make the skill stale (an API change, a renamed
   file) so a future edit knows what to check.

Session journals feed step 1–2: a journal entry that records a reusable gotcha,
decision, or procedure is the raw material for a skill. Promote it only when the
pattern recurs; until then it stays a journal/handoff note. (For the journal
*format* itself, defer to the kernel knowledge/psyche manuals — this reference
only covers using journals as skill seeds.)

## 4. De-private and parameterize before publishing

Anything shipped in the preset skill tree (or proposed for the shared library)
must be scrubbed:

- **Paths:** `/Users/<name>/...` → `<your-lingtai-checkout>` or `~/.lingtai-tui/...`.
- **People:** named individuals → "the maintainer"; private project names → generic.
- **Secrets:** tokens/keys/passwords → `<REDACTED>` or `${ENV_VAR}` references.
- **Channels/IDs:** Telegram chat IDs, emails, recipient lists → parameterized or removed.
- **PR/issue numbers:** concrete numbers → examples only ("e.g. a small PR like #NNN").

It is fine to mention repo-relative paths and generic command examples; only
private, machine-, or person-specific details must go.

## 5. Lightweight pre-publish benchmark / checklist

Before a skill ships, run a quick "pre-flight" — a benchmark/checklist, not a
heavy framework:

- [ ] YAML frontmatter parses; has `name` (letters/numbers/hyphens) and a
      `description` that starts with "Use when…" and lists triggers only (no
      workflow summary).
- [ ] Document structure is scannable: overview/core principle, when-to-use,
      steps, related references.
- [ ] Declared dependencies/cross-linked skills exist and are named correctly.
- [ ] Any commands/snippets were actually executed and produce the claimed output
      (the skill-benchmark practice caught real API breakage this way).
- [ ] De-privatization (§4) is complete.
- [ ] For nested references: parent catalog + routing table + tests updated.

## 6. Shared-library candidate grooming

Not every reusable experience belongs in this repo's preset tree. Triage:

- **Repo dev-guide nested reference** — developer/operator methodology for working
  on LingTai itself (this is where these three new references live).
- **Shared library candidate** — broadly reusable agent behaviour that is not
  LingTai-dev-specific; groom it (de-private, benchmark) and propose it for the
  shared library rather than the preset tree.
- **Project/custom skill** — domain-specific (heliophysics, nutrition, VC, etc.)
  or privacy-contaminated; keep it in the project, do not promote it.

When in doubt, prefer the smallest, most local home that still makes the skill
discoverable; promote later when convergent reuse is demonstrated.

## 7. PR-ready skill cleanup

Bundle skill changes into a reviewable PR with:

- the router catalog/routing-table edits in the same commit as the new/changed
  nested reference;
- updated tests (`tui/internal/tui/skill_files_test.go` for nested-reference
  assertions) and a passing focused run;
- a self-contained HTML explainer (see `reference/pr-review-deliverables/SKILL.md`).

## Related references

- `skills-manual` — generic skill authoring: frontmatter spec, trigger syntax,
  file layout, testing skills. Defer to it for mechanics; this reference covers
  LingTai-specific stewardship.
- `reference/pr-review-deliverables/SKILL.md` — review gates and the HTML
  explainer for a skill-changing PR.
- `reference/contributing/SKILL.md` — issue → worktree → PR → merge loop and the
  same-commit catalog/router/test rule.
