#!/usr/bin/env bash
# Download LingTai's public source archive, build lingtai-tui and lingtai-portal, and install them.
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
ARCHIVE_BASE_URL="https://codeload.github.com/Lingtai-AI/lingtai/tar.gz"
TMPDIR="${TMPDIR:-/tmp}"
WORK_DIR="$TMPDIR/lingtai-install-$$"
ARCHIVE_FILE="$WORK_DIR/source.tar.gz"
EXTRACT_DIR="$WORK_DIR/source"
BUILD_DIR=""

usage() {
  cat <<'EOF'
Download LingTai's public source archive, build lingtai-tui and lingtai-portal, and install them.

Usage:
  curl -sSL https://raw.githubusercontent.com/Lingtai-AI/lingtai/main/install.sh | bash
  ./install.sh [--ref <branch|tag|commit>]

Options:
  --ref <ref>   Git branch, tag, or commit to build (default: main)
  -h, --help    Show this help

The script downloads a public GitHub source archive instead of using `git clone`,
so it should not ask for a GitHub account. Binaries are installed to the first
of: Homebrew's bin directory, a writable /usr/local/bin, or ~/.local/bin. The
portal is skipped when npm is missing. Homebrew remains the primary install path:
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
  rm -rf "$WORK_DIR"
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
if ! command -v curl &>/dev/null; then
  echo "error: curl is required but not found. Install it with:" >&2
  suggest_install curl
  exit 1
fi

if ! command -v tar &>/dev/null; then
  echo "error: tar is required but not found. Install it with your system package manager." >&2
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

echo "==> Downloading lingtai source archive ($REF) ..."
mkdir -p "$EXTRACT_DIR"
ARCHIVE_URL="$ARCHIVE_BASE_URL/$REF"
if ! curl -fL --retry 2 --connect-timeout 10 "$ARCHIVE_URL" -o "$ARCHIVE_FILE"; then
  echo "error: could not download public source archive for ref '$REF'." >&2
  echo "       URL: $ARCHIVE_URL" >&2
  echo "       Check the ref name and network access, then retry." >&2
  exit 1
fi

if ! tar -xzf "$ARCHIVE_FILE" -C "$EXTRACT_DIR"; then
  echo "error: downloaded source archive could not be extracted." >&2
  exit 1
fi

BUILD_DIR=$(find "$EXTRACT_DIR" -mindepth 1 -maxdepth 1 -type d | head -n 1)
if [[ -z "$BUILD_DIR" || ! -d "$BUILD_DIR/tui" ]]; then
  echo "error: downloaded archive did not contain the expected LingTai source tree." >&2
  exit 1
fi

VERSION="$REF"

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
