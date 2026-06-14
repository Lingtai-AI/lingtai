#!/usr/bin/env bash
# Build lingtai-tui and lingtai-portal from source and install them.
#
# This is the source-build helper; Homebrew remains the primary install path
# (brew install lingtai-ai/lingtai/lingtai-tui). Binaries are installed to the
# first of: Homebrew's bin directory, a writable /usr/local/bin, or ~/.local/bin.
#
# Usage:
#   curl -sSL https://raw.githubusercontent.com/Lingtai-AI/lingtai/main/install.sh | bash
#
# To install a specific branch/tag:
#   curl -sSL https://raw.githubusercontent.com/Lingtai-AI/lingtai/main/install.sh | bash -s -- --ref v0.4.43
#
set -euo pipefail

REF="main"
REPO="https://github.com/Lingtai-AI/lingtai.git"
TMPDIR="${TMPDIR:-/tmp}"
BUILD_DIR="$TMPDIR/lingtai-install-$$"

usage() {
  cat <<'EOF'
Build lingtai-tui and lingtai-portal from source and install them.

Usage:
  curl -sSL https://raw.githubusercontent.com/Lingtai-AI/lingtai/main/install.sh | bash
  ./install.sh [--ref <branch|tag|commit>]

Options:
  --ref <ref>   Git branch, tag, or commit to build (default: main)
  -h, --help    Show this help

Binaries are installed to the first of: Homebrew's bin directory, a writable
/usr/local/bin, or ~/.local/bin. The portal is skipped when npm is missing.
Homebrew remains the primary install path:
  brew install lingtai-ai/lingtai/lingtai-tui
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --ref) REF="${2:?error: --ref requires a value}"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown flag: $1" >&2; usage >&2; exit 1 ;;
  esac
done

# Remove the build directory even when a build or install step fails midway.
cleanup() {
  cd / 2>/dev/null || true
  rm -rf "$BUILD_DIR"
}
trap cleanup EXIT

# Print a platform-appropriate install hint for a missing tool. Maps tool
# names to the package each manager actually ships (go is golang-go on
# Debian/Ubuntu, golang on Fedora, etc.).
suggest_install() {
  local tool="$1" pkg="$1"
  if command -v brew &>/dev/null || [[ "$(uname -s)" == "Darwin" ]]; then
    echo "      brew install $tool" >&2
    return
  fi
  if command -v apt-get &>/dev/null; then
    [[ "$tool" == "go" ]] && pkg="golang-go"
    [[ "$tool" == "npm" ]] && pkg="nodejs npm"
    echo "      sudo apt-get update && sudo apt-get install -y $pkg" >&2
  elif command -v dnf &>/dev/null; then
    [[ "$tool" == "go" ]] && pkg="golang"
    [[ "$tool" == "npm" ]] && pkg="nodejs npm"
    echo "      sudo dnf install -y $pkg" >&2
  elif command -v pacman &>/dev/null; then
    [[ "$tool" == "npm" ]] && pkg="nodejs npm"
    echo "      sudo pacman -S --needed $pkg" >&2
  elif command -v apk &>/dev/null; then
    [[ "$tool" == "npm" ]] && pkg="nodejs npm"
    echo "      sudo apk add $pkg" >&2
  elif command -v zypper &>/dev/null; then
    [[ "$tool" == "npm" ]] && pkg="nodejs npm"
    echo "      sudo zypper install $pkg" >&2
  else
    echo "      install '$tool' with your system package manager" >&2
  fi
}

# Auto-detect CN-restricted networks. If proxy.golang.org is unreachable
# within 3 seconds (typical on mainland China without VPN), fall back to
# CN-accessible mirrors for Go modules, the Go checksum database, and npm.
# Users elsewhere see no difference — the probe succeeds quickly and no
# environment is touched. Explicit pre-set env vars are preserved.
if command -v curl &>/dev/null && \
   [ -z "${GOPROXY:-}" ] && \
   ! curl -sSfL --max-time 3 -o /dev/null \
     "https://proxy.golang.org/github.com/golang/go/@latest" 2>/dev/null; then
  echo "==> proxy.golang.org unreachable; using China-friendly build mirrors."
  export GOPROXY="https://goproxy.cn,direct"
  export GOSUMDB="sum.golang.google.cn"
  export NPM_CONFIG_REGISTRY="https://registry.npmmirror.com"
fi

# Detect install path — prefer Homebrew prefix, then a writable /usr/local/bin,
# else fall back to a user-writable dir so non-Homebrew systems don't abort with
# a Permission denied at the install step.
if command -v brew &>/dev/null; then
  BIN_DIR="$(brew --prefix)/bin"
elif [ -w /usr/local/bin ]; then
  BIN_DIR="/usr/local/bin"
else
  BIN_DIR="$HOME/.local/bin"
  mkdir -p "$BIN_DIR"
fi

# Check dependencies — install via brew if available, otherwise point at the
# system package manager.
if ! command -v git &>/dev/null; then
  echo "error: git is required but not found. Install it with:" >&2
  suggest_install git
  exit 1
fi

if ! command -v go &>/dev/null; then
  if command -v brew &>/dev/null; then
    echo "==> Installing Go via Homebrew ..."
    brew install go
  else
    echo "error: go is required but not found. Install it with:" >&2
    suggest_install go
    exit 1
  fi
fi

echo "==> Cloning lingtai ($REF) ..."
if ! git clone --depth 1 --branch "$REF" "$REPO" "$BUILD_DIR" 2>/dev/null; then
  # --branch only resolves branches and tags; fall back to a default clone
  # plus an explicit fetch for commit SHAs and other refs. If that fetch
  # fails too, the ref does not exist — fail instead of silently building main.
  git clone --depth 1 "$REPO" "$BUILD_DIR"
  if [[ "$REF" != "main" ]]; then
    if ! (cd "$BUILD_DIR" && git fetch --depth 1 origin "$REF" && git checkout --quiet FETCH_HEAD); then
      echo "error: ref '$REF' not found in $REPO" >&2
      exit 1
    fi
  fi
fi

VERSION=$(cd "$BUILD_DIR" && git describe --tags --always 2>/dev/null || echo "dev")

echo "==> Building lingtai-tui ($VERSION) ..."
(cd "$BUILD_DIR/tui" && CGO_ENABLED=0 go build -ldflags "-X main.version=$VERSION" -o "$BUILD_DIR/lingtai-tui" .)

echo "==> Building lingtai-portal ($VERSION) ..."
if command -v npm &>/dev/null; then
  (cd "$BUILD_DIR/portal/web" && npm ci --silent && npm run build --silent)
  (cd "$BUILD_DIR/portal" && CGO_ENABLED=0 go build -ldflags "-X main.version=$VERSION" -o "$BUILD_DIR/lingtai-portal" .)
else
  echo "    (skipping portal — npm not found; to include it, install npm and re-run:)"
  suggest_install npm
fi

echo "==> Installing to $BIN_DIR ..."
install -m 755 "$BUILD_DIR/lingtai-tui" "$BIN_DIR/lingtai-tui"
# Create 'lingtai' alias for backward compatibility
# Only if 'lingtai' doesn't exist or is already a symlink to lingtai-tui
if [[ ! -e "$BIN_DIR/lingtai" ]] || [[ -L "$BIN_DIR/lingtai" && "$(readlink "$BIN_DIR/lingtai")" == "$BIN_DIR/lingtai-tui" ]]; then
  ln -sfn "$BIN_DIR/lingtai-tui" "$BIN_DIR/lingtai"
else
  echo "  (skipping 'lingtai' alias — $BIN_DIR/lingtai already exists)"
fi
if [[ -f "$BUILD_DIR/lingtai-portal" ]]; then
  install -m 755 "$BUILD_DIR/lingtai-portal" "$BIN_DIR/lingtai-portal"
fi

echo "==> Done. $("$BIN_DIR/lingtai-tui" version 2>&1 || echo "$VERSION")"

# Tell the user how to put BIN_DIR on PATH if it isn't already, so the next
# shell can find lingtai-tui (common on fresh accounts using the ~/.local/bin fallback).
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    echo "==> Note: $BIN_DIR is not on your PATH. Add it with:"
    echo "      echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.bashrc && source ~/.bashrc"
    ;;
esac

echo "    To revert to Homebrew version later: brew reinstall lingtai-tui"
