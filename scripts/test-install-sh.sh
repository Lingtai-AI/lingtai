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
assert_eq "v1.2.3" "$(version_for_checkout "$repo" "v1.2.3")" "exact tag version"
assert_eq "v1.2.3" "$(version_for_checkout "$repo" "refs/tags/v1.2.3")" "exact full tag ref version"
assert_eq 'quote\"slash\\' "$(json_escape 'quote"slash\')" "json quote/backslash escaping"
assert_eq '\n' "$(json_escape $'\n')" "json newline escaping"
assert_eq '\r' "$(json_escape $'\r')" "json carriage return escaping"
assert_eq '\t' "$(json_escape $'\t')" "json tab escaping"
assert_eq '\b' "$(json_escape $'\b')" "json backspace escaping"
assert_eq '\f' "$(json_escape $'\f')" "json form-feed escaping"
assert_eq '\u0001' "$(json_escape $'\001')" "json generic control-byte escaping"

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

echo "install.sh helper tests passed"
