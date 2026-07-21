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
# lingtai-tui and (when built) lingtai-portal at its top level. The bundle
# manifest may also list a lingtai-<tag>-windows-amd64.zip entry for the same
# release; this installer's strict manifest parser accepts that entry (so a
# Windows archive in the manifest never fails POSIX validation) but never
# selects or downloads it — native Windows installs use install.ps1 instead.
#
# Source policy (--source auto|github|gitee, or LINGTAI_SOURCE env; default
# auto): auto runs a bounded, fail-open public-IP country lookup and prefers
# Gitee (huangzesen1997/lingtai + huangzesen1997/lingtai-kernel) for mainland
# China. Each release publishes a small "bundle manifest" binding one exact
# TUI tag to one exact pinned kernel release/version/artifacts/checksums —
# see RELEASING.md. A provider fallback (Gitee unreachable, or missing an
# asset) always re-fetches the SAME resolved tag/bundle from the other
# provider; it never independently re-resolves "latest" on the fallback. The
# Python `lingtai` runtime is installed from that pinned kernel release
# artifact by explicit local file path — never `pip install lingtai` from any
# package index — with SHA256 verified before install. Third-party
# dependencies still resolve via one configured package index. An explicit
# LINGTAI_PYPI_INDEX_URL always wins; otherwise the final Gitee bundle provider
# uses Tsinghua TUNA and GitHub uses pypi.org. Only lingtai's own bytes are
# pinned. If no compatible platform wheel exists for the runtime's
# interpreter, the pinned sdist is used instead (may require a local build
# toolchain).
#
# LingTai is NEVER installed by requesting the package name "lingtai" from
# any index — there is no PyPI fallback. On the default one-command path a
# pinned bundle is mandatory: if none can be resolved (either provider,
# same-tag fallback attempted), or the resolved bundle's kernel artifact
# fails to verify/install, the installer FAILS LOUD with the exact
# provider/tag/error rather than degrading to a package-index install.
# --ref/source-ref builds have no bundle to pin against and fail loud the
# same way. --skip-python (alias --skip-venv) is the explicit opt-out for a
# TUI/portal-only install; you then provision the Python runtime yourself.
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
PYPI_INDEX_URL_DEFAULT="https://pypi.org/simple"
PYPI_INDEX_URL_GITEE_DEFAULT="https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple"

# Gitee mirror: a real repository, but release assets may not exist for every
# tag yet (see gitee_release_asset_url / gitee_bundle_manifest_url below,
# which never invent a URL — they only return one after confirming presence
# via the Gitee API). GITEE_OWNER/GITEE_REPO name the TUI mirror; the kernel
# mirror repo name is derived per-lookup (see kernel_gitee_api_base).
GITEE_OWNER="${LINGTAI_GITEE_OWNER:-huangzesen1997}"
GITEE_REPO="${LINGTAI_GITEE_REPO:-lingtai}"
GITEE_KERNEL_REPO="${LINGTAI_GITEE_KERNEL_REPO:-lingtai-kernel}"
GITEE_API_BASE="https://gitee.com/api/v5/repos/${GITEE_OWNER}/${GITEE_REPO}"
GITEE_KERNEL_API_BASE="https://gitee.com/api/v5/repos/${GITEE_OWNER}/${GITEE_KERNEL_REPO}"
KERNEL_GH_API_BASE="https://api.github.com/repos/Lingtai-AI/lingtai-kernel"
BUNDLE_TUI_ARCHIVE_SHA=""

# Country-detection endpoints for auto source selection. Two independent,
# unauthenticated, no-signup providers so one outage doesn't force a GitHub
# fallback for every mainland user; each probe is short-timeout and its
# result is discarded (fail-open) on any error. Only the two-letter country
# code of the requester's public IP is requested — no identity, no
# credentials, no persistent client. Overridable for tests/offline use.
COUNTRY_DETECT_URL_1="${LINGTAI_COUNTRY_DETECT_URL_1:-https://ipapi.co/country/}"
COUNTRY_DETECT_URL_2="${LINGTAI_COUNTRY_DETECT_URL_2:-https://ifconfig.co/country-iso}"
MIRROR_TIMEOUT="${LINGTAI_MIRROR_TIMEOUT:-3}"

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
SKIP_VENV=0          # --skip-python (alias: --skip-venv): don't touch the Python runtime venv
INSTALL_KIND=""      # "release-asset" | "source-build" (recorded in metadata)
SOURCE_ARG="${LINGTAI_SOURCE:-auto}"  # --source auto|github|gitee (env LINGTAI_SOURCE)
BUNDLE_PROVIDER=""    # resolved by resolve_source_provider(): "github" | "gitee"
BUNDLE_TAG=""         # resolved release tag shared by the TUI archive + bundle manifest
BUNDLE_MANIFEST_JSON="" # raw bundle manifest body, once fetched
BUNDLE_REQUIRED=0     # 1 on the default release-asset one-command path (no --ref, no --update):
                      # a pinned kernel bundle is mandatory there, so a missing/incoherent/failed
                      # bundle or kernel install must fail loud rather than silently falling back
                      # to `pip install lingtai`. 0 for --ref/source-ref builds, where no bundle is
                      # expected to exist at all — those paths require --skip-python instead (see
                      # ensure_runtime_venv).
KERNEL_SOURCE=""      # "bundle" | "" (recorded in install.json only on a verified bundle install; LingTai is never installed from a package index by name — see ensure_runtime_venv)
KERNEL_BUNDLE_ID=""
KERNEL_VERSION_INSTALLED=""
KERNEL_PROVIDER=""
KERNEL_MANIFEST_PROVIDER=""  # set by fetch_kernel_manifest(); which provider actually served the kernel manifest
KERNEL_MANIFEST_JSON=""      # set by fetch_kernel_manifest() in the same shell as the provider
BUNDLE_MANIFEST_KERNEL_TAG=""
BUNDLE_MANIFEST_KERNEL_VERSION=""
BUNDLE_MANIFEST_KERNEL_FILENAME=""
BUNDLE_MANIFEST_BUNDLE_ID=""

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
  --version <tag>      Release tag to install (default: latest from selected source)
  --ref <ref>          Build a specific git branch/tag/commit from source
  --bin-dir <dir>      Install binaries into <dir>
  --prefix <dir>       Install binaries into <dir>/bin (used by --update)
  --from-source        Always build from source (skip prebuilt release assets)
  --skip-portal        Install only lingtai-tui (no portal)
  --skip-python         Do not create/update the Python runtime venv (explicit
                         opt-out; required when a pinned kernel bundle is
                         unavailable and you still want TUI-only binaries).
                         --skip-venv is a back-compat alias.
  --source <mode>       auto|github|gitee (default: auto, or $LINGTAI_SOURCE).
                         auto prefers Gitee for mainland-China public IPs via
                         a bounded, fail-open country lookup; an explicit
                         override always wins and skips detection.
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

# --- source policy: country detection + GitHub/Gitee provider selection -----

# json_string_field extracts the first string value of a top-level JSON key
# from stdin using the same grep/sed idiom as release_asset_url/latest_release_tag
# above (no jq dependency). Not a general JSON parser — sufficient for the
# flat manifest/API shapes this script reads.
json_string_field() {
  local key="$1"
  grep -o "\"$key\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -1 \
    | sed "s/.*\"$key\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/"
}

# detect_country_cn returns 0 if a bounded, best-effort public-IP lookup says
# the requester is in mainland China, 1 otherwise (including "could not tell"
# — this function is fail-open by contract: a lookup failure or ambiguous
# result must never be treated as CN). Two independent unauthenticated
# providers are tried in order; each is capped at MIRROR_TIMEOUT seconds.
# Only the two-letter country code is requested — no identity, no
# credentials, no persistent client, no request body beyond a plain GET.
detect_country_cn() {
  command -v curl &>/dev/null || return 1
  local cc
  cc="$(curl -fsSL --max-time "$MIRROR_TIMEOUT" "$COUNTRY_DETECT_URL_1" 2>/dev/null | tr -d '[:space:]' || true)"
  if [[ -z "$cc" ]]; then
    cc="$(curl -fsSL --max-time "$MIRROR_TIMEOUT" "$COUNTRY_DETECT_URL_2" 2>/dev/null | tr -d '[:space:]' || true)"
  fi
  [[ "$cc" == "CN" ]]
}

# gitee_reachable is a cheap liveness probe for the Gitee API, bounded the
# same way as the GitHub API calls above.
gitee_reachable() {
  command -v curl &>/dev/null || return 1
  curl -fsSL --max-time "$MIRROR_TIMEOUT" -o /dev/null "https://gitee.com/api/v5/repos/${GITEE_OWNER}/${GITEE_REPO}" 2>/dev/null
}

github_reachable() {
  command -v curl &>/dev/null || return 1
  curl -fsSL --max-time "$MIRROR_TIMEOUT" -o /dev/null "$API_BASE" 2>/dev/null
}

# resolve_source_provider sets BUNDLE_PROVIDER to "github" or "gitee" per the
# --source policy:
#   explicit override (github|gitee) -> that provider, no detection, no
#     reachability fallback (an explicit choice is honored even if degraded;
#     the caller still gets a clear error later if that provider truly has no
#     usable release).
#   auto -> bounded country lookup; CN -> prefer gitee, else github; a failed
#     or ambiguous lookup fails open to github. The preferred provider is then
#     probed for reachability; if unreachable, falls back to the other
#     provider for the SAME resolved tag/bundle (never re-resolves "latest").
resolve_source_provider() {
  case "$SOURCE_ARG" in
    github) BUNDLE_PROVIDER="github"; return 0 ;;
    gitee)  BUNDLE_PROVIDER="gitee"; return 0 ;;
  esac

  local preferred="github"
  if detect_country_cn; then
    preferred="gitee"
  fi

  if [[ "$preferred" == "gitee" ]]; then
    if gitee_reachable; then
      BUNDLE_PROVIDER="gitee"
    else
      note "Gitee unreachable; using GitHub for this install."
      BUNDLE_PROVIDER="github"
    fi
  else
    BUNDLE_PROVIDER="github"
  fi
}

# python_dependency_index_url chooses the single package index used only for
# third-party dependencies of the verified local LingTai artifact. An explicit
# override always wins. Otherwise the provider that actually served the final
# bundle manifest decides the reachable default: Gitee uses Tsinghua TUNA;
# GitHub uses official PyPI.
python_dependency_index_url() {
  if [[ -n "${LINGTAI_PYPI_INDEX_URL:-}" ]]; then
    printf '%s' "$LINGTAI_PYPI_INDEX_URL"
  elif [[ "${BUNDLE_PROVIDER:-github}" == "gitee" ]]; then
    printf '%s' "$PYPI_INDEX_URL_GITEE_DEFAULT"
  else
    printf '%s' "$PYPI_INDEX_URL_DEFAULT"
  fi
}

# --- Gitee release API (mirrors the GitHub helpers above) -------------------

# gitee_latest_release_tag queries Gitee's public "latest release" endpoint.
# Returns nonzero (prints nothing) if Gitee has no releases yet — callers
# must NOT construct a URL from this failure; see the module header note
# about never inventing a Gitee release URL.
gitee_latest_release_tag() {
  local body tag
  command -v curl &>/dev/null || return 1
  body="$(curl -fsSL --max-time 15 "${GITEE_API_BASE}/releases/latest" 2>/dev/null || true)"
  [[ -n "$body" ]] || return 1
  tag="$(printf '%s' "$body" | json_string_field tag_name)"
  [[ -n "$tag" ]] || return 1
  printf '%s' "$tag"
}

# gitee_release_asset_url echoes the browserDownloadUrl for a named attachment
# on a Gitee release tag, or nothing if the release or the named attachment
# does not exist. Uses the release-by-tag + attachment listing so a missing
# asset is detected before any download attempt, exactly like
# release_asset_url's GitHub equivalent.
gitee_release_asset_url() {
  local tag="$1" name="$2" body url
  command -v curl &>/dev/null || return 1
  body="$(curl -fsSL --max-time 15 "${GITEE_API_BASE}/releases/tags/$tag" 2>/dev/null || true)"
  [[ -n "$body" ]] || return 1
  # attach_files is an array of {name, browserDownloadUrl/browser_download_url,
  # ...}; scope the
  # match to the object containing our target name, then pull the URL out of
  # that same fragment so we don't grab an unrelated asset's URL.
  local fragment
  fragment="$(printf '%s' "$body" | grep -o "{[^{}]*\"name\"[[:space:]]*:[[:space:]]*\"$name\"[^{}]*}" | head -1)"
  [[ -n "$fragment" ]] || return 1
  url="$(printf '%s' "$fragment" | sed -n -E 's/.*"browserDownloadUrl"[[:space:]]*:[[:space:]]*"([^"]*)".*/\1/p; s/.*"browser_download_url"[[:space:]]*:[[:space:]]*"([^"]*)".*/\1/p' | head -1)"
  [[ -n "$url" && "$url" != "$fragment" ]] || return 1
  printf '%s' "$url"
}

# --- bundle manifest resolution (schema lingtai.tui.bundle/v1) --------------

# bundle_manifest_url_for_provider echoes the bundle manifest asset URL for a
# tag on the given provider, or nothing if unavailable.
bundle_manifest_url_for_provider() {
  local provider="$1" tag="$2"
  case "$provider" in
    github) release_asset_url "$tag" "lingtai-bundle-manifest.json" ;;
    gitee)  gitee_release_asset_url "$tag" "lingtai-bundle-manifest.json" ;;
    *) return 1 ;;
  esac
}

# fetch_bundle_manifest resolves BUNDLE_TAG (explicit VERSION, else latest on
# the CHOSEN provider) and BUNDLE_MANIFEST_JSON for BUNDLE_PROVIDER. If the
# preferred provider has no manifest for the resolved tag, falls back to the
# OTHER provider for the SAME tag (never re-resolves "latest" on the second
# provider — see the module header contract). Returns nonzero if neither
# provider has a usable manifest for the resolved tag.
fetch_bundle_manifest() {
  local tag="$VERSION" body url

  if [[ -z "$tag" ]]; then
    if [[ "$BUNDLE_PROVIDER" == "gitee" ]]; then
      tag="$(gitee_latest_release_tag || true)"
      if [[ -z "$tag" ]]; then
        note "Gitee has no releases yet; using GitHub to resolve the latest release."
        BUNDLE_PROVIDER="github"
        tag="$(latest_release_tag || true)"
      fi
    else
      tag="$(latest_release_tag || true)"
    fi
  fi
  [[ -n "$tag" ]] || return 1

  url="$(bundle_manifest_url_for_provider "$BUNDLE_PROVIDER" "$tag" || true)"
  if [[ -z "$url" ]]; then
    local other="github"
    [[ "$BUNDLE_PROVIDER" == "github" ]] && other="gitee"
    note "$BUNDLE_PROVIDER has no bundle manifest for $tag; trying $other for the SAME tag."
    url="$(bundle_manifest_url_for_provider "$other" "$tag" || true)"
    [[ -n "$url" ]] || return 1
    BUNDLE_PROVIDER="$other"
  fi

  body="$(curl -fsSL --max-time 30 "$url" 2>/dev/null || true)"
  [[ -n "$body" ]] || return 1
  if ! load_bundle_manifest "$body" "$tag"; then
    echo "error: bundle manifest at $url failed strict validation" >&2
    return 1
  fi

  BUNDLE_TAG="$tag"
  BUNDLE_MANIFEST_JSON="$body"
  return 0
}

# Validate the complete bundle contract at the trust boundary and print the
# canonical digest for this host's one exact archive.
parse_bundle_manifest() {
  local body="$1" expected_tag="$2"
  BODY="$body" python3 - "$expected_tag" "$(detect_os)" "$(detect_arch)" <<'PY'
import datetime, json, os, re, sys
expected_tag, os_name, arch = sys.argv[1:]
def pairs(items):
    result = {}
    for key, value in items:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result
def exact(value, keys, label):
    if not isinstance(value, dict) or set(value) != set(keys):
        raise ValueError(f"{label} has the wrong object shape")
def string(value, label):
    if not isinstance(value, str) or not value:
        raise ValueError(f"{label} must be a nonempty string")
    return value
try:
    data = json.loads(os.environ["BODY"], object_pairs_hook=pairs)
    exact(data, ("schema", "bundle_id", "tui_tag", "tui_commit", "generated_at", "kernel_tag", "kernel_version", "kernel_manifest_filename", "archives", "providers"), "manifest")
    if data["schema"] != "lingtai.tui.bundle/v1": raise ValueError("unexpected schema")
    for key in ("bundle_id", "tui_tag", "tui_commit", "kernel_tag", "kernel_version", "kernel_manifest_filename"): string(data[key], key)
    if data["bundle_id"] != data["tui_tag"] or data["tui_tag"] != expected_tag: raise ValueError("bundle_id/tui_tag does not equal resolved tag")
    if not re.fullmatch(r"[0-9a-f]{40}", data["tui_commit"]): raise ValueError("tui_commit must be a 40-character lowercase commit SHA")
    generated_at = data["generated_at"]
    if not isinstance(generated_at, str) or not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z", generated_at): raise ValueError("generated_at must be YYYY-MM-DDTHH:MM:SSZ")
    datetime.datetime.strptime(generated_at, "%Y-%m-%dT%H:%M:%SZ")
    if not isinstance(data["archives"], list) or not data["archives"]: raise ValueError("archives must be a nonempty array")
    names = set()
    for archive in data["archives"]:
        exact(archive, ("filename", "sha256"), "archive entry")
        name = string(archive["filename"], "archive filename")
        if name in names: raise ValueError("archives contains duplicate filenames")
        names.add(name)
        if not (re.fullmatch(r"lingtai-[^/]+-(?:darwin|linux)-(?:amd64|arm64)\.tar\.gz", name) or re.fullmatch(r"lingtai-[^/]+-windows-amd64\.zip", name)): raise ValueError("archive filename is invalid")
        if not isinstance(archive["sha256"], str) or not re.fullmatch(r"[0-9a-f]{64}", archive["sha256"]): raise ValueError("archive sha256 must be lowercase 64-hex")
    target = f"lingtai-{expected_tag}-{os_name}-{arch}.tar.gz"
    hits = [archive for archive in data["archives"] if archive["filename"] == target]
    if len(hits) != 1: raise ValueError(f"expected exactly one archive for {target}, found {len(hits)}")
    exact(data["providers"], ("github", "gitee"), "providers")
    exact(data["providers"]["github"], ("repo",), "github provider")
    exact(data["providers"]["gitee"], ("owner", "repo"), "gitee provider")
    string(data["providers"]["github"]["repo"], "github repo")
    string(data["providers"]["gitee"]["owner"], "gitee owner")
    string(data["providers"]["gitee"]["repo"], "gitee repo")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", data["providers"]["github"]["repo"]): raise ValueError("github repo is invalid")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", data["providers"]["gitee"]["owner"]): raise ValueError("gitee owner is invalid")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+", data["providers"]["gitee"]["repo"]): raise ValueError("gitee repo is invalid")
except (ValueError, TypeError, json.JSONDecodeError) as exc:
    raise SystemExit(f"invalid strict bundle manifest: {exc}")
print(hits[0]["sha256"])
print(data["kernel_tag"])
print(data["kernel_version"])
print(data["kernel_manifest_filename"])
print(data["bundle_id"])
PY
}

validate_bundle_manifest() { parse_bundle_manifest "$1" "$2" | sed -n '1p'; }

load_bundle_manifest() {
  local body="$1" expected_tag="$2" fields=() field
  while IFS= read -r field; do fields+=("$field"); done < <(parse_bundle_manifest "$body" "$expected_tag")
  [[ "${#fields[@]}" == 5 ]] || return 1
  BUNDLE_TUI_ARCHIVE_SHA="${fields[0]}"
  BUNDLE_MANIFEST_KERNEL_TAG="${fields[1]}"
  BUNDLE_MANIFEST_KERNEL_VERSION="${fields[2]}"
  BUNDLE_MANIFEST_KERNEL_FILENAME="${fields[3]}"
  BUNDLE_MANIFEST_BUNDLE_ID="${fields[4]}"
}

# bundle_manifest_field returns values populated by the strict parser; it
# never reparses raw manifest text.
bundle_manifest_field() {
  case "$1" in
    bundle_id) printf '%s\n' "$BUNDLE_MANIFEST_BUNDLE_ID" ;;
    kernel_tag) printf '%s\n' "$BUNDLE_MANIFEST_KERNEL_TAG" ;;
    kernel_version) printf '%s\n' "$BUNDLE_MANIFEST_KERNEL_VERSION" ;;
    kernel_manifest_filename) printf '%s\n' "$BUNDLE_MANIFEST_KERNEL_FILENAME" ;;
    *) return 1 ;;
  esac
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
      --skip-python|--skip-venv) SKIP_VENV=1; shift ;;
      --source) SOURCE_ARG="${2:?error: --source requires a value}"; shift 2 ;;
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

  case "$SOURCE_ARG" in
    auto|github|gitee) ;;
    *) echo "error: --source must be one of auto|github|gitee, got: $SOURCE_ARG" >&2; usage >&2; exit 1 ;;
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

# write_install_metadata records the install so `lingtai-tui`'s source updater
# can re-run this script for a newer tag. install_method stays "source" for
# updater compatibility regardless of whether we downloaded a prebuilt asset or
# built from source; install_kind records which path was taken (additive field).
write_install_metadata() {
  local global_dir="$1" prefix="$2" bin_dir="$3" repo_url="$4" requested_ref="$5"
  local resolved_ref="$6" resolved_commit="$7" stamped_version="$8" tui_path="$9"
  local portal_path="${10:-}" metadata_path tmp_path installed_at portal_json=""
  local install_kind="${INSTALL_KIND:-source-build}"
  # Bundle provenance is read from globals (set by install_kernel_from_bundle
  # during this run) rather than added as more positional params — this
  # function already has 10. KERNEL_SOURCE is only ever "" (no verified kernel
  # install happened this run — e.g. --skip-python) or "bundle" (LingTai is
  # never installed from a package index by name, so there is no "pypi"
  # value to record here). The block is omitted entirely, not written as
  # empty strings, when KERNEL_SOURCE is "" — old readers see exactly the
  # same install.json shape as before this field existed.
  local bundle_json=""
  if [[ "$KERNEL_SOURCE" == "bundle" ]]; then
    bundle_json="$(printf ',\n  "kernel_source": "bundle",\n  "kernel_bundle_id": "%s",\n  "kernel_version": "%s",\n  "kernel_provider": "%s"' \
      "$(json_escape "$KERNEL_BUNDLE_ID")" "$(json_escape "$KERNEL_VERSION_INSTALLED")" "$(json_escape "$KERNEL_PROVIDER")")"
    bundle_json="$(printf '%s,\n  "bundle_provider": "%s"' "$bundle_json" "$(json_escape "$BUNDLE_PROVIDER")")"
  fi

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
  ]$bundle_json
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

# ensure_runtime_venv creates or updates ~/.lingtai-tui/runtime/venv and
# installs the `lingtai` package into it from the pinned release-bundle
# kernel artifact, by explicit local file path — LingTai itself is NEVER
# requested from a package index by name (only third-party dependencies
# resolve via the configured index; see install_kernel_from_bundle). This is
# mirrored by the TUI's own EnsureVenv logic (uv venv --python 3.13 if uv
# exists, else python3 -m venv; verify import; stamp env marker; symlink
# lingtai-agent).
#
# On the default release-asset one-command path (BUNDLE_REQUIRED=1), a
# resolved bundle + a successful kernel-artifact install are MANDATORY: any
# failure (no bundle manifest, incoherent manifest, no compatible wheel/sdist,
# checksum mismatch, install failure) is a fail-loud error, not a fallback.
# On a --ref source build (BUNDLE_REQUIRED=0), no bundle is expected to
# exist at all for an arbitrary ref, so this function fails loud with a
# distinct "pass --skip-python" message instead of silently reaching for
# PyPI. --skip-python (alias --skip-venv) is the only way to skip the Python
# runtime entirely; venv creation/repair problems below remain best-effort
# (they warn and defer to the TUI's own venv repair) since those are
# genuinely transient environment issues, not a LingTai-source violation.
ensure_runtime_venv() {
  local bin_dir="$1"
  local venv_dir="$HOME/.lingtai-tui/runtime/venv"
  local uv py repair_attempt

  if [[ "$SKIP_VENV" == "1" ]]; then
    note "Skipping Python runtime venv (--skip-python)."
    return 0
  fi

  if [[ -z "$BUNDLE_MANIFEST_JSON" ]]; then
    if [[ "$BUNDLE_REQUIRED" == "1" ]]; then
      echo "error: no pinned kernel release bundle could be resolved for this install." >&2
      echo "       Tried provider(s): $BUNDLE_PROVIDER (with same-tag fallback to the other provider)." >&2
      echo "       LingTai's Python runtime is installed only from a verified pinned release" >&2
      echo "       artifact, never from PyPI/an index by package name — so this is a hard stop," >&2
      echo "       not a silent fallback." >&2
      echo "       Options:" >&2
      echo "         - Retry (the bundle manifest may not be published yet for this exact release)." >&2
      echo "         - Pass --version <tag> for a release known to have a bundle manifest." >&2
      echo "         - Pass --skip-python to install the TUI/portal binaries only, then set up the" >&2
      echo "           Python runtime yourself (e.g. from an editable lingtai-kernel checkout)." >&2
      return 1
    else
      echo "error: --ref/source-ref builds have no pinned kernel release bundle to install from." >&2
      echo "       LingTai's Python runtime is installed only from a verified pinned release" >&2
      echo "       artifact, never from PyPI/an index by package name, so this build cannot" >&2
      echo "       provision the Python runtime automatically." >&2
      echo "       Pass --skip-python to install the TUI/portal binaries only, then set up the" >&2
      echo "       Python runtime yourself — for example an editable install against a local" >&2
      echo "       lingtai-kernel checkout (see RELEASING.md / CLAUDE.md \"Agent venv\")." >&2
      return 1
    fi
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
            warn "uv venv failed and no Python 3.11+ with venv/ensurepip is available; skipping runtime setup."
            warn "Install uv or Python with venv/ensurepip support, then re-run the installer."
            return 0
          fi
        fi
      fi
      if [[ ! -x "$venv_dir/bin/python" && ! -x "$venv_dir/bin/python3" && -z "$uv" ]]; then
        if python_ok; then
          python3 -m venv "$venv_dir" || { warn "failed to create venv"; return 0; }
        else
          warn "Cannot create runtime venv: uv is unavailable and no Python 3.11+ with venv/ensurepip is available."
          warn "Install uv or Python with venv/ensurepip support, then re-run the installer."
          return 0
        fi
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

    if ! "$py" -m pip --version >/dev/null 2>&1 && [[ -z "$uv" ]]; then
      if [[ "$repair_attempt" == "0" ]]; then
        warn "runtime venv pip is missing; retaining it and provisioning a new runtime venv path."
        venv_dir="$HOME/.lingtai-tui/runtime/venv-repair-$$-1"
        repair_attempt=1
        continue
      fi
      warn "runtime venv pip is missing after recreate; TUI will repair it on first launch."
      return 0
    fi

    local install_ok=0
    # The pinned release-bundle kernel artifact is the ONLY LingTai install
    # source (guaranteed present at this point — see the BUNDLE_MANIFEST_JSON
    # guard above). Any failure here (incoherent manifest, no compatible
    # wheel/sdist, checksum mismatch, install command failure) is retried
    # once after a venv recreate (a legitimate transient-environment repair,
    # the same pattern every other step in this loop uses), then FAILS LOUD —
    # it never falls back to `pip install lingtai` from an index.
    if install_kernel_from_bundle "$py" "$uv"; then
      install_ok=1
    fi
    if [[ "$install_ok" != "1" ]]; then
      if [[ "$repair_attempt" == "0" ]]; then
        warn "failed to install the pinned kernel bundle artifact; retaining the venv and provisioning a new runtime venv path."
        venv_dir="$HOME/.lingtai-tui/runtime/venv-repair-$$-1"
        repair_attempt=1
        continue
      fi
      echo "error: failed to install the pinned kernel bundle artifact into the runtime venv after recreate." >&2
      echo "       bundle: $(bundle_manifest_field bundle_id 2>/dev/null || echo "?") kernel: $(bundle_manifest_field kernel_tag 2>/dev/null || echo "?") via $KERNEL_MANIFEST_PROVIDER" >&2
      echo "       LingTai's Python runtime is never installed from PyPI/an index by package name," >&2
      echo "       so this is a hard stop rather than a silent fallback. Re-run the installer, or" >&2
      echo "       pass --skip-python to install the TUI/portal binaries only." >&2
      return 1
    fi

    if ! "$py" -c 'import lingtai; print("lingtai", getattr(lingtai, "__version__", "?"))'; then
      if [[ "$repair_attempt" == "0" ]]; then
        warn "runtime venv failed import check; retaining it and provisioning a new runtime venv path."
        venv_dir="$HOME/.lingtai-tui/runtime/venv-repair-$$-1"
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


# --- kernel bundle artifact install (schema lingtai.kernel.release/v1) ------
#
# Installs the Python `lingtai` runtime from the release-pinned kernel
# artifact named in the TUI bundle manifest, by explicit local file path —
# never `pip install lingtai` against any package index. The configured
# package index (explicit LINGTAI_PYPI_INDEX_URL, otherwise provider default)
# is used ONLY to resolve lingtai's own third-party dependencies during that local-path
# install; lingtai itself is never requested from an index.

# kernel_manifest_url_for_provider echoes the kernel release manifest asset
# URL on the given provider/tag, or nothing if unavailable.
kernel_manifest_url_for_provider() {
  local provider="$1" tag="$2" body
  case "$provider" in
    github)
      command -v curl &>/dev/null || return 1
      body="$(curl -fsSL --max-time 15 "${KERNEL_GH_API_BASE}/releases/tags/$tag" 2>/dev/null || true)"
      [[ -n "$body" ]] || return 1
      if printf '%s' "$body" | grep -q '"name"[[:space:]]*:[[:space:]]*"lingtai-kernel-release-manifest.json"'; then
        printf 'https://github.com/Lingtai-AI/lingtai-kernel/releases/download/%s/lingtai-kernel-release-manifest.json' "$tag"
      else
        return 1
      fi
      ;;
    gitee)
      local saved_api="$GITEE_API_BASE"
      GITEE_API_BASE="$GITEE_KERNEL_API_BASE"
      local url
      url="$(gitee_release_asset_url "$tag" "lingtai-kernel-release-manifest.json" || true)"
      GITEE_API_BASE="$saved_api"
      [[ -n "$url" ]] || return 1
      printf '%s' "$url"
      ;;
    *) return 1 ;;
  esac
}

# fetch_kernel_manifest resolves the pinned kernel tag/manifest for the
# CURRENT BUNDLE_PROVIDER + the bundle's kernel_tag. Falls back to the other
# provider for the SAME kernel tag only (same-bundle-fallback contract).
# Populates KERNEL_MANIFEST_JSON and KERNEL_MANIFEST_PROVIDER in this shell;
# returns nonzero if unavailable on either provider.
fetch_kernel_manifest() {
  local kernel_tag="$1" provider="$BUNDLE_PROVIDER" url body other
  KERNEL_MANIFEST_PROVIDER=""
  KERNEL_MANIFEST_JSON=""

  url="$(kernel_manifest_url_for_provider "$provider" "$kernel_tag" || true)"
  if [[ -z "$url" ]]; then
    other="github"
    [[ "$provider" == "github" ]] && other="gitee"
    # Keep fallback diagnostics on stderr; stdout remains reserved for normal
    # installer output while the manifest is returned through explicit state.
    echo "    $provider has no kernel manifest for $kernel_tag; trying $other for the SAME kernel tag." >&2
    url="$(kernel_manifest_url_for_provider "$other" "$kernel_tag" || true)"
    [[ -n "$url" ]] || return 1
    provider="$other"
  fi

  body="$(curl -fsSL --max-time 30 "$url" 2>/dev/null || true)"
  [[ -n "$body" ]] || return 1
  if ! printf '%s' "$body" | grep -q '"schema"[[:space:]]*:[[:space:]]*"lingtai.kernel.release/v1"'; then
    echo "error: kernel manifest at $url has an unexpected schema" >&2
    return 1
  fi

  KERNEL_MANIFEST_PROVIDER="$provider"
  KERNEL_MANIFEST_JSON="$body"
}

# python_platform_tags asks the venv's own Python for compatible wheel tags,
# one per line, most-specific first. Fresh `uv venv` environments intentionally
# contain neither packaging nor pip, so use their implementations when present
# and otherwise emit a conservative dependency-free CPython/OS/arch set for the
# platform wheels this release pipeline publishes. The installer still lets uv
# enforce final wheel compatibility during installation.
python_platform_tags() {
  local py="$1"
  "$py" - <<'PY' 2>/dev/null
import platform
import sys

sys_tags = None
try:
    from packaging.tags import sys_tags
except ModuleNotFoundError:
    try:
        from pip._vendor.packaging.tags import sys_tags  # type: ignore
    except ModuleNotFoundError:
        pass

if sys_tags is not None:
    for tag in sys_tags():
        print(f"{tag.interpreter}-{tag.abi}-{tag.platform}")
    raise SystemExit(0)

interpreter = f"cp{sys.version_info.major}{sys.version_info.minor}"
abi = interpreter
machine = platform.machine().lower()

def emit(platform_tag):
    print(f"{interpreter}-{abi}-{platform_tag}")

if sys.platform == "darwin":
    arch = "arm64" if machine in {"arm64", "aarch64"} else "x86_64"
    version = platform.mac_ver()[0]
    try:
        major, minor = (int(part) for part in version.split(".")[:2])
    except (TypeError, ValueError):
        major, minor = (11, 0) if arch == "arm64" else (10, 13)
    if major >= 11:
        for compatible_major in range(major, 10, -1):
            emit(f"macosx_{compatible_major}_0_{arch}")
        minor = 16
    if arch == "x86_64" and major >= 10:
        for compatible_minor in range(min(minor, 16), 8, -1):
            emit(f"macosx_10_{compatible_minor}_x86_64")
elif sys.platform.startswith("linux"):
    arch = "aarch64" if machine in {"arm64", "aarch64"} else "x86_64"
    libc_name, libc_version = platform.libc_ver()
    try:
        libc_major, libc_minor = (int(part) for part in libc_version.split(".")[:2])
    except (TypeError, ValueError):
        libc_major, libc_minor = 0, 0
    if libc_name == "glibc" and libc_major == 2 and libc_minor >= 17:
        for compatible_minor in range(libc_minor, 16, -1):
            tag = f"manylinux_2_{compatible_minor}_{arch}"
            if compatible_minor == 17:
                tag += f".manylinux2014_{arch}"
            emit(tag)
elif sys.platform == "win32":
    emit("win_amd64" if machine in {"amd64", "x86_64"} else "win_arm64")
PY
}

# select_kernel_wheel picks the first artifact from a kernel manifest JSON
# body whose "<python_tag>-<abi_tag>-<platform_tag>" combination appears in
# the venv's compatible-tag list (most-specific tags are tried first, so an
# exact match wins over a compatible-but-looser one). Echoes
# "<filename> <sha256>" on a match; returns nonzero (and prints nothing) if no
# wheel matches — the caller falls back to the sdist.
select_kernel_wheel() {
  local manifest_json="$1" py="$2" tags combo manifest_file
  tags="$(python_platform_tags "$py")"
  [[ -n "$tags" ]] || return 1

  manifest_file="$(mktemp "${TMPDIR:-/tmp}/lingtai-kernel-manifest.XXXXXX")"
  printf '%s' "$manifest_json" > "$manifest_file"

  while IFS= read -r combo; do
    [[ -n "$combo" ]] || continue
    # Each artifact object is small and single-line-safe to grep for its tag
    # triple; scope the match to one object at a time via a python one-liner
    # for correctness instead of hand-rolled brace matching across wheels.
    # Manifest is passed by FILE PATH (not stdin) so this command can't
    # collide with a heredoc's stdin takeover.
    local hit
    hit="$(python3 - "$manifest_file" "$combo" <<'PY'
import json, sys
data = json.loads(open(sys.argv[1]).read())
combo = sys.argv[2]
for art in data.get("artifacts", []):
    if art.get("kind") != "wheel":
        continue
    if f"{art['python_tag']}-{art['abi_tag']}-{art['platform_tag']}" == combo:
        print(f"{art['filename']} {art['sha256']}")
        break
PY
)"
    if [[ -n "$hit" ]]; then
      printf '%s' "$hit"
      return 0
    fi
  done <<<"$tags"
  return 1
}

# kernel_sdist_fallback echoes "<filename> <sha256>" for the manifest's
# declared sdist_fallback artifact.
kernel_sdist_fallback() {
  local manifest_json="$1" manifest_file
  manifest_file="$(mktemp "${TMPDIR:-/tmp}/lingtai-kernel-manifest.XXXXXX")"
  printf '%s' "$manifest_json" > "$manifest_file"
  python3 - "$manifest_file" <<'PY'
import json, sys
data = json.loads(open(sys.argv[1]).read())
name = data.get("sdist_fallback", "")
for art in data.get("artifacts", []):
    if art.get("filename") == name:
        print(f"{art['filename']} {art['sha256']}")
        break
PY
}

# kernel_artifact_download_url echoes the download URL for a named kernel
# artifact on the given provider/tag.
kernel_artifact_download_url() {
  local provider="$1" tag="$2" name="$3"
  case "$provider" in
    github) printf 'https://github.com/Lingtai-AI/lingtai-kernel/releases/download/%s/%s' "$tag" "$name" ;;
    gitee)
      local saved_api="$GITEE_API_BASE" url
      GITEE_API_BASE="$GITEE_KERNEL_API_BASE"
      url="$(gitee_release_asset_url "$tag" "$name" || true)"
      GITEE_API_BASE="$saved_api"
      [[ -n "$url" ]] || return 1
      printf '%s' "$url"
      ;;
    *) return 1 ;;
  esac
}

# install_kernel_from_bundle installs the Python `lingtai` runtime from the
# pinned bundle's kernel release, by explicit local file path — this is the
# ONLY way this script installs LingTai; it is never requested from a
# package index by name. Sets
# KERNEL_SOURCE/KERNEL_BUNDLE_ID/KERNEL_VERSION_INSTALLED/KERNEL_PROVIDER on
# success. Returns nonzero (installs nothing, KERNEL_SOURCE left untouched)
# on any failure (missing/incoherent kernel manifest, no compatible
# wheel/sdist, checksum mismatch, install command failure) — the caller
# (ensure_runtime_venv) treats that as a fail-loud install error, not a
# signal to try any other source.
install_kernel_from_bundle() {
  local py="$1" uv="$2"
  [[ -n "$BUNDLE_MANIFEST_JSON" ]] || return 1

  local kernel_tag kernel_manifest artifact_line fname sha download_url dest index_url
  kernel_tag="$(bundle_manifest_field kernel_tag)"
  [[ -n "$kernel_tag" ]] || return 1

  if ! fetch_kernel_manifest "$kernel_tag"; then
    note "Could not fetch the pinned kernel release manifest ($kernel_tag) from GitHub or Gitee."
    return 1
  fi
  kernel_manifest="$KERNEL_MANIFEST_JSON"
  [[ -n "$kernel_manifest" && -n "$KERNEL_MANIFEST_PROVIDER" ]] || {
    note "Kernel manifest resolution returned incomplete provider state."
    return 1
  }

  artifact_line="$(select_kernel_wheel "$kernel_manifest" "$py" || true)"
  if [[ -z "$artifact_line" ]]; then
    note "No platform wheel in kernel release $kernel_tag matches this Python; using the sdist fallback (extra build toolchain may be required)."
    artifact_line="$(kernel_sdist_fallback "$kernel_manifest" || true)"
  fi
  [[ -n "$artifact_line" ]] || return 1
  fname="${artifact_line%% *}"
  sha="${artifact_line##* }"

  download_url="$(kernel_artifact_download_url "$KERNEL_MANIFEST_PROVIDER" "$kernel_tag" "$fname" || true)"
  [[ -n "$download_url" ]] || return 1

  mkdir -p "$BUILD_DIR/kernel-artifact"
  dest="$BUILD_DIR/kernel-artifact/$fname"
  say "Downloading kernel artifact: $fname (from $KERNEL_MANIFEST_PROVIDER, release $kernel_tag) ..."
  if ! curl -fsSL --max-time 300 -o "$dest" "$download_url"; then
    warn "download failed for $download_url"
    return 1
  fi
  if ! verify_sha256 "$dest" "$sha"; then
    echo "error: checksum mismatch for $fname — refusing to install an unverified kernel artifact." >&2
    echo "       retained mismatched artifact for diagnosis: $dest" >&2
    return 1
  fi
  note "Verified SHA256 for $fname."

  index_url="$(python_dependency_index_url)"
  say "Installing lingtai from local artifact (dependencies resolved via $index_url) ..."
  # Explicit local path: pip/uv never requests the package name "lingtai"
  # from any index here — only third-party dependency resolution uses
  # --index-url. This is the "no pip install lingtai from index" guarantee.
  if [[ -n "$uv" ]]; then
    "$uv" pip install --index-url "$index_url" -p "$(dirname "$(dirname "$py")")" "$dest" || return 1
  else
    "$py" -m pip install --index-url "$index_url" "$dest" || return 1
  fi

  if ! "$py" -c 'import lingtai; print("lingtai", getattr(lingtai, "__version__", "?"))'; then
    warn "lingtai import failed after bundle install."
    return 1
  fi

  KERNEL_SOURCE="bundle"
  KERNEL_BUNDLE_ID="$(bundle_manifest_field bundle_id)"
  KERNEL_VERSION_INSTALLED="$(printf '%s' "$kernel_manifest" | json_string_field kernel_version)"
  KERNEL_PROVIDER="$KERNEL_MANIFEST_PROVIDER"
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
  local tag="$1" os arch name url tarball extract_dir provider
  os="$(detect_os)"
  arch="$(detect_arch)"
  if [[ "$os" == "unsupported" || "$arch" == "unsupported" ]]; then
    note "No prebuilt asset for $(uname -s)/$(uname -m); will build from source."
    return 1
  fi
  command -v curl &>/dev/null || { note "curl unavailable; will build from source."; return 1; }

  name="$(asset_name "$tag" "$os" "$arch")"
  if [[ -z "$BUNDLE_MANIFEST_JSON" ]] || [[ "$BUNDLE_TAG" != "$tag" ]]; then
    warn "no validated bundle manifest is bound to TUI tag $tag; refusing the release asset."
    return 1
  fi
  if ! load_bundle_manifest "$BUNDLE_MANIFEST_JSON" "$tag"; then
    warn "validated bundle manifest could not be loaded for $name; refusing the release asset."
    return 1
  fi
  if [[ ! "$BUNDLE_TUI_ARCHIVE_SHA" =~ ^[0-9a-f]{64}$ ]]; then
    warn "validated bundle manifest has no usable digest for $name; refusing the release asset."
    return 1
  fi
  provider="${BUNDLE_PROVIDER:-github}"
  if [[ "$provider" == "gitee" ]]; then
    url="$(gitee_release_asset_url "$tag" "$name" || true)"
    if [[ -z "$url" ]]; then
      note "Gitee has no prebuilt asset ($name) for $tag; trying GitHub for the SAME tag."
      url="$(release_asset_url "$tag" "$name" || true)"
      provider="github"
    fi
  else
    url="$(release_asset_url "$tag" "$name" || true)"
  fi
  if [[ -z "$url" ]]; then
    note "Release $tag has no prebuilt asset ($name) on GitHub or Gitee; will build from source."
    return 1
  fi

  say "Downloading prebuilt binaries: $name (from $provider)"
  mkdir -p "$BUILD_DIR"
  tarball="$BUILD_DIR/$name"
  extract_dir="$BUILD_DIR/asset"
  mkdir -p "$extract_dir"
  if ! curl -fsSL --max-time 120 -o "$tarball" "$url"; then
    warn "download failed for $url; will build from source."
    return 1
  fi

  # Checksum verification: the sidecar .sha256 is fetched from the SAME
  # provider/URL as the tarball itself so a fallback never mixes providers
  # mid-artifact. A missing/unfetchable sidecar is a hard stop for this
  # asset (not silently trusted) — the caller falls back to a source build.
  local sha_url sha_expected
  sha_url="${url}.sha256"
  sha_expected="$(curl -fsSL --max-time 30 "$sha_url" 2>/dev/null | cut -d' ' -f1 || true)"
  if [[ ! "$sha_expected" =~ ^[0-9a-f]{64}$ ]]; then
    warn "could not fetch checksum sidecar for $name; will build from source rather than install unverified bytes."
    return 1
  fi
  if [[ "$sha_expected" != "$BUNDLE_TUI_ARCHIVE_SHA" ]]; then
    warn "provider checksum sidecar disagrees with bundle manifest for $name; refusing mixed provenance."
    return 2
  fi
  if ! verify_sha256 "$tarball" "$BUNDLE_TUI_ARCHIVE_SHA"; then
    echo "error: downloaded bytes for $name disagree with the bundle manifest; refusing this tag." >&2
    return 2
  fi
  note "Verified SHA256 for $name."

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
resolve_source_provider
if [[ "$BUNDLE_PROVIDER" == "gitee" ]]; then
  say "Source: Gitee (${GITEE_OWNER}/${GITEE_REPO}) — override with --source github or LINGTAI_SOURCE=github."
fi

# Resolve one bundle (TUI tag + bundle manifest, which pins an exact kernel
# release) up front, on BUNDLE_PROVIDER, once. Every subsequent step —
# try_release_asset, build_from_source's tag-based source-tarball path, and
# the kernel artifact install in ensure_runtime_venv — reuses this same
# BUNDLE_TAG/BUNDLE_MANIFEST_JSON.
#
# This is the default release-asset one-command path (no --ref, not
# --update): a pinned kernel bundle is REQUIRED here. LingTai must never be
# installed from a package index by name, so if no bundle manifest can be
# resolved, ensure_runtime_venv below fails loud instead of silently
# installing from PyPI — see BUNDLE_REQUIRED.
if [[ -z "$REF" ]]; then
  BUNDLE_REQUIRED=1
  if fetch_bundle_manifest; then
    note "Resolved bundle $BUNDLE_TAG via $BUNDLE_PROVIDER (kernel $(bundle_manifest_field kernel_tag))."
  else
    warn "No bundle manifest available for $([[ -n "$VERSION" ]] && echo "$VERSION" || echo "the latest release") on GitHub or Gitee."
  fi
fi

# Decide what to install.
#   --update      : source-compatible in-place update for the given tag
#   --ref         : explicit source build of that ref
#   --version tag : that release (asset, else source tarball)
#   default       : latest release (asset, else source tarball)
if [[ "$UPDATE_MODE" == "1" ]]; then
  # The TUI source updater expects a source-compatible update. Try the prebuilt
  # asset first (fast), then fall back to building the tag from source.
  TARGET_TAG="${VERSION:-$BUNDLE_TAG}"
  [[ -n "$TARGET_TAG" ]] || { echo "error: --update could not resolve a release tag" >&2; exit 1; }
  VERSION="$TARGET_TAG"
  if [[ "$FROM_SOURCE" != "1" ]]; then
    if try_release_asset "$TARGET_TAG"; then
      :
    else
      asset_rc=$?
      [[ "$asset_rc" != "2" ]] || exit 1
      build_from_source "$TARGET_TAG"
    fi
  else
    build_from_source "$TARGET_TAG"
  fi
elif [[ -n "$REF" ]]; then
  build_from_source "$REF"
else
  TARGET_TAG="$VERSION"
  if [[ -z "$TARGET_TAG" ]]; then
    if [[ -n "$BUNDLE_TAG" ]]; then
      # Reuse the tag already resolved by fetch_bundle_manifest above instead
      # of re-querying "latest" a second time (which could race to a newer
      # tag between the two calls and silently combine a bundle from one
      # release with TUI binaries from another).
      TARGET_TAG="$BUNDLE_TAG"
      say "Latest release is $TARGET_TAG"
    else
      say "Resolving latest release ..."
      if [[ "$BUNDLE_PROVIDER" == "gitee" ]]; then
        TARGET_TAG="$(gitee_latest_release_tag || true)"
      fi
      if [[ -z "$TARGET_TAG" ]]; then
        TARGET_TAG="$(latest_release_tag || true)"
      fi
      if [[ -z "$TARGET_TAG" ]]; then
        echo "error: could not determine the latest release tag from GitHub or Gitee." >&2
        echo "       Pass one explicitly: ./install.sh --version vX.Y.Z" >&2
        exit 1
      fi
      say "Latest release is $TARGET_TAG"
    fi
  fi
  if [[ -z "$(release_tag_name "$TARGET_TAG")" ]]; then
    warn "'$TARGET_TAG' is not a vX.Y.Z release tag; treating it as a source ref."
    build_from_source "$TARGET_TAG"
  elif [[ "$FROM_SOURCE" != "1" ]]; then
    if try_release_asset "$TARGET_TAG"; then
      :
    else
      asset_rc=$?
      [[ "$asset_rc" != "2" ]] || exit 1
      build_from_source "$TARGET_TAG"
    fi
  else
    build_from_source "$TARGET_TAG"
  fi
fi

# Provision the pinned runtime before recording install metadata. This makes
# kernel_source and its bundle fields a postcondition of verified provisioning,
# never a claim about a partially completed install.
if ! ensure_runtime_venv "$BIN_DIR"; then
  echo "error: LingTai install incomplete — the TUI/portal binaries installed, but the" >&2
  echo "       Python runtime could not be provisioned from a verified pinned bundle." >&2
  echo "       See the error above. Re-run, or pass --skip-python if TUI-only is intended." >&2
  exit 1
fi

# Record install metadata for the TUI source updater only after the runtime
# gate above succeeds (or --skip-python explicitly opted out).
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

say "Done. $("$BIN_DIR/lingtai-tui" version 2>&1 || echo "$VERSION")"

# Tell the user how to put BIN_DIR on PATH if it isn't already.
print_path_hint "$BIN_DIR"
}

if [[ "${LINGTAI_INSTALL_SH_SOURCE_ONLY:-0}" != "1" ]]; then
  main "$@"
fi
