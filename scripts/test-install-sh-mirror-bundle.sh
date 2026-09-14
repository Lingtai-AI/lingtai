#!/usr/bin/env bash
# Historical filename retained for callers. This suite now covers the
# decoupled lingtai.ai TUI source and kernel release journeys; no bundle or
# kernel-pin contract remains.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export LINGTAI_INSTALL_SH_SOURCE_ONLY=1
# shellcheck source=../install.sh
source "$ROOT_DIR/install.sh"
unset LINGTAI_INSTALL_SH_SOURCE_ONLY

fail() { echo "test-install-sh-mirror-bundle: $*" >&2; exit 1; }
assert_eq() {
  local want="$1" got="$2" label="$3"
  [[ "$got" == "$want" ]] || fail "$label: got '$got', want '$want'"
}
command -v python3 >/dev/null || fail "python3 is required"

tmp="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-inst-provider-test.XXXXXX")"
tmp="$(cd "$tmp" && pwd -P)"
trap 'rm -rf "$tmp"' EXIT

# The source path must remain source-only and the kernel path must remain an
# independent release resolver. These checks guard the public control flow in
# addition to the offline byte journey below.
install_text="$(<"$ROOT_DIR/install.sh")"
for required in 'resolve_tui_latest' 'download_tui_source_archive' 'build_from_source' 'install_kernel_from_release' 'KERNEL_REPO_SLUG'; do
  [[ "$install_text" == *"$required"* ]] || fail "install.sh lost required decoupled path '$required'"
done
for retired in 'fetch_bundle_manifest' 'install_kernel_from_bundle' 'kernel_tag_for_install' 'kernel-release.json' 'lingtai.tui.bundle' 'SKIP_PORTAL' 'lingtai-portal'; do
  [[ "$install_text" != *"$retired"* ]] || fail "install.sh retains retired path '$retired'"
done

FAKE_CURL_DIR=""
setup_fake_curl() {
  local bindir="$1"
  mkdir -p "$bindir"
  FAKE_CURL_DIR="$tmp/responses-$RANDOM"
  mkdir -p "$FAKE_CURL_DIR"
  export FAKE_CURL_DIR
  cat > "$bindir/curl" <<'SH'
#!/usr/bin/env bash
url=""; out=""; previous=""
for arg in "$@"; do
  case "$arg" in http://*|https://*) url="$arg" ;; esac
  [[ "$previous" == '-o' ]] && out="$arg"
  previous="$arg"
done
key="$(printf '%s' "$url" | tr -c 'A-Za-z0-9' '_')"
response_dir="${FAKE_CURL_DIR:?}"
response="$response_dir/$key"
status_file="$response.status"
[[ -z "${FAKE_CURL_LOG:-}" ]] || printf '%s\n' "$url" >> "$FAKE_CURL_LOG"
[[ -f "$response" ]] || { echo "fake curl: no response for $url" >&2; exit 22; }
status=0
[[ -f "$status_file" ]] && status="$(<"$status_file")"
[[ "$status" == 0 ]] || exit "$status"
if [[ -n "$out" ]]; then cp "$response" "$out"; else cat "$response"; fi
SH
  chmod +x "$bindir/curl"
}

register_response() {
  local url="$1" file="$2" status="${3:-0}" key
  key="$(printf '%s' "$url" | tr -c 'A-Za-z0-9' '_')"
  cp "$file" "$FAKE_CURL_DIR/$key"
  printf '%s' "$status" > "$FAKE_CURL_DIR/$key.status"
}
register_text() {
  local url="$1" body="$2" status="${3:-0}" file="$tmp/response-$RANDOM"
  printf '%s' "$body" > "$file"
  register_response "$url" "$file" "$status"
}
asset_record() {
  local name="$1" file="$2" sha size
  sha="$(shasum -a 256 "$file" | cut -d' ' -f1)"
  size="$(wc -c < "$file" | tr -d '[:space:]')"
  printf '{"name":"%s","sha256":"%s","size":%s}' "$name" "$sha" "$size"
}

# --- TUI: producer-owned source archive, verified and built locally ----------
(
  case_dir="$tmp/tui-source"
  fakebin="$case_dir/bin"
  mkdir -p "$case_dir/archive-root/lingtai-v1.2.3/tui" "$case_dir/bin"
  setup_fake_curl "$fakebin"
  export PATH="$fakebin:/usr/bin:/bin"
  export FAKE_CURL_LOG="$case_dir/requests.log"
  : > "$FAKE_CURL_LOG"
  LINGTAI_WEB_BASE='https://lingtai.ai'
  tag='v1.2.3'
  source_name="$(tui_source_asset_name "$tag")"
  printf 'module example.test/lingtai\n\ngo 1.26.1\n' > "$case_dir/archive-root/lingtai-$tag/tui/go.mod"
  printf 'source archive bytes\n' > "$case_dir/archive-root/lingtai-$tag/README"
  tar -czf "$case_dir/$source_name" -C "$case_dir/archive-root" "lingtai-$tag"
  source_sha="$(shasum -a 256 "$case_dir/$source_name" | cut -d' ' -f1)"
  source_size="$(wc -c < "$case_dir/$source_name" | tr -d '[:space:]')"
  printf '%s  %s\n' "$source_sha" "$source_name" > "$case_dir/$source_name.sha256"
  latest="$(printf '{"schema":"lingtai.release_mirror.latest/v1","source_repo":"Lingtai-AI/lingtai","release_id":1,"tag":"%s","assets":[%s,%s]}' \
    "$tag" "$(asset_record "$source_name" "$case_dir/$source_name")" \
    "$(asset_record "$source_name.sha256" "$case_dir/$source_name.sha256")")"
  register_text 'https://lingtai.ai/dl/Lingtai-AI/lingtai/latest.json' "$latest"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai/$tag/$source_name" "$case_dir/$source_name"

  SOURCE_ARG=auto; VERSION=''; REF=''; FROM_SOURCE=0; UPDATE_MODE=0; LATEST_MAIN_MODE=0
  MIRROR_TUI_LATEST_JSON=''; MIRROR_TUI_LATEST_TAG=''; TUI_TAG=''; TUI_SOURCE_ASSET=''
  resolve_source_provider
  resolve_tui_latest || fail "TUI latest source metadata should resolve"
  assert_eq mirror "$TUI_PROVIDER" "default TUI provider"
  assert_eq "$tag" "$TUI_TAG" "TUI latest tag"
  assert_eq "$source_name" "$TUI_SOURCE_ASSET" "producer-owned TUI source name"
  downloaded="$case_dir/downloaded.tar.gz"
  download_tui_source_archive "$tag" mirror "$downloaded" || fail "TUI source archive should verify/download"
  cmp "$case_dir/$source_name" "$downloaded" || fail "verified TUI source bytes changed"
  [[ "$(grep -cF 'https://lingtai.ai/dl/Lingtai-AI/lingtai/latest.json' "$FAKE_CURL_LOG")" == 1 ]] || fail "TUI latest metadata route missing"
  ! grep -q 'github.com' "$FAKE_CURL_LOG" || fail "healthy TUI mirror route made a GitHub request"

  bad="$case_dir/bad-source.tar.gz"
  printf 'tampered source\n' > "$bad"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai/$tag/$source_name" "$bad"
  set +e
  mirror_error="$(download_tui_source_archive "$tag" mirror "$case_dir/rejected.tar.gz" 2>&1)"
  rc=$?
  set -e
  if [[ "$rc" == 0 ]]; then
    fail "TUI source archive must fail on declared size/SHA mismatch"
  fi
  [[ "$mirror_error" == *"failed size/SHA256 verification"* ]] || fail "mirror checksum failure should identify the concrete verification failure"
  [[ "$mirror_error" != *"--source github"* ]] || fail "mirror checksum failure must not prescribe a provider"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai/$tag/$source_name" "$case_dir/$source_name"

  # A fake compiler is enough here: the real tar extraction and installer
  # control flow prove that the producer archive is used as local build input.
  cat > "$fakebin/go" <<'SH'
#!/usr/bin/env bash
out=""; previous=""
for arg in "$@"; do
  [[ "$previous" == '-o' ]] && out="$arg"
  previous="$arg"
done
printf 'build\n' > "${TUI_BUILD_LOG:?}"
cat > "$out" <<'BIN'
#!/usr/bin/env bash
echo 'lingtai-tui v1.2.3'
BIN
chmod +x "$out"
SH
  chmod +x "$fakebin/go"
  TUI_BUILD_LOG="$case_dir/build.log"; export TUI_BUILD_LOG
  ensure_build_deps() { :; }
  ensure_go_for_source() { :; }
  BIN_DIR="$case_dir/installed"; BUILD_DIR="$case_dir/build"; UPDATE_MODE=0
  mkdir -p "$BIN_DIR"
  build_from_source "$tag" || fail "default TUI source archive should build locally"
  [[ -x "$BIN_DIR/lingtai-tui" ]] || fail "local TUI build did not install lingtai-tui"
  [[ -L "$BIN_DIR/lingtai" ]] || fail "TUI alias should be retained"
  [[ -s "$TUI_BUILD_LOG" ]] || fail "local source build did not invoke the compiler"
  [[ ! -e "$BIN_DIR/lingtai-portal" ]] || fail "TUI-only source build must not install Portal"
)

# A failed mirror source selection returns the TUI-local fallback status; the
# kernel provider is not touched by this component failure.
(
  TUI_PROVIDER=mirror
  ensure_build_deps() { :; }
  ensure_go_for_source() { :; }
  download_tui_source_archive() { return 1; }
  BUILD_DIR="$tmp/tui-fallback-build"
  set +e
  build_from_source v1.2.3 >/dev/null 2>&1
  rc=$?
  set -e
  assert_eq 2 "$rc" "mirror TUI source failure returns the TUI-local fallback status"
  assert_eq mirror "$TUI_PROVIDER" "TUI-local source failure does not mutate provider state"
)

# --- Kernel: independent latest metadata/artifact and independent fallback ----
(
  case_dir="$tmp/kernel-release"
  fakebin="$case_dir/bin"
  mkdir -p "$case_dir" "$case_dir/bin"
  setup_fake_curl "$fakebin"
  export PATH="$fakebin:/usr/bin:/bin"
  export FAKE_CURL_LOG="$case_dir/requests.log"
  : > "$FAKE_CURL_LOG"
  LINGTAI_WEB_BASE='https://lingtai.ai'
  KERNEL_GH_API_BASE='https://api.github.com/repos/Lingtai-AI/lingtai-kernel'
  kernel_tag='v2.3.4'
  wheel_name='lingtai-2.3.4-cp312-cp312-macosx_11_0_arm64.whl'
  printf 'verified wheel bytes\n' > "$case_dir/$wheel_name"
  wheel_sha="$(shasum -a 256 "$case_dir/$wheel_name" | cut -d' ' -f1)"
  sdist_sha='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'
  manifest="$case_dir/kernel-manifest.json"
  cat > "$manifest" <<EOF
{"schema":"lingtai.kernel.release/v1","kernel_version":"2.3.4","kernel_tag":"$kernel_tag","commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","generated_at":"2026-09-14T00:00:00Z","sdist_fallback":"lingtai-2.3.4.tar.gz","artifacts":[{"filename":"$wheel_name","sha256":"$wheel_sha","kind":"wheel","python_tag":"cp312","abi_tag":"cp312","platform_tag":"macosx_11_0_arm64"},{"filename":"lingtai-2.3.4.tar.gz","sha256":"$sdist_sha","kind":"sdist","python_tag":null,"abi_tag":null,"platform_tag":null}]}
EOF
  kernel_latest="$(printf '{"schema":"lingtai.release_mirror.latest/v1","source_repo":"Lingtai-AI/lingtai-kernel","release_id":2,"tag":"%s","assets":[%s,%s]}' \
    "$kernel_tag" "$(asset_record lingtai-kernel-release-manifest.json "$manifest")" \
    "$(asset_record "$wheel_name" "$case_dir/$wheel_name")")"
  register_text 'https://lingtai.ai/dl/Lingtai-AI/lingtai-kernel/latest.json' "$kernel_latest"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai-kernel/$kernel_tag/lingtai-kernel-release-manifest.json" "$manifest"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai-kernel/$kernel_tag/$wheel_name" "$case_dir/$wheel_name"

  real_python="$(command -v python3)"
  cat > "$fakebin/python" <<EOF
#!/usr/bin/env bash
if [[ "\$1" == '-' && "\$#" == 1 ]]; then
  echo 'cp312-cp312-macosx_11_0_arm64'
  exit 0
fi
if [[ "\$1" == '-' && "\$#" -ge 3 ]]; then
  echo '$wheel_name $wheel_sha'
  exit 0
fi
if [[ "\$1" == '-c' ]]; then
  echo 'lingtai 2.3.4'
  exit 0
fi
exec '$real_python' "\$@"
EOF
  chmod +x "$fakebin/python"
  cat > "$fakebin/uv" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" > "${KERNEL_INSTALL_LOG:?}"
exit 0
SH
  chmod +x "$fakebin/uv"
  KERNEL_INSTALL_LOG="$case_dir/kernel-install.log"; export KERNEL_INSTALL_LOG
  BUILD_DIR="$case_dir/build"; mkdir -p "$BUILD_DIR"
  TUI_PROVIDER=mirror; KERNEL_PROVIDER=''; KERNEL_SOURCE=''; KERNEL_RELEASE_TAG=''; KERNEL_VERSION_INSTALLED=''
  MIRROR_KERNEL_LATEST_JSON=''; MIRROR_KERNEL_LATEST_TAG=''; KERNEL_MANIFEST_JSON=''; KERNEL_MANIFEST_PROVIDER=''
  install_kernel_from_release "$fakebin/python" "$fakebin/uv" || fail "kernel mirror release should install"
  assert_eq release "$KERNEL_SOURCE" "kernel source is a verified release"
  assert_eq "$kernel_tag" "$KERNEL_RELEASE_TAG" "kernel mirror latest tag"
  assert_eq 2.3.4 "$KERNEL_VERSION_INSTALLED" "kernel manifest version"
  assert_eq mirror "$KERNEL_PROVIDER" "kernel mirror provider"
  [[ -s "$KERNEL_INSTALL_LOG" ]] || fail "kernel install did not receive a local artifact"
  ! grep -q 'github.com' "$FAKE_CURL_LOG" || fail "healthy kernel mirror route made a GitHub request"
  grep -qF "https://lingtai.ai/dl/Lingtai-AI/lingtai-kernel/latest.json" "$FAKE_CURL_LOG" || fail "kernel latest metadata route missing"

  # Make only the kernel mirror artifact fail. The fallback uses the kernel
  # GitHub release and leaves the TUI provider untouched.
  printf 'tampered kernel\n' > "$case_dir/bad-wheel"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai-kernel/$kernel_tag/$wheel_name" "$case_dir/bad-wheel"
  register_text "$KERNEL_GH_API_BASE/releases/latest" '{"tag_name":"v2.3.4","assets":[{"name":"lingtai-kernel-release-manifest.json"}]}'
  register_response "https://github.com/Lingtai-AI/lingtai-kernel/releases/download/$kernel_tag/lingtai-kernel-release-manifest.json" "$manifest"
  register_response "https://github.com/Lingtai-AI/lingtai-kernel/releases/download/$kernel_tag/$wheel_name" "$case_dir/$wheel_name"
  : > "$FAKE_CURL_LOG"
  KERNEL_PROVIDER=''; KERNEL_SOURCE=''; KERNEL_RELEASE_TAG=''; KERNEL_VERSION_INSTALLED=''
  MIRROR_KERNEL_LATEST_JSON="$kernel_latest"; MIRROR_KERNEL_LATEST_TAG="$kernel_tag"
  install_kernel_from_release "$fakebin/python" "$fakebin/uv" || fail "kernel GitHub fallback should install"
  assert_eq github "$KERNEL_PROVIDER" "kernel fallback provider"
  assert_eq mirror "$TUI_PROVIDER" "kernel fallback does not change TUI provider"
  grep -qF "https://lingtai.ai/dl/Lingtai-AI/lingtai-kernel/$kernel_tag/$wheel_name" "$FAKE_CURL_LOG" || fail "kernel mirror attempt missing before fallback"
  grep -qF "$KERNEL_GH_API_BASE/releases/latest" "$FAKE_CURL_LOG" || fail "kernel GitHub latest fallback missing"
  grep -q 'https://github.com/Lingtai-AI/lingtai-kernel/releases/download' "$FAKE_CURL_LOG" || fail "kernel GitHub artifact fallback missing"
  ! grep -q 'https://lingtai.ai/dl/Lingtai-AI/lingtai/latest.json' "$FAKE_CURL_LOG" || fail "kernel fallback re-resolved TUI latest"
)

echo "test-install-sh-mirror-bundle: decoupled TUI/kernel provider checks passed"
