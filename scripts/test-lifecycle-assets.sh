#!/usr/bin/env bash
# Real, non-mocked regression coverage for the standalone lifecycle maintenance
# assets (update.sh, fix.sh, verify.sh, dev.sh): each is invoked as a real
# subprocess (never sourced — none of them is written as a guarded function
# library) against a real venv, a real installed "lingtai" distribution, and a
# real lingtai.tui.install/v1 receipt built by this test. verify.sh, update.sh,
# and fix.sh are proven end-to-end, including real postconditions (PASS
# output, an actual atomic TUI swap, an actual new runtime venv). dev.sh's
# precondition/argument validation is proven the same way; its own Go/npm
# build is out of scope here — see dev.sh's maintenance header, which reserves
# a real build+install acceptance run for an isolated environment with a real
# TUI/kernel checkout and toolchain, not this fast repo-local gate.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UPDATE_SH="$ROOT_DIR/update.sh"
FIX_SH="$ROOT_DIR/fix.sh"
VERIFY_SH="$ROOT_DIR/verify.sh"
DEV_SH="$ROOT_DIR/dev.sh"

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

echo "test-lifecycle-assets: all real update.sh/fix.sh/verify.sh postcondition and dev.sh precondition checks passed"
