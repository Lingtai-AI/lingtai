# install.sh repair summary — 2026-06-11

Scope kept narrow after direction change: Homebrew remains the primary distribution path; this patch only repairs `install.sh` as a source-build helper.

Changes:
- Corrected header/help text so `install.sh` no longer claims it always installs to Homebrew's bin directory.
- Added `-h/--help` and clearer unknown flag errors.
- Added EXIT trap cleanup for temporary build directory.
- Made invalid `--ref` fail clearly rather than silently building `main`.
- Added platform/package-manager hints for missing `git`, `go`, and optional `npm`.
- Preserved existing #239/#240 behavior: brew bin preference, writable `/usr/local/bin`, `~/.local/bin` fallback, PATH hint, CN mirror detection, npm-missing portal skip.

Validation:
- `bash -n install.sh` passed.
- `./install.sh --help` passed.
- `git diff --check` passed.

Not run:
- Full install/source build, to avoid overwriting local binaries during PR prep.
