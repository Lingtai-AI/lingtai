# install.sh no-GitHub-login repair — 2026-06-11

Scope: follow-up after PR #305 because Jason saw `install.sh` ask for a GitHub account.

Root cause / framing:
- LingTai is public, so installation should not require a GitHub account.
- The script used `git clone https://github.com/...`; on some machines git credential helpers, proxy config, or credential prompts can turn a public clone into an interactive login prompt.
- Installers should be non-interactive and fail predictably.

Change:
- Replace normal source fetch from `git clone` with public GitHub codeload archive download: `https://codeload.github.com/Lingtai-AI/lingtai/tar.gz/<ref>`.
- Remove normal `git` dependency.
- Require/check `curl` and `tar` instead.
- Keep Homebrew as the primary distribution path; `install.sh` remains a source-build helper.
- Preserve writable bin fallback, PATH hint, CN mirror detection, and npm-missing portal skip.

Validation:
- `bash -n install.sh` passed.
- `./install.sh --help` passed.
- Invalid ref smoke test used the public archive URL and exited with a clear 404/error; stdout/stderr did not contain username/password/account prompts.
- `git diff --check` passed.

Not run:
- Full source build/install, to avoid overwriting local binaries.
