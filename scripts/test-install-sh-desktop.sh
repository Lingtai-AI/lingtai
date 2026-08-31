#!/usr/bin/env bash
# Offline fake-HOME journey for install.sh's macOS Desktop orchestration.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_SH="$ROOT_DIR/install.sh"
TEST_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-install-desktop-test.XXXXXX")"
cleanup() {
  if [[ "${KEEP_TEST_ROOT:-0}" == "1" ]]; then
    echo "kept test root: $TEST_ROOT" >&2
  else
    rm -rf "$TEST_ROOT"
  fi
}
trap cleanup EXIT

fail() {
  echo "FAIL: $*" >&2
  exit 1
}

assert_file() {
  [[ -f "$1" ]] || fail "$2: missing $1"
}

assert_absent() {
  [[ ! -e "$1" && ! -L "$1" ]] || fail "$2: unexpected $1"
}

file_mode() {
  if [[ "$(uname -s)" == "Darwin" ]]; then
    stat -f '%Lp' "$1"
  else
    stat -c '%a' "$1"
  fi
}

file_sha256() {
  shasum -a 256 "$1" | awk '{print $1}'
}

assert_file_unchanged() {
  local path="$1" expected_sha="$2" expected_mode="$3" label="$4"
  [[ "$(file_sha256 "$path")" == "$expected_sha" ]] \
    || fail "$label bytes changed"
  [[ "$(file_mode "$path")" == "$expected_mode" ]] \
    || fail "$label mode changed"
}

FIXTURES="$TEST_ROOT/fixtures"
mkdir -p "$FIXTURES"

cat > "$FIXTURES/install-macos-app.py" <<'PY'
#!/usr/bin/env python3
from desktop_user_cli import bootstrap_main

if __name__ == "__main__":
    raise SystemExit(bootstrap_main())
PY

cat > "$FIXTURES/desktop_user_cli.py" <<'PY'
import os
import subprocess
import sys
from pathlib import Path

def bootstrap_main(argv=None):
    expected = os.environ["EXPECTED_DESKTOP_VERSION"]
    arguments = list(sys.argv[1:] if argv is None else argv)
    if arguments != ["--version", expected]:
        raise SystemExit("unexpected Desktop bootstrap argv")
    support = Path(__file__).parent
    verifier = support / "verify-app-archive.py"
    if not verifier.is_file():
        raise SystemExit("independent verifier was not bootstrapped")
    downloads = support / "downloads"
    downloads.mkdir()
    archive = downloads / f"LingTai-{expected}-macOS-universal.app.tar.gz"
    manifest = downloads / f"LingTai-{expected}-macOS-universal.app.manifest.json"
    routes = [
        ("https://api.github.com/repos/Lingtai-AI/lingtai-desktop/releases/"
         f"tags/v{expected}", downloads / "release.json"),
        ("https://github.com/Lingtai-AI/lingtai-desktop/releases/download/"
         f"v{expected}/{archive.name}", archive),
        ("https://github.com/Lingtai-AI/lingtai-desktop/releases/download/"
         f"v{expected}/{manifest.name}", manifest),
    ]
    for url, destination in routes:
        result = subprocess.run(
            ["curl", "-fsSL", "--max-time", "30", "-o", str(destination), url],
            check=False,
        )
        if result.returncode:
            return result.returncode
    result = subprocess.run([sys.executable, str(verifier), str(archive), str(manifest)], check=False)
    if result.returncode:
        return result.returncode
    home = Path(os.environ["HOME"])
    root = home / ".local/share/lingtai-desktop"
    version = root / "versions" / expected
    executable = version / "LingTai.app/Contents/MacOS/LingTai"
    executable.parent.mkdir(parents=True)
    executable.write_text("fixture App executable\n", encoding="utf-8")
    executable.chmod(0o755)
    receipts = root / "receipts"
    receipts.mkdir(parents=True)
    (receipts / f"{expected}.json").write_text('{"fixture":true}\n', encoding="utf-8")
    (root / "cli").mkdir()
    os.symlink(f"versions/{expected}", root / "current")
    launcher = home / ".local/bin/lingtai-desktop"
    launcher.parent.mkdir(parents=True, exist_ok=True)
    launcher.write_text(
        '#!/usr/bin/env bash\nprintf \'%s\\n\' "$*" >> "${DESKTOP_COMMAND_LOG:?}"\n',
        encoding="utf-8",
    )
    launcher.chmod(0o755)
    print(f"installed LingTai Desktop {expected}")
    return 0
PY

cat > "$FIXTURES/verify-app-archive.py" <<'PY'
#!/usr/bin/env python3
import sys
from pathlib import Path

archive, manifest = map(Path, sys.argv[1:])
if archive.read_bytes() != b"fixture Desktop archive\n":
    raise SystemExit("archive fixture mismatch")
if manifest.read_bytes() != b"fixture Desktop manifest\n":
    raise SystemExit("manifest fixture mismatch")
PY

FIXTURE_INSTALLER_SHA256="$(shasum -a 256 "$FIXTURES/install-macos-app.py" | awk '{print $1}')"
FIXTURE_CLI_SHA256="$(shasum -a 256 "$FIXTURES/desktop_user_cli.py" | awk '{print $1}')"
FIXTURE_VERIFIER_SHA256="$(shasum -a 256 "$FIXTURES/verify-app-archive.py" | awk '{print $1}')"

cat > "$TEST_ROOT/fake-curl" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
destination=""
url=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) destination="$2"; shift 2 ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
[[ -n "$destination" && -n "$url" ]] || {
  echo "unexpected curl argv" >&2
  exit 64
}
printf '%s\n' "$url" >> "$CURL_LOG"
[[ "${CURL_FAIL:-0}" != "1" ]] || exit 22
file="${url##*/}"
case "$url" in
  "https://raw.githubusercontent.com/Lingtai-AI/lingtai-desktop/v${EXPECTED_DESKTOP_VERSION}/scripts/"*)
    case "$file" in
      install-macos-app.py|desktop_user_cli.py|verify-app-archive.py) ;;
      *) echo "unexpected Desktop support file: $file" >&2; exit 66 ;;
    esac
    cp "$FIXTURE_DIR/$file" "$destination"
    ;;
  "https://api.github.com/repos/Lingtai-AI/lingtai-desktop/releases/tags/v${EXPECTED_DESKTOP_VERSION}")
    [[ "${CURL_FAIL:-0}" != "release" ]] || exit 22
    printf '{}\n' > "$destination"
    ;;
  *"/LingTai-${EXPECTED_DESKTOP_VERSION}-macOS-universal.app.tar.gz")
    printf 'fixture Desktop archive\n' > "$destination"
    ;;
  *"/LingTai-${EXPECTED_DESKTOP_VERSION}-macOS-universal.app.manifest.json")
    printf 'fixture Desktop manifest\n' > "$destination"
    ;;
  *) echo "unexpected network route: $url" >&2; exit 65 ;;
esac
SH
chmod +x "$TEST_ROOT/fake-curl"

invoke_case() (
  case_root="$1"
  platform="$2"
  curl_mode="$3"
  shift 3

  mkdir -p "$case_root/home" "$case_root/tmp" "$case_root/fakebin"
  cp "$TEST_ROOT/fake-curl" "$case_root/fakebin/curl"
  export HOME="$case_root/home"
  export TMPDIR="$case_root/tmp"
  export PATH="$case_root/fakebin:$PATH"
  export GOPROXY="offline-fixture"
  export FIXTURE_DIR="$FIXTURES"
  export CURL_LOG="$case_root/curl.log"
  export CURL_FAIL="$curl_mode"
  export EXPECTED_DESKTOP_VERSION="0.1.9"
  export LINGTAI_INSTALL_SH_SOURCE_ONLY=1
  # shellcheck source=../install.sh
  source "$INSTALL_SH"
  # Lock the production trust set before source-only tests replace its hashes
  # with the local installer-support fixture hashes.
  [[ "$DESKTOP_VERSION" == "0.1.9" ]] \
    || fail "production Desktop version pin drifted"
  [[ "$DESKTOP_INSTALLER_SHA256" == "d915162c41b144fad19cd47405c36ceb5f408ca15fabd342d3b3615c53f654c9" ]] \
    || fail "production Desktop installer pin drifted"
  [[ "$DESKTOP_CLI_SHA256" == "0a681eacdf71daea137089e68204b780f6e065184689d9b56208a67c24facc95" ]] \
    || fail "production Desktop CLI pin drifted"
  [[ "$DESKTOP_VERIFIER_SHA256" == "5496bbfaa6c5cb4b7744b6e65d799c43acb4942129a6be8e8509a7a36eb9b900" ]] \
    || fail "production Desktop verifier pin drifted"
  DESKTOP_INSTALLER_SHA256="$FIXTURE_INSTALLER_SHA256"
  DESKTOP_CLI_SHA256="$FIXTURE_CLI_SHA256"
  DESKTOP_VERIFIER_SHA256="$FIXTURE_VERIFIER_SHA256"

  detect_os() { printf '%s\n' "$platform"; }
  resolve_source_provider() { BUNDLE_PROVIDER="github"; }
  fetch_bundle_manifest() {
    BUNDLE_TAG="v9.9.9"
    BUNDLE_MANIFEST_JSON='offline-fixture'
    return 0
  }
  bundle_manifest_field() { printf '%s\n' "v1.2.3"; }
  try_release_asset() {
    local tag="$1"
    mkdir -p "$BIN_DIR"
    cat > "$BIN_DIR/lingtai-tui" <<'BIN'
#!/usr/bin/env bash
echo "lingtai-tui v9.9.9"
BIN
    cat > "$BIN_DIR/lingtai-portal" <<'BIN'
#!/usr/bin/env bash
echo "lingtai-portal v9.9.9"
BIN
    chmod 755 "$BIN_DIR/lingtai-tui" "$BIN_DIR/lingtai-portal"
    ln -s "$BIN_DIR/lingtai-tui" "$BIN_DIR/lingtai"
    VERSION="$tag"
    RESOLVED_REF="$tag"
    RESOLVED_COMMIT=""
    INSTALL_KIND="release-asset"
    PORTAL_PATH="$BIN_DIR/lingtai-portal"
  }
  ensure_runtime_venv() {
    RUNTIME_VENV_DIR="$HOME/.lingtai-tui/runtime/venv"
    mkdir -p "$RUNTIME_VENV_DIR"
    KERNEL_SOURCE="bundle"
    KERNEL_BUNDLE_ID="offline-bundle"
    KERNEL_VERSION_INSTALLED="1.2.3"
    KERNEL_PROVIDER="github"
  }

  main --version v9.9.9 --bin-dir "$HOME/.local/bin" --non-interactive "$@"
)

run_desktop_command() (
  case_root="$1"
  curl_mode="$2"
  shift 2
  export HOME="$case_root/home"
  export TMPDIR="$case_root/tmp"
  export PATH="$case_root/fakebin:$PATH"
  export FIXTURE_DIR="$FIXTURES"
  export CURL_LOG="$case_root/curl.log"
  export CURL_FAIL="$curl_mode"
  export EXPECTED_DESKTOP_VERSION="0.1.9"
  export DESKTOP_COMMAND_LOG="$case_root/command.log"
  "$case_root/home/.local/bin/lingtai-desktop" "$@"
)

check_tui_portal_receipt() {
  local case_root="$1"
  assert_file "$case_root/home/.local/bin/lingtai-tui" "TUI install"
  assert_file "$case_root/home/.local/bin/lingtai-portal" "Portal install"
  assert_file "$case_root/home/.lingtai-tui/install.json" "install receipt"
  python3 - "$case_root/home/.lingtai-tui/install.json" "$case_root/home/.local/bin" <<'PY'
import json
import os
import sys
from pathlib import Path

receipt = json.loads(Path(sys.argv[1]).read_text(encoding="utf-8"))
bin_dir = Path(sys.argv[2])
assert receipt["stamped_version"] == "v9.9.9"
assert [os.path.normpath(value) for value in receipt["managed_binaries"]] == [
    os.path.normpath(bin_dir / "lingtai-tui"),
    os.path.normpath(bin_dir / "lingtai-portal"),
]
assert receipt["kernel_source"] == "bundle"
PY
}

# Registration accepts an already-complete official Desktop launcher without
# touching it or consulting Desktop transport. This is the Desktop-first path.
official="$TEST_ROOT/existing-official"
official_target="$official/home/.local/bin/lingtai-desktop"
official_app="$official/home/.local/share/lingtai-desktop/current/LingTai.app/Contents/MacOS/LingTai"
mkdir -p "$(dirname "$official_target")" "$(dirname "$official_app")"
printf '#!/usr/bin/env bash\n# lingtai-desktop-owned-v1\necho official\n' > "$official_target"
printf '#!/usr/bin/env bash\necho app\n' > "$official_app"
chmod 751 "$official_target"
chmod 755 "$official_app"
official_sha="$(file_sha256 "$official_target")"
official_mode="$(file_mode "$official_target")"
invoke_case "$official" darwin 1 > "$official.out" 2>&1
check_tui_portal_receipt "$official"
assert_file_unchanged "$official_target" "$official_sha" "$official_mode" "official Desktop launcher"
assert_absent "$official/curl.log" "existing official Desktop transport"
grep -q 'Existing complete LingTai Desktop command is already installed' "$official.out" \
  || fail "existing official Desktop launcher did not produce the truthful keep note"

# A lazy command deliberately left behind by remove.sh is likewise recognized
# by this installer's marker and preserved byte-for-byte.
orphan="$TEST_ROOT/existing-lazy"
orphan_target="$orphan/home/.local/bin/lingtai-desktop"
mkdir -p "$(dirname "$orphan_target")"
printf '#!/usr/bin/env python3\n# lingtai-desktop-lazy-bootstrap-v1\nprint("orphan")\n' > "$orphan_target"
chmod 710 "$orphan_target"
orphan_sha="$(file_sha256 "$orphan_target")"
orphan_mode="$(file_mode "$orphan_target")"
invoke_case "$orphan" darwin 1 > "$orphan.out" 2>&1
check_tui_portal_receipt "$orphan"
assert_file_unchanged "$orphan_target" "$orphan_sha" "$orphan_mode" "existing lazy Desktop command"
assert_absent "$orphan/curl.log" "existing lazy Desktop transport"
grep -q 'Existing LingTai Desktop lazy command is already registered' "$orphan.out" \
  || fail "existing lazy Desktop command did not produce the truthful keep note"

# A marker does not make a command usable when the selected target itself is
# non-executable. Official and lazy variants both remain strict no-overwrite
# failures, even when the official App is otherwise complete.
nonexec_official="$TEST_ROOT/nonexec-official"
nonexec_official_target="$nonexec_official/home/.local/bin/lingtai-desktop"
nonexec_official_app="$nonexec_official/home/.local/share/lingtai-desktop/current/LingTai.app/Contents/MacOS/LingTai"
mkdir -p "$(dirname "$nonexec_official_target")" "$(dirname "$nonexec_official_app")"
printf '#!/usr/bin/env bash\n# lingtai-desktop-owned-v1\necho nonexec\n' > "$nonexec_official_target"
printf '#!/usr/bin/env bash\necho app\n' > "$nonexec_official_app"
chmod 640 "$nonexec_official_target"
chmod 755 "$nonexec_official_app"
nonexec_official_sha="$(file_sha256 "$nonexec_official_target")"
nonexec_official_mode="$(file_mode "$nonexec_official_target")"
set +e
invoke_case "$nonexec_official" darwin 1 > "$nonexec_official.out" 2>&1
nonexec_official_rc=$?
set -e
[[ "$nonexec_official_rc" != "0" ]] || fail "non-executable official Desktop target must fail registration"
check_tui_portal_receipt "$nonexec_official"
assert_file_unchanged "$nonexec_official_target" "$nonexec_official_sha" "$nonexec_official_mode" "non-executable official Desktop target"
assert_absent "$nonexec_official/curl.log" "non-executable official Desktop transport"

nonexec_lazy="$TEST_ROOT/nonexec-lazy"
nonexec_lazy_target="$nonexec_lazy/home/.local/bin/lingtai-desktop"
mkdir -p "$(dirname "$nonexec_lazy_target")"
printf '#!/usr/bin/env python3\n# lingtai-desktop-lazy-bootstrap-v1\nprint("nonexec")\n' > "$nonexec_lazy_target"
chmod 640 "$nonexec_lazy_target"
nonexec_lazy_sha="$(file_sha256 "$nonexec_lazy_target")"
nonexec_lazy_mode="$(file_mode "$nonexec_lazy_target")"
set +e
invoke_case "$nonexec_lazy" darwin 1 > "$nonexec_lazy.out" 2>&1
nonexec_lazy_rc=$?
set -e
[[ "$nonexec_lazy_rc" != "0" ]] || fail "non-executable lazy Desktop target must fail registration"
check_tui_portal_receipt "$nonexec_lazy"
assert_file_unchanged "$nonexec_lazy_target" "$nonexec_lazy_sha" "$nonexec_lazy_mode" "non-executable lazy Desktop target"
assert_absent "$nonexec_lazy/curl.log" "non-executable lazy Desktop transport"

# Ownership remains strict for arbitrary, incomplete, and symlink targets.
foreign="$TEST_ROOT/existing-foreign"
foreign_target="$foreign/home/.local/bin/lingtai-desktop"
mkdir -p "$(dirname "$foreign_target")"
printf '#!/usr/bin/env bash\necho foreign\n' > "$foreign_target"
chmod 744 "$foreign_target"
foreign_sha="$(file_sha256 "$foreign_target")"
foreign_mode="$(file_mode "$foreign_target")"
set +e
invoke_case "$foreign" darwin 1 > "$foreign.out" 2>&1
foreign_rc=$?
set -e
[[ "$foreign_rc" != "0" ]] || fail "foreign Desktop target must fail registration"
check_tui_portal_receipt "$foreign"
assert_file_unchanged "$foreign_target" "$foreign_sha" "$foreign_mode" "foreign Desktop target"
assert_absent "$foreign/curl.log" "foreign-target Desktop transport"

incomplete="$TEST_ROOT/existing-incomplete-official"
incomplete_target="$incomplete/home/.local/bin/lingtai-desktop"
mkdir -p "$(dirname "$incomplete_target")"
printf '#!/usr/bin/env bash\n# lingtai-desktop-owned-v1\n# lingtai-desktop-lazy-bootstrap-v1\necho incomplete\n' > "$incomplete_target"
chmod 700 "$incomplete_target"
incomplete_sha="$(file_sha256 "$incomplete_target")"
incomplete_mode="$(file_mode "$incomplete_target")"
set +e
invoke_case "$incomplete" darwin 1 > "$incomplete.out" 2>&1
incomplete_rc=$?
set -e
[[ "$incomplete_rc" != "0" ]] || fail "incomplete official Desktop target must fail registration"
check_tui_portal_receipt "$incomplete"
assert_file_unchanged "$incomplete_target" "$incomplete_sha" "$incomplete_mode" "incomplete official Desktop target"
assert_absent "$incomplete/curl.log" "incomplete-official Desktop transport"

symlink_case="$TEST_ROOT/existing-symlink"
symlink_target="$symlink_case/home/.local/bin/lingtai-desktop"
symlink_backing="$symlink_case/foreign-launcher"
mkdir -p "$(dirname "$symlink_target")"
printf '#!/usr/bin/env bash\n# lingtai-desktop-lazy-bootstrap-v1\necho linked\n' > "$symlink_backing"
chmod 755 "$symlink_backing"
ln -s "$symlink_backing" "$symlink_target"
symlink_sha="$(file_sha256 "$symlink_backing")"
symlink_mode="$(file_mode "$symlink_backing")"
set +e
invoke_case "$symlink_case" darwin 1 > "$symlink_case.out" 2>&1
symlink_rc=$?
set -e
[[ "$symlink_rc" != "0" ]] || fail "symlink Desktop target must fail registration"
check_tui_portal_receipt "$symlink_case"
[[ -L "$symlink_target" && "$(readlink "$symlink_target")" == "$symlink_backing" ]] \
  || fail "symlink Desktop target changed"
assert_file_unchanged "$symlink_backing" "$symlink_sha" "$symlink_mode" "symlink backing target"
assert_absent "$symlink_case/curl.log" "symlink-target Desktop transport"

# macOS default: the LingTai install registers only the lazy command. It must
# perform zero Desktop reads and publish zero Desktop managed App state.
success="$TEST_ROOT/default-success"
invoke_case "$success" darwin 0 > "$success.out" 2>&1
check_tui_portal_receipt "$success"
assert_file "$success/home/.local/bin/lingtai-desktop" "Desktop launcher"
grep -q 'lingtai-desktop-lazy-bootstrap-v1' "$success/home/.local/bin/lingtai-desktop" \
  || fail "main install did not register the lazy command"
assert_absent "$success/curl.log" "main-install Desktop transport"
assert_absent "$success/home/.local/share/lingtai-desktop" "main-install Desktop managed state"

# First command execution obtains the three pinned support files, then the
# fixture installer performs release API/archive/manifest reads, invokes its
# independent verifier, atomically publishes managed state, and continues the
# requested doctor command. The bootstrap occupies Desktop's future launcher
# path here, so this also proves the ownership hand-off/replacement path.
run_desktop_command "$success" 0 doctor > "$success.first.out" 2>&1
assert_file "$success/home/.local/share/lingtai-desktop/versions/0.1.9/LingTai.app/Contents/MacOS/LingTai" "Desktop App"
assert_file "$success/home/.local/share/lingtai-desktop/receipts/0.1.9.json" "Desktop receipt"
[[ -L "$success/home/.local/share/lingtai-desktop/current" ]] \
  || fail "Desktop current link was not published"
[[ "$(cat "$success/command.log")" == "doctor" ]] \
  || fail "first invocation did not continue the requested command"
[[ "$(wc -l < "$success/curl.log" | tr -d ' ')" == "6" ]] \
  || fail "first invocation did not make exactly three support and three release fixture reads"
grep -qx 'https://raw.githubusercontent.com/Lingtai-AI/lingtai-desktop/v0.1.9/scripts/install-macos-app.py' "$success/curl.log"
grep -qx 'https://raw.githubusercontent.com/Lingtai-AI/lingtai-desktop/v0.1.9/scripts/desktop_user_cli.py' "$success/curl.log"
grep -qx 'https://raw.githubusercontent.com/Lingtai-AI/lingtai-desktop/v0.1.9/scripts/verify-app-archive.py' "$success/curl.log"
grep -qx 'https://api.github.com/repos/Lingtai-AI/lingtai-desktop/releases/tags/v0.1.9' "$success/curl.log"
grep -qx 'https://github.com/Lingtai-AI/lingtai-desktop/releases/download/v0.1.9/LingTai-0.1.9-macOS-universal.app.tar.gz' "$success/curl.log"
grep -qx 'https://github.com/Lingtai-AI/lingtai-desktop/releases/download/v0.1.9/LingTai-0.1.9-macOS-universal.app.manifest.json' "$success/curl.log"
[[ -z "$(find "$success/tmp" -maxdepth 1 -name 'lingtai-desktop-bootstrap-*' -print -quit)" ]] \
  || fail "successful first invocation leaked its installer-support temporary directory"

# The installed real command now owns the launcher. A second invocation is
# current/idempotent: it continues immediately with no support or release reads.
reads_after_first="$(wc -l < "$success/curl.log" | tr -d ' ')"
run_desktop_command "$success" 0 version > "$success.second.out" 2>&1
[[ "$(wc -l < "$success/curl.log" | tr -d ' ')" == "$reads_after_first" ]] \
  || fail "second invocation unexpectedly repeated Desktop network reads"
[[ "$(tail -n 1 "$success/command.log")" == "version" ]] \
  || fail "second invocation did not use the installed current command"

# Explicit opt-out: the existing TUI/Portal journey remains successful and the
# Desktop transport is never consulted.
opt_out="$TEST_ROOT/opt-out"
invoke_case "$opt_out" darwin 1 --skip-desktop > "$opt_out.out" 2>&1
check_tui_portal_receipt "$opt_out"
assert_absent "$opt_out/home/.local/share/lingtai-desktop" "Desktop opt-out"
assert_absent "$opt_out/home/.local/bin/lingtai-desktop" "Desktop opt-out command"
assert_absent "$opt_out/curl.log" "Desktop opt-out transport"

# Installer support unavailable on first command execution: main installation
# is still successful, the command stays retryable, and no Desktop state exists.
failure="$TEST_ROOT/bootstrap-failure"
invoke_case "$failure" darwin 1 > "$failure.out" 2>&1
check_tui_portal_receipt "$failure"
assert_file "$failure/home/.local/bin/lingtai-desktop" "retryable Desktop bootstrap"
assert_absent "$failure/curl.log" "main-install failed-route transport"
set +e
run_desktop_command "$failure" 1 doctor > "$failure.first.out" 2>&1
failure_rc=$?
set -e
[[ "$failure_rc" != "0" ]] || fail "unavailable first-execution support must fail clearly"
assert_absent "$failure/home/.local/share/lingtai-desktop" "failed Desktop bootstrap"
grep -q 'retry after confirming access to the public release' "$failure.first.out" \
  || fail "failure output did not state the retryable public-release access requirement"
grep -q 'lingtai-desktop-lazy-bootstrap-v1' "$failure/home/.local/bin/lingtai-desktop" \
  || fail "failed first invocation did not preserve the retryable command"

# A failure after installer support is verified but before archive publication
# also restores the bootstrap when it shared Desktop's future launcher path.
release_failure="$TEST_ROOT/release-failure"
invoke_case "$release_failure" darwin release > "$release_failure.out" 2>&1
check_tui_portal_receipt "$release_failure"
assert_absent "$release_failure/curl.log" "main-install release-failure transport"
set +e
run_desktop_command "$release_failure" release doctor > "$release_failure.first.out" 2>&1
release_failure_rc=$?
set -e
[[ "$release_failure_rc" != "0" ]] || fail "unavailable release fixture must fail clearly"
assert_absent "$release_failure/home/.local/share/lingtai-desktop" "failed release Desktop state"
grep -q 'lingtai-desktop-lazy-bootstrap-v1' "$release_failure/home/.local/bin/lingtai-desktop" \
  || fail "release failure did not restore the retryable command"
[[ "$(wc -l < "$release_failure/curl.log" | tr -d ' ')" == "4" ]] \
  || fail "release failure did not stop after support plus release API fixtures"

# Linux: byte-for-byte control flow beyond our fixture overrides stays on the
# pre-existing TUI/Portal/runtime path and never consults Desktop transport.
linux="$TEST_ROOT/linux-unchanged"
invoke_case "$linux" linux 1 > "$linux.out" 2>&1
check_tui_portal_receipt "$linux"
assert_absent "$linux/home/.local/share/lingtai-desktop" "Linux Desktop state"
assert_absent "$linux/home/.local/bin/lingtai-desktop" "Linux Desktop command"
assert_absent "$linux/curl.log" "Linux Desktop transport"

echo "install.sh Desktop fake-HOME journey passed"
