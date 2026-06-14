---
name: dev-guide-releasing
description: >
  Compact lingtai-dev-guide release overview: when you are doing a release, the maintainer-authorization boundary, the version scheme, and a pointer to release-workflow for the full TUI/Portal + kernel publishing checklist, GitHub/PyPI/Homebrew steps, the required HTML release log, and the website release blog.
version: 2.0.0
---

# Releasing — Overview

Nested lingtai-dev-guide reference. Read this after the top-level router sends you
here. This page is the compact entry point; the full procedure lives in
`../release-workflow/SKILL.md`.

## When this applies

You are cutting or publishing a LingTai release across one or more of:

- TUI/Portal repo: `Lingtai-AI/lingtai` (tags, GitHub release, Homebrew tap)
- Kernel repo: `Lingtai-AI/lingtai-kernel` (PyPI package `lingtai`)
- Optional first-party addon repos (e.g. `Lingtai-AI/lingtai-telegram`)
- Website/release-log repo: `lingtai-web`

## Boundary: a checklist is not permission

Pushing tags, creating GitHub releases, uploading to PyPI, committing to the
Homebrew tap, and deploying website copy are external side effects. **Proceed
only after explicit maintainer authorization.** If Jason says “直接 release / 先发 /
publish it” (or equivalent), do the release and send short progress updates
during long gates. Never print or share secrets/credentials. Do not let the
website release log block an authorized software release unless Jason says the
blog must land first.

## Version scheme

- **TUI/Portal:** semantic versioning (`vX.Y.Z`); tags on the `lingtai` repo.
  Homebrew builds from source — no binary assets needed.
- **Kernel:** semantic versioning; published to PyPI as `lingtai`; version in
  `pyproject.toml`. PyPI uploads are immutable — never reuse a published version.
- **Migration versions:** integer counter in `meta.json`, tracked separately for
  per-project (TUI+portal shared) and per-machine (global) migrations.

## Go to the full checklist

`../release-workflow/SKILL.md` owns the complete, command-level procedure:

- establishing scope and candidate heads;
- clean release worktrees;
- TUI/Portal and kernel validation gates;
- GitHub releases, PyPI (`twine`) upload, and Homebrew tap update;
- final public-surface verification and the maintainer report;
- the **required shareable HTML release log** (per-release, self-contained) and
  its validation, plus `../release-html-log-template.html`;
- the **website release blog** and its reusable `../release-workflow/assets/release-blog-template.md`.

Start there for any real publish operation.
