# Releasing lingtai-tui and lingtai-portal

## Release Process

### 1. Commit and push all changes

```bash
git push origin main
```

### 2. Tag the release

```bash
git tag v0.X.Y
git push origin v0.X.Y
```

Pushing a `v*` tag triggers the root GitHub Actions workflow at
`.github/workflows/release.yml`. That workflow has two independent jobs:

- **`release-assets`** — cross-builds `lingtai-tui` + `lingtai-portal` for
  darwin/linux × amd64/arm64, packages each as
  `lingtai-<tag>-<os>-<arch>.tar.gz` (+ `.sha256`), creates the GitHub Release
  if absent, and uploads the tarballs. `install.sh` downloads these for a fast,
  build-free install; the asset name here MUST stay in sync with
  `install.sh`'s `asset_name()`.
- **`update-homebrew`** — computes the source tarball checksum and updates
  `Lingtai-AI/homebrew-lingtai`.

### 3. Create the GitHub release

The `release-assets` job creates the release automatically when it runs. To
create it manually (or to add richer notes), run:

```bash
gh release create v0.X.Y --title "v0.X.Y" --notes "release notes here..."
```

Binary assets are attached by the workflow. If the workflow could not run, the
release still installs — `install.sh` falls back to building from the release
source tarball.

### 4. Verify the automated Homebrew tap update

Check the `Release` workflow run for the tag and confirm it pushed a formula
update to `Lingtai-AI/homebrew-lingtai`.

```bash
gh run list --workflow Release --event push --limit 5
gh run watch <run-id>
```

Then verify the installed version:

```bash
brew update && brew upgrade lingtai-ai/lingtai/lingtai-tui
lingtai-tui version  # should show v0.X.Y
```

### 5. Fallback: update the Homebrew tap manually

Use this only when the root release workflow failed or cannot run. Do not race a
successful workflow with a hand edit.

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

The inactive `tui/.github/workflows/release.yml` path is intentionally not part
of the release process; GitHub only runs workflows from the repository-root
`.github/workflows/` directory. Existing npm package files are outside this
release checklist and are not decided here.

## Installing without Homebrew

The one-shot installer is the Homebrew-free path. It installs the latest
release (prebuilt asset when available, else a source build) and sets up the
Python runtime venv:

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
# or, direct from the repo:
curl -fsSL https://raw.githubusercontent.com/Lingtai-AI/lingtai/main/install.sh | bash
```

Manual source build (if you prefer to build the binaries yourself):

```bash
git clone https://github.com/Lingtai-AI/lingtai.git
cd lingtai/tui && make build
# Binary at tui/bin/lingtai-tui

cd ../portal && make build
# Binary at portal/bin/lingtai-portal
```

Requires Go toolchain and Node.js (for portal web frontend).
