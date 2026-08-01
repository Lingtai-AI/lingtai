#!/usr/bin/env bash
# Real, non-mocked regression coverage for the standalone lifecycle maintenance
# assets (update.sh, fix.sh, verify.sh, dev.sh, remove.sh): each is invoked as
# a real subprocess (never sourced — none of them is written as a guarded
# function library) against a real venv, a real installed "lingtai"
# distribution, and a real lingtai.tui.install/v1 receipt built by this test.
# verify.sh, update.sh, and fix.sh are proven end-to-end, including real
# postconditions (PASS output, an actual atomic TUI swap, an actual new
# runtime venv). dev.sh's precondition/argument validation is proven the same
# way; its own Go/npm build is out of scope here — see dev.sh's maintenance
# header, which reserves a real build+install acceptance run for an isolated
# environment with a real TUI/kernel checkout and toolchain, not this fast
# repo-local gate. remove.sh is proven end-to-end too: a real happy-path
# removal, every dry-failure precondition, Homebrew-shape refusal, idempotent
# second run, and a real fault-injected partial failure that must leave the
# receipt intact.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UPDATE_SH="$ROOT_DIR/update.sh"
FIX_SH="$ROOT_DIR/fix.sh"
VERIFY_SH="$ROOT_DIR/verify.sh"
DEV_SH="$ROOT_DIR/dev.sh"
REMOVE_SH="$ROOT_DIR/remove.sh"

command -v python3 >/dev/null || { echo "test-lifecycle-assets: python3 is required" >&2; exit 1; }
command -v tar >/dev/null || { echo "test-lifecycle-assets: tar is required" >&2; exit 1; }

fail() {
  echo "test-lifecycle-assets: $*" >&2
  exit 1
}

assert_exit() {
  local want="$1" got="$2" label="$3"
  [[ "$got" == "$want" ]] || fail "$label: exit $got, want $want"
}

assert_contains() {
  local haystack="$1" needle="$2" label="$3"
  case "$haystack" in
    *"$needle"*) ;;
    *) fail "$label: output did not contain '$needle'; got: $haystack" ;;
  esac
}

tmp="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-lifecycle-test.XXXXXX")"
tmp="$(cd "$tmp" && pwd -P)"
trap 'rm -rf "$tmp"' EXIT

fake_home="$tmp/home"
bin_dir="$tmp/bin"
runtime_root="$fake_home/.lingtai-tui/runtime"
venv_dir="$runtime_root/venv"
metadata="$fake_home/.lingtai-tui/install.json"
mkdir -p "$bin_dir" "$runtime_root"

# --- build a real venv with a real importable "lingtai" distribution --------
sha256_of() {
  if command -v sha256sum >/dev/null; then sha256sum "$1" | cut -d' ' -f1
  else shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

build_kernel_wheel() {
  local version="$1" out_dir="$2" src
  src="$tmp/kernel-src-$version"
  rm -rf "$src"
  mkdir -p "$src/lingtai/kernel"
  printf '__version__ = "%s"\n' "$version" > "$src/lingtai/__init__.py"
  : > "$src/lingtai/kernel/__init__.py"
  cat > "$src/pyproject.toml" <<EOF
[build-system]
requires = ["setuptools"]
build-backend = "setuptools.build_meta"
[project]
name = "lingtai"
version = "${version#v}"
EOF
  PYTHONPATH= python3 -m pip wheel --disable-pip-version-check -q --no-deps -w "$out_dir" "$src" >/dev/null
  find "$out_dir" -maxdepth 1 -name '*.whl' -print -quit
}

PYTHONPATH= python3 -m venv "$venv_dir"
wheel_dir_v1="$tmp/wheels-v1"
mkdir -p "$wheel_dir_v1"
wheel_v1="$(build_kernel_wheel v1.2.3 "$wheel_dir_v1")"
PYTHONPATH= "$venv_dir/bin/python" -m pip install --disable-pip-version-check -q --no-deps "$wheel_v1"

cat > "$bin_dir/lingtai-tui" <<'EOF'
#!/usr/bin/env bash
echo "v1.2.3"
EOF
chmod +x "$bin_dir/lingtai-tui"

write_receipt() {
  local install_kind="$1" kernel_source_extra="${2:-}"
  cat > "$metadata" <<EOF
{
  "schema": "lingtai.tui.install/v1",
  "schema_version": 1,
  "install_method": "source",
  "install_kind": "$install_kind",
  "bin_dir": "$bin_dir",
  "runtime_venv": "$venv_dir",
  "stamped_version": "v1.2.3",
  "kernel_version": "v1.2.3",
  "managed_binaries": ["$bin_dir/lingtai-tui"]$kernel_source_extra
}
EOF
}
write_receipt "source-build"

# --- verify.sh: real end-to-end PASS -----------------------------------------
out="$(HOME="$fake_home" PYTHONPATH= "$VERIFY_SH" --bin-dir "$bin_dir" --runtime-python "$venv_dir/bin/python" --metadata "$metadata" 2>&1)"
rc=$?
assert_exit "0" "$rc" "verify.sh healthy receipt"
assert_contains "$out" "PASS" "verify.sh healthy receipt output"
assert_contains "$out" "v1.2.3" "verify.sh healthy receipt reports TUI version"

# verify.sh: dev-source receipt without editable kernel_source fails closed.
write_receipt "dev-source"
set +e
out="$(HOME="$fake_home" PYTHONPATH= "$VERIFY_SH" --bin-dir "$bin_dir" --runtime-python "$venv_dir/bin/python" --metadata "$metadata" 2>&1)"
rc=$?
set -e
assert_exit "1" "$rc" "verify.sh rejects dev-source receipt lacking editable kernel_source"
write_receipt "source-build"

# verify.sh: missing required argument is a usage error (exit 2), not a crash.
set +e
out="$("$VERIFY_SH" --bin-dir "$bin_dir" 2>&1)"
rc=$?
set -e
assert_exit "2" "$rc" "verify.sh missing --runtime-python"

# --- update.sh: precondition rejections, then a real successful update ------
set +e
out="$("$UPDATE_SH" --bin-dir "$bin_dir" --runtime-python "$venv_dir/bin/python" \
  --tui-archive "$tmp/none.tar.gz" --tui-sha256 "0000000000000000000000000000000000000000000000000000000000000000" \
  --kernel-artifact "$tmp/none.whl" --kernel-sha256 "0000000000000000000000000000000000000000000000000000000000000000" \
  --tui-tag v1.2.4 --kernel-version v1.2.4 2>&1)"
rc=$?
set -e
assert_exit "1" "$rc" "update.sh refuses to mutate without --yes"
assert_contains "$out" "--yes" "update.sh --yes error names the missing flag"

# Build a real v1.2.4 TUI archive (single executable named lingtai-tui) and a
# real v1.2.4 kernel wheel, then run a genuine update.sh --yes end to end.
tui_stage="$tmp/tui-stage-v1.2.4"
mkdir -p "$tui_stage"
cat > "$tui_stage/lingtai-tui" <<'EOF'
#!/usr/bin/env bash
echo "v1.2.4"
EOF
chmod +x "$tui_stage/lingtai-tui"
tui_archive="$tmp/lingtai-v1.2.4-linux-amd64.tar.gz"
tar -czf "$tui_archive" -C "$tui_stage" lingtai-tui
tui_sha="$(sha256_of "$tui_archive")"

wheel_dir_v2="$tmp/wheels-v1.2.4"
mkdir -p "$wheel_dir_v2"
wheel_v2="$(build_kernel_wheel v1.2.4 "$wheel_dir_v2")"
kernel_sha="$(sha256_of "$wheel_v2")"

out="$(HOME="$fake_home" "$UPDATE_SH" --bin-dir "$bin_dir" --runtime-python "$venv_dir/bin/python" \
  --tui-archive "$tui_archive" --tui-sha256 "$tui_sha" \
  --kernel-artifact "$wheel_v2" --kernel-sha256 "$kernel_sha" \
  --tui-tag v1.2.4 --kernel-version v1.2.4 --yes 2>&1)"
rc=$?
assert_exit "0" "$rc" "update.sh real --yes update"
assert_contains "$out" "PASS" "update.sh real update reports PASS"
assert_eq() { [[ "$2" == "$1" ]] || fail "$3: got '$2', want '$1'"; }
new_tui_output="$("$bin_dir/lingtai-tui" version 2>/dev/null || "$bin_dir/lingtai-tui")"
assert_eq "v1.2.4" "$new_tui_output" "update.sh atomically replaced lingtai-tui"
new_kernel_version="$(PYTHONPATH= "$venv_dir/bin/python" -c 'import lingtai; print(lingtai.__version__)')"
assert_eq "v1.2.4" "$new_kernel_version" "update.sh reinstalled the exact kernel wheel"
receipt_stamp="$(PYTHONPATH= python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["stamped_version"])' "$metadata")"
assert_eq "v1.2.4" "$receipt_stamp" "update.sh atomically revalidated the receipt stamp"

# update.sh must reject a dev-source receipt outright.
write_receipt "dev-source"
set +e
out="$(HOME="$fake_home" "$UPDATE_SH" --bin-dir "$bin_dir" --runtime-python "$venv_dir/bin/python" \
  --tui-archive "$tui_archive" --tui-sha256 "$tui_sha" \
  --kernel-artifact "$wheel_v2" --kernel-sha256 "$kernel_sha" \
  --tui-tag v1.2.4 --kernel-version v1.2.4 --yes 2>&1)"
rc=$?
set -e
assert_exit "1" "$rc" "update.sh refuses a dev-source receipt"
write_receipt "source-build"
# Reset the fixture back to v1.2.3 for the fix.sh section below.
cat > "$bin_dir/lingtai-tui" <<'EOF'
#!/usr/bin/env bash
echo "v1.2.3"
EOF
chmod +x "$bin_dir/lingtai-tui"

# --- fix.sh: read-only diagnosis, then a real --apply --yes repair ----------
out="$(HOME="$fake_home" "$FIX_SH" --bin-dir "$bin_dir" 2>&1)"
rc=$?
assert_exit "0" "$rc" "fix.sh read-only diagnosis"
assert_contains "$out" "Read-only plan" "fix.sh diagnosis is explicitly read-only"
[[ ! -e "$runtime_root/repaired" ]] || fail "fix.sh read-only diagnosis must not create a runtime directory"

wheel_dir_repair="$tmp/wheels-repair"
mkdir -p "$wheel_dir_repair"
wheel_repair="$(build_kernel_wheel v1.2.3 "$wheel_dir_repair")"
repair_sha="$(sha256_of "$wheel_repair")"
out="$(HOME="$fake_home" "$FIX_SH" --bin-dir "$bin_dir" --runtime-dir "$runtime_root/repaired" \
  --kernel-artifact "$wheel_repair" --kernel-sha256 "$repair_sha" --apply --yes 2>&1)"
rc=$?
assert_exit "0" "$rc" "fix.sh real --apply --yes repair"
assert_contains "$out" "PASS" "fix.sh repair reports PASS"
[[ -x "$runtime_root/repaired/bin/python" ]] || fail "fix.sh did not create the named repair venv"
repaired_version="$(PYTHONPATH= "$runtime_root/repaired/bin/python" -c 'import lingtai; print(lingtai.__version__)')"
assert_eq "v1.2.3" "$repaired_version" "fix.sh repair venv has the exact prior kernel version"
new_pointer="$(PYTHONPATH= python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["runtime_venv"])' "$metadata")"
assert_eq "$runtime_root/repaired" "$new_pointer" "fix.sh atomically repointed the receipt to the new runtime"

# --- dev.sh: precondition/argument validation (no real Go/npm build here) ---
set +e
out="$("$DEV_SH" --tui-source "$tmp/no-tui" --kernel-source "$tmp/no-kernel" --bin-dir "$bin_dir" 2>&1)"
rc=$?
set -e
assert_exit "1" "$rc" "dev.sh requires --yes"
assert_contains "$out" "--yes" "dev.sh --yes error names the missing flag"

fake_tui_src="$tmp/fake-tui-src"
mkdir -p "$fake_tui_src/tui"
git init -q "$fake_tui_src"
printf 'module example.test/lingtai\n\ngo 1.26\n' > "$fake_tui_src/tui/go.mod"

fake_kernel_src="$tmp/fake-kernel-src-no-packaging"
mkdir -p "$fake_kernel_src"
git init -q "$fake_kernel_src"

set +e
out="$("$DEV_SH" --tui-source "$fake_tui_src" --kernel-source "$fake_kernel_src" --bin-dir "$bin_dir" --yes 2>&1)"
rc=$?
set -e
assert_exit "1" "$rc" "dev.sh rejects a kernel checkout lacking packaging metadata"
assert_contains "$out" "packaging metadata" "dev.sh error names the missing kernel packaging metadata"

rm -rf "$tmp/lingtai-tui" 2>/dev/null || true

# --- remove.sh: real end-to-end removal, dry failures, and fault injection --
# Uses its own fresh fixture (a fresh fake HOME/bin_dir/venv/receipt),
# independent of the update/fix/dev fixtures above, because removal deletes
# the target state rather than mutating it in place.
remove_fixture() {
  local suffix="$1"
  local r_home="$tmp/remove-home-$suffix"
  local r_bin="$tmp/remove-bin-$suffix"
  local r_runtime="$r_home/.lingtai-tui/runtime"
  local r_venv="$r_runtime/venv"
  mkdir -p "$r_bin" "$r_runtime"
  PYTHONPATH= python3 -m venv "$r_venv"
  cat > "$r_bin/lingtai-tui" <<'BIN'
#!/usr/bin/env bash
echo "v1.2.3"
BIN
  chmod +x "$r_bin/lingtai-tui"
  ln -sfn "$r_bin/lingtai-tui" "$r_bin/lingtai"
  cat > "$r_venv/bin/lingtai-agent" <<'BIN'
#!/usr/bin/env bash
echo agent
BIN
  chmod +x "$r_venv/bin/lingtai-agent"
  ln -sfn "$r_venv/bin/lingtai-agent" "$r_bin/lingtai-agent"
  cat > "$r_home/.lingtai-tui/install.json" <<EOF
{
  "schema": "lingtai.tui.install/v1",
  "schema_version": 1,
  "install_method": "source",
  "install_kind": "source-build",
  "bin_dir": "$r_bin",
  "runtime_venv": "$r_venv",
  "stamped_version": "v1.2.3",
  "kernel_version": "v1.2.3",
  "managed_binaries": ["$r_bin/lingtai-tui"]
}
EOF
  echo "secret" > "$r_home/.lingtai-tui/.env"
  printf '%s\t%s\t%s\n' "$r_home" "$r_bin" "$r_venv"
}

# Happy path: real removal deletes the receipt-owned artifact set and
# preserves the NOT-owned .env sentinel; a second run is idempotent.
IFS="$(printf '\t')" read -r remove_home remove_bin remove_venv <<EOF
$(remove_fixture happy)
EOF
out="$(HOME="$remove_home" "$REMOVE_SH" --bin-dir "$remove_bin" --yes 2>&1)"
rc=$?
assert_exit "0" "$rc" "remove.sh real happy-path removal"
assert_contains "$out" "PASS" "remove.sh happy-path reports PASS"
[[ ! -e "$remove_bin/lingtai-tui" ]] || fail "remove.sh did not delete lingtai-tui"
[[ ! -e "$remove_bin/lingtai" ]] || fail "remove.sh did not delete the lingtai alias symlink"
[[ ! -e "$remove_bin/lingtai-agent" ]] || fail "remove.sh did not delete the lingtai-agent symlink"
[[ ! -e "$remove_venv" ]] || fail "remove.sh did not delete the runtime venv"
[[ ! -e "$remove_home/.lingtai-tui/install.json" ]] || fail "remove.sh did not delete the receipt"
[[ -f "$remove_home/.lingtai-tui/.env" ]] || fail "remove.sh deleted NOT-owned .env; must preserve user secrets"

out="$(HOME="$remove_home" "$REMOVE_SH" --bin-dir "$remove_bin" --yes 2>&1)"
rc=$?
assert_exit "0" "$rc" "remove.sh idempotent second run"
assert_contains "$out" "nothing to remove" "remove.sh second run reports nothing to remove"

# Missing --yes is a runtime failure (exit 1), not a usage error.
IFS="$(printf '\t')" read -r remove_home remove_bin remove_venv <<EOF
$(remove_fixture noyes)
EOF
set +e
out="$("$REMOVE_SH" --bin-dir "$remove_bin" 2>&1)"
rc=$?
set -e
assert_exit "1" "$rc" "remove.sh refuses to mutate without --yes"
assert_contains "$out" "--yes" "remove.sh --yes error names the missing flag"
[[ -f "$remove_bin/lingtai-tui" ]] || fail "remove.sh must not delete anything when --yes is missing"

# Missing --bin-dir is a usage error (exit 2).
set +e
out="$("$REMOVE_SH" --yes 2>&1)"
rc=$?
set -e
assert_exit "2" "$rc" "remove.sh missing --bin-dir is a usage error"

# bin_dir mismatch: refuse, preserve the binary, and surface the receipt's
# own bin_dir so a user who guessed wrong has an actionable next step.
IFS="$(printf '\t')" read -r remove_home remove_bin remove_venv <<EOF
$(remove_fixture mismatch)
EOF
set +e
out="$(HOME="$remove_home" "$REMOVE_SH" --bin-dir "$tmp/remove-bin-mismatch-other" --yes 2>&1)"
rc=$?
set -e
assert_exit "1" "$rc" "remove.sh refuses a bin_dir the receipt does not own"
[[ -f "$remove_bin/lingtai-tui" ]] || fail "remove.sh must not delete anything on a bin_dir mismatch"
assert_contains "$out" "$remove_bin" "remove.sh bin_dir mismatch error surfaces the receipt's actual bin_dir"
assert_contains "$out" "--bin-dir $remove_bin" "remove.sh bin_dir mismatch error names an actionable re-run"

# Homebrew-shaped target: refuse, preserve the binary. Simulated via
# HOMEBREW_PREFIX rather than a real brew install (no real Homebrew is
# assumed present in this repo-local gate).
IFS="$(printf '\t')" read -r remove_home remove_bin remove_venv <<EOF
$(remove_fixture homebrew)
EOF
set +e
out="$(HOME="$remove_home" HOMEBREW_PREFIX="$(dirname "$remove_bin")" "$REMOVE_SH" --bin-dir "$remove_bin" --yes 2>&1)"
rc=$?
set -e
assert_exit "1" "$rc" "remove.sh refuses a Homebrew-shaped target"
assert_contains "$out" "brew uninstall" "remove.sh Homebrew refusal points at brew uninstall"
[[ -f "$remove_bin/lingtai-tui" ]] || fail "remove.sh must not delete a Homebrew-shaped target"

# An unrelated pre-existing file at the "lingtai" alias name must survive —
# same discrimination install.sh's ensure_lingtai_alias applies in reverse.
IFS="$(printf '\t')" read -r remove_home remove_bin remove_venv <<EOF
$(remove_fixture unrelated-alias)
EOF
rm -f "$remove_bin/lingtai"
echo "unrelated content" > "$remove_bin/lingtai"
out="$(HOME="$remove_home" "$REMOVE_SH" --bin-dir "$remove_bin" --yes 2>&1)"
rc=$?
assert_exit "0" "$rc" "remove.sh succeeds even with an unrelated file at the lingtai alias name"
[[ -f "$remove_bin/lingtai" ]] || fail "remove.sh deleted an unrelated pre-existing 'lingtai' file"
assert_contains "$(cat "$remove_bin/lingtai")" "unrelated content" "remove.sh left the unrelated 'lingtai' file's content untouched"

# Fault injection: an undeletable runtime venv must leave the receipt intact
# and honestly report the survivor, never silently claim full removal.
IFS="$(printf '\t')" read -r remove_home remove_bin remove_venv <<EOF
$(remove_fixture partial)
EOF
remove_runtime_root="$(dirname "$remove_venv")"
chmod 555 "$remove_runtime_root"
set +e
out="$(HOME="$remove_home" "$REMOVE_SH" --bin-dir "$remove_bin" --yes 2>&1)"
rc=$?
set -e
chmod 755 "$remove_runtime_root"
assert_exit "1" "$rc" "remove.sh partial failure exits nonzero"
assert_contains "$out" "PARTIAL" "remove.sh reports PARTIAL on fault injection"
[[ -f "$remove_home/.lingtai-tui/install.json" ]] || fail "remove.sh deleted the receipt despite a partial failure"
[[ ! -e "$remove_bin/lingtai-tui" ]] || fail "remove.sh should have removed the binary before hitting the venv fault"
# Retry after the fault is cleared must complete the interrupted removal.
out="$(HOME="$remove_home" "$REMOVE_SH" --bin-dir "$remove_bin" --yes 2>&1)"
rc=$?
assert_exit "0" "$rc" "remove.sh retry after a cleared fault completes removal"
[[ ! -f "$remove_home/.lingtai-tui/install.json" ]] || fail "remove.sh retry did not delete the receipt"

# --- remove.sh must NEVER sweep by filename pattern (B1 regression) --------
# The receipt is the only deletion oracle. A directory merely named like a
# repair venv, that the receipt does not point at, is not proof of ownership
# -- install.sh can create such a name on a real repair retry, but so can
# anything else, and remove.sh must never delete it on pattern match alone.

# Case 1: an unrelated real directory that happens to match the
# venv-repair-* naming convention, but that this installation's receipt does
# not reference at all, must survive removal untouched.
IFS="$(printf '\t')" read -r remove_home remove_bin remove_venv <<EOF
$(remove_fixture venvrepair-unrelated)
EOF
remove_runtime_root="$(dirname "$remove_venv")"
unrelated_repair_dir="$remove_runtime_root/venv-repair-mydata"
mkdir -p "$unrelated_repair_dir"
echo "IRREPLACEABLE USER DATA" > "$unrelated_repair_dir/important.txt"
out="$(HOME="$remove_home" "$REMOVE_SH" --bin-dir "$remove_bin" --yes 2>&1)"
rc=$?
assert_exit "0" "$rc" "remove.sh happy path still succeeds with an unrelated venv-repair-* directory present"
[[ -f "$unrelated_repair_dir/important.txt" ]] || fail "remove.sh deleted an unrelated venv-repair-*-named directory the receipt never referenced (B1 regression)"
assert_contains "$(cat "$unrelated_repair_dir/important.txt")" "IRREPLACEABLE USER DATA" "remove.sh left the unrelated venv-repair-*-named directory's content untouched"
assert_contains "$out" "runtime" "remove.sh reports the surviving runtime/ directory (containing the unrelated venv-repair-* dir) as a survivor"

# Case 2: the receipt's OWN runtime_venv happens to be named venv-repair-*
# (the real shape after install.sh's own repair-retry loop promotes that path
# to the live receipt pointer). It must be removed exactly once, as the
# receipt-declared runtime venv -- never double-listed, and never skipped.
# A separately-retained, unproven old runtime/venv sibling (simulating what
# install.sh actually leaves behind on a real repair: the OLD venv retained,
# not the new venv-repair-* path) must survive as an honestly-reported
# survivor, since nothing in the receipt names it.
r_home="$tmp/remove-home-venvrepair-live"
r_bin="$tmp/remove-bin-venvrepair-live"
r_runtime="$r_home/.lingtai-tui/runtime"
r_venv="$r_runtime/venv-repair-9999-1"
r_old_venv="$r_runtime/venv"
mkdir -p "$r_bin" "$r_runtime"
PYTHONPATH= python3 -m venv "$r_venv"
mkdir -p "$r_old_venv/bin"
echo "old retained venv, not receipt-owned" > "$r_old_venv/bin/OLD"
cat > "$r_bin/lingtai-tui" <<'BIN'
#!/usr/bin/env bash
echo "v1.2.3"
BIN
chmod +x "$r_bin/lingtai-tui"
cat > "$r_home/.lingtai-tui/install.json" <<EOF
{
  "schema": "lingtai.tui.install/v1",
  "schema_version": 1,
  "install_method": "source",
  "install_kind": "source-build",
  "bin_dir": "$r_bin",
  "runtime_venv": "$r_venv",
  "stamped_version": "v1.2.3",
  "kernel_version": "v1.2.3",
  "managed_binaries": ["$r_bin/lingtai-tui"]
}
EOF
out="$(HOME="$r_home" "$REMOVE_SH" --bin-dir "$r_bin" --yes 2>&1)"
rc=$?
assert_exit "0" "$rc" "remove.sh removes a receipt-owned venv-repair-*-named runtime venv"
assert_contains "$out" "PASS" "remove.sh venv-repair-*-named receipt target reports PASS"
plan_occurrences="$(printf '%s\n' "$out" | grep -c -- "$r_venv" || true)"
[[ "$plan_occurrences" -le 1 ]] || fail "remove.sh double-listed the receipt-owned venv-repair-*-named runtime venv in its own output ($plan_occurrences occurrences; B1 regression)"
[[ ! -e "$r_venv" ]] || fail "remove.sh did not delete the receipt-owned venv-repair-*-named runtime venv"
[[ -f "$r_old_venv/bin/OLD" ]] || fail "remove.sh deleted the unproven old runtime/venv sibling the receipt never pointed at (B1 regression)"
assert_contains "$out" "runtime" "remove.sh reports the surviving old runtime/venv as a survivor rather than silently sweeping or silently ignoring it"

echo "test-lifecycle-assets: all real update.sh/fix.sh/verify.sh/remove.sh postcondition and dev.sh precondition checks passed"
