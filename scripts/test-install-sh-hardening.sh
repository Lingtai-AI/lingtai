#!/usr/bin/env bash
# Focused real-filesystem checks for install.sh's ownership, receipt, manifest,
# and import-provenance gates. Release/provider routing lives in the historical
# mirror-bundle filename next to this test.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export LINGTAI_INSTALL_SH_SOURCE_ONLY=1
# shellcheck source=../install.sh
source "$ROOT_DIR/install.sh"
unset LINGTAI_INSTALL_SH_SOURCE_ONLY

fail() { echo "test-install-sh-hardening: $*" >&2; exit 1; }
assert_eq() {
  local want="$1" got="$2" label="$3"
  [[ "$got" == "$want" ]] || fail "$label: got '$got', want '$want'"
}

command -v python3 >/dev/null || fail "python3 is required"
tmp="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-inst-hardening-test.XXXXXX")"
tmp="$(cd "$tmp" && pwd -P)"
trap 'rm -rf "$tmp"' EXIT

# Both installer paths must stay TUI-only. These are exact retired surfaces,
# not a ban on the kernel's current release-manifest filename.
for installer in "$ROOT_DIR/install.sh" "$ROOT_DIR/install.ps1"; do
  text="$(<"$installer")"
  for forbidden in 'lingtai-portal' 'SKIP_PORTAL' '--skip-portal' 'lingtai.tui.bundle' 'kernel-release.json'; do
    if printf '%s' "$text" | grep -Fqi -- "$forbidden"; then
      fail "$installer retains retired installer surface '$forbidden'"
    fi
  done
  if printf '%s' "$text" | grep -Eiq '\b(node|npm)\b'; then
    fail "$installer retains a Node/npm installer requirement"
  fi
done

# --- canonical_runtime_venv: physical ownership, not lexical containment ----
(
  fake_home="$tmp/home-containment"
  mkdir -p "$fake_home"
  HOME="$fake_home"
  runtime_root="$HOME/.lingtai-tui/runtime"
  resolved="$(canonical_runtime_venv "$runtime_root/venv" "$runtime_root")" ||
    fail "canonical_runtime_venv should accept a fresh owned child"
  assert_eq "$runtime_root/venv" "$resolved" "fresh runtime venv physical path"

  mkdir -p "$runtime_root" "$tmp/outside"
  ln -s "$tmp/outside" "$runtime_root/escaped-venv"
  if canonical_runtime_venv "$runtime_root/escaped-venv" "$runtime_root" >/dev/null 2>&1; then
    fail "canonical_runtime_venv must reject a venv symlink escaping the runtime root"
  fi

  other_root="$tmp/other-runtime"
  mkdir -p "$other_root" "$tmp/home-containment-2/.lingtai-tui"
  ln -s "$other_root" "$tmp/home-containment-2/.lingtai-tui/runtime"
  HOME="$tmp/home-containment-2"
  if canonical_runtime_venv "$HOME/.lingtai-tui/runtime/venv" "$HOME/.lingtai-tui/runtime" >/dev/null 2>&1; then
    fail "canonical_runtime_venv must reject a symlinked runtime root"
  fi
)

# --- runtime state/import provenance ----------------------------------------
(
  state_dir="$tmp/runtime-state"
  mkdir -p "$state_dir"
  assert_eq missing "$(runtime_venv_state "$state_dir/missing")" "missing runtime state"
  mkdir -p "$state_dir/broken/bin"
  assert_eq broken "$(runtime_venv_state "$state_dir/broken")" "broken runtime state"

  python3 -m venv "$state_dir/healthy"
  assert_eq healthy "$(runtime_venv_state "$state_dir/healthy")" "healthy runtime state"
  python3 -m venv --without-pip "$state_dir/provenance"
  py="$state_dir/provenance/bin/python"
  site_packages="$(PYTHONPATH= "$py" -c 'import sysconfig; print(sysconfig.get_paths()["purelib"])')"
  mkdir -p "$site_packages/lingtai/kernel"
  printf '__version__ = "1.2.3"\n' > "$site_packages/lingtai/__init__.py"
  : > "$site_packages/lingtai/kernel/__init__.py"
  runtime_health_check "$py" 1.2.3 >/dev/null || fail "owned runtime imports should pass provenance check"
  if runtime_health_check "$py" 9.9.9 >/dev/null 2>&1; then
    fail "runtime_health_check must reject a wrong version"
  fi

  external="$tmp/external-shadow"
  mkdir -p "$external/lingtai/kernel"
  printf '__version__ = "1.2.3"\n' > "$external/lingtai/__init__.py"
  : > "$external/lingtai/kernel/__init__.py"
  printf '%s\n' "$external" > "$site_packages/zz_external_shadow.pth"
  rm -rf "$site_packages/lingtai"
  if runtime_health_check "$py" 1.2.3 >/dev/null 2>&1; then
    fail "runtime_health_check must reject a same-version package outside the owned venv"
  fi
)

# --- fresh-install and receipt ownership gates ------------------------------
(
  fake_home="$tmp/home-fresh"
  mkdir -p "$fake_home/bin"
  HOME="$fake_home"
  BIN_DIR="$fake_home/bin"
  SKIP_VENV=0
  validate_install_target || fail "empty install target should be accepted"
  validate_fresh_install_state || fail "empty install state should be accepted"

  touch "$BIN_DIR/lingtai-tui"
  if validate_install_target >/dev/null 2>&1; then fail "existing TUI target must be refused"; fi
  rm -f "$BIN_DIR/lingtai-tui"
  mkdir -p "$fake_home/.lingtai-tui"
  printf '{}\n' > "$fake_home/.lingtai-tui/install.json"
  if validate_fresh_install_state >/dev/null 2>&1; then fail "existing receipt must be refused"; fi
  rm -f "$fake_home/.lingtai-tui/install.json"

  mkdir -p "$fake_home/.lingtai-tui/runtime"
  if validate_fresh_install_state >/dev/null 2>&1; then fail "existing runtime must be refused without skip-python"; fi
  SKIP_VENV=1
  validate_fresh_install_state || fail "skip-python should preserve a real legacy runtime"
  printf sentinel > "$fake_home/.lingtai-tui/runtime/legacy.txt"

  UPDATE_MODE=0; LATEST_MAIN_MODE=0; REINSTALL_OK=0
  RUNTIME_VENV_DIR=""; KERNEL_SOURCE=""
  write_install_metadata "$fake_home/.lingtai-tui" "$fake_home" "$BIN_DIR" \
    'https://example.invalid/lingtai.git' v1.2.3 v1.2.3 \
    aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa v1.2.3 "$BIN_DIR/lingtai-tui"
  python3 - "$fake_home/.lingtai-tui/install.json" <<'PY'
import json, sys
data = json.load(open(sys.argv[1], encoding='utf-8'))
assert data['managed_binaries'][-1].endswith('/lingtai-tui')
assert len(data['managed_binaries']) == 1
assert 'runtime_venv' not in data
assert 'kernel_release_tag' not in data
assert 'tui_provider' not in data or data['tui_provider'] in ('mirror', 'github')
assert 'portal' not in json.dumps(data).lower()
PY
  [[ "$(cat "$fake_home/.lingtai-tui/runtime/legacy.txt")" == sentinel ]] ||
    fail "skip-python receipt publication must not touch the legacy runtime"
)

(
  meta_home="$tmp/home-metadata"
  mkdir -p "$meta_home/bin"
  UPDATE_MODE=0; LATEST_MAIN_MODE=0; REINSTALL_OK=0
  RUNTIME_VENV_DIR=""; KERNEL_SOURCE=""
  write_install_metadata "$meta_home/.lingtai-tui" "$meta_home" "$meta_home/bin" \
    'https://example.invalid/lingtai.git' v1.0.0 v1.0.0 \
    aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa v1.0.0 "$meta_home/bin/lingtai-tui"
  if write_install_metadata "$meta_home/.lingtai-tui" "$meta_home" "$meta_home/bin" \
    'https://example.invalid/lingtai.git' v2.0.0 v2.0.0 \
    bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb v2.0.0 "$meta_home/bin/lingtai-tui" >/dev/null 2>&1; then
    fail "fresh receipt publication must not clobber existing metadata"
  fi
  UPDATE_MODE=1
  KERNEL_SOURCE=release; KERNEL_RELEASE_TAG=v9.9.9; KERNEL_VERSION_INSTALLED=9.9.9; KERNEL_PROVIDER=mirror
  write_install_metadata "$meta_home/.lingtai-tui" "$meta_home" "$meta_home/bin" \
    'https://example.invalid/lingtai.git' v2.0.0 v2.0.0 \
    bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb v2.0.0 "$meta_home/bin/lingtai-tui"
  python3 - "$meta_home/.lingtai-tui/install.json" <<'PY'
import json, sys
data = json.load(open(sys.argv[1], encoding='utf-8'))
assert data['kernel_source'] == 'release'
assert data['kernel_release_tag'] == 'v9.9.9'
assert data['kernel_provider'] == 'mirror'
assert len(data['managed_binaries']) == 1
PY
)

# --- strict kernel release manifest validation ------------------------------
(
  manifest_dir="$tmp/manifests"
  mkdir -p "$manifest_dir"
  cat > "$manifest_dir/valid.json" <<'EOF'
{"schema":"lingtai.kernel.release/v1","kernel_version":"1.2.3","kernel_tag":"v1.2.3","commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generated_at":"2026-09-14T00:00:00Z","sdist_fallback":"lingtai-1.2.3.tar.gz","artifacts":[{"filename":"lingtai-1.2.3.tar.gz","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","kind":"sdist","python_tag":null,"abi_tag":null,"platform_tag":null}]}
EOF
  update_validate_manifest python3 "$manifest_dir/valid.json" v1.2.3 >/dev/null || fail "valid kernel manifest should pass"
  printf '%s' '{"schema":"lingtai.kernel.release/v1","kernel_version":"1.2.3","artifacts":[]}' > "$manifest_dir/weak.json"
  if update_validate_manifest python3 "$manifest_dir/weak.json" v1.2.3 >/dev/null 2>&1; then
    fail "weak kernel manifest must fail strict validation"
  fi
  if update_validate_manifest python3 "$manifest_dir/valid.json" v9.9.9 >/dev/null 2>&1; then
    fail "manifest for another kernel tag must fail validation"
  fi
)

echo "test-install-sh-hardening: focused ownership/manifest checks passed"
