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
`.github/workflows/release.yml`, which has three jobs:

- **`source-release`** — verifies the pushed tag and creates the public GitHub
  Release when it does not already exist. GitHub supplies the tag source archives;
  this job does not build or upload prebuilt binaries, checksums, or bundles.
- **`update-homebrew`** — computes the GitHub source-tarball checksum and updates
  the source-build formula in `Lingtai-AI/homebrew-lingtai`.
- **`windows-release`** (`needs: source-release`) — builds `lingtai-tui.exe` (and
  `lingtai-portal.exe`, when the portal web build succeeds) for `windows/amd64`,
  packages `lingtai-<tag>-windows-amd64.zip` plus its `.sha256` sidecar, generates
  `lingtai-bundle-manifest.json` (schema `lingtai.tui.bundle/v1`) binding the tag's
  exact commit to the archive digest and to [`kernel-release.json`](kernel-release.json)'s
  pinned kernel tag, and uploads all three to the release. It **fails closed**
  before building anything unless `kernel-release.json`'s pinned kernel release
  already exists and publishes a `cp311`/`cp312`/`cp313` `win_amd64` wheel with a
  verified digest — it never resolves "latest kernel."

### Kernel compatibility metadata

[`kernel-release.json`](kernel-release.json) is the repo-owned pin the
`windows-release` job reads to bind a TUI release to one exact kernel release.
Bump it deliberately, in the same PR/commit that intends to ship a new kernel
version with the next TUI release; the workflow never resolves "latest kernel"
on its own.

### Gitee publication

The tag workflow does not synchronize to Gitee and does not publish TUI
binary/bundle assets there. The existing
[`scripts/sync_gitee_mirror.sh`](scripts/sync_gitee_mirror.sh) and
[`scripts/publish_bundle_to_gitee.sh`](scripts/publish_bundle_to_gitee.sh)
remain explicit maintainer tools; running them requires separate release authority
and is not part of the automatic `v*` workflow.

### Installing on Windows (PowerShell)

```powershell
irm https://lingtai.ai/install.ps1 | iex
# or an exact version (parameters require the scriptblock form, not | iex):
&([scriptblock]::Create((irm https://lingtai.ai/install.ps1))) -Version v0.X.Y
```

`install.ps1`'s public (default) mode resolves one exact release tag, downloads
and strictly validates `lingtai-bundle-manifest.json`, downloads and SHA-256
verifies the Windows archive, confirms the staged `lingtai-tui.exe` reports
exactly that tag, and — unless `-SkipVenv` is passed — provisions
`%USERPROFILE%\.lingtai-tui\runtime\venv` from the bundle's pinned kernel
release: it selects the `cp311`/`cp312`/`cp313` `win_amd64` wheel matching the
venv's actual interpreter, verifies its digest, and installs it by explicit
local file path. LingTai is never installed by package name from any index —
the same "no PyPI fallback" contract `install.sh` holds itself to. `-SkipVenv`
installs the TUI/portal binaries only; `-DryRun` performs the same
resolution/validation reads but writes nothing. See
[`scripts/test-install-ps1.ps1`](scripts/test-install-ps1.ps1) for the full
contract and [`.github/workflows/windows-installer-smoke.yml`](.github/workflows/windows-installer-smoke.yml)
for its Windows PowerShell 5.1 / PowerShell 7 CI coverage.

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

The current release workflow is source-only. Homebrew and the manual commands
below build `lingtai-tui` and `lingtai-portal` from the tagged source. The
one-shot installer may still consume compatible assets from older or explicitly
published releases, but the tag workflow no longer produces them:

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

### Source selection (GitHub vs Gitee) and the Python runtime

The behavior below remains compatibility support for older or separately
published bundle releases; the source-only tag workflow does not create or mirror
these bundle artifacts.

`install.sh --source auto|github|gitee` (or `LINGTAI_SOURCE` env var) controls
where the TUI/portal archives, the bundle manifest, and the pinned kernel
release come from. `auto` (the default) runs a bounded, fail-open public-IP
country lookup and prefers Gitee for mainland-China installs; any lookup or
provider-reachability failure falls back to GitHub. A fallback always re-fetches
the SAME resolved tag/bundle from the other provider — it never independently
resolves "latest" a second time, so a TUI archive from one release can never be
paired with a kernel artifact from a different one.

The Python `lingtai` runtime installs from the bundle's pinned kernel release
artifact (a platform wheel matched to the venv's actual interpreter, or the
pinned sdist as a fallback) by **explicit local file path**. LingTai is
**never** installed by requesting the package name `lingtai` from any package
index — there is no PyPI fallback for LingTai itself. SHA256 is verified
before install. The configured package index (`LINGTAI_PYPI_INDEX_URL`,
default `pypi.org`) is used only to resolve `lingtai`'s third-party
dependencies once the local artifact is being installed.

On the default one-command path (no `--ref`, not `--update`) a resolved
bundle + a successful kernel-artifact install are **mandatory**: if no bundle
manifest can be resolved on either provider (same-tag fallback attempted), or
the resolved bundle's kernel artifact fails to verify or install, `install.sh`
**fails loud** with the provider/tag/error rather than degrading to any other
install source. `--ref`/source-ref builds have no bundle to pin against and
fail loud the same way. `--skip-python` (alias `--skip-venv`) is the explicit,
honest opt-out for a TUI/portal-only install — you then provision the Python
runtime yourself (for example an editable install against a local
`lingtai-kernel` checkout).

`install.json`'s `kernel_source` field is written only on a verified bundle
install (`kernel_source: "bundle"`, plus `kernel_bundle_id`/`kernel_version`/
`kernel_provider`); it is omitted otherwise. The TUI's own runtime updater
(`tui/internal/config/venv.go`) reads this field and skips **both routine and
forced** PyPI queries/installs for a bundle-provisioned runtime — `force=true`
(`doctor`/`/update --force`) reports that the kernel is pinned to the
compatible bundle and directs the user to the one-command installer rather
than reinterpreting "force" as "discard the pin and install latest PyPI."
Legacy runtimes with no `kernel_source` metadata are unaffected: the updater's
existing PyPI-compare/upgrade behavior is unchanged for them, since retracting
that established capability is out of scope here — but the CLI no longer
*introduces* a new PyPI install source for LingTai on a fresh install.
