# Design: Distribute lingtai via pip with bundled TUI binary

**Date:** 2026-03-30
**Status:** Approved

## Goal

`pip install lingtai` becomes the single install method for both the Python agent framework and the TUI binary. Platform-specific wheels include the pre-built `lingtai-tui` Go binary. No separate install script, no curl, no git clone required for end users.

## User Flows

### End user (release)

```sh
pip install lingtai        # installs Python package + TUI binary
lingtai-tui                # runs the TUI (first run: bootstraps venv + presets)
pip install --upgrade lingtai  # upgrades everything
```

### Developer (local)

```sh
git clone https://github.com/huangzesen/lingtai.git
cd lingtai
pip install -e .                           # Python package in editable mode
cd tui && go build -o bin/lingtai-tui .    # build TUI binary, put on PATH
```

The `lingtai-tui` entry point wrapper falls back to `lingtai-tui` on PATH if the bundled binary is not present (editable installs don't include it).

## Package Structure

### Wheels (4 platform-specific)

```
lingtai-0.4.0-py3-none-macosx_11_0_arm64.whl
lingtai-0.4.0-py3-none-macosx_10_9_x86_64.whl
lingtai-0.4.0-py3-none-manylinux_2_17_x86_64.whl
lingtai-0.4.0-py3-none-manylinux_2_17_aarch64.whl
```

Each wheel contains:
- `lingtai/` — the full Python package (same as today)
- `lingtai/bin/lingtai-tui` — single Go binary for that platform (~23MB)

Plus one source distribution (sdist) without any binary.

### Entry Points

```toml
[project.scripts]
lingtai = "lingtai.cli:main"           # existing Python CLI
lingtai-tui = "lingtai._tui:main"      # new: launches bundled Go binary
```

### TUI Launcher (`src/lingtai/_tui.py`)

```python
"""Launch the bundled lingtai-tui binary, or fall back to PATH."""
from __future__ import annotations

import os
import sys


def main() -> None:
    # Try bundled binary first
    bundled = os.path.join(os.path.dirname(__file__), "bin", "lingtai-tui")
    if os.path.isfile(bundled) and os.access(bundled, os.X_OK):
        os.execvp(bundled, [bundled] + sys.argv[1:])

    # Fall back to PATH (dev mode: user built TUI separately)
    import shutil
    path_binary = shutil.which("lingtai-tui")
    if path_binary:
        os.execvp(path_binary, [path_binary] + sys.argv[1:])

    print("lingtai-tui binary not found.", file=sys.stderr)
    print("Install: pip install lingtai", file=sys.stderr)
    print("Or build from source: cd tui && go build -o bin/lingtai-tui .", file=sys.stderr)
    sys.exit(1)
```

## pyproject.toml Changes

```toml
[build-system]
requires = ["setuptools>=68.0"]
build-backend = "setuptools.build_meta"

[project]
name = "lingtai"
version = "0.4.0"
# ... rest unchanged ...

[project.scripts]
lingtai = "lingtai.cli:main"
lingtai-tui = "lingtai._tui:main"

[tool.setuptools.packages.find]
where = ["src"]

[tool.setuptools.package-data]
lingtai = ["i18n/*.json", "capabilities/*.json", "addons/*/*.json", "bin/*"]
```

Key addition: `"bin/*"` in package-data to include the TUI binary.

## GitHub Actions CI

### Workflow: `.github/workflows/release.yml`

**Trigger:** Push tag matching `v*` (e.g. `v0.4.0`).

**Jobs:**

#### 1. `build-wheels` (matrix: 4 platforms)

| Runner | GOOS | GOARCH | Wheel platform tag |
|--------|------|--------|--------------------|
| `macos-14` (arm64) | darwin | arm64 | `macosx_11_0_arm64` |
| `macos-13` (x64) | darwin | amd64 | `macosx_10_9_x86_64` |
| `ubuntu-latest` | linux | amd64 | `manylinux_2_17_x86_64` |
| `ubuntu-latest` | linux | arm64 | `manylinux_2_17_aarch64` |

Each job:
1. Checkout repo
2. Set up Go
3. Cross-compile TUI: `GOOS=$GOOS GOARCH=$GOARCH go build -o src/lingtai/bin/lingtai-tui ./tui/`
4. Set up Python 3.11
5. Build wheel: `python -m build --wheel`
6. Rename wheel to include correct platform tag (using `wheel tags` or manual rename)
7. Upload wheel as artifact

For Linux arm64: use `GOARCH=arm64` cross-compilation on `ubuntu-latest` (no need for actual arm64 runner — Go cross-compiles natively).

#### 2. `build-sdist`

1. Checkout repo
2. Build source dist: `python -m build --sdist` (no binary included)
3. Upload as artifact

#### 3. `publish` (depends on build-wheels + build-sdist)

1. Download all artifacts (4 wheels + 1 sdist)
2. Upload to PyPI via `twine` using `PYPI_API_TOKEN` secret

### Wheel Platform Tagging

The wheels must have correct platform tags so pip selects the right one. Since we're using `py3-none-{platform}`, the approach:

- Build with setuptools (produces `py3-none-any.whl`)
- Use `wheel tags` to retag to the correct platform: `wheel tags --platform-tag macosx_11_0_arm64 lingtai-0.4.0-py3-none-any.whl`
- Or use a custom build script that sets the tag directly

## Files to Create

| File | Purpose |
|------|---------|
| `src/lingtai/_tui.py` | TUI launcher wrapper |
| `src/lingtai/bin/.gitkeep` | Placeholder for binary dir (binary itself is gitignored) |
| `.github/workflows/release.yml` | CI: build + publish |

## Files to Modify

| File | Change |
|------|--------|
| `pyproject.toml` | Add `lingtai-tui` entry point, add `bin/*` to package-data |
| `.gitignore` | Add `src/lingtai/bin/lingtai-tui` (binary is build artifact, not committed) |

## Files to Delete

| File | Reason |
|------|--------|
| `install.sh` | Replaced by `pip install lingtai` |

## TUI Venv Bootstrap

No changes needed. The TUI still creates `~/.lingtai-tui/runtime/venv/` and runs `pip install lingtai`. Since the user already pip-installed lingtai, pip resolves from local cache — fast, no network needed.

## What Does NOT Change

- `lingtai-kernel` remains a separate PyPI package (listed as dependency)
- All runtime behavior (venv isolation, agent processes, filesystem protocol)
- The TUI's own behavior, commands, and views
- `lingtai run` Python CLI
- The TUI Go source code in `tui/`

## Version Bump

This will be version `0.4.0` since it changes the distribution model.
