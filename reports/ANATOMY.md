---
related_files:
  - ANATOMY.md
  - docs/ANATOMY.md
  - RELEASING.md
  - CLAUDE.md
  - reports/graphify-repo-map-20260609.html
  - reports/pr338-dev-guide-subskills-explainer.html
  - reports/postman-retirement-20260718/index.html
  - reports/lingtai-release-v0.9.3-20260620/candidate_head.txt
  - reports/lingtai-release-v0.9.3-20260620/commits.md
  - reports/lingtai-release-v0.9.3-20260620/previous_tag.txt
  - reports/lingtai-release-v0.9.3-20260620/prs.md
  - reports/lingtai-release-v0.9.3-20260620/release-body.md
  - reports/lingtai-release-v0.9.3-20260620/release-log.html
  - reports/lingtai-release-v0.9.3-20260620/release_version.txt
  - reports/lingtai-release-v0.9.3-20260620/raw/commits.tsv
  - reports/lingtai-release-v0.9.3-20260620/raw/diffstat.txt
  - reports/lingtai-release-v0.9.3-20260620/raw/pr_numbers.txt
  - reports/lingtai-release-v0.9.3-20260620/raw/prs.json
  - reports/lingtai-release-v0.9.3-20260620/raw/shortstat.txt
  - reports/lingtai-release-v0.9.3-20260620/raw/validation.log
  - reports/lingtai-release-v0.9.4-20260621/candidate_head.txt
  - reports/lingtai-release-v0.9.4-20260621/commits.md
  - reports/lingtai-release-v0.9.4-20260621/previous_tag.txt
  - reports/lingtai-release-v0.9.4-20260621/prs.md
  - reports/lingtai-release-v0.9.4-20260621/release-body.md
  - reports/lingtai-release-v0.9.4-20260621/release-log.html
  - reports/lingtai-release-v0.9.4-20260621/release_version.txt
  - reports/lingtai-release-v0.9.4-20260621/raw/diffstat.txt
  - reports/lingtai-release-v0.9.4-20260621/raw/shortstat.txt
  - reports/lingtai-release-v0.9.4-20260621/raw/validation.log
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# reports/

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. This file is part of the repo anatomy tree; update it in the same PR that commits or removes a tracked report.

`reports/` is the deliberate-exception directory. Routine PR explainers and generated HTML land here as **local-only, uncommitted** output (see `CLAUDE.md`); the files tracked in git are the small set that were explicitly promoted to durable evidence — one release-evidence bundle per shipped TUI/portal release, plus a few explainers that repo docs intentionally link. This anatomy exists so those tracked files are navigable and so the tracked/untracked boundary is written down rather than remembered.

## Components

| Path | Role |
|---|---|
| `lingtai-release-v0.9.3-20260620/`, `lingtai-release-v0.9.4-20260621/` | Per-release evidence bundles, named `lingtai-release-<tag>-<YYYYMMDD>`. Each holds the published `release-body.md`, the human-readable `commits.md` / `prs.md`, the exact `release_version.txt` / `previous_tag.txt` / `candidate_head.txt` pins that defined the range, and a rendered `release-log.html`. |
| `<bundle>/raw/` | The unedited machine output the bundle above was derived from: `commits.tsv`, `pr_numbers.txt`, `prs.json`, `diffstat.txt`, `shortstat.txt`, and the `validation.log` of the release checks that ran. Kept verbatim so a claim in `release-body.md` can be traced to its source. |
| `graphify-repo-map-20260609.html` | Standalone rendered repository map, referenced from `docs/graphify.md`. |
| `pr338-dev-guide-subskills-explainer.html` | Explainer for the PR #338 dev-guide sub-skills split; committed as the durable rationale for that restructure. No repo doc links it today. |
| `postman-retirement-20260718/index.html` | The Postman-retirement decision record — kept as the durable rationale for a removed subsystem. No repo doc links it today. |

## Connections

- **`RELEASING.md`** owns the release *process*; a bundle here is the evidence that one specific run of that process happened, not a second copy of the procedure.
- **`docs/ANATOMY.md`** states the same boundary from the docs side: routine `reports/*.html` is local-only; long-lived material belongs under `docs/`.
- **`CLAUDE.md`** is where the working rule lives for agents: write explainers to an ignored path and hand the human an absolute path; commit here only when it is deliberate long-term/release documentation, explicitly requested, or intentionally linked by repo docs — with `git add -f` and a reason in the PR.
- Nothing in `tui/` or `portal/` reads this directory. It ships in no binary.

## Composition

- **Parent:** repo root (`ANATOMY.md`)
- **Subfolders:** one directory per promoted bundle; release bundles additionally carry `raw/`.

## State

- Append-only in practice: a bundle records what was true at one release and is not edited afterward. Correct a mistaken claim in a new document rather than by rewriting evidence.

## Notes

- **The default is "do not commit."** Most of what is written here at runtime is ignored. Adding a tracked file to `reports/` is an explicit decision that needs a reason in the PR body, and it must be listed in this anatomy's `related_files` — the repo-wide no-orphan rule (root `ANATOMY.md`, `## Anatomy convention`) applies to promoted reports exactly like source.
- **Bundle naming is the index.** `lingtai-release-<tag>-<YYYYMMDD>` is what makes a bundle findable without a manifest; keep the shape when adding one.
- **`raw/` is verbatim.** Do not reformat, prune, or regenerate it — its whole value is being the untouched output the summary was built from.
