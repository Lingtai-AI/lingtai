# LingTai Release Blog / GitHub Release Template

This template is for **LingTai's real release surfaces**, not for a standalone
review memo. Before drafting, inspect the current website and GitHub Releases.
The 2026-06-13 style anchor is:

- Website release logs live in `lingtai-web/src/data/releases.ts` and render
  through `ReleaseDetail.astro`.
- The public page calls the main body **"New features and improvements"** and
  renders `summary`, `Upgrade / runtime`, feature cards, `Why it matters`,
  contributors, validation, and links.
- GitHub Releases use shorter Markdown notes: `Highlights`, sometimes
  `Release-log coverage` / `Also included since ...`, `Validation`,
  `Contributors`, and `Compare`.

Do not invent a new HTML report style when Jason asks for a release blog. Use the
existing `releases.ts` data shape and the recent GitHub release-note rhythm.

## 0. Publishing boundary

- [ ] Human explicitly asked for a release blog/log draft or publication.
- [ ] If only a draft was requested, stop after local artifact / preview. Do
      **not** push website changes, deploy, tag, publish PyPI, edit Homebrew, or
      merge PRs.
- [ ] If publication was authorized, record the exact wording and still
      build/preview before pushing website changes.
- [ ] Public copy contains no local paths, secrets, raw logs, Telegram/mail IDs,
      or private agent/worktree chatter.
- [ ] The normal user upgrade path remains Homebrew/TUI-managed project runtime;
      do not present bare `pip install/upgrade lingtai` as the ordinary end-user
      path unless Jason explicitly changes that positioning.

## 1. Read the real style anchors first

Run this before writing prose:

```bash
WEB_REPO=/path/to/lingtai-web
cd "$WEB_REPO"
git fetch origin main --prune
sed -n '1,260p' src/data/releases.ts
sed -n '1,220p' src/components/ReleaseDetail.astro

gh release list --repo Lingtai-AI/lingtai --limit 8
gh release view v0.9.0 --repo Lingtai-AI/lingtai --json name,tagName,body,url
gh release view v0.8.16 --repo Lingtai-AI/lingtai --json name,tagName,body,url
gh release list --repo Lingtai-AI/lingtai-kernel --limit 8
```

When reading, extract these facts into notes:

```yaml
style_anchors:
  website_entries_seen:
    - id: "20260613-1"
      versions: "v0.9.0 / v0.12.0"
      feature_count: 8
      theme: "full release-window log, operational quality under real load"
    - id: "20260609-1"
      versions: "v0.8.15 / v0.11.3"
      feature_count: 4
      theme: "runtime cockpit, safer agent operations, teachable skills"
  github_release_notes_seen:
    - tag: "v0.9.0"
      sections: ["Highlights", "Release-log coverage", "Validation", "Contributors"]
    - tag: "v0.8.16"
      sections: ["Highlights", "Also included since v0.8.15", "Validation", "Compare"]
    - tag: "v0.8.15"
      sections: ["Highlights", "User-visible changes", "Skills, docs, and packaged guidance", "Validation"]
```

If these anchors changed, adapt the template to the current files instead of
using this page from memory.

## 2. Decide the release-blog mode

Choose exactly one:

- `small-patch`: strict post-tag delta is small; the blog should look like a
  focused patch release and may use an `Also included/context` section.
- `full-release-window`: the public log intentionally spans the previous
  published release log to now; include broad repo/PR/issue/contributor counts.
- `retrospective`: no new public version; write as a narrative, not a release.

Required distinction for `small-patch`:

1. **Strict new delta** — changes after the latest public tag(s). These may be
   claimed as new in this candidate.
2. **Same-window context** — related work already covered by an earlier tag or
   release log. Mention only if it helps explain the release; label it as
   context or "also included/background", not as new post-tag work.

Recommended wording:

- zh: `这是一篇小版本 release log；严格新 delta 是……。同一工作窗口中已经在上一轮 tag/release log 覆盖的……只作为背景，不作为本 tag 后的新变化。`
- en: `This is a small release log. The strict new delta is …; related same-window work already covered by the previous tag/release log is included only as context, not as new post-tag change.`

## 3. Required release facts block

Fill this before drafting website or GitHub copy. Use `TBD` rather than guessing.

```yaml
release_blog:
  slug: "YYYYMMDD-N"
  date: "YYYY-MM-DD"
  mode: "small-patch | full-release-window | retrospective"
  public_versions:
    tui_portal: "vX.Y.Z | none"
    kernel: "vA.B.C | none"
    addons:
      - repo: "lingtai-telegram"
        version: "vX.Y.Z | context-only | none"
  candidate_heads:
    lingtai: "tag or full sha"
    lingtai-kernel: "tag or full sha"
    addons: []
  baselines:
    website_release_log_baseline: "previous public release log id/tag/date"
    strict_tag_ranges:
      - repo: "lingtai"
        range: "vX..vY or vX..origin/main"
        commits: 0
        files_changed: 0
        insertions: 0
        deletions: 0
      - repo: "lingtai-kernel"
        range: "vX..vY or vX..origin/main"
        commits: 0
        files_changed: 0
        insertions: 0
        deletions: 0
    context_ranges:
      - repo: "optional"
        range: "range already covered or context-only"
        reason: "why it belongs in background/context"
  validation:
    - label: "TUI tests/build"
      result: "passed | skipped with reason | TBD"
    - label: "Kernel tests/build"
      result: "passed | skipped with reason | TBD"
    - label: "Website build"
      result: "passed | skipped because draft only | TBD"
  contributors:
    - "Name or handle"
  public_links:
    - label: "TUI release"
      href: "https://github.com/Lingtai-AI/lingtai/releases/tag/vX.Y.Z"
    - label: "Kernel release"
      href: "https://github.com/Lingtai-AI/lingtai-kernel/releases/tag/vA.B.C"
  audit_artifacts:
    report_dir: "/local/private/report/dir"
```

## 4. Website `src/data/releases.ts` entry shape

Use the current website schema after inspecting it. As of 2026-06-13 it is:

```ts
const release_YYYYMMDD_N: Release = {
  id: 'YYYYMMDD-N',
  version: 'vX.Y.Z / vA.B.C',
  titleEn: 'LingTai TUI/Portal vX.Y.Z + Kernel vA.B.C',
  titleZh: '灵台 TUI/Portal vX.Y.Z 与内核 vA.B.C',
  date: 'YYYY-MM-DD',
  pkg: 'lingtai-tui + lingtai',
  tag: 'vX.Y.Z / vA.B.C',
  install: 'brew update && brew upgrade lingtai-ai/lingtai/lingtai-tui',
  runtimeNoteEn:
    'Explain the runtime/upgrade path and whether this is a paired TUI/kernel log. Keep pip language diagnostic/developer-only unless instructed otherwise.',
  runtimeNoteZh:
    '说明 runtime/升级路径，以及这是否是 TUI/kernel 配对日志。除非另有指示，pip 语言只用于诊断/开发者语境。',
  summaryEn:
    'One long, conclusion-first paragraph in the established release-log voice: scope, counts/ranges if relevant, main user/operator theme, and what changed.',
  summaryZh:
    '一段结论先行的中文摘要：范围、必要的 counts/ranges、用户/操作者主线，以及变化。事实必须和英文一致。',
  features: [
    {
      titleEn: 'Theme title, not a PR title',
      titleZh: '主题标题，不是 PR 标题',
      leadEn: 'One paragraph explaining the theme in user/operator language.',
      leadZh: '用用户/操作者语言解释这个主题。',
      bulletsEn: [
        'Concrete behavior or shipped artifact, with PR/tag details only when useful.',
        'Validation or scope detail if it helps the reader trust the claim.',
      ],
      bulletsZh: [
        '具体行为或交付物；PR/tag 只在有助阅读时出现。',
        '必要时加入验证或范围细节。',
      ],
      whyEn: 'One sentence/paragraph: why this matters to users/operators.',
      whyZh: '一句或一段：说明为什么这对用户/操作者重要。',
    },
  ],
  contributors: ['...'],
  validation: {
    commit: 'full release validation commit sha, or omit for draft',
    items: [
      { label: 'TUI diff check', result: 'passed' },
      { label: 'TUI Go tests', result: 'passed' },
      { label: 'Kernel tests/build', result: 'passed' },
      { label: 'Website build', result: 'passed' },
    ],
  },
  links: [
    { label: 'Previous release log', href: 'https://lingtai.ai/releases/...' },
    { label: 'TUI/Portal GitHub release', href: 'https://github.com/Lingtai-AI/lingtai/releases/tag/vX.Y.Z' },
    { label: 'Kernel GitHub release', href: 'https://github.com/Lingtai-AI/lingtai-kernel/releases/tag/vA.B.C' },
    { label: 'TUI compare', href: 'https://github.com/Lingtai-AI/lingtai/compare/vX...vY' },
  ],
};
```

Website style rules from past entries:

- The `summary` is a substantive release-log paragraph, not a teaser.
- `features` are themes such as "TUI becomes an operator cockpit", "Setup,
  presets, manifests... are safer", or "Release hygiene and packaging...".
  Avoid one feature per commit unless the release is tiny.
- Every feature has `lead`, concrete `bullets`, and `why`.
- Small releases usually need 3–5 features; full release-window logs may need
  6–8.
- `validation.items` can be long and auditable; prefer exact command names and
  results over vague "tested".
- `links` should include previous release log/compare/release/PyPI/Homebrew as
  applicable.
- Newest entries go at the top of the exported `releases` array.

## 5. GitHub release-note shape

For `Lingtai-AI/lingtai` GitHub Releases, use Markdown close to recent tags.

### Full / broad release

```md
## Highlights

- **Major runtime-visibility release:** one concrete, user-facing sentence.
- **Noise-controlled tool replay:** one concrete, user-facing sentence.
- **Setup/preset safety:** one concrete, user-facing sentence.

## Release-log coverage

Explain if the website release log is broader than the last tag window and name
the paired kernel/addon scope.

## Validation

Run from clean release worktree at commit `<sha>`:

- `git diff --check ...` — passed
- `(cd tui && go test -count=1 ./...)` — passed
- `(cd portal/web && npm ci && npm run build)` — passed; include known audit notes

## Contributors

- handle/name
- co-author/model/automation when materially present
```

### Small patch

```md
## Highlights

- **Critical/focused fix or addition:** strict post-tag delta in one sentence.
- **Automatic remediation / operational effect:** if relevant.
- **Small GitHub patch release:** say what this tag publishes on top of the previous tag.

## Also included since vX.Y.Z

- Context bullets that are in the shipped tag range but not the strict headline.

## Validation

Release candidate: `<sha>` (`origin/main`).

Checks run from clean detached worktree `<path>`:

- `git diff --check HEAD` ✅
- `cd tui && go test ./...` ✅

Known note: include non-blocking audit/flaky notes if any.

## Compare

- Range: `vX...vY`
- Commits: N
- Files changed: N (`+A / -D`)
- Previous tag: `vX`
- Target commit: `<sha>`
```

For kernel releases, the recent style can be more direct:

```md
# LingTai kernel vA.B.C — short theme

One paragraph: what changed and whether it is breaking/pre-1.0 cleanup.

## Changes

- Concrete change bullets.

Most users need no manual action... (if true)
```

## 6. Drafting checklist

- [ ] I inspected current `lingtai-web/src/data/releases.ts`, not a stale local
      memory of it.
- [ ] I inspected at least two recent GitHub Releases, including the URL/tag the
      human pointed at when applicable.
- [ ] I chose `small-patch | full-release-window | retrospective` explicitly.
- [ ] Strict post-tag delta is separated from same-window context.
- [ ] Website entry uses `summary` + themed `features` + `why` + `validation` +
      `links`, not a standalone report layout.
- [ ] GitHub release notes use the established Markdown sections.
- [ ] Chinese and English fields carry the same facts.
- [ ] Public copy has no local paths, private chat/mail IDs, secrets, or internal
      agent apology/process chatter.
- [ ] Draft/publication boundary is explicit; no website push/deploy/tag/PyPI/
      Homebrew action without authorization.
