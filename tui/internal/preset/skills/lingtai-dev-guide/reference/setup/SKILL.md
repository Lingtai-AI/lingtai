---
name: dev-guide-setup
description: >
  Nested lingtai-dev-guide reference for local developer environment setup: cloning repos, building TUI/portal, dev-mode symlinks, editable kernel install, MCP addon setup, and verification.
version: 1.0.0
---

# Development Environment Setup


Nested lingtai-dev-guide reference. Read this after the top-level router sends you here.
## Prerequisites

| Tool | Version | Purpose |
|---|---|---|
| **Go** | 1.21+ | Building TUI and portal binaries |
| **Node.js** | 18+ | Building the portal's React frontend |
| **Python** | 3.10+ | Running the kernel (installed automatically by the TUI, but needed for dev) |
| **uv** | latest | Python package management (the TUI's runtime venv is uv-managed) |
| **Git** | any | Version control |
| **gh** | latest | GitHub CLI (for releases) |

## Clone the repos

```bash
# The Go monorepo (TUI + portal)
git clone https://github.com/Lingtai-AI/lingtai.git ~/Documents/GitHub/lingtai

# The Python kernel
git clone https://github.com/Lingtai-AI/lingtai-kernel.git ~/Documents/GitHub/lingtai-kernel

# MCP addon repos (optional, only if developing addons)
git clone https://github.com/Lingtai-AI/lingtai-imap.git ~/Documents/GitHub/lingtai-imap
git clone https://github.com/Lingtai-AI/lingtai-telegram.git ~/Documents/GitHub/lingtai-telegram
git clone https://github.com/Lingtai-AI/lingtai-feishu.git ~/Documents/GitHub/lingtai-feishu
git clone https://github.com/Lingtai-AI/lingtai-wechat.git ~/Documents/GitHub/lingtai-wechat
```

The sibling layout under `~/Documents/GitHub/` is expected by the TUI's auto-upgrader and the CLAUDE.md instructions. If you use a different location, update paths accordingly.

## Build the Go binaries

```bash
# Build the TUI
cd ~/Documents/GitHub/lingtai/tui && make build
# Output: tui/bin/lingtai-tui

# Build the portal (builds embedded web frontend first)
cd ~/Documents/GitHub/lingtai/portal && make build
# Output: portal/bin/lingtai-portal
```

Cross-compilation (darwin/linux/windows × amd64/arm64):
```bash
cd tui && make cross-compile
cd portal && make cross-compile
```

## Set up dev mode (symlinks)

Dev mode means `/opt/homebrew/bin/lingtai-{tui,portal}` are symlinks to the freshly-built binaries, so `which lingtai-tui` resolves to your local build. Every `make build` is immediately picked up by your shell — no `brew reinstall` cycle.

```bash
# One-time setup (only if symlinks don't already exist)
ln -sf ~/Documents/GitHub/lingtai/tui/bin/lingtai-tui /opt/homebrew/bin/lingtai-tui
ln -sf ~/Documents/GitHub/lingtai/portal/bin/lingtai-portal /opt/homebrew/bin/lingtai-portal
```

Verify dev mode is active:
```bash
readlink -f $(which lingtai-tui)   # → ~/Documents/GitHub/lingtai/tui/bin/lingtai-tui
lingtai-tui --version              # → vX.Y.Z-N-gSHORTSHA (git describe)
```

A clean `vX.Y.Z` version string means the brew-installed binary is in front. A `-N-gSHORTSHA` suffix means dev mode is live.

## Set up the kernel (editable install)

The TUI's runtime venv lives at `~/.lingtai-tui/runtime/venv/`. For kernel development, install the kernel in editable mode:

```bash
# The TUI creates the venv on first run. If it doesn't exist yet, run lingtai-tui once.

# Install kernel in editable mode
~/.local/bin/uv pip install -e ~/Documents/GitHub/lingtai-kernel \
    -p ~/.lingtai-tui/runtime/venv
```

Verify:
```bash
~/.lingtai-tui/runtime/venv/bin/python -c "import lingtai; print(lingtai.__file__)"
# Should resolve to the kernel source tree, not site-packages
```

**Important:** Use `uv`, not `pip` — the venv is uv-managed and has no `pip` symlink, only `pip3`.

## Verify the runtime checkout a running agent actually uses

After merging or updating kernel code, do not assume a running agent is using the
checkout you just edited. The TUI runtime venv may point at an editable checkout
that is behind `origin/main`, or at a detached worktree left by an earlier test.
Before declaring a runtime fix live, probe the interpreter the agent will use,
update the checkout if needed, refresh the agent, then probe again.

Use the TUI runtime venv unless the agent's `init.json` explicitly names a
different Python executable:

```bash
RUNTIME_PY="$HOME/.lingtai-tui/runtime/venv/bin/python"
$RUNTIME_PY - <<'PY'
import importlib, pathlib, subprocess
mods = [
    "lingtai",
    "lingtai_kernel",
    "lingtai.mcp_servers",
    "lingtai.mcp_servers.telegram",
    "lingtai_telegram",
]
for name in mods:
    try:
        mod = importlib.import_module(name)
    except Exception as exc:
        print(f"{name}: NOT IMPORTABLE ({type(exc).__name__}: {exc})")
        continue
    path = pathlib.Path(getattr(mod, "__file__", "")).resolve()
    print(f"{name}: {path}")
    cur = path
    for parent in [cur, *cur.parents]:
        if (parent / ".git").exists() or (parent / ".git").is_file():
            try:
                head = subprocess.check_output(
                    ["git", "-C", str(parent), "rev-parse", "--short=12", "HEAD"],
                    text=True,
                ).strip()
                branch = subprocess.check_output(
                    ["git", "-C", str(parent), "branch", "--show-current"],
                    text=True,
                ).strip() or "(detached)"
                print(f"  git: {parent} {branch} {head}")
            except Exception as exc:
                print(f"  git probe failed: {exc}")
            break
PY
```

If the probe resolves to the expected editable kernel checkout but the HEAD is
behind, fast-forward that checkout first:

```bash
cd ~/Documents/GitHub/lingtai-kernel   # or the path printed by the probe
git fetch origin main
git switch main                         # only if it is safe to leave a worktree branch
git pull --ff-only origin main
```

Then refresh the agent so it reloads Python modules, MCP registrations, prompt
sections, and runtime config:

```python
system(action="refresh", reason="pick up updated runtime checkout")
```

Finally rerun the import probe from the same interpreter. For addon/MCP work,
verify both the curated package path (for example `lingtai.mcp_servers.telegram`)
and any compatibility wrapper (`lingtai_telegram`) so stale external addon
checkouts do not masquerade as the active implementation.

## Set up MCP addons (optional)

If developing MCP server addons (imap, telegram, feishu, wechat):

```bash
# Install in editable mode
~/.local/bin/uv pip install -e ~/Documents/GitHub/lingtai-imap \
    -p ~/.lingtai-tui/runtime/venv
```

Register the MCP server via the `mcp-manual` skill's workflow.

## Verify the full stack

```bash
# 1. TUI builds and runs
lingtai-tui --version

# 2. Portal builds and runs
lingtai-portal --version

# 3. Kernel is editable
~/.lingtai-tui/runtime/venv/bin/python -c "import lingtai; print(lingtai.__file__)"

# 4. Create a test project and launch an agent
mkdir /tmp/test-lingtai && cd /tmp/test-lingtai
lingtai-tui
# Go through the first-run wizard to create an agent
```

## IDE setup

### Go (TUI + portal)

The Go modules are `github.com/anthropics/lingtai-tui` and `github.com/anthropics/lingtai-portal` (historical naming — not moving to `Lingtai-AI/`). Standard Go tooling (gopls, golangci-lint) works out of the box.

### Python (kernel)

The kernel uses `pyproject.toml` for project metadata. Standard Python tooling (pyright, ruff) works. The editable install means changes to the kernel source are reflected immediately in the running agent — no rebuild needed.
