#!/usr/bin/env bash
# Real, non-mocked regression coverage for the capabilities integrated into
# canonical install.sh from the lingtai-web fork (kernel-release-pin support,
# strict kernel-manifest validation, symlink-escape-safe runtime venv
# containment, existing-runtime/fresh-install-state fail-loud checks, and
# exclusive-create receipt publication). Functions are sourced directly (this
# script, like test-install-sh.sh, treats install.sh as a guarded function
# library via LINGTAI_INSTALL_SH_SOURCE_ONLY) and exercised against real
# filesystem state and a real python3 — no fake CLI output, no source grep.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

export LINGTAI_INSTALL_SH_SOURCE_ONLY=1
# shellcheck source=../install.sh
source "$ROOT_DIR/install.sh"
unset LINGTAI_INSTALL_SH_SOURCE_ONLY

fail() {
  echo "test-install-sh-hardening: $*" >&2
  exit 1
}

assert_eq() {
  local want="$1" got="$2" label="$3"
  [[ "$got" == "$want" ]] || fail "$label: got '$got', want '$want'"
}

command -v python3 >/dev/null || fail "python3 is required"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-inst-hardening-test.XXXXXX")"
tmp="$(cd "$tmp" && pwd -P)"
trap 'rm -rf "$tmp"' EXIT

# --- canonical_runtime_venv: physical containment, not just lexical --------

(
  fake_home="$tmp/home-containment"
  mkdir -p "$fake_home"
  HOME="$fake_home"
  runtime_root="$HOME/.lingtai-tui/runtime"
  mkdir -p "$runtime_root"

  # Ordinary direct child: accepted.
  venv_dir="$runtime_root/venv"
  resolved="$(canonical_runtime_venv "$venv_dir" "$runtime_root")" ||
    fail "canonical_runtime_venv should accept an ordinary not-yet-created direct child"
  assert_eq "$runtime_root/venv" "$resolved" "canonical_runtime_venv resolves the expected physical path"

  # A symlinked venv path whose real target escapes the owned root must be
  # rejected outright — this is the actual security property under test.
  escape_target="$tmp/outside-root"
  mkdir -p "$escape_target"
  ln -s "$escape_target" "$runtime_root/escaped-venv"
  if canonical_runtime_venv "$runtime_root/escaped-venv" "$runtime_root" >/dev/null 2>&1; then
    fail "canonical_runtime_venv must reject a venv path whose real target escapes the owned runtime root"
  fi

  # A symlinked runtime ROOT itself must also be rejected.
  other_root="$tmp/other-root-containment"
  mkdir -p "$other_root"
  fake_home2="$tmp/home-containment-2"
  mkdir -p "$fake_home2/.lingtai-tui"
  ln -s "$other_root" "$fake_home2/.lingtai-tui/runtime"
  HOME="$fake_home2"
  if canonical_runtime_venv "$fake_home2/.lingtai-tui/runtime/venv" "$fake_home2/.lingtai-tui/runtime" >/dev/null 2>&1; then
    fail "canonical_runtime_venv must reject a symlinked runtime root"
  fi
)

# --- runtime_venv_state: missing / broken / healthy, against a real venv ---

(
  state_dir="$tmp/runtime-venv-state"
  mkdir -p "$state_dir"

  missing_venv="$state_dir/missing"
  assert_eq "missing" "$(runtime_venv_state "$missing_venv")" "runtime_venv_state reports missing for a nonexistent path"

  broken_venv="$state_dir/broken"
  mkdir -p "$broken_venv/bin"
  # No python/python3 launcher at all -> broken.
  assert_eq "broken" "$(runtime_venv_state "$broken_venv")" "runtime_venv_state reports broken when no interpreter exists"

  healthy_venv="$state_dir/healthy"
  PYTHONPATH= python3 -m venv "$healthy_venv"
  assert_eq "healthy" "$(runtime_venv_state "$healthy_venv")" "runtime_venv_state reports healthy for a real, working venv"

  # A launcher that is a symlink to a DIFFERENT venv's python must be reported
  # broken by runtime_prefix_matches_venv, not silently treated as this venv's
  # own interpreter.
  other_venv="$state_dir/other"
  PYTHONPATH= python3 -m venv "$other_venv"
  shadow_venv="$state_dir/shadow"
  mkdir -p "$shadow_venv/bin"
  ln -s "$other_venv/bin/python3" "$shadow_venv/bin/python"
  assert_eq "broken" "$(runtime_venv_state "$shadow_venv")" "runtime_venv_state rejects a launcher symlinked to a different venv's interpreter"
)

# --- runtime_health_check: rejects a same-version package shadowed from -----
# --- outside the selected venv's own physical prefix ------------------------

(
  health_venv="$tmp/health-venv"
  PYTHONPATH= python3 -m venv "$health_venv"
  py="$health_venv/bin/python"

  # Build and install a real "lingtai"+"lingtai.kernel" package inside the venv.
  build_fake_kernel() {
    local dest="$1" version="$2" src
    src="$tmp/kernel-src-$version-$RANDOM"
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
    PYTHONPATH= "$dest" -m pip install --disable-pip-version-check -q "$src"
  }
  build_fake_kernel "$py" "1.2.3"

  # Real installed package inside the venv's own prefix: passes.
  out="$(runtime_health_check "$py" "1.2.3")" || fail "runtime_health_check should pass for a genuinely installed package"
  [[ "$out" == *$'\t'* ]] || fail "runtime_health_check output should contain the module __file__"

  # Wrong expected version: fails closed.
  if runtime_health_check "$py" "9.9.9" >/dev/null 2>&1; then
    fail "runtime_health_check must fail when the installed version does not match the expected version"
  fi

  # Genuine physical-shadow attack: a .pth file dropped into the venv's own
  # site-packages that puts an external, same-version "lingtai" earlier on
  # sys.path than the venv's real install. This is exactly the attack
  # runtime_health_check's __file__-physically-inside-prefix check defends
  # against — a plain `import lingtai; check __version__` would pass here,
  # since the shadowed package reports the identical expected version.
  external_pkg_dir="$tmp/external-lingtai-shadow"
  mkdir -p "$external_pkg_dir/lingtai/kernel"
  printf '__version__ = "1.2.3"\n' > "$external_pkg_dir/lingtai/__init__.py"
  : > "$external_pkg_dir/lingtai/kernel/__init__.py"
  site_packages="$(PYTHONPATH= "$py" -c 'import sysconfig; print(sysconfig.get_paths()["purelib"])')"
  printf '%s\n' "$external_pkg_dir" > "$site_packages/zz_shadow_external_lingtai.pth"
  # Confirm the shadow actually takes effect (sys.path .pth files load in
  # alphabetical order; "zz_" sorts after the real package's own metadata, but
  # site.py appends .pth-added directories to the END of sys.path — so instead
  # prove the shadow directly by removing the venv's real install first).
  PYTHONPATH= "$py" -m pip uninstall -y -q lingtai
  shadow_output="$(PYTHONPATH= "$py" -c 'import lingtai; print(lingtai.__file__)')"
  case "$shadow_output" in
    "$external_pkg_dir"*) ;;
    *) fail "test setup error: .pth shadow did not take effect; got $shadow_output" ;;
  esac
  if runtime_health_check "$py" "1.2.3" >/dev/null 2>&1; then
    fail "runtime_health_check must reject a same-version package whose __file__ resolves outside the venv's own prefix"
  fi
)

# --- validate_install_target / validate_fresh_install_state -----------------

(
  fresh_home="$tmp/home-fresh"
  mkdir -p "$fresh_home"
  HOME="$fresh_home"
  BIN_DIR="$fresh_home/bin"
  mkdir -p "$BIN_DIR"

  validate_install_target || fail "validate_install_target should pass for a genuinely empty bin dir"
  validate_fresh_install_state || fail "validate_fresh_install_state should pass with no prior receipt/runtime"

  # A pre-existing managed binary must be refused.
  touch "$BIN_DIR/lingtai-tui"
  if validate_install_target >/dev/null 2>&1; then
    fail "validate_install_target must refuse an existing managed lingtai-tui binary"
  fi
  rm -f "$BIN_DIR/lingtai-tui"

  # A symlinked bin dir must be refused.
  real_dir="$tmp/real-bin-dir"
  mkdir -p "$real_dir"
  symlinked_bin="$fresh_home/bin-symlink"
  ln -s "$real_dir" "$symlinked_bin"
  BIN_DIR="$symlinked_bin"
  if validate_install_target >/dev/null 2>&1; then
    fail "validate_install_target must refuse a symlinked bin dir"
  fi
  BIN_DIR="$fresh_home/bin"

  # An existing install.json receipt must be refused.
  mkdir -p "$fresh_home/.lingtai-tui"
  echo '{}' > "$fresh_home/.lingtai-tui/install.json"
  if validate_fresh_install_state >/dev/null 2>&1; then
    fail "validate_fresh_install_state must refuse an existing install receipt"
  fi
  rm -f "$fresh_home/.lingtai-tui/install.json"

  # An existing runtime root (even without a receipt) must also be refused.
  mkdir -p "$fresh_home/.lingtai-tui/runtime"
  if validate_fresh_install_state >/dev/null 2>&1; then
    fail "validate_fresh_install_state must refuse an existing runtime root"
  fi
)

# --- write_install_metadata: exclusive-create / no-clobber on fresh install -

(
  meta_home="$tmp/home-metadata"
  global_dir="$meta_home/.lingtai-tui"
  bin_dir="$meta_home/bin"
  mkdir -p "$bin_dir"
  UPDATE_MODE=0
  RUNTIME_VENV_DIR=""
  KERNEL_SOURCE=""

  write_install_metadata \
    "$global_dir" "$meta_home" "$bin_dir" "https://example.test/repo.git" \
    "v1.0.0" "v1.0.0" "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" "v1.0.0" \
    "$bin_dir/lingtai-tui" ""
  [[ -f "$global_dir/install.json" ]] || fail "write_install_metadata should have created install.json"
  perm="$(python3 -c 'import os,stat,sys; print(oct(stat.S_IMODE(os.stat(sys.argv[1]).st_mode)))' "$global_dir/install.json")"
  assert_eq "0o600" "$perm" "write_install_metadata publishes install.json at mode 600"

  # A second fresh (non-update) call must refuse to clobber the existing
  # receipt — exclusive-create semantics, not a silent overwrite.
  set +e
  write_install_metadata \
    "$global_dir" "$meta_home" "$bin_dir" "https://example.test/repo.git" \
    "v2.0.0" "v2.0.0" "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" "v2.0.0" \
    "$bin_dir/lingtai-tui" "" 2>/tmp/write-metadata-stderr.$$
  rc=$?
  set -e
  [[ "$rc" != "0" ]] || fail "write_install_metadata must refuse to overwrite an existing receipt on a fresh (non-update) call"
  stamped="$(python3 -c 'import json; print(json.load(open("'"$global_dir"'/install.json"))["stamped_version"])')"
  assert_eq "v1.0.0" "$stamped" "the original receipt must survive an attempted second fresh write untouched"
  rm -f /tmp/write-metadata-stderr.$$

  # --update legitimately re-publishes over its own existing receipt. Exercise
  # this under a permissive umask (022) to catch the receipt mode regressing
  # from 0600 to world-readable on republish (the republish branch used a
  # bare shell-redirection temp file with no chmod, unlike the fresh-install
  # branch's mktemp+chmod 600).
  UPDATE_MODE=1
  old_umask="$(umask)"
  umask 022
  write_install_metadata \
    "$global_dir" "$meta_home" "$bin_dir" "https://example.test/repo.git" \
    "v2.0.0" "v2.0.0" "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" "v2.0.0" \
    "$bin_dir/lingtai-tui" ""
  umask "$old_umask"
  stamped2="$(python3 -c 'import json; print(json.load(open("'"$global_dir"'/install.json"))["stamped_version"])')"
  assert_eq "v2.0.0" "$stamped2" "UPDATE_MODE=1 legitimately republishes the receipt in place"
  perm_update="$(python3 -c 'import os,stat,sys; print(oct(stat.S_IMODE(os.stat(sys.argv[1]).st_mode)))' "$global_dir/install.json")"
  assert_eq "0o600" "$perm_update" "UPDATE_MODE=1 republish under umask 022 must still leave install.json at mode 600"
  UPDATE_MODE=0

  # release-pin receipt shape, end to end.
  rm -rf "$global_dir"
  KERNEL_SOURCE="release-pin"
  KERNEL_RELEASE_TAG="v9.9.9"
  KERNEL_PIN_TUI_TAG="v1.0.0"
  KERNEL_VERSION_INSTALLED="9.9.9"
  KERNEL_PROVIDER="github"
  write_install_metadata \
    "$global_dir" "$meta_home" "$bin_dir" "https://example.test/repo.git" \
    "v1.0.0" "v1.0.0" "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" "v1.0.0" \
    "$bin_dir/lingtai-tui" ""
  python3 - "$global_dir/install.json" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
assert data["kernel_source"] == "release-pin", data
assert data["kernel_release_tag"] == "v9.9.9", data
assert data["tui_release_tag"] == "v1.0.0", data
assert data["kernel_version"] == "9.9.9", data
PY
  KERNEL_SOURCE=""
)

# --- --latest (LATEST_MAIN_MODE=1) is re-runnable, same as --update ---------
# --- (Opus review Blockers 1 and 2: both new guards below were originally  --
# --- gated only on UPDATE_MODE!=1, so a second --latest run hard-failed —  --
# --- ensure_runtime_venv's existing-runtime check aborted outright, and    --
# --- write_install_metadata's no-clobber publication refused to record a   --
# --- second run's newly-built binaries/runtime, worse because it fired     --
# --- AFTER those already mutated on disk. Fixed by exempting               --
# --- LATEST_MAIN_MODE==1 at both sites, exactly like UPDATE_MODE==1.)      --

(
  latest_home="$tmp/home-latest-runtime"
  mkdir -p "$latest_home"
  HOME="$latest_home"
  UPDATE_MODE=0
  LATEST_MAIN_MODE=1
  SKIP_VENV=0
  BUNDLE_MANIFEST_JSON=""

  # A pre-existing (here: broken/empty) runtime venv from a prior --latest
  # run must not trip the ordinary-install existing-runtime guard: that guard
  # is exempt for LATEST_MAIN_MODE==1, the same as it already is for
  # UPDATE_MODE==1. ensure_runtime_venv still runs its own repair loop and
  # ultimately fails on install_kernel_from_main (no real kernel checkout in
  # this test), but that failure must come from the kernel-install step, not
  # from the existing-runtime guard firing first — the guard's own message
  # ("ordinary install will not adopt or repair it") must never appear.
  mkdir -p "$latest_home/.lingtai-tui/runtime/venv/bin"
  bin_dir="$latest_home/bin"
  mkdir -p "$bin_dir"
  KERNEL_SOURCE_DIR=""
  KERNEL_MAIN_SHA=""
  out="$(ensure_runtime_venv "$bin_dir" 2>&1)" && rc=0 || rc=$?
  case "$out" in
    *"will not adopt or repair it"*)
      fail "LATEST_MAIN_MODE=1 must not trip the ordinary-install existing-runtime guard (Opus Blocker 1); got: $out"
      ;;
  esac
  LATEST_MAIN_MODE=0
  UPDATE_MODE=0
)

(
  latest_meta_home="$tmp/home-latest-metadata"
  global_dir="$latest_meta_home/.lingtai-tui"
  bin_dir="$latest_meta_home/bin"
  mkdir -p "$bin_dir"
  UPDATE_MODE=0
  LATEST_MAIN_MODE=1
  RUNTIME_VENV_DIR=""
  KERNEL_SOURCE=""

  # First --latest run publishes a fresh receipt.
  write_install_metadata \
    "$global_dir" "$latest_meta_home" "$bin_dir" "https://example.test/repo.git" \
    "main" "main" "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" "dev" \
    "$bin_dir/lingtai-tui" ""
  [[ -f "$global_dir/install.json" ]] || fail "first --latest receipt write should have created install.json"
  first_commit="$(python3 -c 'import json; print(json.load(open("'"$global_dir"'/install.json"))["resolved_commit"])')"
  assert_eq "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" "$first_commit" "first --latest receipt records the first resolved commit"

  # A second --latest run (the documented re-runnable dev-mode iterate-on-main
  # loop) must republish its own receipt in place with the NEW commit, not
  # refuse as though this were a fresh (non-update, non-latest) install —
  # Opus Blocker 2: the old code refused this exact call with "install
  # receipt appeared before metadata creation". Also exercised under a
  # permissive umask (022) to catch the receipt mode regressing from 0600 to
  # world-readable on republish/rerun, reproducing the macOS acceptance find.
  old_umask="$(umask)"
  umask 022
  write_install_metadata \
    "$global_dir" "$latest_meta_home" "$bin_dir" "https://example.test/repo.git" \
    "main" "main" "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" "dev" \
    "$bin_dir/lingtai-tui" ""
  umask "$old_umask"
  second_commit="$(python3 -c 'import json; print(json.load(open("'"$global_dir"'/install.json"))["resolved_commit"])')"
  assert_eq "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" "$second_commit" \
    "a second --latest run must republish the receipt in place recording the new commit, not refuse (Opus Blocker 2)"
  perm_latest="$(python3 -c 'import os,stat,sys; print(oct(stat.S_IMODE(os.stat(sys.argv[1]).st_mode)))' "$global_dir/install.json")"
  assert_eq "0o600" "$perm_latest" \
    "a second --latest republish under umask 022 must still leave install.json at mode 600, not regress to 0644"
  LATEST_MAIN_MODE=0
  UPDATE_MODE=0
)

# --- ordinary fresh-install guards remain green after the --latest exemption -

(
  regress_home="$tmp/home-regress-check"
  bin_dir="$regress_home/bin"
  mkdir -p "$bin_dir"
  HOME="$regress_home"
  BIN_DIR="$bin_dir"
  UPDATE_MODE=0
  LATEST_MAIN_MODE=0

  validate_install_target || fail "ordinary install target validation must still pass for a genuinely empty bin dir after the --latest exemption"
  validate_fresh_install_state || fail "ordinary fresh-install-state validation must still pass with no prior receipt/runtime after the --latest exemption"

  global_dir="$regress_home/.lingtai-tui"
  RUNTIME_VENV_DIR=""
  KERNEL_SOURCE=""
  write_install_metadata \
    "$global_dir" "$regress_home" "$bin_dir" "https://example.test/repo.git" \
    "v1.0.0" "v1.0.0" "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" "v1.0.0" \
    "$bin_dir/lingtai-tui" ""
  set +e
  write_install_metadata \
    "$global_dir" "$regress_home" "$bin_dir" "https://example.test/repo.git" \
    "v2.0.0" "v2.0.0" "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" "v2.0.0" \
    "$bin_dir/lingtai-tui" "" 2>/dev/null
  rc=$?
  set -e
  [[ "$rc" != "0" ]] || fail "ordinary (non-update, non-latest) fresh install must still refuse to clobber an existing receipt after the --latest exemption"
)

# --- update_validate_manifest: strict rejection of a malformed manifest -----

(
  manifest_dir="$tmp/manifest-validate"
  mkdir -p "$manifest_dir"

  valid_manifest="$manifest_dir/valid.json"
  cat > "$valid_manifest" <<'EOF'
{"schema":"lingtai.kernel.release/v1","kernel_version":"1.2.3","kernel_tag":"v1.2.3","commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generated_at":"2026-07-15T00:00:00Z","sdist_fallback":"lingtai-1.2.3.tar.gz","artifacts":[{"filename":"lingtai-1.2.3.tar.gz","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","kind":"sdist","python_tag":null,"abi_tag":null,"platform_tag":null}]}
EOF
  update_validate_manifest python3 "$valid_manifest" v1.2.3 >/dev/null ||
    fail "update_validate_manifest should accept a genuinely well-formed manifest"

  # Old canonical behavior only grepped for the schema substring anywhere in
  # the body; this manifest contains that exact substring but is otherwise
  # missing every other required field and would have passed the old check.
  weak_manifest="$manifest_dir/weak.json"
  printf '{"schema":"lingtai.kernel.release/v1","kernel_version":"1.2.3","artifacts":[]}' > "$weak_manifest"
  if update_validate_manifest python3 "$weak_manifest" v1.2.3 >/dev/null 2>&1; then
    fail "update_validate_manifest must reject a manifest missing required keys, even though it contains the schema substring"
  fi

  # Mismatched requested tag/version must be rejected.
  if update_validate_manifest python3 "$valid_manifest" v9.9.9 >/dev/null 2>&1; then
    fail "update_validate_manifest must reject a manifest for a different tag than requested"
  fi
)

# --- kernel-release-pin resolution: fetch_kernel_pin / kernel_tag_for_install /
# --- kernel_source_for_install, end to end with a real HTTP-less fixture ----

(
  pin_home="$tmp/home-kernel-pin"
  mkdir -p "$pin_home"
  BUNDLE_MANIFEST_JSON=""
  KERNEL_PIN_JSON=""
  KERNEL_PIN_TAG=""
  KERNEL_PIN_PROVIDER=""
  KERNEL_PIN_TUI_TAG=""
  BUNDLE_PROVIDER="github"

  kernel_pin_url_for_provider() {
    [[ "$1" == "github" ]] && printf 'https://example.invalid/%s/kernel-release.json' "$2"
  }
  curl() {
    printf '%s' '{"schema":"lingtai.tui.kernel-pin/v1","kernel_tag":"v0.19.0","comment":"pinned for this TUI release"}'
  }

  fetch_kernel_pin "v1.5.0" || fail "fetch_kernel_pin should resolve a well-formed pin"
  assert_eq "v0.19.0" "$KERNEL_PIN_TAG" "fetch_kernel_pin populates KERNEL_PIN_TAG"
  assert_eq "github" "$KERNEL_PIN_PROVIDER" "fetch_kernel_pin records the resolving provider"
  assert_eq "v1.5.0" "$KERNEL_PIN_TUI_TAG" "fetch_kernel_pin records the exact TUI tag it was resolved for"

  # With no bundle manifest, kernel_tag_for_install/kernel_source_for_install
  # must fall through to the resolved pin.
  assert_eq "v0.19.0" "$(kernel_tag_for_install)" "kernel_tag_for_install falls back to the release pin when no bundle exists"
  assert_eq "release-pin" "$(kernel_source_for_install)" "kernel_source_for_install reports release-pin when only a pin was resolved"

  # A bundle manifest, when present, still wins over the pin (bundle stays
  # first-priority).
  BUNDLE_MANIFEST_JSON='{"fake":"bundle"}'
  BUNDLE_MANIFEST_KERNEL_TAG="v2.0.0"
  assert_eq "v2.0.0" "$(kernel_tag_for_install)" "kernel_tag_for_install prefers an existing bundle manifest over the pin"
  assert_eq "bundle" "$(kernel_source_for_install)" "kernel_source_for_install reports bundle when a bundle manifest exists"
  BUNDLE_MANIFEST_JSON=""
)

echo "test-install-sh-hardening: all real integrated-capability checks passed"
