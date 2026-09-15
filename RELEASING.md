# Releasing lingtai-tui

## Release Process

### 1. Commit and push all changes

Update [`migration/migration.md`](migration/migration.md) only when the release
needs a user-facing migration note. Its versioned release sections are history;
there is no repo-owned kernel pin to update for a current release. The retained
`lingtai-portal` codebase is deprecated and is not a release artifact.

```bash
git push origin main
```

### 2. Tag the release

```bash
git tag v0.X.Y
git push origin v0.X.Y
```

Pushing a `v*` tag triggers the root GitHub Actions workflow at
`.github/workflows/release.yml`, which has two jobs:

- **`source-release`** — validates the exact `vX.Y.Z` tag, peels annotated tags
  to the commit SHA, creates a deterministic `lingtai-<tag>-source.tar.gz`,
  writes its checksum sidecar, and publishes both assets to the GitHub release.
  It also sends a source-only `release-asset-published` dispatch describing the
  verified archive and checksum to `Lingtai-AI/lingtai-web`.
- **`update-homebrew`** (`needs: source-release`) — consumes the producer-owned
  source archive and checksum and writes a TUI-only source-build formula in
  `Lingtai-AI/homebrew-lingtai`. It fails closed unless the tag is exactly
  `vX.Y.Z`; prerelease and other `v*` tags are rejected by `source-release` and
  do not publish current release or Homebrew assets.

The workflow does not build or publish Portal, a Windows bundle, or a coupled
kernel artifact. The TUI and kernel are released and resolved independently;
there is no repository-owned kernel pin or workflow-side “latest kernel” pin.

### Legacy Gitee publication tools

The tag workflow does not synchronize to Gitee or publish current release
assets there. The existing scripts may still handle historical binary/bundle
publication when explicitly invoked by an authorized maintainer; they are not
current installer inputs. The existing
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

`install.ps1`'s public (default) mode resolves and verifies the latest TUI source
and latest kernel release independently through `lingtai.ai`, with each
component's own GitHub fallback. It always builds `lingtai-tui.exe` locally and
builds/installs the manifest-declared, verified kernel source archive into
`%USERPROFILE%\.lingtai-tui\runtime\venv` unless `-SkipVenv` is passed. It
never installs LingTai by package name from an index. `-ArchivePath` plus
`-ChecksumPath` is the explicit local TUI-artifact mode; `-DryRun` is a
mode-specific, read-only plan: source and current-main paths check prerequisites
and report the relevant TUI selection, without downloading or verifying source
archives or resolving/installing a kernel release or artifact; local-artifact
mode validates its supplied checksum. No writes occur. Portal is not built or
installed by this path. The Windows Installer Smoke workflow covers the
contract suite under PowerShell 5.1 and PowerShell 7 on PR/push. See
[`scripts/test-install-ps1.ps1`](scripts/test-install-ps1.ps1) for the full
contract and [`.github/workflows/windows-installer-smoke.yml`](.github/workflows/windows-installer-smoke.yml)
for its Windows PowerShell 5.1 / PowerShell 7 CI coverage.

### 3. Create the GitHub release

The `source-release` job creates the GitHub release and publishes the
deterministic source archive plus checksum. To create a release manually (or to
add richer notes), run:

```bash
gh release create v0.X.Y --title "v0.X.Y" --notes "release notes here..."
```

The workflow owns the source assets. If it could not run, the release can be
created manually with the same source archive and checksum; installers build
the TUI from verified source and resolve the kernel separately.

Immediately after the release above is published, `source-release`'s
"Notify lingtai-web download mirror" step sends one `repository_dispatch`
(`release-asset-published`) to `Lingtai-AI/lingtai-web` naming this release's
tag and the source archive/checksum with sha256/size recomputed fresh from the
still-on-disk bytes. This exists solely so `lingtai.ai` can relay the same
verified producer assets for mainland-China download acceleration; it is a
generic relay and does not build or reinterpret source. GitHub remains the sole
official release authority, and a missing or failed dispatch never edits,
retries, or undoes the GitHub release itself. Requires the
`LINGTAI_WEB_DISPATCH_TOKEN` repository secret (a token with
`repository_dispatch` write access on `Lingtai-AI/lingtai-web`) as a
deployment prerequisite; without it the step prints a `::warning::` and exits
0, so its absence cannot fail a release. See `Lingtai-AI/lingtai-web`'s
`docs/release-mirror/CONTRACT.md` for the receiving side's contract.

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
# Get the producer-owned source archive checksum
curl -sL "https://github.com/Lingtai-AI/lingtai/releases/download/v0.X.Y/lingtai-v0.X.Y-source.tar.gz" | shasum -a 256

# Edit the formula
cd $(brew --repository)/Library/Taps/lingtai-ai/homebrew-lingtai
# In lingtai-tui.rb: update the url tag and sha256
git add lingtai-tui.rb
git commit -m "bump lingtai-tui to v0.X.Y"
git push
```

The inactive `tui/.github/workflows/release.yml` path is intentionally not part
of the release process; GitHub only runs workflows from the repository-root
`.github/workflows/` directory. Existing npm package files and historical
release assets are outside this current installer input contract.

## Installing without Homebrew

The tag workflow publishes a deterministic GitHub source archive and checksum.
The installer and Homebrew consume TUI source; the kernel is resolved and
verified independently. The retained Portal codebase is deprecated and can be
built separately for repository development, but it is not part of the
installer, release workflow, or Homebrew:

```bash
curl -fsSL https://lingtai.ai/install.sh | bash
# or, direct from the repo:
curl -fsSL https://raw.githubusercontent.com/Lingtai-AI/lingtai/main/install.sh | bash
```

On the ordinary stable macOS path with no existing command, `install.sh`
registers a self-contained lazy `lingtai-desktop` command and stops. A stable,
version-pinned `--update` does the same when the command is absent, and may
atomically refresh only an executable, regular, non-symlink command carrying
this installer's lazy-bootstrap marker. Registration in either mode does not
contact Desktop, invoke its installer, or create any Desktop
App/current/receipt/cache/version state. The command embeds Desktop `0.1.10`
plus SHA-256 pins audited from its commit
`fd39dd61e4d123b2835064c1c148566d8b36ceb0`. On its first execution only, it
downloads the matching `install-macos-app.py`, `desktop_user_cli.py`, and
independent `verify-app-archive.py`, rejects any byte mismatch, invokes the
Desktop installer, and then continues the user's requested command. Archive,
manifest, managed-support generation/update, smoke, atomic-publication, and
later-update policy stay entirely in the Desktop code. Desktop v0.1.10 is the
verified N→N+1 managed-support generation, `0.1.10-b4575bccdedd`; its
self-update contract advances from the v0.1.9 public generation without
inferring or migrating private pre-generation layouts. The public LingTai
entry pins only the raw bootstrap trust set rather than duplicating Desktop's App/support
manifest digests. Later command executions delegate to the installed current
CLI without reinstalling. `--skip-desktop` opts out of registration.

LingTai's root installers do not own or delete Desktop's App or command state
through the TUI receipt. A later main install
therefore treats a regular, executable, non-symlink command target as already satisfied only
when it is this installer's marked lazy bootstrap, or when it carries Desktop's
official launcher marker and the managed current App executable is complete.
An ordinary stable install preserves the marked lazy command's bytes and mode;
both ordinary install and stable update preserve a complete official launcher
and App executable byte-for-byte and mode-for-mode. A symlink, arbitrary file,
or official-marker launcher without its regular executable App remains a loud
no-overwrite failure.

Linux/WSL, an ordinary existing-install re-run, `--latest`, arbitrary `--ref`,
and `--skip-desktop` installs do not register or refresh the command; the sole
update exception is stable, version-pinned `--update`. Windows is platform-N/A
because LingTai Desktop itself is macOS-only. The exact
[`v0.1.10` public release](https://github.com/Lingtai-AI/lingtai-desktop/releases/tag/v0.1.10)
provides the tag and assets read by the first `lingtai-desktop` execution.
Temporary public-support, release, or transport unavailability fails clearly,
leaves the command retryable, and publishes no partial Desktop state. Version
and support-file hashes are one fixed trust set; there is no free-form Desktop
version override.

Manual source build (if you prefer to build the binaries yourself):

```bash
git clone https://github.com/Lingtai-AI/lingtai.git
cd lingtai/tui && make build
# Binary at tui/bin/lingtai-tui

cd ../portal && make build
# Binary at portal/bin/lingtai-portal
```

Requires the Go toolchain for a TUI build; the separate deprecated Portal
development build additionally requires Node.js/npm.

### Source selection (GitHub vs the lingtai.ai mirror) and the Python runtime

The POSIX installer has one explicit non-release mode:

```bash
curl -fsSL https://lingtai.ai/install.sh | bash -s -- --latest
```

`--latest` resolves `refs/heads/main` independently in the TUI repository and
`lingtai-kernel`, verifies each shallow checkout against its resolved full SHA,
builds the TUI from source, and installs the kernel from the checked-out local
source tree. It prints both SHAs and records them in `~/.lingtai-tui/install.json`
under `source_mode: "latest-main"`, `tui_commit`, and `kernel_commit`. This mode
is deliberately separate from the default stable release, `--version`,
`--ref`, and `--update` paths; conflicts fail before network access, and a
failed main checkout or kernel install never falls back to a stable release or
package-index install. `install.ps1 -Latest` preserves the same independent
full-SHA TUI/kernel main-source behavior with Windows-specific prerequisites.

For the ordinary stable install, `--source auto|github|mirror` (or
`LINGTAI_SOURCE`; `gitee` is retired) controls the TUI source transport only.
The normal `auto`/`mirror` route obtains producer-owned source metadata through
`lingtai.ai`, verifies the source archive, and builds `lingtai-tui` locally. The
release workflow separately peels annotated tags to commit SHAs for its archive
and provenance. If that TUI route fails, it falls back only to the latest
GitHub TUI source release. Explicit version/ref/source/update modes retain
their existing distinctions, and every stable TUI route builds locally from
verified source. The GitHub fallback uses its verified source archive or an
exact peeled-tag source checkout; `--from-source` remains a backwards-
compatible selector for that GitHub source path.

The kernel has its own latest-release resolution through `lingtai.ai`, its own
GitHub fallback, and its own manifest/source-archive checksum verification. The
manifest-declared source distribution is built and installed by explicit local
path; package indexes are used only for third-party dependencies. No provider switch
or release bundle couples the TUI to a kernel version, and no repository-owned
kernel pin is an installer or release input. The PowerShell
installer follows the same independent TUI/kernel contract; `-SkipVenv` is the
explicit runtime opt-out, while `-ArchivePath` plus `-ChecksumPath` is an
explicit local TUI-artifact mode.

The release workflow's `source-release` job sends only the verified source
archive/checksum metadata to `Lingtai-AI/lingtai-web`. That service is a generic
relay of producer assets for `lingtai.ai`; it does not build or reinterpret
source. Existing historical release assets remain valid history but are not
current installer inputs. The explicit Gitee synchronization and publication
tools remain separate maintainer tools and are not invoked by the tag workflow.
