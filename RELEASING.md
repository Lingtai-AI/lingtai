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
`.github/workflows/release.yml`. That workflow has three jobs:

- **`release-assets`** — cross-builds `lingtai-tui` + `lingtai-portal` for
  darwin/linux × amd64/arm64, packages each as
  `lingtai-<tag>-<os>-<arch>.tar.gz` (+ `.sha256`), creates the GitHub Release
  if absent, and uploads the tarballs. `install.sh` downloads these for a fast,
  build-free install; the asset name here MUST stay in sync with
  `install.sh`'s `asset_name()`.
- **`publish-bundle`** (`needs: release-assets`) — reads the repo-owned kernel
  pin at [`kernel-release.json`](kernel-release.json), verifies that exact
  kernel release already exists on `Lingtai-AI/lingtai-kernel` with a
  `lingtai-kernel-release-manifest.json` (fails the job loud if not — this
  never ships a half-bundle), downloads the four archives `release-assets`
  just uploaded (same bytes, no rebuild), and writes a small bundle manifest
  (schema `lingtai.tui.bundle/v1`) binding: this TUI tag/commit, the pinned
  kernel tag/version, the kernel manifest filename, and the archive
  filenames/checksums. Uploads the bundle manifest to the GitHub release, then
  non-force-synchronizes the exact commit/tag to Gitee and publishes the
  bundle there — **for real** when `GITEE_ACCESS_TOKEN` is configured, since
  this job only ever runs on a genuine `v*` tag push (there is no separate
  manual-dispatch trigger for this workflow — the tag push IS the authorized
  release action). See "Gitee publication" below.
- **`update-homebrew`** — computes the source tarball checksum and updates
  `Lingtai-AI/homebrew-lingtai`.

### Bumping the kernel pin

[`kernel-release.json`](kernel-release.json) is the **one source of truth**
for which kernel release a TUI release binds into its bundle manifest. Bump it
deliberately, in the same commit/PR that intends to ship against a new kernel
version — the release workflow never resolves "latest kernel" on its own; it
only reads this file. See `lingtai-kernel`'s own `RELEASING.md` for how kernel
releases publish their `lingtai-kernel-release-manifest.json`.

### Gitee publication

`install.sh` prefers Gitee (`huangzesen1997/lingtai` +
`huangzesen1997/lingtai-kernel`) for mainland-China installs (`--source auto`,
bounded public-IP country lookup, fail-open to GitHub). For that to actually
work, both mirrors need release assets — which requires the
`GITEE_ACCESS_TOKEN` secret to be configured. Once it is, `publish-bundle`
publishes automatically on every `v*` tag push:

1. **[`scripts/sync_gitee_mirror.sh`](scripts/sync_gitee_mirror.sh)**
   non-force-pushes the exact release commit to Gitee's `main` (fast-forward
   only) and creates the exact release tag (create-only, never overwrites an
   existing tag). Either failing is a **fail-loud stop** — the workflow does
   not proceed to Gitee upload against an unsynchronized mirror. The token
   travels via a short-lived, owner-only-permission `GIT_ASKPASS` helper file
   (deleted after use), never in argv/a URL/a log line.
2. **[`scripts/publish_bundle_to_gitee.sh`](scripts/publish_bundle_to_gitee.sh)**
   verifies local asset bytes against the bundle manifest, re-verifies the
   Gitee tag is synchronized (belt-and-braces after step 1), creates the
   release if needed, and uploads any attachment not already present by name
   — never delete-and-replace.

When `GITEE_ACCESS_TOKEN` is unset, both steps skip cleanly (print why, exit
0) — the GitHub release side of `publish-bundle` still completes normally.
Every mutating action in both scripts requires the explicit `--execute` flag;
`publish-bundle` passes it on the real tag-push trigger. For local/manual
testing without touching the token or the live workflow:

```bash
# dry run — no token required, prints the plan only
./scripts/sync_gitee_mirror.sh --commit "$(git rev-parse HEAD)" --tag vX.Y.Z --branch main
./scripts/publish_bundle_to_gitee.sh --tag vX.Y.Z --bundle-dir <dir-with-archives+manifest>
```

```bash
# real publish — maintainer-run only, outside this task's authorization
export GITEE_ACCESS_TOKEN=...  # never echo or log this
./scripts/sync_gitee_mirror.sh --commit "$(git rev-parse HEAD)" --tag vX.Y.Z --branch main --execute
./scripts/publish_bundle_to_gitee.sh --tag vX.Y.Z --bundle-dir <dir-with-archives+manifest> --execute
```

As of this writing the Gitee TUI mirror's `main` matches GitHub's current
`main`, but release tags lag — do not assume a release tag exists there
without step 1 running first.

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

### Source selection (GitHub vs Gitee) and the Python runtime

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
