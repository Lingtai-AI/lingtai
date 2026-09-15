#!/usr/bin/env bash
# One-shot installer for lingtai-tui and the Python `lingtai` runtime venv at
# ~/.lingtai-tui/runtime/venv.
#
# Homebrew is NOT required. The no-argument path resolves the current release
# from lingtai.ai, then verifies and builds the producer-owned TUI source
# archive locally. If lingtai.ai cannot provide it, the installer independently
# falls back to the latest GitHub TUI source release and builds it. Explicit version, update,
# source-provider, and current-main modes retain their existing source-build behavior.
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
# Native Windows installs use install.ps1 and its explicit local-artifact mode.
#
# Source policy (--source auto|github|mirror, or LINGTAI_SOURCE): the ordinary
# no-version `auto`/`mirror` route reads lingtai.ai latest metadata to select one
# exact stable source tag and source archive, then always builds the TUI locally.
# If that source resolution is unavailable, it independently resolves the latest
# GitHub TUI source release and builds it. The kernel resolves its own latest
# source archive and fallback provider.
# Explicit --version, --source github, --ref/--from-source/--update, and --latest
# retain their existing source-build behavior. --source gitee is retired.
#
# LingTai is NEVER installed by requesting the package name "lingtai" from
# any index. The default kernel source archive is independently selected and
# verified from lingtai.ai, with a GitHub source fallback for that component
# only. --ref/source-ref builds have no release kernel to install and therefore require
# --skip-python. That flag is the explicit binary-only opt-out.
set -euo pipefail

REPO_SLUG="Lingtai-AI/lingtai"
REPO="https://github.com/${REPO_SLUG}.git"
KERNEL_REPO_SLUG="Lingtai-AI/lingtai-kernel"
KERNEL_REPO="https://github.com/${KERNEL_REPO_SLUG}.git"
API_BASE="https://api.github.com/repos/${REPO_SLUG}"
DOWNLOAD_BASE="https://github.com/${REPO_SLUG}/releases/download"
RAW_INSTALL_URL="https://raw.githubusercontent.com/${REPO_SLUG}/main/install.sh"
GO_DL_BASE="${LINGTAI_GO_DL_BASE:-https://go.dev/dl}"  # official Go toolchain downloads
UV_INSTALLER_URL="${LINGTAI_UV_INSTALLER_URL:-https://astral.sh/uv/install.sh}"  # official uv bootstrap installer
DESKTOP_VERSION="0.1.10"
DESKTOP_RAW_BASE="https://raw.githubusercontent.com/Lingtai-AI/lingtai-desktop"
DESKTOP_RELEASE_BASE="https://github.com/Lingtai-AI/lingtai-desktop/releases/download"
# Audited from lingtai-desktop v0.1.10 commit
# fd39dd61e4d123b2835064c1c148566d8b36ceb0. These are deliberately not
# environment-overridable: the registered lazy bootstrap accepts only these
# exact installer-support bytes before running them.
DESKTOP_INSTALLER_SHA256="d915162c41b144fad19cd47405c36ceb5f408ca15fabd342d3b3615c53f654c9"
DESKTOP_CLI_SHA256="0a681eacdf71daea137089e68204b780f6e065184689d9b56208a67c24facc95"
DESKTOP_VERIFIER_SHA256="745374c0634709fa235cd7b63af6cd78b79f99ef5d290157d6e7bd281b3e8fc2"
DESKTOP_BOOTSTRAP_SHA256="6c246f7af6602eeee0d697bcd5c830029939bd786ba3ecbf3cf8c41846ac02e6"

# The single package index used ONLY for third-party dependencies of the
# verified local LingTai artifact — see python_dependency_index_url. Tsinghua
# TUNA is a cloud-neutral domestic PyPI mirror; it is the mirror-path default
# because pypi.org is not reliably reachable from mainland-China hosts, which
# makes dependency resolution the remaining failure point of an otherwise
# checksum-verified mirror install.
PYPI_INDEX_URL_DEFAULT="https://pypi.org/simple"
PYPI_INDEX_URL_MIRROR_DEFAULT="https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple"

KERNEL_GH_API_BASE="https://api.github.com/repos/Lingtai-AI/lingtai-kernel"

# Country-detection endpoints for auto source selection. Two independent,
# unauthenticated, no-signup providers so one outage doesn't force a GitHub
# fallback for every mainland user; each probe is short-timeout and its
# result is discarded (fail-open) on any error. Only the two-letter country
# code of the requester's public IP is requested — no identity, no
# credentials, no persistent client. Overridable for tests/offline use.
COUNTRY_DETECT_URL_1="${LINGTAI_COUNTRY_DETECT_URL_1:-https://ipapi.co/country/}"
COUNTRY_DETECT_URL_2="${LINGTAI_COUNTRY_DETECT_URL_2:-https://ifconfig.co/country-iso}"
MIRROR_TIMEOUT="${LINGTAI_MIRROR_TIMEOUT:-30}"

# Canonical URLs for the standalone maintenance scripts (update.sh/fix.sh/
# verify.sh/dev.sh). The lingtai-web sync-installers workflow publishes those
# under help/reference/installation/assets/ while install.sh/remove.sh stay at
# the web root -- so a hint that only names the script would lead a user to
# guess the root URL and hit a 404. Overridable for tests/offline use.
LINGTAI_WEB_BASE="${LINGTAI_WEB_BASE:-https://lingtai.ai}"
LINGTAI_SCRIPTS_ASSETS="$LINGTAI_WEB_BASE/help/reference/installation/assets"

TMPDIR="${TMPDIR:-/tmp}"
BUILD_DIR="$TMPDIR/lingtai-install-$$"

# --- flags / state -----------------------------------------------------------
REF=""               # explicit source ref (branch/tag/commit) => forces source build
VERSION=""           # explicit release tag to install (default: latest release)
LATEST_MAIN_MODE=0   # --latest: explicit current-main TUI + kernel source install
UPDATE_MODE=0        # --update: re-run for an existing source/user-local install
REINSTALL_OK=0       # 1 when the default one-command path finds an existing receipt: reinstall in place (binaries + runtime refreshed; credentials/config untouched)
INSTALL_PREFIX=""    # --prefix: install root (bin_dir = <prefix>/bin)
BIN_DIR_OVERRIDE=""  # --bin-dir: explicit bin directory
NON_INTERACTIVE=0    # --non-interactive: never prompt / never sudo-install packages
FROM_SOURCE=0        # --from-source: backwards-compatible GitHub source-build selector
SOURCE_ONLY_DEFAULT=0 # ordinary lingtai.ai route: always build; GitHub source fallback
SKIP_VENV=0          # --skip-python (alias: --skip-venv): don't touch the Python runtime venv
SKIP_DESKTOP=0       # --skip-desktop: don't register the macOS-only lazy Desktop command
INSTALL_KIND=""      # "source-build" (recorded in metadata)
SOURCE_ARG="${LINGTAI_SOURCE:-auto}"  # auto is the default lingtai.ai route; github is explicit
TUI_PROVIDER=""       # independently resolved: github | mirror
TUI_TAG=""
TUI_SOURCE_ASSET=""
KERNEL_SOURCE=""      # "release" | "main" | "" (recorded after verified provisioning)
KERNEL_RELEASE_TAG=""
KERNEL_VERSION_INSTALLED=""
KERNEL_PROVIDER=""
KERNEL_MANIFEST_PROVIDER=""  # set by fetch_kernel_manifest(); which provider actually served the kernel manifest
KERNEL_MANIFEST_JSON=""      # set by fetch_kernel_manifest() in the same shell as the provider
KERNEL_LATEST_TAG=""         # set by resolve_latest_kernel_release(); newest published kernel release
MIRROR_TUI_LATEST_JSON=""
MIRROR_TUI_LATEST_TAG=""
MIRROR_KERNEL_LATEST_JSON=""
MIRROR_KERNEL_LATEST_TAG=""
TUI_MAIN_SHA=""
KERNEL_MAIN_SHA=""
KERNEL_SOURCE_DIR=""
RUNTIME_VENV_DIR=""   # set by ensure_runtime_venv() on success; read by write_install_metadata

usage() {
  cat <<'EOF'
One-shot installer for lingtai-tui, the Python runtime, and
LingTai Desktop on macOS.

Homebrew is not required. By default lingtai.ai resolves the latest stable
source release and the TUI is always built locally. If that resolution is
unavailable, the latest GitHub source release is built instead.

Usage:
  curl -fsSL https://lingtai.ai/install.sh | bash
  curl -fsSL https://lingtai.ai/install.sh | bash -s -- --latest
  ./install.sh [--version <tag>] [--bin-dir <dir>|--prefix <dir>]
  ./install.sh --update --prefix <prefix> --version <tag> --non-interactive

Options:
  --latest             Explicitly build TUI main + kernel main from source;
                       records and prints both resolved full commit SHAs
  --version <tag>      Release tag to install (default: latest from --source)
  --ref <ref>          Build a specific git branch/tag/commit from source
  --bin-dir <dir>      Install binaries into <dir>
  --prefix <dir>       Install binaries into <dir>/bin (used by --update)
  --from-source        Select the GitHub source-build path (backwards-compatible)
  --skip-python         Do not create/update the Python runtime venv (explicit
                         opt-out; required for arbitrary source refs).
                         If a legacy ~/.lingtai-tui/runtime directory already
                         exists without a native install receipt, it is
                         preserved unchanged so the TUI binary can be
                         installed beside it. --skip-venv is a back-compat alias.
  --skip-desktop        On macOS, do not register the lazy lingtai-desktop
                         command. Registration is the default for an ordinary
                         stable install and a version-pinned --update; it
                         downloads no Desktop App data. The command's first
                         execution installs Desktop. Linux, WSL, Windows,
                         --latest, --ref, and ordinary existing-receipt
                         reinstalls are unaffected. This installer pins Desktop
                         v0.1.10 and its audited four-file installer-support
                         checksums as one trust set.
  --source <mode>       auto|github|mirror (default: auto, or $LINGTAI_SOURCE).
                         auto/mirror use lingtai.ai to resolve the ordinary
                         no-version source release, always build it locally,
                         and fall back to the latest GitHub source release when
                         that resolution is unavailable. Explicit versions and
                         source/current-main modes use their source-build paths;
                         --source github forces GitHub. --source gitee is retired.
  --update             Update an existing source/user-local install in place;
                         on macOS, register or refresh the lazy Desktop command
  --non-interactive    Never prompt; never install OS packages; fail instead
  -h, --help           Show this help

Binaries install to --bin-dir/--prefix if given, otherwise a writable
/usr/local/bin, otherwise ~/.local/bin. The Python runtime venv lives at
~/.lingtai-tui/runtime/venv.

For a Homebrew-to-native migration with a legacy ~/.lingtai-tui/runtime but
no native install receipt, preserve that runtime and install only the native
TUI target with:
  curl -fsSL https://lingtai.ai/install.sh | bash -s -- --version vX.Y.Z --non-interactive --skip-python
This creates a native receipt without adopting or changing the legacy runtime.
Provision or repair a separate runtime only after reviewing the exact
postconditions. For an exact-artifact update, bounded repair, read-only
verification, an explicit editable development install, or full removal of an
existing installation, use the standalone maintenance entrypoints instead of
this script: update.sh, fix.sh, verify.sh, dev.sh, remove.sh (each has its own
--help). See ANATOMY.md for their exact preconditions, allowed writes, and
postconditions.
EOF
}

# --- messaging helpers -------------------------------------------------------
say()  { echo "==> $*"; }
warn() { echo "warning: $*" >&2; }
note() { echo "    $*"; }

# print_path_hint gives the user a shell-specific command without changing the
# current process PATH or writing a shell rc file. SHELL is the user's login
# shell on the supported macOS/Linux paths; use a direct export for an
# unrecognized or unset shell rather than guessing its startup file.
print_path_hint() {
  local bin_dir="$1" shell_name="${SHELL:-}" rc_file
  case ":${PATH}:" in
    *":${bin_dir}:"*) return 0 ;;
  esac
  case "${shell_name##*/}" in
    zsh)  rc_file="$HOME/.zshrc" ;;
    bash) rc_file="$HOME/.bashrc" ;;
    *)
      say "Note: $bin_dir is not on your PATH. Add this export to your shell startup file:"
      note "export PATH=\"$bin_dir:\$PATH\""
      return 0
      ;;
  esac
  say "Note: $bin_dir is not on your PATH. Add it with:"
  note "echo 'export PATH=\"$bin_dir:\$PATH\"' >> \"$rc_file\" && source \"$rc_file\""
}

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
    [[ "$tool" == "python3" ]] && pkg="python3 python3-venv python3-pip"
    echo "      sudo apt-get update && sudo apt-get install -y $pkg" >&2
  elif command -v dnf &>/dev/null; then
    [[ "$tool" == "go" ]] && pkg="golang"
    [[ "$tool" == "python3" ]] && pkg="python3 python3-pip"
    echo "      sudo dnf install -y $pkg" >&2
  elif command -v pacman &>/dev/null; then
    [[ "$tool" == "python3" ]] && pkg="python python-pip"
    echo "      sudo pacman -S --needed $pkg" >&2
  elif command -v apk &>/dev/null; then
    [[ "$tool" == "python3" ]] && pkg="python3 py3-pip"
    echo "      sudo apk add $pkg" >&2
  elif command -v zypper &>/dev/null; then
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

# Desktop has its own release train and verified installer. The public LingTai
# installer only registers its lazy command on an ordinary stable macOS install
# or its version-pinned internal update; current-main/arbitrary-ref workflows
# keep their existing ownership.
should_install_desktop() {
  [[ "$SKIP_DESKTOP" != "1" ]] || return 1
  [[ "$(detect_os)" == "darwin" ]] || return 1
  [[ "$LATEST_MAIN_MODE" != "1" && "$REINSTALL_OK" != "1" && -z "$REF" ]]
}

# Register only a self-contained lazy command. It performs no Desktop network
# access and creates no Desktop managed state. On its first execution the
# command downloads the four exact, SHA-pinned installer-support files audited
# above; that existing Desktop code retains exclusive ownership of release API,
# archive/manifest verification, atomic App publication, and command semantics.
# A stable update may atomically refresh only this installer's marked lazy
# command; complete official Desktop state and every unowned target stay intact.
register_desktop_bootstrap() {
  local target="$BIN_DIR/lingtai-desktop"
  local app_executable="$HOME/.local/share/lingtai-desktop/current/LingTai.app/Contents/MacOS/LingTai"
  local template="$BUILD_DIR/lingtai-desktop-bootstrap.py.in"
  local staged="$BUILD_DIR/lingtai-desktop-bootstrap.py"
  local refresh_lazy=0

  if [[ -f "$target" && -x "$target" && ! -L "$target" ]]; then
    if grep -Fq '# lingtai-desktop-owned-v1' "$target"; then
      if [[ -f "$app_executable" && -x "$app_executable" && ! -L "$app_executable" ]]; then
        note "Existing complete LingTai Desktop command is already installed; keeping it unchanged: $target"
        return 0
      fi
      echo "error: existing Desktop command target was found; refusing to overwrite it: $target" >&2
      return 1
    fi
    if grep -Fq 'lingtai-desktop-lazy-bootstrap-v1' "$target"; then
      if [[ "$UPDATE_MODE" == "1" ]]; then
        refresh_lazy=1
      else
        note "Existing LingTai Desktop lazy command is already registered; keeping it unchanged: $target"
        return 0
      fi
    fi
  fi
  if [[ "$refresh_lazy" != "1" && ( -e "$target" || -L "$target" ) ]]; then
    echo "error: existing Desktop command target was found; refusing to overwrite it: $target" >&2
    return 1
  fi
  mkdir -p "$BUILD_DIR"
  cat > "$template" <<'PY'
#!/usr/bin/env python3
"""LingTai Desktop lazy bootstrap, registered by the public LingTai installer."""

from __future__ import annotations

import hashlib
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

DESKTOP_VERSION = "@DESKTOP_VERSION@"
RAW_BASE = "@DESKTOP_RAW_BASE@"
RELEASE_BASE = "@DESKTOP_RELEASE_BASE@"
RELEASE_ASSETS = {"desktop_user_cli.py", "verify-app-archive.py"}
SUPPORT = {
    "install-macos-app.py": "@DESKTOP_INSTALLER_SHA256@",
    "desktop_user_cli.py": "@DESKTOP_CLI_SHA256@",
    "verify-app-archive.py": "@DESKTOP_VERIFIER_SHA256@",
    "support_bootstrap.py": "@DESKTOP_BOOTSTRAP_SHA256@",
}
BOOTSTRAP_MARKER = "lingtai-desktop-lazy-bootstrap-v1"


def fail(message: str) -> int:
    print(f"lingtai-desktop: {message}", file=sys.stderr)
    return 1


def invoked_path() -> Path:
    candidate = sys.argv[0]
    if os.sep not in candidate:
        candidate = shutil.which(candidate) or candidate
    return Path(candidate).resolve(strict=True)


def installed_paths() -> tuple[Path, Path]:
    home = Path.home()
    launcher = home / ".local/bin/lingtai-desktop"
    executable = (
        home / ".local/share/lingtai-desktop/current/"
        "LingTai.app/Contents/MacOS/LingTai"
    )
    return launcher, executable


def is_same_file(left: Path, right: Path) -> bool:
    try:
        return os.path.samefile(left, right)
    except OSError:
        return False


def exec_installed(launcher: Path, arguments: list[str]) -> None:
    os.execv(os.fspath(launcher), [os.fspath(launcher), *arguments])


def main() -> int:
    arguments = sys.argv[1:]
    bootstrap = invoked_path()
    launcher, app_executable = installed_paths()
    if launcher.is_file() and app_executable.is_file() and not is_same_file(bootstrap, launcher):
        exec_installed(launcher, arguments)

    curl = shutil.which("curl")
    if curl is None:
        return fail("curl is required for the first Desktop command execution")

    with tempfile.TemporaryDirectory(prefix="lingtai-desktop-bootstrap-") as temporary:
        support_root = Path(temporary)
        scripts_root = support_root / "scripts"
        scripts_root.mkdir(mode=0o700)
        for name, expected_sha in SUPPORT.items():
            destination = scripts_root / name
            if name in RELEASE_ASSETS:
                url = f"{RELEASE_BASE}/v{DESKTOP_VERSION}/{name}"
            else:
                url = f"{RAW_BASE}/v{DESKTOP_VERSION}/scripts/{name}"
            result = subprocess.run(
                [curl, "-fsSL", "--max-time", "30", "-o", os.fspath(destination), url],
                check=False,
            )
            if result.returncode != 0:
                return fail(
                    f"Desktop installer support is unavailable for v{DESKTOP_VERSION}; "
                    "retry after confirming access to the public release"
                )
            actual_sha = hashlib.sha256(destination.read_bytes()).hexdigest()
            if actual_sha != expected_sha:
                return fail(f"Desktop installer support checksum mismatch: {name}")
            destination.chmod(0o600)

        backup: Path | None = None
        if launcher.exists() and is_same_file(bootstrap, launcher):
            backup = launcher.with_name(f".lingtai-desktop.bootstrap.{os.getpid()}")
            if backup.exists() or backup.is_symlink():
                return fail("Desktop bootstrap backup path is unexpectedly occupied")
            os.replace(launcher, backup)

        result = subprocess.run(
            [sys.executable, os.fspath(scripts_root / "install-macos-app.py"),
             "--version", DESKTOP_VERSION],
            check=False,
        )
        if result.returncode != 0:
            retryable = backup is None
            if backup is not None and not launcher.exists() and not launcher.is_symlink():
                os.replace(backup, launcher)
                retryable = True
            suffix = (
                "the lazy command remains retryable"
                if retryable else "inspect the Desktop installer error above"
            )
            return fail(f"verified Desktop installation failed; {suffix}")
        if not launcher.is_file() or not app_executable.is_file():
            retryable = backup is None
            if backup is not None and not launcher.exists() and not launcher.is_symlink():
                os.replace(backup, launcher)
                retryable = True
            suffix = (
                "the lazy command remains retryable"
                if retryable else "incomplete Desktop state requires inspection"
            )
            return fail(f"Desktop installer returned success without a complete managed installation; {suffix}")
        if backup is not None:
            backup.unlink()
    exec_installed(launcher, arguments)
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
PY
  sed \
    -e "s|@DESKTOP_VERSION@|$DESKTOP_VERSION|g" \
    -e "s|@DESKTOP_RAW_BASE@|$DESKTOP_RAW_BASE|g" \
    -e "s|@DESKTOP_RELEASE_BASE@|$DESKTOP_RELEASE_BASE|g" \
    -e "s|@DESKTOP_INSTALLER_SHA256@|$DESKTOP_INSTALLER_SHA256|g" \
    -e "s|@DESKTOP_CLI_SHA256@|$DESKTOP_CLI_SHA256|g" \
    -e "s|@DESKTOP_VERIFIER_SHA256@|$DESKTOP_VERIFIER_SHA256|g" \
    -e "s|@DESKTOP_BOOTSTRAP_SHA256@|$DESKTOP_BOOTSTRAP_SHA256|g" \
    "$template" > "$staged"
  chmod 755 "$staged"
  install_binary_atomically "$staged" "$target" || return 1
  if [[ "$refresh_lazy" == "1" ]]; then
    say "Refreshed lazy LingTai Desktop command at $target"
  else
    say "Registered lazy LingTai Desktop command at $target"
  fi
  note "The Desktop App will be downloaded and independently verified only when lingtai-desktop is first run."
}

# detect_arch prints amd64|arm64, or "unsupported".
detect_arch() {
  case "$(uname -m)" in
    x86_64 | amd64)          echo "amd64" ;;
    arm64 | aarch64)         echo "arm64" ;;
    *)                       echo "unsupported" ;;
  esac
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
    if [[ -n "$tag" && -n "$(release_tag_name "$tag")" ]]; then
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

# --- lingtai.ai mirror asset resolution -------------------------------------

# Validate the already-live latest metadata and print its exact tag. The
# existing Python JSON helper is used rather than introducing another parser.
parse_mirror_latest() {
  local body="$1" expected_repo="$2"
  run_manifest_python "$body" - "$expected_repo" <<'PY'
import json, os, re, sys
repo = sys.argv[1]
try:
    data = json.loads(os.environ["BODY"])
    if not isinstance(data, dict) or set(data) != {"schema", "source_repo", "release_id", "tag", "assets"}:
        raise ValueError("wrong top-level shape")
    if data["schema"] != "lingtai.release_mirror.latest/v1" or data["source_repo"] != repo:
        raise ValueError("wrong schema or source_repo")
    if isinstance(data["release_id"], bool) or not isinstance(data["release_id"], int) or data["release_id"] <= 0:
        raise ValueError("release_id must be positive")
    if not isinstance(data["tag"], str) or not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", data["tag"]):
        raise ValueError("tag must be vX.Y.Z")
    if not isinstance(data["assets"], list) or not data["assets"]:
        raise ValueError("assets must be nonempty")
    names = set()
    for asset in data["assets"]:
        if not isinstance(asset, dict) or set(asset) != {"name", "sha256", "size"}:
            raise ValueError("wrong asset shape")
        name = asset["name"]
        if not isinstance(name, str) or not re.fullmatch(r"[A-Za-z0-9._+-]+", name) or name in names:
            raise ValueError("invalid or duplicate asset name")
        names.add(name)
        if not isinstance(asset["sha256"], str) or not re.fullmatch(r"[0-9a-f]{64}", asset["sha256"]):
            raise ValueError("invalid asset sha256")
        if isinstance(asset["size"], bool) or not isinstance(asset["size"], int) or asset["size"] <= 0:
            raise ValueError("invalid asset size")
except (KeyError, TypeError, ValueError, json.JSONDecodeError) as exc:
    raise SystemExit(f"invalid mirror latest metadata: {exc}")
print(data["tag"])
PY
}

fetch_mirror_latest() {
  local repo_slug="$1" body tag url
  url="$LINGTAI_WEB_BASE/dl/$repo_slug/latest.json"
  body="$(curl -fsSL --max-time "$MIRROR_TIMEOUT" "$url" 2>/dev/null || true)"
  if [[ -z "$body" ]] || ! tag="$(parse_mirror_latest "$body" "$repo_slug" 2>/dev/null)"; then
    echo "error: lingtai.ai could not provide valid latest metadata at $url." >&2
    return 1
  fi
  case "$repo_slug" in
    "$REPO_SLUG") MIRROR_TUI_LATEST_JSON="$body"; MIRROR_TUI_LATEST_TAG="$tag" ;;
    "$KERNEL_REPO_SLUG") MIRROR_KERNEL_LATEST_JSON="$body"; MIRROR_KERNEL_LATEST_TAG="$tag" ;;
    *) return 1 ;;
  esac
}

# Print "sha256 size" for one asset selected from cached latest metadata.
mirror_asset_record() {
  local repo_slug="$1" tag="$2" name="$3" body
  case "$repo_slug" in
    "$REPO_SLUG") body="$MIRROR_TUI_LATEST_JSON" ;;
    "$KERNEL_REPO_SLUG") body="$MIRROR_KERNEL_LATEST_JSON" ;;
    *) return 1 ;;
  esac
  [[ -n "$body" ]] || return 1
  run_manifest_python "$body" - "$repo_slug" "$tag" "$name" <<'PY'
import json, os, sys
repo, tag, name = sys.argv[1:]
data = json.loads(os.environ["BODY"])
if data.get("source_repo") != repo or data.get("tag") != tag:
    raise SystemExit(1)
hits = [a for a in data.get("assets", []) if a.get("name") == name]
if len(hits) != 1:
    raise SystemExit(1)
print(hits[0]["sha256"], hits[0]["size"])
PY
}

mirror_release_asset_url() {
  local repo_slug="$1" tag="$2" name="$3"
  mirror_asset_record "$repo_slug" "$tag" "$name" >/dev/null 2>&1 || return 1
  printf '%s/dl/%s/%s/%s' "$LINGTAI_WEB_BASE" "$repo_slug" "$tag" "$name"
}

download_mirror_asset() {
  local repo_slug="$1" tag="$2" name="$3" dest="$4" record expected_sha expected_size actual_size url
  record="$(mirror_asset_record "$repo_slug" "$tag" "$name" 2>/dev/null || true)"
  if [[ -z "$record" ]]; then
    echo "error: lingtai.ai latest metadata does not list required asset $repo_slug/$tag/$name." >&2
    return 1
  fi
  read -r expected_sha expected_size <<<"$record"
  url="$LINGTAI_WEB_BASE/dl/$repo_slug/$tag/$name"
  if ! curl -fsSL --max-time 300 -o "$dest" "$url"; then
    echo "error: selected lingtai.ai asset failed: $url" >&2
    return 1
  fi
  actual_size="$(wc -c < "$dest" | tr -d '[:space:]')"
  if [[ "$actual_size" != "$expected_size" ]] || ! verify_sha256 "$dest" "$expected_sha"; then
    echo "error: selected lingtai.ai asset failed size/SHA256 verification: $url" >&2
    return 1
  fi
}

mirror_asset_text() {
  local repo_slug="$1" tag="$2" name="$3" path
  mkdir -p "$BUILD_DIR/mirror-metadata"
  path="$BUILD_DIR/mirror-metadata/$name"
  download_mirror_asset "$repo_slug" "$tag" "$name" "$path" || return 1
  cat "$path"
}

# --- independent source/provider resolution --------------------------------

tui_source_asset_name() {
  local tag="$1"
  printf 'lingtai-%s-source.tar.gz' "$tag"
}

# Annotated tags resolve to two SHAs: the tag object and its peeled commit.
# Checkout provenance must always use the latter.
peeled_tag_commit() {
  local repo="$1" tag="$2" line sha target
  line="$(git ls-remote --tags "$repo" "refs/tags/$tag^{}" 2>/dev/null | head -1 || true)"
  read -r sha target <<< "$line"
  if [[ ! "$sha" =~ ^[0-9a-f]{40}$ ]]; then
    line="$(git ls-remote --tags "$repo" "refs/tags/$tag" 2>/dev/null | head -1 || true)"
    read -r sha target <<< "$line"
  fi
  [[ "$sha" =~ ^[0-9a-f]{40}$ ]] || return 1
  printf '%s' "$sha"
}

json_string_field() {
  local key="$1"
  grep -o "\"$key\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -1 |
    sed "s/.*\"$key\"[[:space:]]*:[[:space:]]*\"\\([^\"]*\\)\".*/\\1/"
}

# Metadata parsing happens before the managed runtime exists.
run_manifest_python() {
  local body="$1"
  shift
  if command -v python3 >/dev/null 2>&1; then
    BODY="$body" python3 "$@"
    return
  fi
  ensure_uv >/dev/null || return 1
  local uv
  uv="$(find_uv 2>/dev/null || true)"
  [[ -n "$uv" && -x "$uv" ]] || return 1
  BODY="$body" "$uv" run --no-project --managed-python --python 3.13 -- python "$@"
}

# This controls the TUI path only. Kernel provider selection is performed
# independently by install_kernel_from_release.
resolve_source_provider() {
  SOURCE_ONLY_DEFAULT=0
  case "$SOURCE_ARG" in
    github) TUI_PROVIDER="github" ;;
    auto|mirror)
      if [[ -n "$VERSION" || -n "$REF" || "$FROM_SOURCE" == "1" ||
            "$UPDATE_MODE" == "1" || "$LATEST_MAIN_MODE" == "1" ]]; then
        TUI_PROVIDER="github"
      else
        TUI_PROVIDER="mirror"
        SOURCE_ONLY_DEFAULT=1
      fi
      ;;
    *) return 1 ;;
  esac
}

python_dependency_index_url() {
  if [[ -n "${LINGTAI_PYPI_INDEX_URL:-}" ]]; then
    printf '%s' "$LINGTAI_PYPI_INDEX_URL"
  elif [[ "${KERNEL_PROVIDER:-github}" == "mirror" ]]; then
    printf '%s' "$PYPI_INDEX_URL_MIRROR_DEFAULT"
  else
    printf '%s' "$PYPI_INDEX_URL_DEFAULT"
  fi
}

# Resolve the TUI's latest source asset only. Kernel metadata is never read by
# this function, which keeps provider failure scoped to one component.
resolve_tui_latest() {
  fetch_mirror_latest "$REPO_SLUG" || return 1
  TUI_TAG="$MIRROR_TUI_LATEST_TAG"
  TUI_SOURCE_ASSET="$(tui_source_asset_name "$TUI_TAG")"
  mirror_asset_record "$REPO_SLUG" "$TUI_TAG" "$TUI_SOURCE_ASSET" >/dev/null 2>&1
}

# verify_sha256 checks a file against an expected lowercase hex digest using
# whichever checksum tool is available. Returns nonzero on mismatch or if no
# checksum tool exists (callers must treat "no tool" as a hard failure, not a
# skip — this installer never installs unverified release bytes).
verify_sha256() {
  local file="$1" expected="$2" actual
  if command -v sha256sum &>/dev/null; then
    actual="$(sha256sum "$file" | cut -d' ' -f1)"
  elif command -v shasum &>/dev/null; then
    actual="$(shasum -a 256 "$file" | cut -d' ' -f1)"
  else
    echo "error: no sha256sum/shasum tool available to verify $file" >&2
    return 1
  fi
  [[ "$actual" == "$expected" ]]
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

# resolve_main_branch_sha resolves one exact full commit before either source
# checkout begins. The subsequent clones are checked against these pins; a
# moving main branch therefore fails loudly instead of building a mixed pair.
resolve_main_branch_sha() {
  local repo_url="$1" sha
  command -v git &>/dev/null || return 1
  sha="$(git ls-remote "$repo_url" refs/heads/main 2>/dev/null | awk 'NF { print $1; exit }')"
  [[ "$sha" =~ ^[0-9a-f]{40}$ ]] || return 1
  printf '%s\n' "$sha"
}

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
  local saw_latest=0 saw_ref=0 saw_version=0 saw_from_source=0 saw_skip_python=0 saw_source=0 saw_update=0
  LATEST_MAIN_MODE=0
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --latest) LATEST_MAIN_MODE=1; saw_latest=1; shift ;;
      --ref) REF="${2:?error: --ref requires a value}"; saw_ref=1; shift 2 ;;
      --version) VERSION="${2:?error: --version requires a value}"; saw_version=1; shift 2 ;;
      --prefix) INSTALL_PREFIX="${2:?error: --prefix requires a value}"; shift 2 ;;
      --bin-dir) BIN_DIR_OVERRIDE="${2:?error: --bin-dir requires a value}"; shift 2 ;;
      --from-source) FROM_SOURCE=1; saw_from_source=1; shift ;;
      --skip-python|--skip-venv) SKIP_VENV=1; saw_skip_python=1; shift ;;
      --skip-desktop) SKIP_DESKTOP=1; shift ;;
      --source) SOURCE_ARG="${2:?error: --source requires a value}"; saw_source=1; shift 2 ;;
      --update) UPDATE_MODE=1; saw_update=1; shift ;;
      --non-interactive) NON_INTERACTIVE=1; shift ;;
      -h|--help) usage; exit 0 ;;
      *) echo "error: unknown flag: $1" >&2; usage >&2; exit 1 ;;
    esac
  done

  if [[ "$saw_latest" == "1" ]]; then
    if [[ "$saw_ref" == "1" || "$saw_version" == "1" || "$saw_from_source" == "1" || "$saw_skip_python" == "1" || "$saw_source" == "1" || "$saw_update" == "1" ]]; then
      echo "error: --latest cannot be combined with --ref, --version, --from-source, --skip-python/--skip-venv, --source/LINGTAI_SOURCE, or --update" >&2
      usage >&2
      exit 1
    fi
  fi

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

  case "$SOURCE_ARG" in
    auto|github|mirror) ;;
    gitee)
      echo "error: --source gitee has been retired; lingtai.ai now provides the China-accelerated mirror." >&2
      echo "       Use --source mirror or --source auto." >&2
      usage >&2
      exit 1
      ;;
    *) echo "error: --source must be one of auto|github|mirror, got: $SOURCE_ARG" >&2; usage >&2; exit 1 ;;
  esac
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

# write_install_metadata records only TUI ownership plus independent runtime
# provenance. No retired bundle fields are emitted.
write_install_metadata() {
  local global_dir="$1" prefix="$2" bin_dir="$3" repo_url="$4" requested_ref="$5"
  local resolved_ref="$6" resolved_commit="$7" stamped_version="$8" tui_path="$9"
  local metadata_path tmp_path installed_at runtime_json="" provenance_json=""
  local install_kind="${INSTALL_KIND:-source-build}"

  if [[ -n "${RUNTIME_VENV_DIR:-}" && "$SKIP_VENV" != "1" ]]; then
    if ! canonical_runtime_venv "$RUNTIME_VENV_DIR" "$HOME/.lingtai-tui/runtime" >/dev/null; then
      echo "error: refusing to persist a runtime pointer outside the canonical owned runtime root: $RUNTIME_VENV_DIR" >&2
      return 1
    fi
    runtime_json="$(printf ',\n  "runtime_venv": "%s"' "$(json_escape "${RUNTIME_VENV_DIR%/}")")"
  fi
  if [[ "$KERNEL_SOURCE" == "release" ]]; then
    provenance_json="$(printf ',\n  "kernel_source": "release",\n  "kernel_release_tag": "%s",\n  "kernel_version": "%s",\n  "kernel_provider": "%s"' \
      "$(json_escape "$KERNEL_RELEASE_TAG")" "$(json_escape "$KERNEL_VERSION_INSTALLED")" \
      "$(json_escape "$KERNEL_PROVIDER")")"
  elif [[ "$KERNEL_SOURCE" == "main" ]]; then
    provenance_json="$(printf ',\n  "source_mode": "latest-main",\n  "tui_commit": "%s",\n  "kernel_source": "main",\n  "kernel_commit": "%s",\n  "kernel_version": "%s",\n  "kernel_provider": "github"' \
      "$(json_escape "$TUI_MAIN_SHA")" "$(json_escape "$KERNEL_MAIN_SHA")" \
      "$(json_escape "$KERNEL_VERSION_INSTALLED")")"
  fi
  if [[ -n "${TUI_PROVIDER:-}" ]]; then
    if [[ -n "$provenance_json" ]]; then
      provenance_json="$(printf '%s,\n  "tui_provider": "%s"' "$provenance_json" "$(json_escape "$TUI_PROVIDER")")"
    else
      provenance_json="$(printf ',\n  "tui_provider": "%s"' "$(json_escape "$TUI_PROVIDER")")"
    fi
  fi

  metadata_path="$global_dir/install.json"
  installed_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  if [[ -L "$global_dir" ]]; then
    echo "error: install metadata directory is a symlink; refusing redirected state: $global_dir" >&2
    return 1
  fi
  mkdir -p "$global_dir"

  if [[ "$UPDATE_MODE" != "1" && "$LATEST_MAIN_MODE" != "1" && "$REINSTALL_OK" != "1" ]]; then
    if [[ -e "$metadata_path" || -L "$metadata_path" ]]; then
      echo "error: install receipt appeared before metadata creation; refusing to replace it: $metadata_path" >&2
      return 1
    fi
    tmp_path="$(mktemp "$global_dir/.install.json.XXXXXX")" || return 1
  else
    tmp_path="$metadata_path.tmp.$$"
    : > "$tmp_path" || return 1
  fi
  chmod 600 "$tmp_path" || { rm -f "$tmp_path"; return 1; }

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
    "$(json_escape "$tui_path")"
  ]$provenance_json$runtime_json
}
EOF

  if [[ "$UPDATE_MODE" != "1" && "$LATEST_MAIN_MODE" != "1" && "$REINSTALL_OK" != "1" ]]; then
    if [[ -L "$global_dir" || -e "$metadata_path" || -L "$metadata_path" ]]; then
      rm -f "$tmp_path"
      echo "error: install receipt appeared during metadata creation; refusing to replace it: $metadata_path" >&2
      return 1
    fi
    if ! ln "$tmp_path" "$metadata_path"; then
      rm -f "$tmp_path"
      echo "error: install receipt could not be published exclusively; existing state was preserved: $metadata_path" >&2
      return 1
    fi
    rm -f "$tmp_path"
  else
    mv "$tmp_path" "$metadata_path"
  fi
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
  [[ -n "${UV_INSTALL_DIR:-}" && -x "$UV_INSTALL_DIR/uv" ]] && { echo "$UV_INSTALL_DIR/uv"; return 0; }
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

# canonical_runtime_venv resolves a candidate runtime venv path to its
# physical location and requires that physical location to be canonically
# contained under $HOME/.lingtai-tui/runtime — not merely lexically prefixed.
# A symlinked venv directory (or a symlinked ancestor) whose real target
# escapes the owned runtime root is rejected outright: this installer must
# never adopt or mutate a venv outside the root it claims to own. Prints the
# physical path and returns 0 only when containment holds.
canonical_runtime_venv() {
  local dir="$1" runtime_root="$2" physical_root physical_dir physical_home expected_root
  local parent base physical_parent root_parent root_base root_grandparent root_parent_base

  physical_home="$(cd "$HOME" 2>/dev/null && pwd -P)" || return 1
  expected_root="$physical_home/.lingtai-tui/runtime"

  # Ownership is both lexical and physical: a path outside the declared root
  # is not adopted merely because a symlink happens to point back inside it.
  [[ "$dir" == "$runtime_root"/* ]] || return 1
  [[ ! -L "$runtime_root" ]] || return 1

  if [[ -d "$runtime_root" ]]; then
    physical_root="$(cd "$runtime_root" 2>/dev/null && pwd -P)" || return 1
  elif [[ -e "$runtime_root" ]]; then
    return 1
  else
    # Resolve the not-yet-created owned root without mkdir. A completely fresh
    # install may also lack its .lingtai-tui parent, so append at most those two
    # fixed missing components to an existing physical HOME ancestor.
    root_parent="$(dirname "$runtime_root")"
    root_base="$(basename "$runtime_root")"
    if [[ -d "$root_parent" ]]; then
      physical_root="$(cd "$root_parent" 2>/dev/null && pwd -P)/$root_base" || return 1
    else
      [[ ! -e "$root_parent" && ! -L "$root_parent" ]] || return 1
      root_grandparent="$(dirname "$root_parent")"
      root_parent_base="$(basename "$root_parent")"
      [[ -d "$root_grandparent" ]] || return 1
      physical_root="$(cd "$root_grandparent" 2>/dev/null && pwd -P)/$root_parent_base/$root_base" || return 1
    fi
  fi
  # `$HOME` itself may be a symlink, but `.lingtai-tui` and `runtime` may not
  # redirect ownership elsewhere. The resolved root must be exactly beneath the
  # canonical physical HOME, not merely whatever `pwd -P` found through an
  # ancestor symlink.
  [[ "$physical_root" == "$expected_root" ]] || return 1

  if [[ -d "$dir" ]]; then
    physical_dir="$(cd "$dir" 2>/dev/null && pwd -P)" || return 1
  else
    # A file or symlink (including dangling) is occupied untrusted state, not a
    # free final child that venv creation may replace or follow.
    [[ ! -e "$dir" && ! -L "$dir" ]] || return 1
    parent="$(dirname "$dir")"
    base="$(basename "$dir")"
    [[ "$base" != "." && "$base" != ".." ]] || return 1
    if [[ "$parent" == "$runtime_root" && ! -e "$runtime_root" ]]; then
      physical_parent="$physical_root"
    else
      physical_parent="$(cd "$parent" 2>/dev/null && pwd -P)" || return 1
    fi
    physical_dir="$physical_parent/$base"
  fi
  [[ "$physical_dir" == "$physical_root"/* ]] || return 1
  printf '%s\n' "$physical_dir"
}

# runtime_python_for_venv resolves the python/python3 launcher under a venv
# directory, or nothing if neither exists.
runtime_python_for_venv() {
  local venv_dir="$1"
  if [[ -x "$venv_dir/bin/python" ]]; then
    printf '%s\n' "$venv_dir/bin/python"
  elif [[ -x "$venv_dir/bin/python3" ]]; then
    printf '%s\n' "$venv_dir/bin/python3"
  fi
}

# A launcher located under the selected venv is not enough ownership proof: it
# may be a symlink to another environment. Check sys.prefix before any pip/uv
# operation so an external interpreter is never mutated and rejected only later.
runtime_prefix_matches_venv() {
  local py="$1" venv_dir="$2" selected_prefix
  selected_prefix="$(cd "$venv_dir" 2>/dev/null && pwd -P)" || return 1
  PYTHONPATH= "$py" - "$selected_prefix" <<'PY' >/dev/null 2>&1
import os
import sys

selected_prefix = os.path.realpath(sys.argv[1])
raise SystemExit(0 if os.path.realpath(sys.prefix) == selected_prefix else 1)
PY
}

# ensure_runtime_pip repairs pip only inside the brand-new owned venv created by
# this invocation. It first asks that exact interpreter to seed itself, then (if
# available) uses the already selected uv scoped to the same venv. Prefix checks
# before and after the attempt prevent either fallback from reaching another
# interpreter. Existing runtimes are rejected before this helper is called.
ensure_runtime_pip() {
  local py="$1" venv_dir="$2" uv="${3:-}" index_url
  runtime_prefix_matches_venv "$py" "$venv_dir" || return 1
  "$py" -m pip --version >/dev/null 2>&1 && return 0

  warn "pip is missing from the new owned runtime; trying that interpreter's ensurepip."
  "$py" -m ensurepip --upgrade || warn "ensurepip could not seed pip in the new owned runtime."
  "$py" -m pip --version >/dev/null 2>&1 && return 0

  if [[ -n "$uv" ]]; then
    index_url="$(python_dependency_index_url)"
    warn "pip is still missing; using selected uv only inside the new owned runtime."
    "$uv" pip install --index-url "$index_url" -p "$venv_dir" pip || warn "uv could not seed pip in the new owned runtime."
  fi

  runtime_prefix_matches_venv "$py" "$venv_dir" || return 1
  "$py" -m pip --version >/dev/null 2>&1
}

# runtime_venv_state classifies an existing venv path as missing/broken/healthy
# without mutating it: missing (no directory at all), broken (no interpreter,
# too old, prefix does not match, or no working pip), or healthy. Ordinary
# install uses this to refuse silently reusing an existing runtime — see the
# guard at the top of ensure_runtime_venv below.
runtime_venv_state() {
  local venv_dir="$1" py
  [[ -d "$venv_dir" ]] || { printf '%s\n' missing; return 0; }
  py="$(runtime_python_for_venv "$venv_dir")"
  [[ -n "$py" ]] || { printf '%s\n' broken; return 0; }
  "$py" -c 'import sys; sys.exit(0 if sys.version_info >= (3, 11) else 1)' 2>/dev/null || { printf '%s\n' broken; return 0; }
  runtime_prefix_matches_venv "$py" "$venv_dir" || { printf '%s\n' broken; return 0; }
  "$py" -m pip --version >/dev/null 2>&1 || { printf '%s\n' broken; return 0; }
  printf '%s\n' healthy
}

# runtime_health_check is the install postcondition: both the public package
# and its kernel module must import from the selected interpreter, the
# package version must equal the exact manifest/pin version, AND both module
# __file__ paths must resolve physically underneath the selected venv's own
# canonical prefix — not merely be importable. This rejects a same-version
# package injected through an external `.pth` entry, system site-packages, or
# any other interpreter path configuration that would let a bare `import
# lingtai` check pass while the kernel actually loads from outside the venv
# this installer claims is healthy. PYTHONPATH= alone does not cover that
# case, so the check is done in Python against sys.prefix/realpath.
runtime_health_check() {
  local py="$1" expected="${2:-}" output selected_prefix
  selected_prefix="$(cd "$(dirname "$py")/.." 2>/dev/null && pwd -P)" || return 1
  output="$(PYTHONPATH= "$py" - "$expected" "$selected_prefix" <<'PY'
import importlib
import os
import sys

expected = sys.argv[1]
selected_prefix = os.path.realpath(sys.argv[2])
prefix = os.path.realpath(sys.prefix)
if prefix != selected_prefix:
    raise SystemExit(1)
module = importlib.import_module("lingtai")
kernel = importlib.import_module("lingtai.kernel")
version = str(getattr(module, "__version__", ""))
if not version or (expected and version.lstrip("v") != expected.lstrip("v")):
    raise SystemExit(1)
for mod in (module, kernel):
    mod_path = os.path.realpath(getattr(mod, "__file__", "") or "")
    if not mod_path or not (mod_path == selected_prefix or mod_path.startswith(selected_prefix + os.sep)):
        raise SystemExit(1)
print(f"{version}\t{module.__file__}")
PY
  )" || return 1
  [[ "$output" == *$'\t'* ]] || return 1
  printf '%s\n' "$output"
}

# ensure_runtime_venv creates or updates ~/.lingtai-tui/runtime/venv and
# installs the `lingtai` package from an independently verified kernel release
# artifact by explicit local file path. LingTai itself is NEVER requested from
# a package index by name; only third-party dependencies use the configured
# index. This is
# mirrored by the TUI's own EnsureVenv logic (uv venv --python 3.13 if uv
# exists, else python3 -m venv; verify import; stamp env marker; symlink
# lingtai-agent).
#
# Ordinary (non---update) install refuses to silently reuse an existing
# runtime venv at all — healthy or broken — pointing to fix.sh instead: see
# the guard immediately below, which runs before canonical's own
# repair-loop. That existing loop's own venv-repair-$$-N recreation covers a
# DIFFERENT case (a transient failure discovered DURING this run's own venv
# creation/kernel-install attempt), not an already-occupied runtime from a
# prior run.
#
# A release install must resolve and install the manifest-declared kernel source
# artifact; all runtime setup and artifact failures are fail-loud. An arbitrary
# --ref has no independently versioned kernel release and therefore requires
# --skip-python.
# --skip-python (alias --skip-venv) is the only way to skip the runtime.
ensure_runtime_venv() {
  local bin_dir="$1"
  local venv_dir="$HOME/.lingtai-tui/runtime/venv"
  local uv py repair_attempt

  if [[ "$SKIP_VENV" == "1" ]]; then
    note "Skipping Python runtime venv (--skip-python)."
    return 0
  fi

  if [[ -n "$REF" ]]; then
    echo "error: --ref/source builds have no independently versioned release kernel; pass --skip-python." >&2
    return 1
  fi

  if [[ "$UPDATE_MODE" != "1" && "$LATEST_MAIN_MODE" != "1" && "$REINSTALL_OK" != "1" ]]; then
    local runtime_root="$HOME/.lingtai-tui/runtime" existing_state
    if ! canonical_runtime_venv "$venv_dir" "$runtime_root" >/dev/null; then
      echo "error: selected runtime venv is not a canonical child of the owned runtime root: $venv_dir" >&2
      echo "       Refusing to adopt or create a venv outside the root this installer owns." >&2
      return 1
    fi
    if [[ -L "$runtime_root" ]]; then
      echo "error: runtime root is a symlink: $runtime_root" >&2
      return 1
    fi
    existing_state="$(runtime_venv_state "$venv_dir")"
    if [[ "$existing_state" != "missing" ]]; then
      echo "error: existing runtime at $venv_dir is $existing_state; ordinary install will not adopt or repair it." >&2
      echo "       Rerun install.sh to restore the latest canonical installation." >&2
      return 1
    fi
  fi

  say "Setting up Python runtime venv at $venv_dir ..."
  if ! ensure_python; then
    echo "error: Python 3.11+ with venv support is required for the kernel runtime." >&2
    return 1
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
        echo "error: $recreate_reason after recreate; refusing an unprovisioned runtime." >&2
        return 1
      fi
      warn "$recreate_reason; retaining it and provisioning a new runtime venv path."
      venv_dir="$HOME/.lingtai-tui/runtime/venv-repair-$$-1"
      repair_attempt=1
      py=""
    fi

    if [[ -z "$py" ]]; then
      if [[ -n "$uv" ]]; then
        if ! "$uv" venv --python 3.13 "$venv_dir"; then
          if python_ok; then
            warn "uv venv failed; falling back to python3 -m venv"
            uv=""
          else
            echo "error: uv could not create the runtime venv and no supported Python fallback is available." >&2
            return 1
          fi
        fi
      fi
      if [[ ! -x "$venv_dir/bin/python" && ! -x "$venv_dir/bin/python3" && -z "$uv" ]]; then
        if python_ok; then
          if ! python3 -m venv "$venv_dir"; then
            echo "error: failed to create the runtime venv." >&2
            return 1
          fi
        else
          echo "error: no supported Python/uv runtime is available." >&2
          return 1
        fi
      fi
      if [[ -x "$venv_dir/bin/python" ]]; then
        py="$venv_dir/bin/python"
      elif [[ -x "$venv_dir/bin/python3" ]]; then
        py="$venv_dir/bin/python3"
      else
        echo "error: runtime venv has no Python interpreter at $venv_dir." >&2
        return 1
      fi
      # Re-check Python version after creating/recreating the venv.
      continue
    fi

    if ! ensure_runtime_pip "$py" "$venv_dir" "$uv"; then
      if [[ "$repair_attempt" == "0" ]]; then
        warn "runtime venv pip is missing and could not be self-healed; retaining it and provisioning a new runtime venv path."
        venv_dir="$HOME/.lingtai-tui/runtime/venv-repair-$$-1"
        repair_attempt=1
        continue
      fi
      echo "error: runtime venv has no pip/uv installer after recreate." >&2
      return 1
    fi

    local install_ok=0
    # The verified kernel source archive is the ONLY LingTai install source.
    # Any failure here (incoherent manifest, missing source artifact,
    # checksum mismatch, install command failure) is retried
    # once after a venv recreate (a legitimate transient-environment repair,
    # the same pattern every other step in this loop uses), then FAILS LOUD —
    # it never falls back to `pip install lingtai` from an index.
    if [[ "$LATEST_MAIN_MODE" == "1" ]]; then
      if install_kernel_from_main "$py" "$uv"; then
        install_ok=1
      fi
    elif install_kernel_from_release "$py" "$uv"; then
      install_ok=1
    fi
    if [[ "$install_ok" != "1" ]]; then
      if [[ "$repair_attempt" == "0" ]]; then
        warn "failed to install the verified kernel source archive; retaining the venv and provisioning a new runtime venv path."
        venv_dir="$HOME/.lingtai-tui/runtime/venv-repair-$$-1"
        repair_attempt=1
        continue
      fi
      if [[ "$LATEST_MAIN_MODE" == "1" ]]; then
        echo "error: failed to install kernel main commit $KERNEL_MAIN_SHA into the runtime venv after recreate." >&2
      else
        echo "error: failed to install a verified kernel source archive after recreate (tag ${KERNEL_RELEASE_TAG:-unknown}, provider ${KERNEL_PROVIDER:-unknown})." >&2
      fi
      echo "       LingTai's Python runtime is never installed from an index by package name." >&2
      echo "       Fix the reported provider/artifact error and re-run, or pass --skip-python." >&2
      return 1
    fi

    # Postcondition: version + BOTH modules' __file__ must resolve physically
    # inside this exact venv's prefix — not merely importable — so a
    # same-version package reachable through an external .pth entry or system
    # site-packages can never be mistaken for a healthy owned install.
    if ! runtime_health_check "$py" "$KERNEL_VERSION_INSTALLED" >/dev/null; then
      if [[ "$repair_attempt" == "0" ]]; then
        warn "runtime venv failed import/provenance check; retaining it and provisioning a new runtime venv path."
        venv_dir="$HOME/.lingtai-tui/runtime/venv-repair-$$-1"
        repair_attempt=1
        continue
      fi
      if [[ "$LATEST_MAIN_MODE" == "1" ]]; then
        echo "error: kernel main import/provenance check failed after reinstall; refusing a partial --latest install." >&2
      else
        echo "error: runtime venv is still unhealthy after reinstall; refusing an incomplete install." >&2
      fi
      return 1
    fi
    break
  done

  RUNTIME_VENV_DIR="$venv_dir"

  # Stamp the env marker (best-effort — older kernels may lack the subcommand).
  "$py" -m lingtai.venv_resolve env-marker stamp --venv "$venv_dir" >/dev/null 2>&1 || true

  # Symlink lingtai-agent into the chosen bin dir (best-effort).
  if [[ -x "$venv_dir/bin/lingtai-agent" ]]; then
    ln -sfn "$venv_dir/bin/lingtai-agent" "$bin_dir/lingtai-agent" 2>/dev/null \
      || warn "could not symlink lingtai-agent into $bin_dir"
  fi
  return 0
}


# --- independent kernel source install (schema lingtai.kernel.release/v1) ---

# update_validate_manifest strictly validates a kernel release manifest
# (schema lingtai.kernel.release/v1): every required top-level key present
# and no others, exact schema/tag/version match, and every artifact's shape,
# digest, and (for wheels) filename/tag self-consistency — this replaces a
# substring schema check that could be satisfied by any JSON containing that
# string anywhere, including in an unrelated field, with a real parser that
# rejects duplicate keys, wrong shapes, and mismatched artifact metadata.
update_validate_manifest() {
  local py="$1" manifest_file="$2" expected_tag="$3"
  "$py" - "$manifest_file" "$expected_tag" <<'PY'
import json
import re
import sys

path, expected_tag = sys.argv[1:]
expected_version = expected_tag[1:] if expected_tag.startswith("v") else expected_tag

def pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result

def fail(message):
    raise SystemExit(f"invalid kernel release manifest: {message}")

try:
    with open(path, encoding="utf-8") as stream:
        data = json.load(stream, object_pairs_hook=pairs)
except (OSError, ValueError, json.JSONDecodeError) as exc:
    fail(str(exc))

required = {"schema", "kernel_version", "kernel_tag", "commit", "generated_at", "artifacts", "sdist_fallback"}
if not isinstance(data, dict) or set(data) != required:
    fail("unexpected top-level keys")
if data["schema"] != "lingtai.kernel.release/v1":
    fail("unexpected schema")
for key in ("kernel_version", "kernel_tag", "commit", "generated_at", "sdist_fallback"):
    if not isinstance(data[key], str) or not data[key]:
        fail(f"{key} must be a non-empty string")
if data["kernel_tag"] != expected_tag or data["kernel_version"] != expected_version:
    fail(f"manifest is for {data['kernel_tag']}/{data['kernel_version']}, expected {expected_tag}/{expected_version}")
if not isinstance(data["artifacts"], list) or not data["artifacts"]:
    fail("artifacts must be a non-empty list")

seen = set()
has_sdist = False
has_declared_sdist = False
for index, artifact in enumerate(data["artifacts"]):
    if not isinstance(artifact, dict) or set(artifact) != {"filename", "sha256", "kind", "python_tag", "abi_tag", "platform_tag"}:
        fail(f"artifacts[{index}] has the wrong shape")
    filename = artifact["filename"]
    digest = artifact["sha256"]
    kind = artifact["kind"]
    if not isinstance(filename, str) or not filename or filename in seen:
        fail(f"artifacts[{index}] has an invalid or duplicate filename")
    seen.add(filename)
    if not isinstance(digest, str) or not re.fullmatch(r"[0-9a-f]{64}", digest):
        fail(f"artifacts[{index}].sha256 is not lowercase 64-hex")
    if kind == "wheel":
        if any(not isinstance(artifact[key], str) or not artifact[key] for key in ("python_tag", "abi_tag", "platform_tag")):
            fail(f"artifacts[{index}] wheel tags must be non-empty strings")
        parts = filename[:-4].split("-") if filename.endswith(".whl") else []
        if len(parts) != 5 or parts[0] != "lingtai" or parts[1] != expected_version:
            fail(f"artifacts[{index}] filename is not the selected lingtai version")
        if tuple(parts[2:]) != (artifact["python_tag"], artifact["abi_tag"], artifact["platform_tag"]):
            fail(f"artifacts[{index}] filename tags disagree with metadata")
    elif kind == "sdist":
        has_sdist = True
        if filename == data["sdist_fallback"]:
            has_declared_sdist = True
        if filename != f"lingtai-{expected_version}.tar.gz":
            fail(f"artifacts[{index}] sdist filename is not the selected version")
        if any(artifact[key] is not None for key in ("python_tag", "abi_tag", "platform_tag")):
            fail(f"artifacts[{index}] sdist has wheel tags")
    else:
        fail(f"artifacts[{index}] has unsupported kind {kind!r}")
if not has_sdist or not has_declared_sdist:
    fail("sdist fallback is not a listed sdist")

print(json.dumps(data, sort_keys=True, separators=(",", ":")))
PY
}

# fetch_kernel_manifest loads and strictly validates the manifest for one
# provider and exact kernel tag. It never changes the TUI provider.
fetch_kernel_manifest() {
  local kernel_tag="$1" provider="${2:-${KERNEL_PROVIDER:-mirror}}" url body
  local validator="${3:-$(command -v python3 || true)}" manifest_file
  KERNEL_MANIFEST_PROVIDER=""
  KERNEL_MANIFEST_JSON=""

  if [[ "$provider" == "mirror" ]]; then
    body="$(mirror_asset_text "$KERNEL_REPO_SLUG" "$kernel_tag" "lingtai-kernel-release-manifest.json")" || return 1
    url="$LINGTAI_WEB_BASE/dl/$KERNEL_REPO_SLUG/$kernel_tag/lingtai-kernel-release-manifest.json"
  else
    url="$(kernel_manifest_url_for_provider github "$kernel_tag" || true)"
    [[ -n "$url" ]] || return 1
    body="$(curl -fsSL --max-time 30 "$url" 2>/dev/null || true)"
    [[ -n "$body" ]] || return 1
  fi
  [[ -n "$validator" ]] || {
    echo "error: Python is required to validate the kernel release manifest at $url" >&2
    return 1
  }
  manifest_file="$(mktemp "${TMPDIR:-/tmp}/lingtai-kernel-manifest-validate.XXXXXX")"
  printf '%s' "$body" > "$manifest_file"
  if ! update_validate_manifest "$validator" "$manifest_file" "$kernel_tag" >/dev/null 2>&1; then
    rm -f "$manifest_file"
    echo "error: kernel manifest at $url failed strict validation" >&2
    return 1
  fi
  rm -f "$manifest_file"
  KERNEL_MANIFEST_PROVIDER="$provider"
  KERNEL_MANIFEST_JSON="$body"
}

# kernel_manifest_url_for_provider returns the exact manifest route for one
# provider. Kernel fallback is intentionally independent from TUI provider
# selection, so this helper must not consult or mutate TUI state.
kernel_manifest_url_for_provider() {
  local provider="$1" tag="$2"
  case "$provider" in
    github) printf 'https://github.com/Lingtai-AI/lingtai-kernel/releases/download/%s/lingtai-kernel-release-manifest.json' "$tag" ;;
    mirror) printf '%s/dl/%s/%s/lingtai-kernel-release-manifest.json' "$LINGTAI_WEB_BASE" "$KERNEL_REPO_SLUG" "$tag" ;;
    *) return 1 ;;
  esac
}

# kernel_source_artifact echoes "<filename> <sha256>" for the manifest's
# declared sdist_fallback source artifact. This is the sole stable/default
# kernel artifact selection path; wheel metadata is retained only for manifest
# validation and is never selected or installed here.
kernel_source_artifact() {
  local manifest_json="$1" py="${2:-python3}" manifest_file
  manifest_file="$(mktemp "${TMPDIR:-/tmp}/lingtai-kernel-manifest.XXXXXX")"
  printf '%s' "$manifest_json" > "$manifest_file"
  "$py" - "$manifest_file" <<'PY'
import json, sys
data = json.loads(open(sys.argv[1]).read())
name = data.get("sdist_fallback", "")
for art in data.get("artifacts", []):
    if art.get("kind") == "sdist" and art.get("filename") == name:
        print(f"{art['filename']} {art['sha256']}")
        break
PY
}

# kernel_artifact_download_url echoes the exact release route for an artifact.
kernel_artifact_download_url() {
  local provider="$1" tag="$2" name="$3"
  case "$provider" in
    github) printf 'https://github.com/Lingtai-AI/lingtai-kernel/releases/download/%s/%s' "$tag" "$name" ;;
    mirror) printf '%s/dl/%s/%s/%s' "$LINGTAI_WEB_BASE" "$KERNEL_REPO_SLUG" "$tag" "$name" ;;
    *) return 1 ;;
  esac
}

# resolve_latest_kernel_release resolves one provider's latest kernel
# release. A mirror latest document is authoritative for the mirror tag; the
# GitHub API is used only for the GitHub fallback.
resolve_latest_kernel_release() {
  local provider="${1:-${KERNEL_PROVIDER:-mirror}}" body tag
  KERNEL_LATEST_TAG=""
  if [[ "$provider" == "mirror" ]]; then
    fetch_mirror_latest "$KERNEL_REPO_SLUG" || return 1
    KERNEL_LATEST_TAG="$MIRROR_KERNEL_LATEST_TAG"
    return 0
  fi
  body="$(curl -fsSL --max-time 15 "${KERNEL_GH_API_BASE}/releases/latest" 2>/dev/null || true)"
  [[ -n "$body" ]] || return 1
  tag="$(printf '%s' "$body" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)"
  [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || return 1
  printf '%s' "$body" | grep -q 'lingtai-kernel-release-manifest.json' || return 1
  KERNEL_LATEST_TAG="$tag"
}

# install_kernel_from_release installs the independently resolved kernel
# artifact from an exact local path. Mirror failure falls back to GitHub for the
# kernel only; TUI_PROVIDER is never changed.
install_kernel_from_release() {
  local py="$1" uv="$2" provider kernel_tag kernel_manifest artifact_line
  local fname sha download_url dest index_url version
  local providers=("mirror" "github")

  for provider in "${providers[@]}"; do
    KERNEL_PROVIDER="$provider"
    KERNEL_MANIFEST_PROVIDER=""
    KERNEL_MANIFEST_JSON=""
    KERNEL_LATEST_TAG=""
    if ! resolve_latest_kernel_release "$provider"; then
      warn "could not resolve latest kernel release through $provider"
      continue
    fi
    kernel_tag="$KERNEL_LATEST_TAG"
    say "Resolved latest kernel release: $kernel_tag (provider $provider)"
    if ! fetch_kernel_manifest "$kernel_tag" "$provider" "$py"; then
      warn "could not load the verified kernel manifest for $kernel_tag through $provider"
      continue
    fi
    kernel_manifest="$KERNEL_MANIFEST_JSON"
    artifact_line="$(kernel_source_artifact "$kernel_manifest" "$py" || true)"
    [[ -n "$artifact_line" ]] || { warn "kernel release $kernel_tag has no declared source artifact"; continue; }
    fname="${artifact_line%% *}"
    sha="${artifact_line##* }"
    download_url="$(kernel_artifact_download_url "$provider" "$kernel_tag" "$fname" || true)"
    [[ -n "$download_url" ]] || continue

    mkdir -p "$BUILD_DIR/kernel-artifact"
    dest="$BUILD_DIR/kernel-artifact/$fname"
    say "Downloading kernel source artifact: $fname (from $provider, release $kernel_tag) ..."
    if [[ "$provider" == "mirror" ]]; then
      download_mirror_asset "$KERNEL_REPO_SLUG" "$kernel_tag" "$fname" "$dest" || continue
    elif ! curl -fsSL --max-time 300 -o "$dest" "$download_url"; then
      warn "download failed for $download_url"
      continue
    fi
    if ! verify_sha256 "$dest" "$sha"; then
      echo "error: checksum mismatch for $fname — refusing unverified kernel bytes." >&2
      continue
    fi
    index_url="$(python_dependency_index_url)"
    say "Building and installing lingtai from verified local source archive (dependencies via $index_url) ..."
    if [[ -n "$uv" ]]; then
      if ! "$uv" pip install --index-url "$index_url" -p "$(dirname "$(dirname "$py")")" "$dest"; then
        warn "kernel source installation failed through $provider"
        continue
      fi
    elif ! "$py" -m pip install --index-url "$index_url" "$dest"; then
      warn "kernel source installation failed through $provider"
      continue
    fi
    if ! "$py" -c 'import lingtai; print("lingtai", getattr(lingtai, "__version__", "?"))'; then
      warn "lingtai import failed after kernel source install"
      continue
    fi

    KERNEL_SOURCE="release"
    KERNEL_RELEASE_TAG="$kernel_tag"
    KERNEL_VERSION_INSTALLED="$(printf '%s' "$kernel_manifest" | json_string_field kernel_version)"
    KERNEL_PROVIDER="$provider"
    return 0
  done
  return 1
}

# install_kernel_from_main installs the checked-out kernel main source tree by
# local path. Dependencies may use the configured index, but the LingTai source
# itself is never looked up by package name and never falls back to PyPI.
install_kernel_from_main() {
  local py="$1" uv="$2" index_url="${LINGTAI_PYPI_INDEX_URL:-https://pypi.org/simple}"
  [[ -n "$KERNEL_SOURCE_DIR" && -d "$KERNEL_SOURCE_DIR" ]] || return 1
  [[ "$KERNEL_MAIN_SHA" =~ ^[0-9a-f]{40}$ ]] || return 1
  [[ "$(git -C "$KERNEL_SOURCE_DIR" rev-parse HEAD 2>/dev/null || true)" == "$KERNEL_MAIN_SHA" ]] || {
    echo "error: kernel source checkout no longer matches resolved main commit $KERNEL_MAIN_SHA" >&2
    return 1
  }
  say "Installing lingtai from kernel main source ($KERNEL_MAIN_SHA; dependencies via $index_url) ..."
  if [[ -n "$uv" ]]; then
    "$uv" pip install --index-url "$index_url" -p "$(dirname "$(dirname "$py")")" "$KERNEL_SOURCE_DIR" || return 1
  else
    "$py" -m pip install --index-url "$index_url" "$KERNEL_SOURCE_DIR" || return 1
  fi
  "$py" -c 'import lingtai; print("lingtai", getattr(lingtai, "__version__", "?"))' || return 1
  KERNEL_SOURCE="main"
  KERNEL_VERSION_INSTALLED="$("$py" -c 'import lingtai; print(getattr(lingtai, "__version__", "?"))' 2>/dev/null || true)"
  KERNEL_PROVIDER="github"
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

# validate_install_target refuses ordinary (non---update) install over an
# existing managed binary at the selected bin dir — ordinary install is
# first-install-only; adopting or overwriting an existing target requires an
# explicit standalone maintenance asset (update.sh/fix.sh) instead.
validate_install_target() {
  [[ "$BIN_DIR" == /* && "$BIN_DIR" != *$'\n'* && "$BIN_DIR" != *$'\t'* && "$BIN_DIR" != */../* && "$BIN_DIR" != */./* ]] || {
    echo "error: install target is not an exact absolute directory: $BIN_DIR" >&2; return 1;
  }
  [[ ! -L "$BIN_DIR" ]] || { echo "error: install target is a symlink: $BIN_DIR" >&2; return 1; }
  local managed
  for managed in lingtai-tui lingtai lingtai-agent; do
    if [[ -e "$BIN_DIR/$managed" || -L "$BIN_DIR/$managed" ]]; then
      echo "error: existing managed target $BIN_DIR/$managed was found; ordinary install will not adopt or overwrite it." >&2
      echo "       Rerun install.sh to restore the latest canonical installation." >&2
      return 1
    fi
  done
}

# validate_fresh_install_state refuses ordinary install over an existing
# install receipt or runtime root — checked before any target creation,
# release resolution, download, or binary/runtime mutation, so a different
# empty --bin-dir cannot turn ordinary install into silent adoption of
# pre-existing state elsewhere under $HOME/.lingtai-tui. A deliberate
# --skip-python TUI-only install is the one safe exception for an existing,
# real runtime directory: it does not inspect, adopt, repair, or overwrite that
# legacy runtime, and records no runtime pointer until a later explicit setup.
validate_fresh_install_state() {
  local state_root="$HOME/.lingtai-tui"
  local metadata="$state_root/install.json"
  local runtime_root="$state_root/runtime"
  if [[ -e "$metadata" || -L "$metadata" ]]; then
    echo "error: existing install receipt $metadata was found; ordinary install is first-install-only." >&2
    echo "       Rerun install.sh to restore the latest canonical installation." >&2
    return 1
  fi
  if [[ -e "$runtime_root" || -L "$runtime_root" ]]; then
    if [[ "$SKIP_VENV" == "1" && -d "$runtime_root" && ! -L "$runtime_root" ]]; then
      note "Preserving existing runtime state at $runtime_root (--skip-python); no runtime will be adopted or changed."
      return 0
    fi
    echo "error: existing runtime state $runtime_root was found; ordinary install will not adopt or repair it." >&2
    echo "       Rerun install.sh to restore the latest canonical installation, or pass --skip-python for a TUI-only install." >&2
    return 1
  fi
}

# download_tui_source_archive obtains the producer-owned source archive from
# the exact tag-scoped mirror route and verifies its generic mirror record.
download_tui_source_archive() {
  local tag="$1" provider="$2" dest="$3" name url sidecar digest
  name="$(tui_source_asset_name "$tag")"
  if [[ "$provider" == "mirror" ]]; then
    download_mirror_asset "$REPO_SLUG" "$tag" "$name" "$dest"
    return
  fi
  url="$(release_asset_url "$tag" "$name" || true)"
  [[ -n "$url" ]] || return 1
  curl -fsSL --max-time 300 -o "$dest" "$url" || return 1
  sidecar="$dest.sha256"
  curl -fsSL --max-time 30 "${url}.sha256" -o "$sidecar" || return 1
  digest="$(cut -d' ' -f1 < "$sidecar" || true)"
  [[ "$digest" =~ ^[0-9a-f]{64}$ ]] || return 1
  verify_sha256 "$dest" "$digest"
}

clone_tui_tag() {
  local tag="$1" expected actual
  ensure_build_deps 1
  git clone --depth 1 --branch "$tag" "$REPO" "$BUILD_DIR"
  expected="$(peeled_tag_commit "$REPO" "$tag" || true)"
  actual="$(git -C "$BUILD_DIR" rev-parse HEAD 2>/dev/null || true)"
  [[ "$expected" =~ ^[0-9a-f]{40}$ && "$actual" == "$expected" ]] || {
    echo "error: TUI checkout for $tag is not the peeled release commit (expected $expected, got $actual)" >&2
    return 1
  }
  RESOLVED_COMMIT="$actual"
}

# build_from_source builds only lingtai-tui. Release tags use the producer
# archive when available; GitHub can use an exact peeled-tag checkout for
# historical releases that predate the producer archive.
build_from_source() {
  local ref="$1" requested_tag source_tarball provider
  requested_tag="$(release_tag_name "$ref")"
  mkdir -p "$(dirname "$BUILD_DIR")"
  rm -rf "$BUILD_DIR"

  if [[ -n "$requested_tag" ]]; then
    provider="${TUI_PROVIDER:-github}"
    ensure_build_deps 0
    command -v curl &>/dev/null || { echo "error: curl is required to download TUI source" >&2; return 1; }
    command -v tar &>/dev/null || { echo "error: tar is required to extract TUI source" >&2; return 1; }
    source_tarball="$BUILD_DIR/$(tui_source_asset_name "$requested_tag")"
    mkdir -p "$BUILD_DIR"
    if download_tui_source_archive "$requested_tag" "$provider" "$source_tarball"; then
      say "Extracting TUI source archive $requested_tag (from $provider) ..."
      tar -xzf "$source_tarball" -C "$BUILD_DIR" --strip-components 1 || return 2
      if [[ "$provider" == "github" ]]; then
        RESOLVED_COMMIT="$(peeled_tag_commit "$REPO" "$requested_tag" || true)"
        [[ "$RESOLVED_COMMIT" =~ ^[0-9a-f]{40}$ ]] || return 1
      else
        RESOLVED_COMMIT=""
      fi
    elif [[ "$provider" == "github" ]]; then
      rm -rf "$BUILD_DIR"
      clone_tui_tag "$requested_tag"
    else
      return 2
    fi
    VERSION="$requested_tag"
    RESOLVED_REF="$requested_tag"
  else
    ensure_build_deps 1
    say "Cloning lingtai ($ref) ..."
    if ! git clone --depth 1 --branch "$ref" "$REPO" "$BUILD_DIR" 2>/dev/null; then
      git clone --depth 1 "$REPO" "$BUILD_DIR"
      if [[ "$ref" != "main" ]]; then
        if ! (cd "$BUILD_DIR" && git fetch --depth 1 origin "$ref" && git checkout --quiet FETCH_HEAD); then
          echo "error: ref '$ref' not found in $REPO" >&2
          return 1
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
  (cd "$BUILD_DIR/tui" && CGO_ENABLED=0 go build -buildvcs=false -ldflags "-X main.version=$VERSION" -o "$BUILD_DIR/lingtai-tui" .)

  if [[ "$UPDATE_MODE" == "1" ]]; then
    local stage_bin="$BUILD_DIR/stage/bin"
    mkdir -p "$stage_bin"
    install -m 755 "$BUILD_DIR/lingtai-tui" "$stage_bin/lingtai-tui"
    verify_tui_binary_version "$stage_bin/lingtai-tui" "$VERSION"
    say "Installing update to $BIN_DIR ..."
    install_binary_atomically "$stage_bin/lingtai-tui" "$BIN_DIR/lingtai-tui"
  else
    say "Installing to $BIN_DIR ..."
    install -m 755 "$BUILD_DIR/lingtai-tui" "$BIN_DIR/lingtai-tui"
  fi
  ensure_lingtai_alias "$BIN_DIR"
  verify_tui_binary_version "$BIN_DIR/lingtai-tui" "$VERSION"
}

# build_latest_from_main resolves and pins both repositories before building. It
# deliberately does not consult release metadata: --latest is a separate,
# explicit current-main mode and never falls back to a stable release.
build_latest_from_main() {
  local actual_kernel_sha
  TUI_MAIN_SHA="$(resolve_main_branch_sha "$REPO")" || {
    echo "error: could not resolve the full TUI main commit from $REPO" >&2
    return 1
  }
  KERNEL_MAIN_SHA="$(resolve_main_branch_sha "$KERNEL_REPO")" || {
    echo "error: could not resolve the full kernel main commit from $KERNEL_REPO" >&2
    return 1
  }
  say "Resolved TUI main commit: $TUI_MAIN_SHA"
  say "Resolved kernel main commit: $KERNEL_MAIN_SHA"

  # Reuse the existing source-build path for the TUI. Its shallow clone is
  # accepted only when it lands on the exact SHA resolved above.
  build_from_source main
  if [[ "${RESOLVED_COMMIT:-}" != "$TUI_MAIN_SHA" ]]; then
    echo "error: TUI main moved during checkout (resolved $TUI_MAIN_SHA, cloned ${RESOLVED_COMMIT:-unknown})" >&2
    return 1
  fi

  KERNEL_SOURCE_DIR="$BUILD_DIR/kernel"
  ensure_build_deps 1
  say "Cloning lingtai-kernel (main at $KERNEL_MAIN_SHA) ..."
  git clone --depth 1 --branch main "$KERNEL_REPO" "$KERNEL_SOURCE_DIR"
  actual_kernel_sha="$(git -C "$KERNEL_SOURCE_DIR" rev-parse HEAD)"
  if [[ "$actual_kernel_sha" != "$KERNEL_MAIN_SHA" ]]; then
    echo "error: kernel main moved during checkout (resolved $KERNEL_MAIN_SHA, cloned $actual_kernel_sha)" >&2
    return 1
  fi
  TUI_MAIN_SHA="$RESOLVED_COMMIT"
  KERNEL_MAIN_SHA="$actual_kernel_sha"
  say "Using TUI main commit: $TUI_MAIN_SHA"
  say "Using kernel main commit: $KERNEL_MAIN_SHA"
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

  if [[ -L "$HOME/.lingtai-tui" ]]; then
    echo "error: $HOME/.lingtai-tui is a symlink; refusing redirected install state." >&2
    exit 1
  fi

  cleanup() {
    cd / 2>/dev/null || true
    rm -rf "$BUILD_DIR"
  }
  trap cleanup EXIT

  if is_wsl; then
    say "Detected Windows Subsystem for Linux (WSL)."
    note "Binaries and the Python runtime install into your Linux home ($HOME)."
  fi

  if command -v curl &>/dev/null && [ -z "${GOPROXY:-}" ] &&
     ! curl -sSfL --max-time 3 -o /dev/null \
       "https://proxy.golang.org/github.com/golang/go/@latest" 2>/dev/null; then
    say "proxy.golang.org unreachable; using China-friendly Go build mirrors."
    export GOPROXY="https://goproxy.cn,direct"
    export GOSUMDB="sum.golang.google.cn"
  fi

  resolve_bin_dir
  if [[ "$LATEST_MAIN_MODE" == "1" ]]; then
    build_latest_from_main || exit 1
    KERNEL_PROVIDER="github"
  else
    if [[ "$UPDATE_MODE" != "1" && "$REINSTALL_OK" != "1" ]]; then
      if [[ -n "$REF" ]]; then
        validate_fresh_install_state || exit 1
      elif [[ -e "$HOME/.lingtai-tui/install.json" ]]; then
        REINSTALL_OK=1
        say "Existing installation detected; reinstalling in place."
      elif [[ -e "$HOME/.lingtai-tui/runtime" || -L "$HOME/.lingtai-tui/runtime" ]]; then
        # No receipt means there is no supported installation to migrate or
        # adopt. Ignore an old caller's pinned version and run the one normal
        # latest-release install path over the canonical runtime location.
        REINSTALL_OK=1
        VERSION=""
        say "Non-canonical runtime detected; reinstalling the latest release."
      else
        validate_fresh_install_state || exit 1
      fi
      if [[ "$REINSTALL_OK" != "1" ]]; then
        validate_install_target || exit 1
      fi
    fi

    resolve_source_provider || {
      echo "error: unsupported TUI source provider: $SOURCE_ARG" >&2
      exit 1
    }
    # The mirror is the default kernel source even when TUI selection is
    # explicit; a failed component never changes the other component.
    KERNEL_PROVIDER="mirror"

    TARGET_TAG="$VERSION"
    if [[ -z "$REF" && -z "$TARGET_TAG" ]]; then
      if [[ "$SOURCE_ONLY_DEFAULT" == "1" ]]; then
        if resolve_tui_latest; then
          TARGET_TAG="$TUI_TAG"
          say "Latest TUI source release is $TARGET_TAG (lingtai.ai)"
        else
          warn "lingtai.ai TUI source metadata is unavailable; falling back to latest GitHub TUI source release."
          TUI_PROVIDER="github"
          TARGET_TAG="$(latest_release_tag || true)"
        fi
      else
        TARGET_TAG="$(latest_release_tag || true)"
      fi
      [[ -n "$TARGET_TAG" ]] || {
        echo "error: could not determine the latest TUI release tag." >&2
        exit 1
      }
    fi

    if [[ "$UPDATE_MODE" == "1" ]]; then
      [[ -n "$TARGET_TAG" ]] || {
        echo "error: --update could not resolve a TUI release tag." >&2
        exit 1
      }
      TUI_PROVIDER="github"
      build_from_source "$TARGET_TAG"
    elif [[ -n "$REF" ]]; then
      build_from_source "$REF"
    elif [[ "$SOURCE_ONLY_DEFAULT" == "1" || "$FROM_SOURCE" == "1" ]]; then
      tui_build_status=0
      build_from_source "$TARGET_TAG" || tui_build_status=$?
      if [[ "$tui_build_status" != "0" ]]; then
        if [[ "$SOURCE_ONLY_DEFAULT" == "1" && "$TUI_PROVIDER" == "mirror" && "$tui_build_status" == "2" ]]; then
          warn "lingtai.ai TUI source archive failed; falling back to latest GitHub TUI source release."
          TUI_PROVIDER="github"
          TARGET_TAG="$(latest_release_tag || true)"
          [[ -n "$TARGET_TAG" ]] || exit 1
          build_from_source "$TARGET_TAG"
        else
          echo "error: TUI source build failed for exact selection $TARGET_TAG." >&2
          exit 1
        fi
      fi
    else
      build_from_source "$TARGET_TAG"
    fi
  fi

  if ! ensure_runtime_venv "$BIN_DIR"; then
    echo "error: LingTai install incomplete — the TUI binary is present, but the Python runtime could not be provisioned." >&2
    if [[ "$LATEST_MAIN_MODE" == "1" ]]; then
      echo "       Kernel main commit: $KERNEL_MAIN_SHA" >&2
    else
      echo "       No package-index install was attempted. Fix the provider/artifact error or pass --skip-python." >&2
    fi
    exit 1
  fi

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
    "$BIN_DIR/lingtai-tui"
  say "Wrote install metadata to $GLOBAL_DIR/install.json"

  if should_install_desktop; then
    if ! register_desktop_bootstrap; then
      echo "error: LingTai TUI/runtime installation succeeded, but the lazy macOS Desktop command could not be registered." >&2
      echo "       No Desktop App state or Desktop network access occurred; use --skip-desktop to keep the completed installation." >&2
      exit 1
    fi
  fi

  say "Done. $("$BIN_DIR/lingtai-tui" version 2>&1 || echo "$VERSION")"
  print_path_hint "$BIN_DIR"
}
if [[ "${LINGTAI_INSTALL_SH_SOURCE_ONLY:-0}" != "1" ]]; then
  main "$@"
fi
