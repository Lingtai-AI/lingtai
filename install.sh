#!/usr/bin/env bash
# One-shot installer for lingtai-tui and lingtai-portal, plus the Python
# `lingtai` runtime venv at ~/.lingtai-tui/runtime/venv.
#
# Homebrew is NOT required. By default this installs the latest GitHub Release:
# it downloads a prebuilt per-platform binary tarball when one exists, and
# otherwise falls back to building the release source tarball with Go/npm. If
# the installed Go is missing or older than tui/go.mod requires (distro
# packages often are), the official Go toolchain tarball is downloaded for the
# build. It then creates or updates the Python runtime venv and installs the
# `lingtai` package into it.
#
# Public entry point (once served from the website):
#   curl -fsSL https://lingtai.ai/install.sh | bash
#
# Direct-from-repo equivalent:
#   curl -fsSL https://raw.githubusercontent.com/Lingtai-AI/lingtai/main/install.sh | bash
#
# Install a specific release:
#   ./install.sh --version v0.10.5
#
# Binary release assets follow this naming convention (also produced by
# .github/workflows/release.yml):
#   lingtai-<tag>-<os>-<arch>.tar.gz    e.g. lingtai-v0.10.5-linux-amd64.tar.gz
# where <os> is darwin|linux and <arch> is amd64|arm64. The tarball contains
# lingtai-tui and (when built) lingtai-portal at its top level.
set -euo pipefail

REPO_SLUG="Lingtai-AI/lingtai"
REPO="https://github.com/${REPO_SLUG}.git"
API_BASE="https://api.github.com/repos/${REPO_SLUG}"
DOWNLOAD_BASE="https://github.com/${REPO_SLUG}/releases/download"
RAW_INSTALL_URL="https://raw.githubusercontent.com/${REPO_SLUG}/main/install.sh"
GO_DL_BASE="${LINGTAI_GO_DL_BASE:-https://go.dev/dl}"  # official Go toolchain downloads
NODE_DL_BASE="${LINGTAI_NODE_DL_BASE:-https://nodejs.org/dist}"
UV_INSTALLER_URL="${LINGTAI_UV_INSTALLER_URL:-https://astral.sh/uv/install.sh}"  # official uv bootstrap installer
NODE_TOOLCHAIN_VERSION="${LINGTAI_NODE_VERSION:-22.12.0}"

TMPDIR="${TMPDIR:-/tmp}"
BUILD_DIR="$TMPDIR/lingtai-install-$$"

# --- flags / state -----------------------------------------------------------
REF=""               # explicit source ref (branch/tag/commit) => forces source build
VERSION=""           # explicit release tag to install (default: latest release)
UPDATE_MODE=0        # --update: re-run for an existing source/user-local install
INSTALL_PREFIX=""    # --prefix: install root (bin_dir = <prefix>/bin)
BIN_DIR_OVERRIDE=""  # --bin-dir: explicit bin directory
NON_INTERACTIVE=0    # --non-interactive: never prompt / never sudo-install packages
FROM_SOURCE=0        # --from-source: skip release-asset download, always build
SKIP_PORTAL=0        # --skip-portal: TUI only
SKIP_VENV=0          # --skip-venv: don't touch the Python runtime venv
INSTALL_KIND=""      # "release-asset" | "source-build" (recorded in metadata)

usage() {
  cat <<'EOF'
One-shot installer for lingtai-tui, lingtai-portal, and the Python runtime.

Homebrew is not required. By default the latest GitHub Release is installed:
a prebuilt per-platform tarball when available, otherwise a source build.

Usage:
  curl -fsSL https://lingtai.ai/install.sh | bash
  ./install.sh [--version <tag>] [--bin-dir <dir>|--prefix <dir>]
  ./install.sh --update --prefix <prefix> --version <tag> --non-interactive

Options:
  --version <tag>      Release tag to install (default: latest GitHub release)
  --ref <ref>          Build a specific git branch/tag/commit from source
  --bin-dir <dir>      Install binaries into <dir>
  --prefix <dir>       Install binaries into <dir>/bin (used by --update)
  --from-source        Always build from source (skip prebuilt release assets)
  --skip-portal        Install only lingtai-tui (no portal)
  --skip-venv          Do not create/update the Python runtime venv
  --update             Update an existing source/user-local install in place
  --non-interactive    Never prompt; never install OS packages; fail instead
  -h, --help           Show this help

Binaries install to --bin-dir/--prefix if given, otherwise a writable
/usr/local/bin, otherwise ~/.local/bin. The portal is skipped when it can be
built from source but npm is missing. The Python runtime venv lives at
~/.lingtai-tui/runtime/venv.
EOF
}

# --- messaging helpers -------------------------------------------------------
say()  { echo "==> $*"; }
warn() { echo "warning: $*" >&2; }
note() { echo "    $*"; }

# is_wsl reports whether we're running under Windows Subsystem for Linux.
is_wsl() {
  if [[ -n "${WSL_DISTRO_NAME:-}" || -n "${WSL_INTEROP:-}" ]]; then
    return 0
  fi
  if [[ -r /proc/version ]] && grep -qiE 'microsoft|wsl' /proc/version 2>/dev/null; then
    return 0
  fi
  return 1
}

# Print a platform-appropriate install hint for a missing tool. Maps tool
# names to the package each manager actually ships (go is golang-go on
# Debian/Ubuntu, golang on Fedora, etc.). Homebrew is only suggested on macOS,
# never as the primary Linux path.
suggest_install() {
  local tool="$1" pkg="$1"
  if command -v apt-get &>/dev/null; then
    [[ "$tool" == "go" ]] && pkg="golang-go"
    [[ "$tool" == "npm" ]] && pkg="nodejs npm"
    [[ "$tool" == "python3" ]] && pkg="python3 python3-venv python3-pip"
    echo "      sudo apt-get update && sudo apt-get install -y $pkg" >&2
  elif command -v dnf &>/dev/null; then
    [[ "$tool" == "go" ]] && pkg="golang"
    [[ "$tool" == "npm" ]] && pkg="nodejs npm"
    [[ "$tool" == "python3" ]] && pkg="python3 python3-pip"
    echo "      sudo dnf install -y $pkg" >&2
  elif command -v pacman &>/dev/null; then
    [[ "$tool" == "npm" ]] && pkg="nodejs npm"
    [[ "$tool" == "python3" ]] && pkg="python python-pip"
    echo "      sudo pacman -S --needed $pkg" >&2
  elif command -v apk &>/dev/null; then
    [[ "$tool" == "npm" ]] && pkg="nodejs npm"
    [[ "$tool" == "python3" ]] && pkg="python3 py3-pip"
    echo "      sudo apk add $pkg" >&2
  elif command -v zypper &>/dev/null; then
    [[ "$tool" == "npm" ]] && pkg="nodejs npm"
    [[ "$tool" == "python3" ]] && pkg="python3 python3-pip"
    echo "      sudo zypper install $pkg" >&2
  elif [[ "$(uname -s)" == "Darwin" ]] || command -v brew &>/dev/null; then
    echo "      brew install $tool" >&2
  else
    echo "      install '$tool' with your system package manager" >&2
  fi
}

# --- platform detection ------------------------------------------------------

# detect_os prints darwin|linux, or "unsupported".
detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux)  echo "linux" ;;
    *)      echo "unsupported" ;;
  esac
}

# detect_arch prints amd64|arm64, or "unsupported".
detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64)          echo "amd64" ;;
    arm64 | aarch64)         echo "arm64" ;;
    *)                       echo "unsupported" ;;
  esac
}

# asset_name builds the release asset filename for a tag/os/arch triple. Keep
# this identical to the workflow's packaging step.
asset_name() {
  local tag="$1" os="$2" arch="$3"
  printf 'lingtai-%s-%s-%s.tar.gz' "$tag" "$os" "$arch"
}

# --- release metadata --------------------------------------------------------

# release_tag_name echoes its argument only when it is a strict vX.Y.Z tag,
# tolerating a refs/tags/ prefix. Empty output means "not an exact release tag".
release_tag_name() {
  local ref="${1#refs/tags/}"
  if [[ "$ref" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    printf '%s' "$ref"
  fi
}

# latest_release_tag queries the GitHub API for the latest published release
# tag. Falls back to the newest v* git tag if the API is unreachable.
latest_release_tag() {
  local body tag
  if command -v curl &>/dev/null; then
    body="$(curl -fsSL --max-time 15 "$API_BASE/releases/latest" 2>/dev/null || true)"
    tag="$(printf '%s' "$body" | grep -o '"tag_name"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')"
    if [[ -n "$tag" ]]; then
      printf '%s' "$tag"
      return 0
    fi
  fi
  # Fallback: newest semver-looking tag from the git remote.
  if command -v git &>/dev/null; then
    tag="$(git ls-remote --tags "$REPO" 'v*' 2>/dev/null \
      | sed 's#.*refs/tags/##; s/\^{}//' \
      | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
      | sort -t. -k1,1V | tail -1)"
    if [[ -n "$tag" ]]; then
      printf '%s' "$tag"
      return 0
    fi
  fi
  return 1
}

# release_asset_url echoes the download URL for an asset if the release exposes
# it, otherwise nothing. Uses the release API listing so a 404 tarball is not
# mistaken for a present asset.
release_asset_url() {
  local tag="$1" name="$2" body
  command -v curl &>/dev/null || return 1
  body="$(curl -fsSL --max-time 15 "$API_BASE/releases/tags/$tag" 2>/dev/null || true)"
  [[ -n "$body" ]] || return 1
  if printf '%s' "$body" | grep -q "\"name\"[[:space:]]*:[[:space:]]*\"$name\""; then
    printf '%s/%s/%s' "$DOWNLOAD_BASE" "$tag" "$name"
    return 0
  fi
  return 1
}

# --- git checkout version helpers (used by source build + tests) -------------

is_exact_checkout_tag() {
  local repo_dir="$1" tag="$2" tag_commit head_commit
  tag_commit="$(git -C "$repo_dir" rev-parse --verify --quiet "refs/tags/$tag^{commit}" 2>/dev/null || true)"
  if [[ -z "$tag_commit" ]]; then
    return 1
  fi
  head_commit="$(git -C "$repo_dir" rev-parse --verify HEAD 2>/dev/null || true)"
  if [[ -z "$head_commit" ]]; then
    return 1
  fi
  [[ "$head_commit" == "$tag_commit" ]]
}

version_for_checkout() {
  local repo_dir="$1" requested_ref="$2" requested_tag
  requested_tag="$(release_tag_name "$requested_ref")"
  if [[ -n "$requested_tag" ]] && is_exact_checkout_tag "$repo_dir" "$requested_tag"; then
    printf '%s\n' "$requested_tag"
    return
  fi
  git -C "$repo_dir" describe --tags --always 2>/dev/null || echo "dev"
}

resolved_ref_for_checkout() {
  local repo_dir="$1" exact_tag branch
  exact_tag="$(git -C "$repo_dir" describe --tags --exact-match 2>/dev/null || true)"
  if [[ -n "$exact_tag" ]]; then
    printf '%s\n' "$exact_tag"
    return
  fi
  branch="$(git -C "$repo_dir" symbolic-ref --quiet --short HEAD 2>/dev/null || true)"
  if [[ -n "$branch" ]]; then
    printf '%s\n' "$branch"
    return
  fi
  git -C "$repo_dir" rev-parse --short HEAD
}

# --- bin dir / prefix helpers ------------------------------------------------

prefix_for_bin_dir() {
  local bin_dir="$1"
  if [[ "$(basename "$bin_dir")" == "bin" ]]; then
    dirname "$bin_dir"
  else
    printf '%s\n' "$bin_dir"
  fi
}

bin_dir_for_prefix() {
  local prefix="$1"
  printf '%s/bin\n' "${prefix%/}"
}

install_binary_atomically() {
  local src="$1" dst="$2" dir base tmp
  dir="$(dirname "$dst")"
  base="$(basename "$dst")"
  tmp="$dir/.$base.tmp.$$"
  install -m 755 "$src" "$tmp"
  mv -f "$tmp" "$dst"
}

verify_tui_binary_version() {
  local binary="$1" want="$2" output
  output="$("$binary" version 2>&1)"
  case "$output" in
    *"$want"*) ;;
    *)
      echo "error: built lingtai-tui reports '$output', expected '$want'" >&2
      return 1
      ;;
  esac
}

ensure_lingtai_alias() {
  local bin_dir="$1"
  if [[ ! -e "$bin_dir/lingtai" ]] || [[ -L "$bin_dir/lingtai" && "$(readlink "$bin_dir/lingtai")" == "$bin_dir/lingtai-tui" ]]; then
    ln -sfn "$bin_dir/lingtai-tui" "$bin_dir/lingtai"
  else
    echo "  (skipping 'lingtai' alias — $bin_dir/lingtai already exists)"
  fi
}

# --- arg parsing -------------------------------------------------------------

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --ref) REF="${2:?error: --ref requires a value}"; shift 2 ;;
      --version) VERSION="${2:?error: --version requires a value}"; shift 2 ;;
      --prefix) INSTALL_PREFIX="${2:?error: --prefix requires a value}"; shift 2 ;;
      --bin-dir) BIN_DIR_OVERRIDE="${2:?error: --bin-dir requires a value}"; shift 2 ;;
      --from-source) FROM_SOURCE=1; shift ;;
      --skip-portal) SKIP_PORTAL=1; shift ;;
      --skip-venv) SKIP_VENV=1; shift ;;
      --update) UPDATE_MODE=1; shift ;;
      --non-interactive) NON_INTERACTIVE=1; shift ;;
      -h|--help) usage; exit 0 ;;
      *) echo "error: unknown flag: $1" >&2; usage >&2; exit 1 ;;
    esac
  done

  # --update is the TUI source updater contract: it passes --prefix and
  # --version and expects an in-place source-compatible update.
  if [[ "$UPDATE_MODE" == "1" ]]; then
    if [[ -z "$INSTALL_PREFIX" ]]; then
      echo "error: --update requires --prefix <prefix>" >&2
      usage >&2
      exit 1
    fi
    if [[ -z "$(release_tag_name "$VERSION")" ]]; then
      echo "error: --update requires --version <release-tag>" >&2
      usage >&2
      exit 1
    fi
  fi
}

# --- install metadata --------------------------------------------------------

json_escape() {
  local s="$1" ch ord
  local LC_ALL=C
  # LC_ALL=C makes Bash indexing byte-wise: UTF-8 metadata bytes pass through; JSON controls are escaped.
  local i

  for (( i = 0; i < ${#s}; i++ )); do
    ch="${s:i:1}"
    case "$ch" in
      \\) printf '\\\\' ;;
      '"') printf '\\"' ;;
      $'\b') printf '\\b' ;;
      $'\f') printf '\\f' ;;
      $'\n') printf '\\n' ;;
      $'\r') printf '\\r' ;;
      $'\t') printf '\\t' ;;
      *)
        printf -v ord '%d' "'$ch"
        (( ord < 0 )) && ord=$(( ord + 256 ))
        if (( ord < 32 )); then
          printf '\\u%04x' "$ord"
        else
          printf '%s' "$ch"
        fi
        ;;
    esac
  done
}

# write_install_metadata records the install so `lingtai-tui`'s source updater
# can re-run this script for a newer tag. install_method stays "source" for
# updater compatibility regardless of whether we downloaded a prebuilt asset or
# built from source; install_kind records which path was taken (additive field).
write_install_metadata() {
  local global_dir="$1" prefix="$2" bin_dir="$3" repo_url="$4" requested_ref="$5"
  local resolved_ref="$6" resolved_commit="$7" stamped_version="$8" tui_path="$9"
  local portal_path="${10:-}" metadata_path tmp_path installed_at portal_json=""
  local install_kind="${INSTALL_KIND:-source-build}"

  metadata_path="$global_dir/install.json"
  tmp_path="$metadata_path.tmp.$$"
  installed_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

  mkdir -p "$global_dir"
  if [[ -n "$portal_path" ]]; then
    portal_json="$(printf ',\n    "%s"' "$(json_escape "$portal_path")")"
  fi

  cat > "$tmp_path" <<EOF
{
  "schema": "lingtai.tui.install/v1",
  "schema_version": 1,
  "install_method": "source",
  "install_kind": "$(json_escape "$install_kind")",
  "prefix": "$(json_escape "$prefix")",
  "bin_dir": "$(json_escape "$bin_dir")",
  "repo_url": "$(json_escape "$repo_url")",
  "requested_ref": "$(json_escape "$requested_ref")",
  "resolved_ref": "$(json_escape "$resolved_ref")",
  "resolved_commit": "$(json_escape "$resolved_commit")",
  "stamped_version": "$(json_escape "$stamped_version")",
  "installed_at": "$(json_escape "$installed_at")",
  "managed_binaries": [
    "$(json_escape "$tui_path")"$portal_json
  ]
}
EOF
  mv "$tmp_path" "$metadata_path"
}

# --- OS package installation (Linux/WSL) -------------------------------------

# have_sudo reports whether we can run sudo non-interactively-ish. Root needs no
# sudo; otherwise sudo must exist.
have_root_or_sudo() {
  [[ "$(id -u)" == "0" ]] && return 0
  command -v sudo &>/dev/null
}

as_root() {
  if [[ "$(id -u)" == "0" ]]; then
    "$@"
  else
    sudo "$@"
  fi
}

# apt_install installs packages when interactive and root/sudo is available;
# otherwise prints the exact command and returns non-zero.
apt_install() {
  local why="$1"; shift
  if [[ "$NON_INTERACTIVE" == "1" ]] || ! have_root_or_sudo; then
    warn "$why — install the packages first:"
    echo "      sudo apt-get update && sudo apt-get install -y $*" >&2
    return 1
  fi
  say "Installing $why via apt: $*"
  as_root apt-get update
  as_root apt-get install -y "$@"
}

# --- Python runtime venv -----------------------------------------------------

find_uv() {
  if command -v uv &>/dev/null; then command -v uv; return 0; fi
  [[ -x "$HOME/.local/bin/uv" ]] && { echo "$HOME/.local/bin/uv"; return 0; }
  return 1
}

# ensure_uv resolves an executable uv, bootstrapping it if necessary. uv can
# download its own Python toolchain (uv venv --python 3.13), which is the only
# reliable way to get Python 3.11+ on distros whose system packages are older
# (e.g. Ubuntu jammy ships Python 3.10). If uv is already present it is reused;
# otherwise the official installer is downloaded to a temp file and run with an
# explicit UV_INSTALL_DIR so the result lands in a known location. On success it
# echoes the uv path and returns 0; on failure it warns loudly and returns 1
# without aborting the overall install.
ensure_uv() {
  local uv installer rc
  uv="$(find_uv 2>/dev/null || true)"
  if [[ -n "$uv" ]]; then
    echo "$uv"
    return 0
  fi

  if ! command -v curl &>/dev/null; then
    warn "curl is required to bootstrap uv but was not found."
    return 1
  fi

  local install_dir="${UV_INSTALL_DIR:-$HOME/.local/bin}"
  say "Bootstrapping uv (for a self-contained Python runtime) ..."
  mkdir -p "$install_dir"

  installer="$BUILD_DIR/uv-install.sh"
  mkdir -p "$BUILD_DIR"
  # Download to a temp file first so the script is fetched (and can be inspected)
  # before it is executed, rather than piping an unseen body straight into sh.
  if ! curl -fsSL --retry 3 --max-time 120 -o "$installer" "$UV_INSTALLER_URL"; then
    warn "failed to download the uv installer from $UV_INSTALLER_URL"
    return 1
  fi

  # UV_INSTALL_DIR pins where the uv binary lands; UV_NO_MODIFY_PATH keeps the
  # installer from editing shell rc files during a one-shot install.
  UV_INSTALL_DIR="$install_dir" UV_NO_MODIFY_PATH=1 sh "$installer" >/dev/null 2>&1
  rc=$?
  if [[ "$rc" -ne 0 ]]; then
    warn "the uv installer exited with status $rc"
    return 1
  fi

  # The freshly-installed uv may not be on PATH yet; find_uv also probes
  # ~/.local/bin, and we fold in an explicit install_dir check for custom dirs.
  uv="$(find_uv 2>/dev/null || true)"
  if [[ -z "$uv" && -x "$install_dir/uv" ]]; then
    uv="$install_dir/uv"
  fi
  if [[ -z "$uv" || ! -x "$uv" ]]; then
    warn "uv installer ran but no executable uv was found under $install_dir."
    return 1
  fi
  say "Bootstrapped uv at $uv."
  echo "$uv"
  return 0
}

# python_ok reports whether a python3 with venv/ensurepip support and >=3.11 is present.
python_ok() {
  command -v python3 &>/dev/null || return 1
  python3 -c 'import sys; sys.exit(0 if sys.version_info >= (3, 11) else 1)' 2>/dev/null || return 1
  python3 -c 'import venv, ensurepip' 2>/dev/null || return 1
  return 0
}

# ensure_python makes a usable Python interpreter available for the runtime venv.
# uv is preferred because it can download its own Python 3.13 toolchain, which is
# the only reliable path on distros whose packages are too old (Ubuntu jammy ships
# Python 3.10, so `apt install python3` does NOT yield a usable interpreter here).
# Order of preference:
#   1. an existing uv                       -> done (uv downloads Python itself)
#   2. an already-adequate system python3   -> done
#   3. bootstrap uv via the official installer (needs curl) -> done
#   4. apt-install python3/venv/pip and re-check python_ok (for distros where it
#      actually yields Python 3.11+, or where curl is unavailable for step 3)
ensure_python() {
  if find_uv >/dev/null 2>&1; then
    return 0  # uv can download Python itself
  fi
  if python_ok; then
    return 0
  fi
  # Try to bootstrap uv before falling back to system packages: on jammy the apt
  # python3 is 3.10, so uv is the only way to reach Python 3.11+.
  if ensure_uv >/dev/null; then
    return 0
  fi
  if command -v apt-get &>/dev/null; then
    apt_install "Python 3.11+ with venv/pip" python3 python3-venv python3-pip || return 1
    python_ok && return 0
    warn "apt-installed python3 is still older than 3.11 (or lacks venv); uv bootstrap is required."
  fi
  warn "Python 3.11+ (via uv or system packages) is required for the runtime venv. Install uv or Python 3.11+ with:"
  suggest_install python3
  return 1
}

# ensure_runtime_venv creates or updates ~/.lingtai-tui/runtime/venv and
# installs/upgrades the `lingtai` package into it, mirroring the TUI's own
# EnsureVenv logic (uv venv --python 3.13 if uv exists, else python3 -m venv;
# uv pip / pip install lingtai; verify import; stamp env marker; symlink
# lingtai-agent). Best-effort: a venv failure warns but does not abort the
# binary install.
ensure_runtime_venv() {
  local bin_dir="$1"
  local venv_dir="$HOME/.lingtai-tui/runtime/venv"
  local uv py repair_attempt

  if [[ "$SKIP_VENV" == "1" ]]; then
    note "Skipping Python runtime venv (--skip-venv)."
    return 0
  fi

  say "Setting up Python runtime venv at $venv_dir ..."
  if ! ensure_python; then
    warn "Skipping Python runtime venv — Python prerequisites are missing."
    warn "Re-run install after installing Python, or the TUI will create the venv on first launch."
    return 0
  fi

  mkdir -p "$(dirname "$venv_dir")"
  repair_attempt=0

  while true; do
    uv="$(find_uv 2>/dev/null || true)"
    py=""
    if [[ -x "$venv_dir/bin/python" ]]; then
      py="$venv_dir/bin/python"
    elif [[ -x "$venv_dir/bin/python3" ]]; then
      py="$venv_dir/bin/python3"
    fi

    local recreate_reason=""
    if [[ -d "$venv_dir" && -z "$py" ]]; then
      recreate_reason="runtime venv Python is missing"
    elif [[ -n "$py" ]] && ! "$py" -c 'import sys; sys.exit(0 if sys.version_info >= (3, 11) else 1)' 2>/dev/null; then
      recreate_reason="runtime venv Python is older than 3.11"
    fi

    if [[ -n "$recreate_reason" ]]; then
      if [[ "$repair_attempt" != "0" ]]; then
        warn "$recreate_reason after recreate; leaving runtime venv repair to the TUI."
        return 0
      fi
      warn "$recreate_reason; recreating runtime venv."
      rm -rf "$venv_dir"
      repair_attempt=1
      py=""
    fi

    if [[ -z "$py" ]]; then
      if [[ -n "$uv" ]]; then
        "$uv" venv --python 3.13 "$venv_dir" || { warn "uv venv failed; falling back to python3 -m venv"; uv=""; }
      fi
      if [[ ! -x "$venv_dir/bin/python" && ! -x "$venv_dir/bin/python3" && -z "$uv" ]]; then
        python3 -m venv "$venv_dir" || { warn "failed to create venv"; return 0; }
      fi
      if [[ -x "$venv_dir/bin/python" ]]; then
        py="$venv_dir/bin/python"
      elif [[ -x "$venv_dir/bin/python3" ]]; then
        py="$venv_dir/bin/python3"
      else
        warn "venv python not found at $venv_dir; skipping runtime setup."
        return 0
      fi
      # Re-check Python version after creating/recreating the venv.
      continue
    fi

    say "Installing/upgrading the lingtai Python package ..."
    local install_ok=0
    if [[ -n "$uv" ]]; then
      "$uv" pip install --upgrade lingtai -p "$venv_dir" && install_ok=1
    else
      if ! "$py" -m pip --version >/dev/null 2>&1; then
        if [[ "$repair_attempt" == "0" ]]; then
          warn "runtime venv pip is missing; recreating runtime venv."
          rm -rf "$venv_dir"
          repair_attempt=1
          continue
        fi
        warn "runtime venv pip is missing after recreate; TUI will repair it on first launch."
        return 0
      fi
      "$py" -m pip install --upgrade pip >/dev/null 2>&1 || true
      "$py" -m pip install --upgrade lingtai && install_ok=1
    fi
    if [[ "$install_ok" != "1" ]]; then
      if [[ "$repair_attempt" == "0" ]]; then
        warn "failed to install lingtai into runtime venv; recreating runtime venv once."
        rm -rf "$venv_dir"
        repair_attempt=1
        continue
      fi
      warn "failed to install lingtai into runtime venv after recreate; TUI will repair it on first launch."
      return 0
    fi

    if ! "$py" -c 'import lingtai; print("lingtai", getattr(lingtai, "__version__", "?"))'; then
      if [[ "$repair_attempt" == "0" ]]; then
        warn "runtime venv failed import check; recreating runtime venv once."
        rm -rf "$venv_dir"
        repair_attempt=1
        continue
      fi
      warn "runtime venv is still unhealthy after reinstall; TUI will repair it on first launch."
      return 0
    fi
    break
  done

  # Stamp the env marker (best-effort — older kernels may lack the subcommand).
  "$py" -m lingtai.venv_resolve env-marker stamp --venv "$venv_dir" >/dev/null 2>&1 || true

  # Symlink lingtai-agent into the chosen bin dir (best-effort).
  if [[ -x "$venv_dir/bin/lingtai-agent" ]]; then
    ln -sfn "$venv_dir/bin/lingtai-agent" "$bin_dir/lingtai-agent" 2>/dev/null \
      || warn "could not symlink lingtai-agent into $bin_dir"
  fi
  return 0
}


# --- install flows -----------------------------------------------------------

# resolve_bin_dir picks the install bin directory honoring --bin-dir/--prefix
# and, for --update, the existing prefix. Prefers user-writable locations; never
# prefers Homebrew.
resolve_bin_dir() {
  if [[ "$UPDATE_MODE" == "1" ]]; then
    BIN_DIR="$(bin_dir_for_prefix "$INSTALL_PREFIX")"
    if [[ ! -d "$BIN_DIR" ]]; then
      echo "error: update target bin dir does not exist: $BIN_DIR" >&2
      exit 1
    fi
    return
  fi
  if [[ -n "$BIN_DIR_OVERRIDE" ]]; then
    BIN_DIR="$BIN_DIR_OVERRIDE"
  elif [[ -n "$INSTALL_PREFIX" ]]; then
    BIN_DIR="$(bin_dir_for_prefix "$INSTALL_PREFIX")"
  elif [[ -w /usr/local/bin ]]; then
    BIN_DIR="/usr/local/bin"
  else
    BIN_DIR="$HOME/.local/bin"
  fi
  mkdir -p "$BIN_DIR"
}

# try_release_asset attempts to install prebuilt binaries for the tag. Returns 0
# on success (binaries installed to BIN_DIR), 1 if no asset was usable so the
# caller should fall back to a source build.
try_release_asset() {
  local tag="$1" os arch name url tarball extract_dir
  os="$(detect_os)"
  arch="$(detect_arch)"
  if [[ "$os" == "unsupported" || "$arch" == "unsupported" ]]; then
    note "No prebuilt asset for $(uname -s)/$(uname -m); will build from source."
    return 1
  fi
  command -v curl &>/dev/null || { note "curl unavailable; will build from source."; return 1; }

  name="$(asset_name "$tag" "$os" "$arch")"
  if ! url="$(release_asset_url "$tag" "$name")"; then
    note "Release $tag has no prebuilt asset ($name); will build from source."
    return 1
  fi

  say "Downloading prebuilt binaries: $name"
  mkdir -p "$BUILD_DIR"
  tarball="$BUILD_DIR/$name"
  extract_dir="$BUILD_DIR/asset"
  mkdir -p "$extract_dir"
  if ! curl -fsSL --max-time 120 -o "$tarball" "$url"; then
    warn "download failed for $url; will build from source."
    return 1
  fi
  if ! tar -xzf "$tarball" -C "$extract_dir"; then
    warn "could not extract $tarball; will build from source."
    return 1
  fi

  local tui portal
  tui="$(find "$extract_dir" -type f -name lingtai-tui | head -1)"
  if [[ -z "$tui" ]]; then
    warn "asset $name did not contain lingtai-tui; will build from source."
    return 1
  fi

  install -m 755 "$tui" "$BIN_DIR/lingtai-tui"
  PORTAL_PATH=""
  if [[ "$SKIP_PORTAL" != "1" ]]; then
    portal="$(find "$extract_dir" -type f -name lingtai-portal | head -1)"
    if [[ -n "$portal" ]]; then
      install -m 755 "$portal" "$BIN_DIR/lingtai-portal"
      PORTAL_PATH="$BIN_DIR/lingtai-portal"
    fi
  fi

  ensure_lingtai_alias "$BIN_DIR"
  VERSION="$tag"
  RESOLVED_REF="$tag"
  RESOLVED_COMMIT=""
  INSTALL_KIND="release-asset"
  # Verify the downloaded binary reports the expected version.
  verify_tui_binary_version "$BIN_DIR/lingtai-tui" "$tag" || {
    warn "prebuilt lingtai-tui version mismatch; will rebuild from source."
    return 1
  }
  return 0
}

# build_from_source clones REF (or the release source tarball for a tag) and
# builds both binaries. Installs to BIN_DIR. Sets VERSION/RESOLVED_*/PORTAL_PATH.
build_from_source() {
  local ref="$1" requested_tag source_tarball

  requested_tag="$(release_tag_name "$ref")"
  mkdir -p "$(dirname "$BUILD_DIR")"
  rm -rf "$BUILD_DIR"

  if [[ -n "$requested_tag" ]]; then
    # Release installs must stay GitHub-Release based even when no prebuilt
    # asset exists. Use the release source tarball instead of cloning raw main.
    ensure_build_deps 0
    command -v curl &>/dev/null || { echo "error: curl is required to download the release source tarball" >&2; exit 1; }
    command -v tar &>/dev/null || { echo "error: tar is required to extract the release source tarball" >&2; exit 1; }
    say "Downloading lingtai release source ($requested_tag) ..."
    source_tarball="$TMPDIR/lingtai-$requested_tag-src-$$.tar.gz"
    curl -fsSL --max-time 120 \
      -o "$source_tarball" \
      "https://github.com/${REPO_SLUG}/archive/refs/tags/${requested_tag}.tar.gz"
    mkdir -p "$BUILD_DIR"
    tar -xzf "$source_tarball" -C "$BUILD_DIR" --strip-components 1
    rm -f "$source_tarball"
    VERSION="$requested_tag"
    RESOLVED_REF="$requested_tag"
    RESOLVED_COMMIT="$(git ls-remote --tags "$REPO" "refs/tags/$requested_tag" 2>/dev/null | awk '{print $1}' | head -1 || true)"
  else
    ensure_build_deps 1
    say "Cloning lingtai ($ref) ..."
    if ! git clone --depth 1 --branch "$ref" "$REPO" "$BUILD_DIR" 2>/dev/null; then
      # --branch only resolves branches and tags; fall back to a default clone
      # plus an explicit fetch for commit SHAs and other refs.
      git clone --depth 1 "$REPO" "$BUILD_DIR"
      if [[ "$ref" != "main" ]]; then
        if ! (cd "$BUILD_DIR" && git fetch --depth 1 origin "$ref" && git checkout --quiet FETCH_HEAD); then
          echo "error: ref '$ref' not found in $REPO" >&2
          exit 1
        fi
      fi
    fi

    VERSION="$(version_for_checkout "$BUILD_DIR" "$ref")"
    RESOLVED_REF="$(resolved_ref_for_checkout "$BUILD_DIR")"
    RESOLVED_COMMIT="$(git -C "$BUILD_DIR" rev-parse HEAD)"
  fi
  INSTALL_KIND="source-build"

  ensure_go_for_source "$BUILD_DIR"

  say "Building lingtai-tui ($VERSION) ..."
  (cd "$BUILD_DIR/tui" && CGO_ENABLED=0 go build -ldflags "-X main.version=$VERSION" -o "$BUILD_DIR/lingtai-tui" .)

  PORTAL_BUILT=0
  if [[ "$SKIP_PORTAL" == "1" ]]; then
    note "Skipping portal (--skip-portal)."
  else
    if ensure_node_for_portal; then
      say "Building lingtai-portal ($VERSION) ..."
      if (cd "$BUILD_DIR/portal/web" && npm ci --silent && npm run build --silent) &&          (cd "$BUILD_DIR/portal" && CGO_ENABLED=0 go build -ldflags "-X main.version=$VERSION" -o "$BUILD_DIR/lingtai-portal" .); then
        PORTAL_BUILT=1
      else
        warn "Skipping portal — portal build failed; continuing with lingtai-tui only."
        note "$(portal_node_requirement_note)"
      fi
    else
      warn "Skipping portal — could not prepare a supported Node.js/npm toolchain."
      note "$(portal_node_requirement_note)"
    fi
  fi

  # Install binaries.
  PORTAL_PATH=""
  if [[ "$UPDATE_MODE" == "1" ]]; then
    local stage_bin="$BUILD_DIR/stage/bin"
    mkdir -p "$stage_bin"
    install -m 755 "$BUILD_DIR/lingtai-tui" "$stage_bin/lingtai-tui"
    verify_tui_binary_version "$stage_bin/lingtai-tui" "$VERSION"
    say "Installing update to $BIN_DIR ..."
    install_binary_atomically "$stage_bin/lingtai-tui" "$BIN_DIR/lingtai-tui"
    if [[ "$PORTAL_BUILT" == "1" ]]; then
      install -m 755 "$BUILD_DIR/lingtai-portal" "$stage_bin/lingtai-portal"
      install_binary_atomically "$stage_bin/lingtai-portal" "$BIN_DIR/lingtai-portal"
      PORTAL_PATH="$BIN_DIR/lingtai-portal"
    fi
  else
    say "Installing to $BIN_DIR ..."
    install -m 755 "$BUILD_DIR/lingtai-tui" "$BIN_DIR/lingtai-tui"
    if [[ "$PORTAL_BUILT" == "1" ]]; then
      install -m 755 "$BUILD_DIR/lingtai-portal" "$BIN_DIR/lingtai-portal"
      PORTAL_PATH="$BIN_DIR/lingtai-portal"
    fi
  fi
  ensure_lingtai_alias "$BIN_DIR"
  if [[ "$UPDATE_MODE" == "1" ]]; then
    verify_tui_binary_version "$BIN_DIR/lingtai-tui" "$VERSION"
  fi
}

# normalize_go_version prints MAJOR.MINOR.PATCH for Go language/toolchain
# versions (for example: 1.26 -> 1.26.0, go1.26.1 -> 1.26.1).
normalize_go_version() {
  local version="${1#go}"
  if [[ "$version" =~ ^([0-9]+)\.([0-9]+)$ ]]; then
    printf '%s.%s.0\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}"
    return 0
  fi
  if [[ "$version" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    printf '%s.%s.%s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}"
    return 0
  fi
  return 1
}

# go_version_ge returns success when $1 >= $2 using numeric major/minor/patch
# comparison. Both inputs may optionally include the leading "go" prefix.
go_version_ge() {
  local have required hmaj hmin hpatch rmaj rmin rpatch
  have="$(normalize_go_version "$1")" || return 1
  required="$(normalize_go_version "$2")" || return 1
  IFS=. read -r hmaj hmin hpatch <<<"$have"
  IFS=. read -r rmaj rmin rpatch <<<"$required"
  (( hmaj > rmaj )) && return 0
  (( hmaj < rmaj )) && return 1
  (( hmin > rmin )) && return 0
  (( hmin < rmin )) && return 1
  (( hpatch >= rpatch ))
}

installed_go_version() {
  command -v go &>/dev/null || return 1
  go version 2>/dev/null | sed -n 's/^go version go\([0-9][0-9.]*\).*/\1/p' | head -1
}

required_go_version_for_source() {
  local source_dir="$1" version
  version="$(awk '$1 == "go" { print $2; exit }' "$source_dir/tui/go.mod" 2>/dev/null || true)"
  [[ -n "$version" ]] || return 1
  normalize_go_version "$version"
}

go_toolchain_archive_name() {
  local version="$1" os="$2" arch="$3"
  printf 'go%s.%s-%s.tar.gz\n' "$version" "$os" "$arch"
}

go_toolchain_download_url() {
  local version="$1" os="$2" arch="$3"
  printf '%s/%s\n' "${GO_DL_BASE%/}" "$(go_toolchain_archive_name "$version" "$os" "$arch")"
}

install_go_toolchain() {
  local version="$1" os arch root archive url fallback_url installed
  os="$(detect_os)"
  arch="$(detect_arch)"
  if [[ "$os" == "unsupported" || "$arch" == "unsupported" ]]; then
    echo "error: Go $version is required, but automatic Go toolchain download is unsupported on $(uname -s)/$(uname -m)." >&2
    echo "Install Go $version or newer manually, then re-run this installer." >&2
    exit 1
  fi
  command -v curl &>/dev/null || { echo "error: curl is required to download Go $version" >&2; exit 1; }
  command -v tar &>/dev/null || { echo "error: tar is required to extract Go $version" >&2; exit 1; }

  root="$BUILD_DIR/go-toolchain"
  archive="$root/$(go_toolchain_archive_name "$version" "$os" "$arch")"
  rm -rf "$root"
  mkdir -p "$root"
  url="$(go_toolchain_download_url "$version" "$os" "$arch")"
  fallback_url="https://dl.google.com/go/$(go_toolchain_archive_name "$version" "$os" "$arch")"

  say "Downloading Go $version toolchain for source build ($os/$arch) ..."
  if ! curl -fsSL --retry 3 --max-time 300 -o "$archive" "$url"; then
    if [[ "$url" != "$fallback_url" ]]; then
      warn "Go download failed from $url; retrying $fallback_url"
      curl -fsSL --retry 3 --max-time 300 -o "$archive" "$fallback_url"
    else
      return 1
    fi
  fi
  tar -xzf "$archive" -C "$root"
  export PATH="$root/go/bin:$PATH"
  installed="$(installed_go_version || true)"
  if ! go_version_ge "$installed" "$version"; then
    echo "error: downloaded Go toolchain is $installed, expected $version or newer" >&2
    exit 1
  fi
}

ensure_go_for_source() {
  local source_dir="$1" required installed
  required="$(required_go_version_for_source "$source_dir")" || {
    echo "error: could not read required Go version from $source_dir/tui/go.mod" >&2
    exit 1
  }
  installed="$(installed_go_version || true)"
  if [[ -n "$installed" ]] && go_version_ge "$installed" "$required"; then
    note "Using Go $installed for source build (requires >= $required)."
    return 0
  fi
  if [[ -n "$installed" ]]; then
    note "Installed Go $installed is older than required $required; using official Go toolchain for this build."
  else
    note "Go is not installed; using official Go $required toolchain for this build."
  fi
  install_go_toolchain "$required"
}

normalize_node_version() {
  local version="${1#v}"
  if [[ "$version" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    printf '%s.%s.%s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}"
    return 0
  fi
  return 1
}

installed_node_version() {
  command -v node &>/dev/null || return 1
  node --version 2>/dev/null | sed -n 's/^v\([0-9][0-9.]*\)$/\1/p' | head -1
}

portal_node_supported() {
  local version major minor patch
  version="$(normalize_node_version "$1")" || return 1
  IFS=. read -r major minor patch <<<"$version"
  if (( major == 20 )); then
    (( minor >= 19 ))
    return
  fi
  if (( major == 22 )); then
    (( minor >= 12 ))
    return
  fi
  (( major > 22 ))
}

portal_node_requirement_note() {
  echo "Node.js 20.19+ or 22.12+ is required to build lingtai-portal. The installer can use an official temporary Node toolchain; if that download fails, upgrade Node and re-run the installer to add the portal binary."
}

node_toolchain_arch() {
  case "$(detect_arch)" in
    amd64) echo "x64" ;;
    arm64) echo "arm64" ;;
    *) echo "unsupported" ;;
  esac
}

node_toolchain_archive_name() {
  local version="$1" os="$2" arch="$3"
  printf 'node-v%s-%s-%s.tar.gz\n' "$version" "$os" "$arch"
}

node_toolchain_download_url() {
  local version="$1" os="$2" arch="$3"
  printf '%s/v%s/%s\n' "${NODE_DL_BASE%/}" "$version" "$(node_toolchain_archive_name "$version" "$os" "$arch")"
}

install_node_toolchain() {
  local version="${1:-$NODE_TOOLCHAIN_VERSION}" os arch root archive url dirname installed
  os="$(detect_os)"
  arch="$(node_toolchain_arch)"
  if [[ "$os" == "unsupported" || "$arch" == "unsupported" ]]; then
    warn "Automatic Node.js toolchain download is unsupported on $(uname -s)/$(uname -m)."
    return 1
  fi
  command -v curl &>/dev/null || { warn "curl is required to download Node.js $version"; return 1; }
  command -v tar &>/dev/null || { warn "tar is required to extract Node.js $version"; return 1; }

  root="$BUILD_DIR/node-toolchain"
  archive="$root/$(node_toolchain_archive_name "$version" "$os" "$arch")"
  dirname="node-v${version}-${os}-${arch}"
  rm -rf "$root"
  mkdir -p "$root"
  url="$(node_toolchain_download_url "$version" "$os" "$arch")"

  say "Downloading Node.js $version toolchain for portal build ($os/$arch) ..."
  if ! curl -fsSL --retry 3 --max-time 300 -o "$archive" "$url"; then
    warn "Node.js download failed from $url"
    return 1
  fi
  if ! tar -xzf "$archive" -C "$root"; then
    warn "Node.js archive extraction failed"
    return 1
  fi
  export PATH="$root/$dirname/bin:$PATH"
  installed="$(installed_node_version || true)"
  if [[ -z "$installed" ]] || ! portal_node_supported "$installed"; then
    warn "Downloaded Node.js toolchain is ${installed:-unavailable}, expected $version or another supported version"
    return 1
  fi
}

ensure_node_for_portal() {
  local installed
  installed="$(installed_node_version || true)"
  if [[ -n "$installed" ]] && portal_node_supported "$installed" && command -v npm &>/dev/null; then
    note "Using Node.js $installed for portal build."
    return 0
  fi
  if [[ -n "$installed" ]]; then
    warn "Node.js $installed is not supported for portal build; using official Node.js $NODE_TOOLCHAIN_VERSION for this build."
  else
    warn "Node.js is not available; using official Node.js $NODE_TOOLCHAIN_VERSION for the portal build."
  fi
  install_node_toolchain "$NODE_TOOLCHAIN_VERSION"
}


# ensure_build_deps checks/installs non-Go source-build dependencies. Go is
# validated after the source tree is available, because tui/go.mod declares the
# required version and distro packages (for example Ubuntu jammy Go 1.18) may be
# too old.
ensure_build_deps() {
  local need_git="${1:-1}"
  if [[ "$need_git" == "1" ]] && ! command -v git &>/dev/null; then
    if command -v apt-get &>/dev/null && apt_install "git (build dependency)" git; then
      :
    else
      echo "error: git is required for --ref source builds but not found. Install it with:" >&2
      suggest_install git
      exit 1
    fi
  fi
}

# --- main --------------------------------------------------------------------

main() {
parse_args "$@"

# Remove the build directory even when a build or install step fails midway.
cleanup() {
  cd / 2>/dev/null || true
  rm -rf "$BUILD_DIR"
}
trap cleanup EXIT

if is_wsl; then
  say "Detected Windows Subsystem for Linux (WSL)."
  note "Binaries and the Python runtime install into your Linux home ($HOME)."
  note "Run lingtai-tui from your WSL shell, not Windows PowerShell."
fi

# Auto-detect CN-restricted networks. If proxy.golang.org is unreachable
# within 3 seconds (typical on mainland China without VPN), fall back to
# CN-accessible mirrors for Go modules, the Go checksum database, and npm.
# Only relevant when we build from source, but harmless otherwise. Explicit
# pre-set env vars are preserved.
if command -v curl &>/dev/null && \
   [ -z "${GOPROXY:-}" ] && \
   ! curl -sSfL --max-time 3 -o /dev/null \
     "https://proxy.golang.org/github.com/golang/go/@latest" 2>/dev/null; then
  say "proxy.golang.org unreachable; using China-friendly build mirrors."
  export GOPROXY="https://goproxy.cn,direct"
  export GOSUMDB="sum.golang.google.cn"
  export NPM_CONFIG_REGISTRY="https://registry.npmmirror.com"
fi

resolve_bin_dir

# Decide what to install.
#   --update      : source-compatible in-place update for the given tag
#   --ref         : explicit source build of that ref
#   --version tag : that release (asset, else source tarball)
#   default       : latest release (asset, else source tarball)
if [[ "$UPDATE_MODE" == "1" ]]; then
  # The TUI source updater expects a source-compatible update. Try the prebuilt
  # asset first (fast), then fall back to building the tag from source.
  if [[ "$FROM_SOURCE" != "1" ]] && try_release_asset "$VERSION"; then
    :
  else
    build_from_source "$VERSION"
  fi
elif [[ -n "$REF" ]]; then
  build_from_source "$REF"
else
  TARGET_TAG="$VERSION"
  if [[ -z "$TARGET_TAG" ]]; then
    say "Resolving latest release ..."
    if ! TARGET_TAG="$(latest_release_tag)"; then
      echo "error: could not determine the latest release tag from GitHub." >&2
      echo "       Pass one explicitly: ./install.sh --version vX.Y.Z" >&2
      exit 1
    fi
    say "Latest release is $TARGET_TAG"
  fi
  if [[ -z "$(release_tag_name "$TARGET_TAG")" ]]; then
    warn "'$TARGET_TAG' is not a vX.Y.Z release tag; treating it as a source ref."
    build_from_source "$TARGET_TAG"
  elif [[ "$FROM_SOURCE" != "1" ]] && try_release_asset "$TARGET_TAG"; then
    :
  else
    build_from_source "$TARGET_TAG"
  fi
fi

# Record install metadata for the TUI source updater.
GLOBAL_DIR="$HOME/.lingtai-tui"
PREFIX="$(prefix_for_bin_dir "$BIN_DIR")"
REQUESTED_REF="${REF:-${VERSION:-main}}"
write_install_metadata \
  "$GLOBAL_DIR" \
  "$PREFIX" \
  "$BIN_DIR" \
  "$REPO" \
  "$REQUESTED_REF" \
  "${RESOLVED_REF:-$VERSION}" \
  "${RESOLVED_COMMIT:-}" \
  "$VERSION" \
  "$BIN_DIR/lingtai-tui" \
  "$PORTAL_PATH"
say "Wrote install metadata to $GLOBAL_DIR/install.json"

# One-shot Python runtime venv (skipped for --update to keep the updater fast;
# the TUI verifies/creates the venv itself after a source update).
if [[ "$UPDATE_MODE" != "1" ]]; then
  ensure_runtime_venv "$BIN_DIR"
fi

say "Done. $("$BIN_DIR/lingtai-tui" version 2>&1 || echo "$VERSION")"

# Tell the user how to put BIN_DIR on PATH if it isn't already.
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    say "Note: $BIN_DIR is not on your PATH. Add it with:"
    note "echo 'export PATH=\"$BIN_DIR:\$PATH\"' >> ~/.bashrc && source ~/.bashrc"
    ;;
esac
}

if [[ "${LINGTAI_INSTALL_SH_SOURCE_ONLY:-0}" != "1" ]]; then
  main "$@"
fi
