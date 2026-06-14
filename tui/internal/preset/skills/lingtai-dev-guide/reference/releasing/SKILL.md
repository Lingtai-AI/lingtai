---
name: dev-guide-releasing
description: >
  Nested lingtai-dev-guide reference for release work: required HTML release logs, TUI/portal tagging and Homebrew tap updates, kernel clean-worktree releases, PyPI upload, verification, and post-release hygiene.
version: 1.0.0
---

# Release Process


Nested lingtai-dev-guide reference. Read this after the top-level router sends you here.

> For consequential paired TUI/Portal + kernel releases, website release-log/blog work, or any release train involving GitHub/PyPI/Homebrew plus public website copy, read `reference/release-workflow/SKILL.md` after this compact procedure. It owns the full checklist and reusable release blog template.

## Shareable HTML release log (required for every public release)

Every public LingTai release must produce a polished, self-contained HTML release log before the final human report. This applies to all public release surfaces: the TUI/portal Homebrew release from the `lingtai` repo, the kernel `lingtai` package on PyPI, and first-party MCP/addon packages. Treat the HTML file as the canonical external-facing changelog artifact for the release: it should be ready for Jason or another maintainer to send to users, investors, or collaborators without needing extra context from the agent transcript.

Use the recent `lingtai 0.10.8` PyPI log as the model. At minimum, the HTML release log must include:

- **Executive summary:** one concise paragraph saying what shipped and why it matters.
- **Release metadata:** repo, package/binary name, version, tag, release branch or PR, main/release commit, release date/time, and publisher/operator.
- **What changed:** grouped bullets for the user-visible changes, developer-facing changes, and docs/process changes included in the release.
- **Validation:** tests, linters, build checks, clean-install checks, Homebrew/PyPI/GitHub verification, and any intentionally skipped noisy suites.
- **Artifacts:** links plus hashes/sizes when applicable (PyPI wheel/sdist, GitHub release tarball checksum, Homebrew formula commit, downloadable assets).
- **Operator notes and risks:** known caveats, propagation delays, rollback/retry notes, compatibility warnings, or follow-up work that should not block the release.
- **Next steps:** the exact user/maintainer actions that remain, or an explicit “none” if the release is complete.

Format rules:

- Keep it **self-contained**: inline CSS, no remote fonts, scripts, or assets, and no dependency on local paths.
- Keep it **shareable**: write for an external reader, not for an agent; do not include secrets, raw private paths unless they are public repo paths, or internal message IDs.
- Keep it **verifiable**: every version/tag/hash/test count in the report must come from commands run during the release.
- Save the file under the releasing repo's `reports/` directory with a descriptive name, for example `reports/lingtai-0.10.8-release-log.html`.
- Use `../release-html-log-template.html` as the starter skeleton when you do not already have a stronger release-specific design.

Before announcing completion, validate the log itself:

```bash
python - <<'PY'
from html.parser import HTMLParser
from pathlib import Path
path = Path('reports/<release-log>.html')
data = path.read_text(encoding='utf-8')
data.encode('utf-8')
class Parser(HTMLParser):
    pass
Parser().feed(data)
print('bytes:', path.stat().st_size)
print('control chars:', sum(1 for c in data if ord(c) < 32 and c not in '\n\r\t'))
PY
```

Attach or send the HTML release log to Jason on the same channel as the release request, then include its path and validation result in the final release report.

## Releasing the TUI and Portal (`lingtai` repo)

### 1. Commit and push all changes

```bash
cd ~/Documents/GitHub/lingtai
git push origin main
```

### 2. Tag the release

```bash
git tag v0.X.Y
git push origin v0.X.Y
```

### 3. Create the GitHub release

```bash
gh release create v0.X.Y --title "v0.X.Y" --notes "release notes here..."
```

No binary assets needed — Homebrew builds from source, Linux users build locally.

### 4. Write release notes and the human-facing release log

Every TUI/portal release should have two written artifacts:

1. **GitHub Release notes** — short, public, and user-facing. These are pasted into `gh release create` with `--notes-file` or edited on the GitHub Release page. Use this structure:

   ```markdown
   ## Highlights

   - User-visible feature or fix, phrased as an outcome.
   - Another high-impact change.

   ## Skills, docs, and packaged guidance

   - Shipped skill/procedure/doc updates that change how agents or developers work.

   ## Validation

   Release candidate: `<full commit>` (`upstream/main`).

   Checks run from a clean detached release worktree:

   - `cd tui && go test ./...` ✅
   - `cd tui && make clean && make build && ./bin/lingtai-tui version` ✅
   - `cd portal/web && npm ci && npm run build` ✅
   - `cd portal && go test ./...` ✅ after building `web/dist` for embedded assets
   - `cd portal && make clean && make build` ✅

   Known note: `<anything users/maintainers should know, or “none.”>`
   ```

2. **Human-facing HTML release log** — longer, reviewable, and shareable with the maintainer before publishing. Generate it under the agent/report workspace (for example `reports/lingtai-tui-v0.X.Y-release.html`) and send the absolute path to the human. Do **not** attach it to GitHub Release or commit it unless the human explicitly asks. Use one self-contained HTML file (inline CSS, no remote assets) with these sections:

   - **Summary / 摘要** — whether the release is worth upgrading to, the scope boundary (TUI/portal vs kernel/PyPI), the tag/commit, and any caveats.
   - **Features and fixes / 新功能与修复** — cards grouped by user-visible outcome, with PR/issue links and contributor attribution.
   - **Contributors / 贡献者** — distinguish external/community contributors, maintainer/release work, automation (`github-actions[bot]`), and AI co-author trailers. Do not count bots or AI trailers as human contributors.
   - **Validation / 发布验证** — exact commands run, success/failure, workflow/tap links if already published, and known caveats such as `npm audit` findings.
   - **Commits / 提交明细** — compare range (`v0.X.(Y-1)..v0.X.Y` or `last-release..main`) with commit hash, author, and subject.

Recommended data-gathering commands before writing either artifact:

```bash
gh release view v0.X.(Y-1) --repo Lingtai-AI/lingtai
gh api repos/Lingtai-AI/lingtai/compare/v0.X.(Y-1)...main \
  --jq '{ahead_by,total_commits,commits:[.commits[]|{sha:(.sha[0:7]),message:.commit.message,author:.commit.author.name,date:.commit.author.date}],files:[.files[]|{filename,status,additions,deletions}]}'
gh pr view <PR> --repo Lingtai-AI/lingtai --json number,title,author,mergedAt,mergeCommit,url,body,files
```

### 5. Update the Homebrew tap

```bash
# Get the source tarball checksum
curl -sL "https://github.com/Lingtai-AI/lingtai/archive/refs/tags/v0.X.Y.tar.gz" | shasum -a 256

# Edit the formula
cd $(brew --repository)/Library/Taps/lingtai-ai/homebrew-lingtai
# In lingtai-tui.rb: update the url tag and sha256
git add lingtai-tui.rb
git commit -m "bump lingtai-tui to v0.X.Y"
git push
```

### 6. Verify

```bash
brew update && brew upgrade lingtai-ai/lingtai/lingtai-tui
lingtai-tui version  # should show v0.X.Y
```

### 7. Rebuild dev mode symlinks (if applicable)

After a release, your dev-mode symlinks still point at the old binary. Rebuild both:

```bash
cd ~/Documents/GitHub/lingtai/tui && make build
cd ~/Documents/GitHub/lingtai/portal && make build
```

## Releasing the kernel (`lingtai-kernel` repo)

The kernel is published to PyPI as package `lingtai`. Do not reuse a version that already exists on PyPI; PyPI uploads are immutable. If the current `pyproject.toml` version is already published and there are new commits on `main`, bump to the next patch version first.

### 1. Inspect current state

```bash
cd ~/Documents/GitHub/lingtai-kernel
git status --short --branch
git tag --sort=-v:refname | head
python - <<'PY'
import tomllib, pathlib
obj = tomllib.loads(pathlib.Path('pyproject.toml').read_text())
print(obj['project']['name'], obj['project']['version'])
PY
python -m pip index versions lingtai
```

If `pip index` lags behind immediately after an upload, query PyPI JSON directly:

```bash
python - <<'PY'
import json, urllib.request
obj = json.load(urllib.request.urlopen('https://pypi.org/pypi/lingtai/json'))
print('latest:', obj['info']['version'])
print('files for 0.X.Y:', len(obj['releases'].get('0.X.Y', [])))
PY
```

### 2. Use a clean release worktree

Prefer a clean worktree so unrelated local files do not leak into the release. Avoid `git fetch --tags` if this checkout has historical tag divergence; fetching `origin main` is enough for a patch release from current main.

```bash
cd ~/Documents/GitHub/lingtai-kernel
git fetch origin main
rm -rf .worktrees/release-0.X.Y
git worktree add -b release/0.X.Y .worktrees/release-0.X.Y origin/main
cd .worktrees/release-0.X.Y
```

Bump `pyproject.toml`:

```bash
python - <<'PY'
from pathlib import Path
p = Path('pyproject.toml')
s = p.read_text()
p.write_text(s.replace('version = "0.X.(Y-1)"', 'version = "0.X.Y"', 1))
PY
git diff -- pyproject.toml
```

### 3. Run focused release checks

At minimum, rerun the tests that cover the changes in the release plus metadata/build checks. For notification/manifest releases this looked like:

```bash
python -m ruff check \
  src/lingtai_kernel/intrinsics/soul/inquiry.py \
  tests/test_notification_sync.py \
  tests/test_agent_preset_manifest.py
pytest -q \
  tests/test_agent_preset_manifest.py \
  tests/test_notification_sync.py \
  tests/test_layers_email.py::test_email_receive_notification \
  tests/test_agent.py::test_mail_inbox_wiring -q
```

If you know the full suite is currently noisy/crashy, state that in the release report and use the focused suite that covers the release's touched behavior.

### 4. Build clean artifacts

Remove ignored build byproducts and `__pycache__` before the final build. Otherwise setuptools may warn about `__pycache__` and, in bad cases, include stale `.pyc` files as package data.

```bash
find src -type d -name __pycache__ -prune -exec rm -rf {} +
rm -rf dist build *.egg-info src/*.egg-info
python -m build
python -m twine check dist/*
```

Confirm artifact metadata and that no pycache entries are packaged:

```bash
python - <<'PY'
import pathlib, tarfile, zipfile
for p in sorted(pathlib.Path('dist').iterdir()):
    print('artifact', p.name, p.stat().st_size)
    if p.suffix == '.whl':
        with zipfile.ZipFile(p) as z:
            names = z.namelist()
            print('pycache_count', sum('__pycache__' in n for n in names))
            meta = [n for n in names if n.endswith('METADATA')][0]
            text = z.read(meta).decode()
            print('\n'.join(line for line in text.splitlines() if line.startswith(('Name:', 'Version:'))))
    elif p.name.endswith('.tar.gz'):
        with tarfile.open(p) as t:
            names = t.getnames()
            print('pycache_count', sum('__pycache__' in n for n in names))
            member = [m for m in t.getmembers() if m.name.endswith('PKG-INFO')][0]
            text = t.extractfile(member).read().decode()
            print('\n'.join(line for line in text.splitlines() if line.startswith(('Name:', 'Version:'))))
PY
```

### 5. Commit, tag, and push

Commit the version bump, push a release branch and tag, then fast-forward `main`. If `main` is checked out in another worktree, do the `main` fast-forward from that main worktree.

```bash
git add pyproject.toml
git commit -m 'chore: bump version to 0.X.Y'
git push -u origin release/0.X.Y
git tag v0.X.Y
git push origin v0.X.Y
```

Then, from the main worktree if needed:

```bash
cd ~/Documents/GitHub/lingtai-kernel
git fetch origin main release/0.X.Y
git checkout main
git merge --ff-only release/0.X.Y
git push origin main
git ls-remote origin refs/heads/main refs/tags/v0.X.Y
```

### 6. Upload to PyPI

Use the configured Twine credentials (usually `~/.pypirc`, never print secrets). Check the version is still absent, then upload:

```bash
python - <<'PY'
import json, urllib.request
obj = json.load(urllib.request.urlopen('https://pypi.org/pypi/lingtai/json'))
print('latest:', obj['info']['version'])
print('0.X.Y files:', len(obj['releases'].get('0.X.Y', [])))
PY
python -m twine upload dist/*
```

### 7. Verify the published package

PyPI JSON usually updates before `pip index`, which can lag. Verify with JSON, then install from PyPI in a clean venv:

```bash
python - <<'PY'
import json, time, urllib.request
for i in range(12):
    obj = json.load(urllib.request.urlopen('https://pypi.org/pypi/lingtai/json'))
    files = obj['releases'].get('0.X.Y', [])
    print('attempt', i + 1, 'latest', obj['info']['version'], 'files', len(files))
    if files:
        for f in files:
            print(f['filename'], f['packagetype'], f['size'], f['upload_time_iso_8601'])
        break
    time.sleep(5)
PY

TMPVENV=$(mktemp -d)
python -m venv "$TMPVENV/venv"
"$TMPVENV/venv/bin/python" -m pip install --upgrade pip
"$TMPVENV/venv/bin/python" -m pip install --no-cache-dir --no-deps lingtai==0.X.Y
"$TMPVENV/venv/bin/python" - <<'PY'
import importlib.metadata as md
import lingtai, lingtai_kernel
print('dist version:', md.version('lingtai'))
print('lingtai:', lingtai.__file__)
print('lingtai_kernel:', lingtai_kernel.__file__)
PY
rm -rf "$TMPVENV"
```

### 8. Report and local hygiene

Report: version, PyPI URL, tag/main commit, tests/checks, artifact sizes, clean-venv install result, and any known caveats. Pull/update any local runtime checkout as needed so auto-upgraders do not clobber an editable install.

```bash
cd ~/Documents/GitHub/lingtai-kernel
git pull --ff-only origin main
```

Or use the project's CI/CD pipeline if configured.

## Installing without Homebrew

```bash
git clone https://github.com/Lingtai-AI/lingtai.git
cd lingtai/tui && make build
# Binary at tui/bin/lingtai-tui

cd ../portal && make build
# Binary at portal/bin/lingtai-portal
```

Requires Go toolchain and Node.js (for portal web frontend).

## Version scheme

- **TUI/portal:** Semantic versioning (`v0.X.Y`). Tags on the `lingtai` repo.
- **Kernel:** Semantic versioning. Published to PyPI. Version in `pyproject.toml`.
- **Migration versions:** Integer counter in `meta.json`. Separate for per-project (TUI+portal shared) and per-machine (global migrations).

## Post-release checklist

- [ ] Tag pushed to `lingtai` repo
- [ ] GitHub release created
- [ ] Homebrew tap updated
- [ ] `brew upgrade` verified
- [ ] Kernel version bumped, tag pushed, and main fast-forwarded
- [ ] Kernel artifacts built cleanly and `twine check` passed
- [ ] Kernel published to PyPI (if kernel changed)
- [ ] Clean venv can install the new PyPI version
- [ ] Dev-mode symlinks rebuilt
- [ ] Kernel repo pulled on dev machine
