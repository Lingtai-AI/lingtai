#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

export LINGTAI_INSTALL_SH_SOURCE_ONLY=1
# shellcheck source=../install.sh
source "$ROOT_DIR/install.sh"
unset LINGTAI_INSTALL_SH_SOURCE_ONLY

fail() {
  echo "test-install-sh: $*" >&2
  exit 1
}

assert_eq() {
  local want="$1" got="$2" label="$3"
  if [[ "$got" != "$want" ]]; then
    fail "$label: got '$got', want '$want'"
  fi
}

command -v git >/dev/null || fail "git is required"
command -v python3 >/dev/null || fail "python3 is required"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-inst-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

repo="$tmp/repo"
git init -q "$repo"
git -C "$repo" config user.email "test@example.invalid"
git -C "$repo" config user.name "Install Test"
printf 'first\n' > "$repo/file.txt"
git -C "$repo" add file.txt
git -C "$repo" commit -qm "initial"
tagged_commit="$(git -C "$repo" rev-parse HEAD)"
git -C "$repo" tag v1.2.3

assert_eq "v1.2.3" "$(release_tag_name "v1.2.3")" "plain release tag"
assert_eq "v1.2.3" "$(release_tag_name "refs/tags/v1.2.3")" "full release tag ref"
assert_eq "" "$(release_tag_name "v1.2")" "partial release tag rejected"
assert_eq "1.26.0" "$(normalize_go_version 1.26)" "go language version normalization"
assert_eq "1.26.1" "$(normalize_go_version go1.26.1)" "go toolchain version normalization"
go_version_ge 1.26.1 1.26.1 || fail "same Go version should satisfy requirement"
go_version_ge 1.27.0 1.26.9 || fail "newer Go minor should satisfy older requirement"
if go_version_ge 1.18.1 1.26.1; then
  fail "old Go version should not satisfy newer go.mod requirement"
fi
source_tree="$tmp/source-tree"
mkdir -p "$source_tree/tui"
printf 'module example.test/lingtai\n\ngo 1.26.1\n' > "$source_tree/tui/go.mod"
assert_eq "1.26.1" "$(required_go_version_for_source "$source_tree")" "required Go version parsed from tui/go.mod"
assert_eq "go1.26.1.linux-amd64.tar.gz" "$(go_toolchain_archive_name 1.26.1 linux amd64)" "Go toolchain archive name"
old_go_dl_base="$GO_DL_BASE"
GO_DL_BASE="https://example.test/go"
assert_eq "https://example.test/go/go1.26.1.linux-amd64.tar.gz" "$(go_toolchain_download_url 1.26.1 linux amd64)" "Go toolchain download URL"
GO_DL_BASE="$old_go_dl_base"
assert_eq "20.18.0" "$(normalize_node_version v20.18.0)" "node version normalization"
if portal_node_supported 20.18.0; then
  fail "Node 20.18 should not satisfy portal requirement"
fi
portal_node_supported 20.19.0 || fail "Node 20.19 should satisfy portal requirement"
if portal_node_supported 22.11.0; then
  fail "Node 22.11 should not satisfy portal requirement"
fi
portal_node_supported 22.12.0 || fail "Node 22.12 should satisfy portal requirement"
portal_node_supported 23.0.0 || fail "Node 23 should satisfy portal requirement"
assert_eq "node-v22.12.0-linux-x64.tar.gz" "$(node_toolchain_archive_name 22.12.0 linux x64)" "Node toolchain archive name"
old_node_dl_base="$NODE_DL_BASE"
NODE_DL_BASE="https://example.test/node"
assert_eq "https://example.test/node/v22.12.0/node-v22.12.0-linux-x64.tar.gz" "$(node_toolchain_download_url 22.12.0 linux x64)" "Node toolchain download URL"
NODE_DL_BASE="$old_node_dl_base"
assert_eq "v1.2.3" "$(version_for_checkout "$repo" "v1.2.3")" "exact tag version"
assert_eq "v1.2.3" "$(version_for_checkout "$repo" "refs/tags/v1.2.3")" "exact full tag ref version"
assert_eq 'quote\"slash\\' "$(json_escape 'quote"slash\')" "json quote/backslash escaping"
assert_eq '\n' "$(json_escape $'\n')" "json newline escaping"
assert_eq '\r' "$(json_escape $'\r')" "json carriage return escaping"
assert_eq '\t' "$(json_escape $'\t')" "json tab escaping"
assert_eq '\b' "$(json_escape $'\b')" "json backspace escaping"
assert_eq '\f' "$(json_escape $'\f')" "json form-feed escaping"
assert_eq '\u0001' "$(json_escape $'\001')" "json generic control-byte escaping"
assert_eq "$tmp/prefix/bin" "$(bin_dir_for_prefix "$tmp/prefix")" "bin dir from prefix"
assert_eq "$tmp/prefix/bin" "$(bin_dir_for_prefix "$tmp/prefix/")" "bin dir from slash-suffixed prefix"

# --- PATH guidance is shell-aware and does not mutate the host -----------------
(
  hint_home="$tmp/path-hint-zsh-home"
  hint_bin="$tmp/path-hint-zsh-bin"
  mkdir -p "$hint_home"
  export HOME="$hint_home"
  export SHELL="/bin/zsh"
  export PATH="/usr/bin:/bin"

  out="$(print_path_hint "$hint_bin")"
  case "$out" in
    *"Note: $hint_bin is not on your PATH."*) ;;
    *) fail "zsh PATH hint should explain that the bin dir is absent: $out" ;;
  esac
  case "$out" in
    *"$hint_home/.zshrc"*) ;;
    *) fail "zsh PATH hint should target ~/.zshrc: $out" ;;
  esac
  [[ ! -e "$hint_home/.zshrc" ]] || fail "PATH hint must not write the zsh rc file"
  assert_eq "/usr/bin:/bin" "$PATH" "zsh PATH hint leaves current PATH unchanged"
)

(
  hint_home="$tmp/path-hint-bash-home"
  hint_bin="$tmp/path-hint-bash-bin"
  mkdir -p "$hint_home"
  export HOME="$hint_home"
  export SHELL="/bin/bash"
  export PATH="/usr/bin:/bin"

  out="$(print_path_hint "$hint_bin")"
  case "$out" in
    *"$hint_home/.bashrc"*) ;;
    *) fail "bash PATH hint should target ~/.bashrc: $out" ;;
  esac
  [[ ! -e "$hint_home/.bashrc" ]] || fail "PATH hint must not write the bash rc file"
)

(
  hint_home="$tmp/path-hint-present-home"
  hint_bin="$tmp/path-hint-present-bin"
  mkdir -p "$hint_home"
  export HOME="$hint_home"
  export SHELL="/bin/zsh"
  export PATH="$hint_bin:/usr/bin:/bin"

  out="$(print_path_hint "$hint_bin")"
  assert_eq "" "$out" "PATH hint is silent when bin dir is already present"
  [[ ! -e "$hint_home/.zshrc" ]] || fail "present PATH check must not write the zsh rc file"
)

(
  hint_home="$tmp/path-hint-unknown-home"
  hint_bin="$tmp/path-hint-unknown-bin"
  mkdir -p "$hint_home"
  export HOME="$hint_home"
  export SHELL="/bin/fish"
  export PATH="/usr/bin:/bin"

  out="$(print_path_hint "$hint_bin")"
  case "$out" in
    *"shell startup file"*) ;;
    *) fail "unknown shell PATH hint should name the shell startup file: $out" ;;
  esac
  case "$out" in
    *"export PATH="*) ;;
    *) fail "unknown shell PATH hint should provide a direct export: $out" ;;
  esac
  [[ ! -e "$hint_home/.profile" ]] || fail "unknown-shell PATH hint must not write .profile"
)

# --- uv bootstrap: no uv, system python3 too old (jammy scenario) -------------
# Simulates Ubuntu jammy: python3 exists but reports 3.10 and there is no uv on
# PATH. ensure_uv must download the official installer (via a fake curl) and run
# it (a fake installer that drops a uv binary into UV_INSTALL_DIR), and
# ensure_python must then succeed by resolving the bootstrapped uv.
(
  fakebin="$tmp/uv-fakebin"
  fakehome="$tmp/uv-home"
  mkdir -p "$fakebin" "$fakehome/.local/bin"

  # Fake python3 reporting 3.10: sys.version_info check in python_ok fails.
  cat > "$fakebin/python3" <<'SH'
#!/usr/bin/env bash
case "$*" in
  *"version_info >= (3, 11)"*) exit 1 ;;  # too old
  *"import venv"*)             exit 0 ;;
esac
exit 0
SH
  chmod +x "$fakebin/python3"

  # Fake curl: the -o argument names the file the uv installer is written to.
  # Emit an installer that copies a real uv stub into UV_INSTALL_DIR.
  cat > "$fakebin/curl" <<'SH'
#!/usr/bin/env bash
out=""
prev=""
for a in "$@"; do
  [[ "$prev" == "-o" ]] && out="$a"
  prev="$a"
done
[[ -n "$out" ]] || { echo "fake curl: no -o target" >&2; exit 1; }
cat > "$out" <<'INSTALLER'
#!/usr/bin/env sh
dir="${UV_INSTALL_DIR:?UV_INSTALL_DIR must be set}"
mkdir -p "$dir"
cat > "$dir/uv" <<'UVSTUB'
#!/usr/bin/env bash
echo "uv 0.0.0-fake"
UVSTUB
chmod +x "$dir/uv"
INSTALLER
exit 0
SH
  chmod +x "$fakebin/curl"

  # Isolate PATH so no real uv (typically under ~/.local/bin) is reachable; only
  # the fake python3/curl plus system coreutils are visible.
  export PATH="$fakebin:/usr/bin:/bin"
  export HOME="$fakehome"
  export UV_INSTALL_DIR="$fakehome/.local/bin"
  BUILD_DIR="$tmp/uv-build"
  UV_INSTALLER_URL="https://example.invalid/uv/install.sh"

  # Preconditions: python_ok fails (too old), find_uv finds nothing yet.
  if python_ok; then fail "fake python3 3.10 should make python_ok fail"; fi
  if find_uv >/dev/null 2>&1; then fail "no uv should exist before bootstrap"; fi

  ensure_uv >/dev/null || fail "ensure_uv should bootstrap uv via fake curl/installer"
  bootstrapped="$(find_uv)" || fail "find_uv should locate bootstrapped uv"
  assert_eq "$UV_INSTALL_DIR/uv" "$bootstrapped" "find_uv resolves bootstrapped uv path"
  [[ -x "$bootstrapped" ]] || fail "bootstrapped uv should be executable"

  ensure_python || fail "ensure_python should succeed once uv is bootstrapped"
)

# --- ensure_python drives the bootstrap itself from a clean state ------------
# Same jammy scenario, but exercise ensure_python end-to-end (no prior ensure_uv
# call): it must bootstrap uv and succeed without a usable system Python.
(
  fakebin="$tmp/uv-fakebin2"
  fakehome="$tmp/uv-home2"
  mkdir -p "$fakebin" "$fakehome/.local/bin"
  cat > "$fakebin/python3" <<'SH'
#!/usr/bin/env bash
case "$*" in
  *"version_info >= (3, 11)"*) exit 1 ;;
  *"import venv"*)             exit 0 ;;
esac
exit 0
SH
  chmod +x "$fakebin/python3"
  cat > "$fakebin/curl" <<'SH'
#!/usr/bin/env bash
out=""; prev=""
for a in "$@"; do [[ "$prev" == "-o" ]] && out="$a"; prev="$a"; done
[[ -n "$out" ]] || { echo "fake curl: no -o target" >&2; exit 1; }
cat > "$out" <<'INSTALLER'
#!/usr/bin/env sh
dir="${UV_INSTALL_DIR:?}"; mkdir -p "$dir"
printf '#!/usr/bin/env bash\necho uv-fake\n' > "$dir/uv"; chmod +x "$dir/uv"
INSTALLER
exit 0
SH
  chmod +x "$fakebin/curl"
  export PATH="$fakebin:/usr/bin:/bin"
  export HOME="$fakehome"
  export UV_INSTALL_DIR="$fakehome/.local/bin"
  BUILD_DIR="$tmp/uv-build2"
  UV_INSTALLER_URL="https://example.invalid/uv/install.sh"

  ensure_python || fail "ensure_python should bootstrap uv end-to-end on jammy-like systems"
  find_uv >/dev/null 2>&1 || fail "ensure_python should leave a resolvable uv behind"
)

# --- ensure_python bootstraps uv when ensurepip is unavailable ---------------
# Some distro Pythons are new enough and expose the venv module, but lack
# ensurepip/python3-venv. Treating those as adequate makes the bad-venv repair
# path fall back to `python3 -m venv`, which fails with "ensurepip is not
# available". In that case ensure_python must bootstrap uv instead.
(
  fakebin="$tmp/uv-fakebin-no-ensurepip"
  fakehome="$tmp/uv-home-no-ensurepip"
  mkdir -p "$fakebin" "$fakehome/.local/bin"
  cat > "$fakebin/python3" <<'SH'
#!/usr/bin/env bash
case "$*" in
  *"version_info >= (3, 11)"*) exit 0 ;;  # version is adequate
  *"import venv, ensurepip"*)   exit 1 ;;  # but ensurepip is missing
  *"import venv"*)              exit 0 ;;
esac
exit 0
SH
  chmod +x "$fakebin/python3"
  cat > "$fakebin/curl" <<'SH'
#!/usr/bin/env bash
out=""; prev=""
for a in "$@"; do [[ "$prev" == "-o" ]] && out="$a"; prev="$a"; done
[[ -n "$out" ]] || { echo "fake curl: no -o target" >&2; exit 1; }
cat > "$out" <<'INSTALLER'
#!/usr/bin/env sh
dir="${UV_INSTALL_DIR:?}"; mkdir -p "$dir"
printf '#!/usr/bin/env bash\necho uv-fake\n' > "$dir/uv"; chmod +x "$dir/uv"
INSTALLER
exit 0
SH
  chmod +x "$fakebin/curl"
  export PATH="$fakebin:/usr/bin:/bin"
  export HOME="$fakehome"
  export UV_INSTALL_DIR="$fakehome/.local/bin"
  BUILD_DIR="$tmp/uv-build-no-ensurepip"
  UV_INSTALLER_URL="https://example.invalid/uv/install.sh"

  if python_ok; then fail "fake python without ensurepip should not be accepted"; fi
  ensure_python || fail "ensure_python should bootstrap uv when ensurepip is unavailable"
  find_uv >/dev/null 2>&1 || fail "ensure_python should leave uv available after ensurepip-missing bootstrap"
)

# --- find_uv honors custom UV_INSTALL_DIR -----------------------------------
# ensure_uv can install into UV_INSTALL_DIR, so subsequent find_uv calls must
# probe that directory even when it is not on PATH and not ~/.local/bin.
(
  fakehome="$tmp/uv-home-custom-dir"
  custom_uv_dir="$tmp/uv-custom-dir"
  mkdir -p "$fakehome" "$custom_uv_dir"
  cat > "$custom_uv_dir/uv" <<'SH'
#!/usr/bin/env bash
echo uv-custom
SH
  chmod +x "$custom_uv_dir/uv"
  export PATH="/usr/bin:/bin"
  export HOME="$fakehome"
  export UV_INSTALL_DIR="$custom_uv_dir"

  assert_eq "$custom_uv_dir/uv" "$(find_uv)" "find_uv should probe UV_INSTALL_DIR"
)

# --- runtime venv does not fall back to Python without ensurepip -------------
# If uv is present but cannot create the venv, do not fall back to a system
# Python that python_ok rejects (e.g. WSL/Ubuntu Python with venv but no
# ensurepip). That fallback produces confusing ensurepip errors or stale venvs.
(
  fakebin="$tmp/runtime-no-ensurepip-bin"
  fakehome="$tmp/runtime-no-ensurepip-home"
  fake_uv_dir="$tmp/runtime-no-ensurepip-uv"
  mkdir -p "$fakebin" "$fakehome" "$fake_uv_dir"
  cat > "$fake_uv_dir/uv" <<'SH'
#!/usr/bin/env bash
if [[ "$1" == "venv" ]]; then
  echo "fake uv venv failed" >&2
  exit 42
fi
if [[ "$1" == "pip" ]]; then exit 43; fi
echo uv-fake
SH
  chmod +x "$fake_uv_dir/uv"
  cat > "$fakebin/python3" <<SH
#!/usr/bin/env bash
case "\$*" in
  *"version_info >= (3, 11)"*) exit 0 ;;
  *"import venv, ensurepip"*)   exit 1 ;;
  *"-m venv"*)                  touch "$fakehome/fallback-invoked"; exit 1 ;;
esac
exit 0
SH
  chmod +x "$fakebin/python3"
  export PATH="$fakebin:/usr/bin:/bin"
  export HOME="$fakehome"
  export UV_INSTALL_DIR="$fake_uv_dir"
  export SKIP_VENV=0
  # Exercise the venv-creation failure path rather than the new fail-loud
  # missing-bundle guard; the manifest body is not parsed until after venv setup.
  export BUNDLE_MANIFEST_JSON='{}'
  export BUNDLE_REQUIRED=1

  out="$(ensure_runtime_venv "$fakehome/bin" 2>&1)"
  case "$out" in
    *"no Python 3.11+ with venv/ensurepip is available"*) ;;
    *) fail "runtime venv should warn when uv fails and system Python lacks ensurepip; output: $out" ;;
  esac
  [[ ! -e "$fakehome/fallback-invoked" ]] || fail "ensure_runtime_venv fell back to rejected system python"
  [[ ! -d "$fakehome/.lingtai-tui/runtime/venv" ]] || fail "ensure_runtime_venv left a stale venv after uv failure"
)

# --- uv bootstrap idempotency: existing uv is reused, no download ------------
(
  fakebin="$tmp/uv-existing"
  mkdir -p "$fakebin"
  cat > "$fakebin/uv" <<'SH'
#!/usr/bin/env bash
echo "uv 9.9.9-preinstalled"
SH
  chmod +x "$fakebin/uv"
  # A curl that always fails: reuse path must not invoke it.
  cat > "$fakebin/curl" <<'SH'
#!/usr/bin/env bash
echo "fake curl should not be called when uv exists" >&2
exit 22
SH
  chmod +x "$fakebin/curl"
  export PATH="$fakebin:$PATH"
  assert_eq "$fakebin/uv" "$(ensure_uv)" "ensure_uv reuses an existing uv without downloading"
)

REF=""
VERSION=""
UPDATE_MODE=0
INSTALL_PREFIX=""
NON_INTERACTIVE=0
parse_args --update --prefix "$tmp/prefix" --version v1.2.3 --non-interactive
assert_eq "1" "$UPDATE_MODE" "update mode flag"
assert_eq "$tmp/prefix" "$INSTALL_PREFIX" "update prefix flag"
assert_eq "v1.2.3" "$VERSION" "update version flag"
assert_eq "1" "$NON_INTERACTIVE" "non-interactive flag"

# --bin-dir / --from-source / --skip-* flags parse into their globals.
REF=""
VERSION=""
UPDATE_MODE=0
INSTALL_PREFIX=""
BIN_DIR_OVERRIDE=""
FROM_SOURCE=0
SKIP_PORTAL=0
SKIP_VENV=0
NON_INTERACTIVE=0
parse_args --version v9.9.9 --bin-dir "$tmp/mybin" --from-source --skip-portal --skip-venv
assert_eq "v9.9.9" "$VERSION" "version flag (install mode)"
assert_eq "$tmp/mybin" "$BIN_DIR_OVERRIDE" "bin-dir flag"
assert_eq "1" "$FROM_SOURCE" "from-source flag"
assert_eq "1" "$SKIP_PORTAL" "skip-portal flag"
assert_eq "1" "$SKIP_VENV" "skip-venv flag"

# --ref selects a source build ref.
REF=""
VERSION=""
parse_args --ref feature/x
assert_eq "feature/x" "$REF" "ref flag"

REF=""
VERSION=""
UPDATE_MODE=0
INSTALL_PREFIX=""
BIN_DIR_OVERRIDE=""
FROM_SOURCE=0
SKIP_PORTAL=0
SKIP_VENV=0
NON_INTERACTIVE=0

printf 'second\n' >> "$repo/file.txt"
git -C "$repo" commit -qam "second"
branch_version="$(version_for_checkout "$repo" "main")"
case "$branch_version" in
  v1.2.3-1-g*) ;;
  *) fail "branch/hash installs should keep git describe fallback, got '$branch_version'" ;;
esac

prefix="$tmp/prefix"
bin_dir="$prefix/bin"
global_dir="$tmp/home/.lingtai-tui"
mkdir -p "$bin_dir"
tui_path="$bin_dir/lingtai-tui"
portal_path="$bin_dir/lingtai-portal"
touch "$tui_path" "$portal_path"

replacement_src="$tmp/replacement-src"
printf 'new-binary\n' > "$replacement_src"
printf 'old-binary\n' > "$tui_path"
install_binary_atomically "$replacement_src" "$tui_path"
assert_eq "new-binary" "$(cat "$tui_path")" "atomic replacement content"
if compgen -G "$bin_dir/.lingtai-tui.tmp.*" >/dev/null; then
  fail "atomic replacement left temp files in $bin_dir"
fi

fake_tui="$tmp/fake-lingtai-tui"
cat > "$fake_tui" <<'SH'
#!/usr/bin/env bash
echo "lingtai-tui v1.2.3"
SH
chmod +x "$fake_tui"
verify_tui_binary_version "$fake_tui" "v1.2.3"
if verify_tui_binary_version "$fake_tui" "v9.9.9" 2>/dev/null; then
  fail "version verifier accepted mismatched version"
fi

write_install_metadata \
  "$global_dir" \
  "$prefix" \
  "$bin_dir" \
  "$REPO" \
  "v1.2.3" \
  "v1.2.3" \
  "$tagged_commit" \
  "v1.2.3" \
  "$tui_path" \
  "$portal_path"

python3 - "$global_dir/install.json" "$prefix" "$bin_dir" "$tagged_commit" "$tui_path" "$portal_path" <<'PY'
import json
import sys
from pathlib import Path

path, prefix, bin_dir, commit, tui_path, portal_path = sys.argv[1:]
data = json.loads(Path(path).read_text())

assert data["schema"] == "lingtai.tui.install/v1"
assert data["schema_version"] == 1
assert data["install_method"] == "source"
assert data["prefix"] == prefix
assert data["bin_dir"] == bin_dir
assert data["repo_url"] == "https://github.com/Lingtai-AI/lingtai.git"
assert data["requested_ref"] == "v1.2.3"
assert data["resolved_ref"] == "v1.2.3"
assert data["resolved_commit"] == commit
assert data["stamped_version"] == "v1.2.3"
assert data["managed_binaries"] == [tui_path, portal_path]
assert "/lingtai-install-" not in json.dumps(data)
PY

single_global_dir="$tmp/home-single/.lingtai-tui"
write_install_metadata \
  "$single_global_dir" \
  "$prefix" \
  "$bin_dir" \
  "$REPO" \
  "main" \
  "main" \
  "$tagged_commit" \
  "v1.2.3-1-gabcdef0" \
  "$tui_path" \
  ""

python3 - "$single_global_dir/install.json" "$tui_path" <<'PY'
import json
import sys
from pathlib import Path

path, tui_path = sys.argv[1:]
data = json.loads(Path(path).read_text())

assert data["requested_ref"] == "main"
assert data["stamped_version"] == "v1.2.3-1-gabcdef0"
assert data["managed_binaries"] == [tui_path]
PY

special_global_dir="$tmp/home-special/.lingtai-tui"
special_prefix=$'prefix"\\\n\t\b\f\001'
special_bin_dir=$'bin\rdir'
special_ref=$'feature/"metadata"\\line\n'
special_version=$'version\t1'
write_install_metadata \
  "$special_global_dir" \
  "$special_prefix" \
  "$special_bin_dir" \
  "$REPO" \
  "$special_ref" \
  "$special_ref" \
  "$tagged_commit" \
  "$special_version" \
  "$tui_path" \
  ""

python3 - "$special_global_dir/install.json" "$special_prefix" "$special_bin_dir" "$special_ref" "$special_version" "$tui_path" <<'PY'
import json
import sys
from pathlib import Path

path, prefix, bin_dir, ref, version, tui_path = sys.argv[1:]
data = json.loads(Path(path).read_text())

assert data["prefix"] == prefix
assert data["bin_dir"] == bin_dir
assert data["requested_ref"] == ref
assert data["resolved_ref"] == ref
assert data["stamped_version"] == version
assert data["managed_binaries"] == [tui_path]
PY

non_ascii_global_dir="$tmp/home-non-ascii/.lingtai-tui"
non_ascii_prefix="$(printf '/Users/jos\303\251/\350\267\257\345\276\204')"
non_ascii_bin_dir="$non_ascii_prefix/bin"
non_ascii_ref="$(printf 'feature/jos\303\251-\350\267\257\345\276\204')"
non_ascii_version="$(printf 'v1.2.3-jos\303\251-\350\267\257\345\276\204')"
non_ascii_tui_path="$non_ascii_bin_dir/lingtai-tui"
non_ascii_portal_path="$non_ascii_bin_dir/lingtai-portal"
write_install_metadata \
  "$non_ascii_global_dir" \
  "$non_ascii_prefix" \
  "$non_ascii_bin_dir" \
  "$REPO" \
  "$non_ascii_ref" \
  "$non_ascii_ref" \
  "$tagged_commit" \
  "$non_ascii_version" \
  "$non_ascii_tui_path" \
  "$non_ascii_portal_path"

python3 - "$non_ascii_global_dir/install.json" "$non_ascii_prefix" "$non_ascii_bin_dir" "$non_ascii_ref" "$non_ascii_version" "$non_ascii_tui_path" "$non_ascii_portal_path" <<'PY'
import json
import sys
from pathlib import Path

path, prefix, bin_dir, ref, version, tui_path, portal_path = sys.argv[1:]
data = json.loads(Path(path).read_text(encoding="utf-8"))

assert data["prefix"] == prefix
assert data["bin_dir"] == bin_dir
assert data["requested_ref"] == ref
assert data["resolved_ref"] == ref
assert data["stamped_version"] == version
assert data["managed_binaries"] == [tui_path, portal_path]
PY

echo "install.sh helper tests passed"
