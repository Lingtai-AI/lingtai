---
name: dev-guide-release-workflow
description: >
  Nested lingtai-dev-guide reference for consequential LingTai releases: paired
  TUI/Portal + kernel release planning, clean worktrees, validation gates,
  GitHub/PyPI/Homebrew publishing boundaries, website release-log/blog drafting,
  and the reusable release blog template.
version: 1.0.0
---

# Release Workflow

Nested lingtai-dev-guide reference. Read this after the top-level router sends
you here for release preparation, release publication, or release blog work.

Use this workflow for releases spanning:

- TUI/Portal repo: `Lingtai-AI/lingtai`
- Kernel repo: `Lingtai-AI/lingtai-kernel`
- Optional first-party addon repos, e.g. `Lingtai-AI/lingtai-telegram`
- Website/release-log repo: `lingtai-web`
- PyPI package: `lingtai`
- Homebrew tap: `Lingtai-AI/homebrew-lingtai`

This is a release checklist, **not permission**. Publishing tags, GitHub
releases, PyPI packages, website commits/deploys, or Homebrew tap commits are
external side effects: proceed only after explicit maintainer authorization. If
Jason says “直接 release / 先发 / publish it” or equivalent, do the release and
send progress updates during long steps.

Important release-log rule from 2026-06-13: **do not let the website release log
block the actual software release** unless Jason explicitly says the blog must
land first. When authorized to release, publish the versions after gates pass;
then finish the full release log as a separate reviewable artifact.

## 0. Communication discipline

1. Read the latest human thread on the channel where it arrived.
2. Acknowledge quickly before long gates/builds.
3. For long-running work, send short progress updates.
4. Keep exact local paths, commit SHAs, release URLs, PyPI/Homebrew verification,
   and caveats in durable notes.
5. Do not share secrets or local credentials.

## 1. Establish scope and candidate heads

1. Fetch repos and tags:
   ```bash
   cd /path/to/lingtai && git fetch origin main --tags --prune
   cd /path/to/lingtai-kernel && git fetch origin main --tags --prune
   # Optional related repos
   cd /path/to/lingtai-telegram && git fetch origin main --tags --prune
   ```
2. Identify previous tags and current candidates:
   ```bash
   git -C /path/to/lingtai tag --sort=-v:refname | head
   git -C /path/to/lingtai-kernel tag --sort=-v:refname | head
   git -C /path/to/lingtai rev-parse origin/main
   git -C /path/to/lingtai-kernel rev-parse origin/main
   ```
3. State whether this is:
   - a small patch / maintenance release;
   - a full release-window log;
   - a retrospective/non-versioned blog;
   - or a real publish operation involving tags/PyPI/Homebrew/website deploy.

Record the exact candidate tuple before running gates, e.g.:

```text
TUI/Portal: v0.9.1 candidate @ <lingtai sha>
Kernel: v0.12.2 candidate @ <lingtai-kernel sha>
Addons: context-only unless explicitly included
```

## 2. Use clean release worktrees

Do not release from a dirty feature branch or an old worktree with unrelated
changes.

Recommended shape:

```bash
REPO=/Users/.../lingtai
BR=release-vX.Y.Z-YYYYMMDD
WT="$REPO/.worktrees/$BR"
git -C "$REPO" fetch origin main --tags --prune
git -C "$REPO" worktree add -b "$BR" "$WT" origin/main

git -C "$WT" status --short --branch
```

Rules:

- Release worktrees start from the intended release head.
- Keep TUI/Portal, kernel, website, and tap changes in the correct repos.
- If you must amend code during release validation, commit it before tagging.
- Before push/tag/publish, show the human the final candidate refs when approval
  wording is ambiguous.

## 3. TUI/Portal gates

### 3.1 Diff-check and known whitespace pitfalls

Before tests:

```bash
cd /path/to/lingtai-release-worktree
git diff --check
git status --short --branch
```

Known pitfalls:

- `tui/internal/tui/stars.csv` may use CRLF intentionally. Do not “normalize” it
  accidentally while editing unrelated files.
- Portal tests may require frontend assets to be built first.
- If changing utility skills, update nested-reference tests and any root router
  catalog entries in the same commit.

### 3.2 Tests and builds

Run targeted tests for the changed area, then broader gates appropriate to the
release:

```bash
cd /path/to/lingtai-release-worktree/tui
go test ./internal/tui ./internal/preset
# For release candidates or broad TUI work:
go test ./...

# After frontend changes or before portal validation:
npm --prefix portal/web ci
npm --prefix portal/web run build
go test ./portal/...
```

If a known flaky test fails, rerun the specific test and record both the initial
failure and the rerun result. Do not hide flakiness.

## 4. Kernel gates

### 4.1 Version bump

For real kernel package releases:

- update package version in the canonical project metadata;
- ensure changelog/release notes match the published version;
- verify the tag does not already exist locally or remotely.

```bash
cd /path/to/lingtai-kernel-release-worktree
git fetch origin main --tags --prune
git tag -l 'vX.Y.Z'
git ls-remote --tags origin 'vX.Y.Z'
```

### 4.2 Tests and build

Run focused tests, then packaging gates:

```bash
cd /path/to/lingtai-kernel-release-worktree
python -m pytest -q
python -m build
python -m twine check dist/*
```

For curated MCP/addon layout changes, also verify wheel contents and a clean-venv
import path before publishing.

## 5. GitHub Releases, PyPI, and Homebrew

Only perform this section after explicit maintainer authorization.

TUI/Portal release outline:

```bash
cd /path/to/lingtai-release-worktree
git tag -a vX.Y.Z -m 'LingTai TUI/Portal vX.Y.Z'
git push origin HEAD:main
git push origin vX.Y.Z
gh release create vX.Y.Z --title 'LingTai TUI/Portal vX.Y.Z' --notes-file /path/to/notes.md
```

Kernel release outline:

```bash
cd /path/to/lingtai-kernel-release-worktree
git tag -a vA.B.C -m 'lingtai vA.B.C'
git push origin HEAD:main
git push origin vA.B.C
python -m twine upload dist/*
gh release create vA.B.C --title 'lingtai vA.B.C' --notes-file /path/to/notes.md
```

Homebrew outline:

```bash
# Get the source tarball checksum for the pushed TUI tag.
curl -L -o /tmp/lingtai-tui-vX.Y.Z.tar.gz \
  https://github.com/Lingtai-AI/lingtai/archive/refs/tags/vX.Y.Z.tar.gz
shasum -a 256 /tmp/lingtai-tui-vX.Y.Z.tar.gz

# Edit Formula/lingtai-tui.rb or lingtai-tui.rb: url + sha256.
# Then run brew audit/test/install as appropriate for the tap workflow.
```

## 6. Final release verification and maintainer report

Verify public surfaces before declaring done:

```bash
gh release view vX.Y.Z --repo Lingtai-AI/lingtai
gh release view vA.B.C --repo Lingtai-AI/lingtai-kernel
python -m pip index versions lingtai | head
brew update
brew info lingtai-ai/lingtai/lingtai-tui
```

Report to the maintainer with:

- final versions and commit SHAs;
- tags and release URLs;
- PyPI/Homebrew verification result;
- tests/builds run;
- known caveats/flaky tests and reruns;
- website blog status (drafted / previewed / published / intentionally deferred).

## 7. Website release log / blog

Do this when requested, but remember: the blog should not block an authorized
software release unless Jason says so.

### 7.1 Always inspect the live website structure first

Do **not** assume `src/content/blog/*.md` or `src/data/releases.ts` from memory.
The current site structure can change. Inspect the checkout before editing:

```bash
WEB_REPO=/path/to/lingtai-web
cd "$WEB_REPO"
git fetch origin main --prune
find src -maxdepth 3 -type f | sort | sed -n '1,200p'
git ls-tree -r --name-only origin/main | grep -E 'release|blog|data|astro|mdx?|ts$' | sort | sed -n '1,200p'
```

Then read the most recent published release entry/page. If Jason references a
page such as `https://lingtai.ai/zh/releases/20260609-1/`, use it as the style
and scope anchor. Mirror its structure, tone, and multilingual behavior.

### 7.2 Determine the release-window baseline from the previous published log

Do not just compare previous tag to current tag. First identify what the previous
public release log covered, then collect from that baseline to now.

For each involved repo, determine:

- previous release-log baseline tag/commit/date;
- current release tag/head;
- whether the repo should be included even if no new tag was cut.

Record the chosen ranges explicitly, e.g.:

- `lingtai v0.8.15..v0.9.0`
- `lingtai-kernel v0.11.3..v0.12.0`
- `lingtai-telegram v0.3.0..origin/main`

### 7.3 Collect commits, LOC, PRs, issues, and contributors

For each repo/range collect:

```bash
cd /path/to/repo
git log --oneline <base>..<head> | wc -l
git diff --shortstat <base>...<head>
git log --format='%H%x1f%an%x1f%ae%x1f%s%x1e%b' <base>..<head> > /tmp/repo-commits.raw
```

Co-author parsing must use robust record separators. Do not rely on simple
line-by-line parsing of `git log` body; it undercounts `Co-authored-by` entries.

Collect GitHub participation, not only merged code:

```bash
gh pr list --state all --base main --search 'updated:>=YYYY-MM-DD' \
  --json number,title,state,author,createdAt,updatedAt,closedAt,mergedAt,url

gh issue list --state all --search 'updated:>=YYYY-MM-DD' \
  --json number,title,state,author,createdAt,updatedAt,closedAt,url,comments
```

Contributor inclusion rule for release logs:

- commit authors;
- co-authors from commit trailers;
- merged PR authors;
- closed unmerged PR authors, when part of the release-window discussion/work;
- closed issue reporters and meaningful participants, even if the idea was not
  adopted;
- automation/bots when they materially changed release artifacts.

Keep raw JSON/script outputs under a report directory so the contributor list is
auditable.

### 7.4 Draft both zh and en

The release log must include Chinese and English versions when the site supports
both. If the site stores release entries as data (e.g. `src/data/releases.ts`),
update the localized fields there. If it stores pages as Markdown/Astro content,
create matching zh/en content files. **Inspect first.**

Substantial release logs should be conclusion-first and concrete:

- State the whole release window, not just one tag.
- Include commit count, files changed, LOC added/removed, PR/issue counts.
- Explain user-visible behavior, not just PR titles.
- Include contributors comprehensively and safely.
- Mention validation/release hygiene as part of delivery.

Suggested section shape for a large cockpit/kernel release:

1. This is a release window, not a narrow patch note.
2. TUI/Portal user-facing changes.
3. Setup/preset/manifest/install safety.
4. Kernel observability/tool execution/runtime governance.
5. Daemon/avatar/soul/idle-care or long subprocess operations.
6. Knowledge/skills/tutorial/research workflow teaching material.
7. MCP/chat/email/addon integration reliability.
8. Release hygiene, packaging, validation, and thanks.

### 7.5 Use the reusable release-blog template

When the blog is more than a one-line changelog, start from the template asset:

- `assets/release-blog-template.md`

The template is anchored to LingTai's real release surfaces, not a standalone
review-report style. It forces these decisions before prose starts:

- inspect the current `lingtai-web/src/data/releases.ts` and
  `ReleaseDetail.astro` before drafting;
- inspect recent GitHub Releases, especially the release URL/tag the maintainer
  points at;
- choose whether the entry is a `small-patch`, `full-release-window`, or
  `retrospective`;
- record exact public versions, candidate heads, baselines, and repo ranges;
- keep strict post-tag delta separate from same-window foundation/context;
- write the website entry in the existing `summary` + themed `features` + `why`
  + `validation` + `links` data shape;
- write GitHub release notes in the established `Highlights` / `Validation` /
  `Compare` rhythm;
- keep bilingual zh/en field parity and public/private copy checks.

For small patch blogs, do not inflate earlier work into a new-delta claim. Put
newly merged post-tag changes in the strict-delta section, and list older
same-window work only as context. For full release-window logs, do the broader
audit in 7.2–7.3 and include contributor coverage beyond commit authors.

### 7.6 Build and preview, but do not deploy without approval

Run the website build locally. Provide self-contained or otherwise easy-to-open
previews if Jason needs review.

```bash
cd "$WEB_REPO"
npm ci
npm run build
```

Do not push website changes, deploy, or publish the blog until Jason explicitly
approves.

## 8. After release

1. Update durable notes with final release state.
2. If the release workflow exposed reusable pitfalls, update this subskill
   immediately.
3. Clean old worktrees later, not during the critical publish window unless
   necessary.
4. If Jason asks for a repo cleanup proposal rather than direct edits, use a
   read-only daemon/Claude Code first and report its proposal before touching
   code.
