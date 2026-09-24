#!/usr/bin/env bash
# Focused architecture tests for the source-build installer. These tests model
# native Apple Silicon, an x86_64 process under Rosetta, Intel macOS, and Linux
# without changing the host platform or installing anything.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export LINGTAI_INSTALL_SH_SOURCE_ONLY=1
# shellcheck source=../install.sh
source "$ROOT_DIR/install.sh"
unset LINGTAI_INSTALL_SH_SOURCE_ONLY

fail() { echo "test-install-sh-architecture: $*" >&2; exit 1; }
assert_eq() {
  local want="$1" got="$2" label="$3"
  [[ "$got" == "$want" ]] || fail "$label: got '$got', want '$want'"
}

command -v git >/dev/null || fail "git is required"
command -v python3 >/dev/null || fail "python3 is required"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-inst-arch-test.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

mock_bin="$tmp/mock-bin"
mkdir -p "$mock_bin"
cat > "$mock_bin/uname" <<'MOCK'
#!/usr/bin/env bash
case "${1:-}" in
  -s) printf '%s\n' "${MOCK_UNAME_S:?}" ;;
  -m) printf '%s\n' "${MOCK_UNAME_M:?}" ;;
  *) printf '%s\n' "${MOCK_UNAME_M:?}" ;;
esac
MOCK
cat > "$mock_bin/sysctl" <<'MOCK'
#!/usr/bin/env bash
[[ "$*" == "-in hw.optional.arm64" ]] || exit 1
printf '%s\n' "${MOCK_ARM64_CAPABILITY:-0}"
MOCK
cat > "$mock_bin/file" <<'MOCK'
#!/usr/bin/env bash
if [[ "${MOCK_FILE_FAIL:-0}" == "1" ]]; then exit 1; fi
printf '%s\n' "${MOCK_FILE_DESCRIPTION:?}"
MOCK
chmod 755 "$mock_bin"/*

# The physical-host probe must override a Rosetta x86_64 process on Apple
# Silicon, while preserving Intel macOS and ordinary Linux behavior.
(
  export PATH="$mock_bin:$PATH" MOCK_UNAME_S=Darwin MOCK_UNAME_M=arm64 MOCK_ARM64_CAPABILITY=1
  assert_eq arm64 "$(detect_arch)" "native Apple Silicon architecture"
)
(
  export PATH="$mock_bin:$PATH" MOCK_UNAME_S=Darwin MOCK_UNAME_M=x86_64 MOCK_ARM64_CAPABILITY=1
  assert_eq arm64 "$(detect_arch)" "Rosetta physical-host architecture"
)
(
  export PATH="$mock_bin:$PATH" MOCK_UNAME_S=Darwin MOCK_UNAME_M=x86_64 MOCK_ARM64_CAPABILITY=0
  assert_eq amd64 "$(detect_arch)" "Intel macOS architecture"
)
(
  export PATH="$mock_bin:$PATH" MOCK_UNAME_S=Linux MOCK_UNAME_M=aarch64 MOCK_ARM64_CAPABILITY=0
  assert_eq arm64 "$(detect_arch)" "Linux arm64 architecture"
)
(
  export PATH="$mock_bin:$PATH" MOCK_UNAME_S=Linux MOCK_UNAME_M=s390x MOCK_ARM64_CAPABILITY=0
  assert_eq unsupported "$(detect_arch)" "unsupported architecture"
)

# Architecture verification is fail-closed and accepts the platform spellings
# emitted by macOS file(1) and Linux file(1).
probe="$tmp/probe-binary"
printf 'not-a-real-binary\n' > "$probe"
(
  export PATH="$mock_bin:$PATH" MOCK_FILE_DESCRIPTION='Mach-O 64-bit executable arm64'
  verify_tui_binary_arch "$probe" arm64
  if verify_tui_binary_arch "$probe" amd64 >/dev/null 2>&1; then
    fail "arm64 file description must not pass amd64 verification"
  fi
)
(
  export PATH="$mock_bin:$PATH" MOCK_FILE_DESCRIPTION='ELF 64-bit LSB pie executable, x86-64'
  verify_tui_binary_arch "$probe" amd64
  if verify_tui_binary_arch "$probe" arm64 >/dev/null 2>&1; then
    fail "amd64 file description must not pass arm64 verification"
  fi
)
(
  export PATH="$mock_bin:$PATH" MOCK_FILE_DESCRIPTION='unrecognized object format'
  if verify_tui_binary_arch "$probe" arm64 >/dev/null 2>&1; then
    fail "unrecognized file description must fail closed"
  fi
)
(
  export PATH="$mock_bin:$PATH" MOCK_FILE_FAIL=1 MOCK_FILE_DESCRIPTION='ignored'
  if verify_tui_binary_arch "$probe" arm64 >/dev/null 2>&1; then
    fail "file inspection failure must fail closed"
  fi
)

# Exercise the real build_from_source control flow with a local Git fixture and
# a fake Go compiler. The capture proves GOOS/GOARCH are explicit, while the
# second pass proves a mismatched artifact is rejected before replacing the
# existing update target.
fixture="$tmp/fixture"
git init -q "$fixture"
git -C "$fixture" config user.email test@example.invalid
git -C "$fixture" config user.name 'Architecture Test'
mkdir -p "$fixture/tui"
printf 'module example.invalid/lingtai\n\ngo 1.26.1\n' > "$fixture/tui/go.mod"
printf 'package main\n' > "$fixture/tui/main.go"
git -C "$fixture" add tui
git -C "$fixture" commit -qm fixture
git -C "$fixture" branch -M main

capture="$tmp/go-target"
cat > "$mock_bin/go" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
printf '%s/%s\n' "$GOOS" "$GOARCH" > "${GO_TARGET_CAPTURE:?}"
out=''
version='unknown'
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    -ldflags) version="${2#*-X main.version=}"; shift 2 ;;
    *) shift ;;
  esac
done
[[ -n "$out" ]] || { echo 'fake go: missing -o' >&2; exit 1; }
printf '#!/usr/bin/env bash\nprintf "lingtai-tui %%s\\n" %q\n' "$version" > "$out"
chmod 755 "$out"
MOCK
chmod 755 "$mock_bin/go"

(
  export PATH="$mock_bin:$PATH" GO_TARGET_CAPTURE="$capture" MOCK_FILE_DESCRIPTION='Mach-O 64-bit executable arm64'
  detect_os() { printf 'darwin\n'; }
  detect_arch() { printf 'arm64\n'; }
  ensure_build_deps() { :; }
  ensure_go_for_source() { :; }
  REPO="$fixture"
  BUILD_DIR="$tmp/build-good"
  BIN_DIR="$tmp/install/bin"
  mkdir -p "$BIN_DIR"
  UPDATE_MODE=1
  TUI_PROVIDER=github
  build_from_source main
  assert_eq darwin/arm64 "$(cat "$capture")" "explicit source-build target"
  verify_tui_binary_arch "$BIN_DIR/lingtai-tui" arm64
)

sentinel="$tmp/install/bin/lingtai-tui"
printf 'sentinel-before-mismatch\n' > "$sentinel"
(
  export PATH="$mock_bin:$PATH" GO_TARGET_CAPTURE="$capture" MOCK_FILE_DESCRIPTION='Mach-O 64-bit executable x86_64'
  detect_os() { printf 'darwin\n'; }
  detect_arch() { printf 'arm64\n'; }
  ensure_build_deps() { :; }
  ensure_go_for_source() { :; }
  REPO="$fixture"
  BUILD_DIR="$tmp/build-bad"
  BIN_DIR="$tmp/install/bin"
  UPDATE_MODE=1
  TUI_PROVIDER=github
  if build_from_source main >/dev/null 2>&1; then
    fail "mismatched source build must fail"
  fi
)
assert_eq 'sentinel-before-mismatch' "$(cat "$sentinel")" "mismatch is rejected before target replacement"

echo 'test-install-sh-architecture: PASS'
