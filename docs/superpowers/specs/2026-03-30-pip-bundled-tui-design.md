# Design: Distribute lingtai-tui via Homebrew, Python via pip

**Date:** 2026-03-30
**Status:** Approved

## Goal

Two distribution channels, each handling what it's good at:
- **Homebrew** distributes the TUI binary: `brew install huangzesen/lingtai/lingtai-tui`
- **PyPI** distributes the Python agent package: TUI bootstraps `pip install lingtai` into an isolated venv

The TUI checks for Python package updates on every launch and auto-upgrades.

## User Flows

### End user (install)

```sh
brew install huangzesen/lingtai/lingtai-tui
lingtai-tui                    # first run: creates venv, pip install lingtai, bootstraps presets
```

### End user (upgrade)

```sh
brew upgrade lingtai-tui                       # upgrades TUI binary
# Python package auto-upgrades on next lingtai-tui launch
```

### Developer (local)

```sh
git clone https://github.com/huangzesen/lingtai.git
cd lingtai
pip install -e .                           # Python package in editable mode
cd tui && go build -o bin/lingtai-tui .    # build TUI binary, add to PATH
```

Dev mode detection in the TUI is unchanged — if local source repos exist, it installs with `-e` from source instead of PyPI.

## Homebrew Tap

### Repository: `huangzesen/homebrew-lingtai`

Contains a single formula: `lingtai-tui.rb`

```ruby
class LingtaiTui < Formula
  desc "Terminal UI for the Lingtai AI agent framework"
  homepage "https://github.com/huangzesen/lingtai"
  version "0.3.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/huangzesen/lingtai/releases/download/v0.3.0/lingtai-darwin-arm64"
      sha256 "PLACEHOLDER"
    end
    on_intel do
      url "https://github.com/huangzesen/lingtai/releases/download/v0.3.0/lingtai-darwin-x64"
      sha256 "PLACEHOLDER"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/huangzesen/lingtai/releases/download/v0.3.0/lingtai-linux-arm64"
      sha256 "PLACEHOLDER"
    end
    on_intel do
      url "https://github.com/huangzesen/lingtai/releases/download/v0.3.0/lingtai-linux-x64"
      sha256 "PLACEHOLDER"
    end
  end

  def install
    bin.install stable.url.split("/").last => "lingtai-tui"
  end

  test do
    assert_match "lingtai-tui", shell_output("#{bin}/lingtai-tui version 2>&1", 0)
  end
end
```

### GitHub Actions: `.github/workflows/release.yml` (in lingtai repo)

**Trigger:** Push tag matching `v*`

**Jobs:**

#### 1. `build` (matrix: 4 platforms)

| GOOS | GOARCH | Asset name |
|------|--------|------------|
| darwin | arm64 | `lingtai-darwin-arm64` |
| darwin | amd64 | `lingtai-darwin-x64` |
| linux | amd64 | `lingtai-linux-x64` |
| linux | arm64 | `lingtai-linux-arm64` |

Each job:
1. Checkout repo
2. Set up Go
3. Build: `CGO_ENABLED=0 GOOS=$GOOS GOARCH=$GOARCH go build -o lingtai-${GOOS}-${ARCH} ./tui/`
4. Upload binary as release asset

#### 2. `release`

1. Create GitHub release from tag
2. Attach all 4 binaries

#### 3. `update-homebrew`

1. Compute sha256 for each binary
2. Update formula in `huangzesen/homebrew-lingtai` repo (via GitHub API or checkout + push)

## TUI Auto-Upgrade Check

### Current behavior (unchanged)

On every launch, the TUI calls `NeedsVenv()` — if no venv exists, it creates one and `pip install lingtai`.

### New behavior: version check on every launch

Add to the TUI startup path (in `main.go` or `venv.go`):

1. Read the currently installed lingtai version: `python -c "import lingtai; print(lingtai.__version__)"`
2. Check the latest version on PyPI: `curl -s https://pypi.org/pypi/lingtai/json | jq -r .info.version` (or equivalent Go HTTP call)
3. If versions differ, run `pip install --upgrade lingtai` in the venv
4. This runs on every TUI launch, but the version check is fast (~100ms HTTP call). The pip upgrade only runs when needed.

The check should be **non-blocking** — if PyPI is unreachable, skip silently and use the installed version. Don't block the TUI from starting.

Implementation in `venv.go`:

```go
// CheckUpgrade compares installed version to PyPI latest.
// Returns true if upgrade was performed.
func CheckUpgrade(globalDir string) bool {
    python := VenvPython(RuntimeVenvDir(globalDir))

    // Get installed version
    installed, err := exec.Command(python, "-c",
        "import lingtai; print(lingtai.__version__)").Output()
    if err != nil {
        return false
    }

    // Get latest from PyPI (with 3s timeout)
    client := &http.Client{Timeout: 3 * time.Second}
    resp, err := client.Get("https://pypi.org/pypi/lingtai/json")
    if err != nil {
        return false  // offline, skip
    }
    defer resp.Body.Close()
    var pypi struct{ Info struct{ Version string } }
    json.NewDecoder(resp.Body).Decode(&pypi)

    if strings.TrimSpace(string(installed)) == pypi.Info.Version {
        return false  // up to date
    }

    // Upgrade
    pip := filepath.Join(filepath.Dir(python), "pip")
    exec.Command(pip, "install", "--upgrade", "lingtai").Run()
    return true
}
```

Called from `main.go` startup, after `NeedsVenv` check, before launching agents.

## Files to Create

| File | Repo | Purpose |
|------|------|---------|
| `lingtai-tui.rb` | `huangzesen/homebrew-lingtai` | Homebrew formula |
| `.github/workflows/release.yml` | `huangzesen/lingtai` | CI: build binaries + GitHub release + update formula |

## Files to Modify

| File | Repo | Change |
|------|------|--------|
| `tui/internal/config/venv.go` | `huangzesen/lingtai` | Add `CheckUpgrade()` function |
| `tui/main.go` | `huangzesen/lingtai` | Call `CheckUpgrade()` on startup |

## Files to Delete

| File | Repo | Reason |
|------|------|--------|
| `install.sh` | `huangzesen/lingtai` | Replaced by `brew install huangzesen/lingtai/lingtai-tui` |

## Files to Revert (from earlier this session)

| File | Change |
|------|--------|
| `src/lingtai/_tui.py` | Delete (no longer needed) |
| `src/lingtai/bin/.gitkeep` | Delete (no longer needed) |
| `pyproject.toml` | Remove `lingtai-tui` entry point, remove `bin/*` from package-data |
| `.gitignore` | Remove `src/lingtai/bin/lingtai-tui` line |

## What Does NOT Change

- `lingtai-kernel` remains a separate PyPI package
- All runtime behavior (venv isolation, agent processes, filesystem protocol)
- The TUI's own behavior, commands, and views
- `lingtai run` Python CLI
- The TUI Go source code in `tui/`
- PyPI distribution of the Python `lingtai` package (pure Python, no binary)

## Distribution Summary

| Component | Channel | Install | Upgrade |
|-----------|---------|---------|---------|
| TUI binary | Homebrew | `brew install huangzesen/lingtai/lingtai-tui` | `brew upgrade lingtai-tui` |
| Python agent | PyPI (via TUI) | Auto on first `lingtai-tui` run | Auto on every `lingtai-tui` launch |
| Python agent (dev) | Local source | `pip install -e .` | `git pull` |
