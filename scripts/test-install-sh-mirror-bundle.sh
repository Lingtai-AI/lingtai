#!/usr/bin/env bash
# Focused tests for the mirror-aware (lingtai.ai download acceleration) bundle
# installer additions to install.sh: --source override validation (including
# the explicit retirement of --source gitee), country detection
# (success/failure/fail-open), mirror response parsing, same-tag/same-bundle
# fallback to GitHub, the third-party dependency index that the final bundle
# provider selects (asserted on the real install command's argv), checksum
# mismatch fail-loud, bundle/kernel manifest schema handling, and kernel wheel
# selection. Kept as a separate file from scripts/test-install-sh.sh (which
# predates this feature) rather than growing that file further.
#
# Renamed from test-install-sh-gitee-bundle.sh when the CN-mirror provider
# replaced Gitee as install.sh's download-acceleration source (GitHub remains
# the sole release/version authority either way). The strict bundle-manifest
# parser tests below still exercise the immutable `providers: {github, gitee}`
# manifest schema on purpose: that JSON shape is already published on every
# past TUI release and is validated-but-unconsumed (install.sh never reads
# GITEE_OWNER/GITEE_REPO from it), so changing it would break parsing of
# already-published manifests for no operational benefit. Only the ACTIVE
# --source selection/download logic is renamed to "mirror" here.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

export LINGTAI_INSTALL_SH_SOURCE_ONLY=1
# shellcheck source=../install.sh
source "$ROOT_DIR/install.sh"
unset LINGTAI_INSTALL_SH_SOURCE_ONLY

fail() {
  echo "test-install-sh-mirror-bundle: $*" >&2
  exit 1
}

assert_eq() {
  local want="$1" got="$2" label="$3"
  if [[ "$got" != "$want" ]]; then
    fail "$label: got '$got', want '$want'"
  fi
}

tmp="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-inst-mirror-test.XXXXXX")"

# --- --source flag validation -------------------------------------------------

(
  SOURCE_ARG="auto"
  parse_args --source github
  assert_eq "github" "$SOURCE_ARG" "--source github is accepted"
)

(
  SOURCE_ARG="auto"
  parse_args --source mirror
  assert_eq "mirror" "$SOURCE_ARG" "--source mirror is accepted"
)

if (SOURCE_ARG="auto"; parse_args --source bogus) >/dev/null 2>&1; then
  fail "--source bogus should be rejected (parse_args should exit non-zero)"
fi

retirement_out="$( (SOURCE_ARG="auto"; parse_args --source gitee) 2>&1 )" && \
  fail "--source gitee must be explicitly retired (rejected), got: $retirement_out"
echo "$retirement_out" | grep -qi "retired" ||
  fail "--source gitee rejection should explain the retirement, got: $retirement_out"
echo "$retirement_out" | grep -q -- "--source mirror" ||
  fail "--source gitee rejection should point at --source mirror as the replacement, got: $retirement_out"

# --- json_string_field --------------------------------------------------------

assert_eq "v0.16.4" \
  "$(printf '{"schema":"x","kernel_tag":"v0.16.4","other":1}' | json_string_field kernel_tag)" \
  "json_string_field extracts a top-level string"
assert_eq "" \
  "$(printf '{"schema":"x"}' | json_string_field missing_key)" \
  "json_string_field returns empty for a missing key"

# --- strict bundle parser regressions ---------------------------------------

strict_archive="lingtai-v0.11.0-$(detect_os)-$(detect_arch).tar.gz"
strict_manifest="$(printf '%s' '{"schema":"lingtai.tui.bundle/v1","bundle_id":"v0.11.0","tui_tag":"v0.11.0","tui_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generated_at":"2026-07-15T00:00:00Z","kernel_tag":"v0.16.4","kernel_version":"0.16.4","kernel_manifest_filename":"lingtai-kernel-release-manifest.json","archives":[{"filename":"ARCHIVE","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"providers":{"github":{"repo":"Lingtai-AI/lingtai"},"gitee":{"owner":"huangzesen1997","repo":"lingtai"}}}' | sed "s/ARCHIVE/$strict_archive/")"
assert_eq "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" \
  "$(validate_bundle_manifest "$strict_manifest" v0.11.0)" \
  "strict parser returns the manifest-authoritative host archive SHA"
load_bundle_manifest "$strict_manifest" v0.11.0 || fail "strict manifest should load"
assert_eq "v0.16.4" "$(bundle_manifest_field kernel_tag)" "strict parser stores kernel tag"
assert_eq "0.16.4" "$(bundle_manifest_field kernel_version)" "strict parser stores kernel version"
assert_eq "lingtai-kernel-release-manifest.json" "$(bundle_manifest_field kernel_manifest_filename)" "strict parser stores kernel manifest filename"

(
  # GitHub's v1.0.7/v1.0.8 manifests publish the Windows archive only. On a
  # POSIX host that is still a valid exact-tag kernel binding; the missing host
  # binary digest must stay empty so try_release_asset falls back to source.
  windows_only_manifest="$(printf '%s' "$strict_manifest" | python3 -c 'import json,sys; d=json.load(sys.stdin); d["archives"]=[{"filename":"lingtai-v0.11.0-windows-amd64.zip","sha256":"b"*64}]; print(json.dumps(d))')"
  assert_eq "" "$(validate_bundle_manifest "$windows_only_manifest" v0.11.0)" \
    "strict parser accepts a Windows-only manifest on POSIX without inventing a host digest"
  load_bundle_manifest "$windows_only_manifest" v0.11.0 || \
    fail "Windows-only manifest should remain a valid exact-tag kernel binding on POSIX"
  assert_eq "" "$BUNDLE_TUI_ARCHIVE_SHA" \
    "Windows-only manifest leaves the optional POSIX archive digest empty"
  assert_eq "v0.16.4" "$(bundle_manifest_field kernel_tag)" \
    "Windows-only manifest still stores the pinned kernel tag"
  BUNDLE_MANIFEST_JSON="$windows_only_manifest"
  BUNDLE_TAG="v0.11.0"
  if out="$(try_release_asset v0.11.0 2>&1)"; then
    fail "try_release_asset should fall back when the bundle omits this POSIX host archive"
  fi
  echo "$out" | grep -q "does not list.*will build.*from source" ||
    fail "missing-host fallback should explain the source build, got: $out"
)

expect_bad_bundle() {
  local label="$1" body="$2" tag="${3:-v0.11.0}"
  if validate_bundle_manifest "$body" "$tag" >/dev/null 2>&1; then
    fail "strict parser accepted $label"
  fi
}
expect_bad_bundle "top-level duplicate key" '{"schema":"lingtai.tui.bundle/v1","schema":"other"}'
expect_bad_bundle "nested duplicate provider key" '{"providers":{"repo":"a","repo":"b"}}'
expect_bad_bundle "malformed generated_at" "${strict_manifest/2026-07-15T00:00:00Z/2026-99-99T00:00:00Z}"
providers_wrong_type="$(printf '%s' "$strict_manifest" | python3 -c 'import json,sys; d=json.load(sys.stdin); d["providers"]=[]; print(json.dumps(d))')"
expect_bad_bundle "providers wrong type" "$providers_wrong_type"
expect_bad_bundle "required string wrong type" "${strict_manifest/\"kernel_version\":\"0.16.4\"/\"kernel_version\":17}"
expect_bad_bundle "wrong resolved tag" "$strict_manifest" v0.11.1
missing_archive="$(printf '%s' "$strict_manifest" | python3 -c 'import json,sys; d=json.load(sys.stdin); d["archives"]=[]; print(json.dumps(d))')"
expect_bad_bundle "missing archive" "$missing_archive"
duplicate_archive="$(printf '%s' "$strict_manifest" | python3 -c 'import json,sys; d=json.load(sys.stdin); d["archives"].append(dict(d["archives"][0])); print(json.dumps(d))')"
expect_bad_bundle "duplicate archive" "$duplicate_archive"
ambiguous_manifest='{"kernel_tag":"v0.16.4","kernel_tag":"v9.9.9"}'
expect_bad_bundle "first-vs-last kernel tag ambiguity" "$ambiguous_manifest"

# --- verify_sha256 -------------------------------------------------------------

(
  f="$tmp/checksum-target.bin"
  printf 'hello world' > "$f"
  expected="$(shasum -a 256 "$f" | cut -d' ' -f1)"
  verify_sha256 "$f" "$expected" || fail "verify_sha256 should accept a matching digest"
  if verify_sha256 "$f" "0000000000000000000000000000000000000000000000000000000000000"; then
    fail "verify_sha256 should reject a mismatched digest"
  fi
)

# --- fake-curl harness: dispatch canned responses by URL substring -----------
#
# fake_curl_serving writes a fake `curl` into $1 (a directory to prepend to
# PATH) that reads a URL->response-file mapping from $2 (a directory of files
# named after a sanitized URL substring key) — see register_response below.
# Any -o target is honored (file is copied there); otherwise the response is
# printed to stdout, matching how install.sh's helpers consume curl output.
FAKE_CURL_DIR=""

setup_fake_curl() {
  local bindir="$1"
  mkdir -p "$bindir"
  FAKE_CURL_DIR="$tmp/fake-curl-responses.$$"
  mkdir -p "$FAKE_CURL_DIR"
  cat > "$bindir/curl" <<SH
#!/usr/bin/env bash
url=""
out=""
prev=""
for a in "\$@"; do
  case "\$a" in
    http://*|https://*) url="\$a" ;;
  esac
  [[ "\$prev" == "-o" ]] && out="\$a"
  prev="\$a"
done
key="\$(printf '%s' "\$url" | tr -c 'A-Za-z0-9' '_')"
resp_file="$FAKE_CURL_DIR/\$key"
status_file="\$resp_file.status"
[[ -z "\${FAKE_CURL_LOG:-}" ]] || printf '%s\n' "\$url" >> "\$FAKE_CURL_LOG"
if [[ ! -f "\$resp_file" ]]; then
  echo "fake curl: no registered response for \$url" >&2
  exit 22
fi
status="0"
[[ -f "\$status_file" ]] && status="\$(cat "\$status_file")"
if [[ "\$status" != "0" ]]; then
  exit "\$status"
fi
if [[ -n "\$out" ]]; then
  cp "\$resp_file" "\$out"
else
  cat "\$resp_file"
fi
exit 0
SH
  chmod +x "$bindir/curl"
}

# register_response <url> <body-file> [exit_status]
register_response() {
  local url="$1" body_file="$2" status="${3:-0}"
  local key
  key="$(printf '%s' "$url" | tr -c 'A-Za-z0-9' '_')"
  cp "$body_file" "$FAKE_CURL_DIR/$key"
  printf '%s' "$status" > "$FAKE_CURL_DIR/$key.status"
}

register_response_text() {
  local url="$1" body="$2" status="${3:-0}"
  local f="$tmp/resp-body.$$.$RANDOM"
  printf '%s' "$body" > "$f"
  register_response "$url" "$f" "$status"
}

# --- deterministic provider routing and latest mirror records ----------------

(
  SOURCE_ARG="auto"; VERSION=""; REF=""; FROM_SOURCE=0; UPDATE_MODE=0; LATEST_MAIN_MODE=0
  resolve_source_provider
  assert_eq "mirror" "$BUNDLE_PROVIDER" "default/no-version route selects lingtai.ai"
)
(
  SOURCE_ARG="github"; VERSION=""; REF=""; FROM_SOURCE=0; UPDATE_MODE=0; LATEST_MAIN_MODE=0
  resolve_source_provider
  assert_eq "github" "$BUNDLE_PROVIDER" "explicit GitHub route stays GitHub"
)
(
  SOURCE_ARG="auto"; VERSION="v0.10.0"; REF=""; FROM_SOURCE=0; UPDATE_MODE=0; LATEST_MAIN_MODE=0
  resolve_source_provider
  assert_eq "github" "$BUNDLE_PROVIDER" "explicit version routes to GitHub before metadata/assets"
)
(
  SOURCE_ARG="mirror"; VERSION=""; REF="main"; FROM_SOURCE=0; UPDATE_MODE=0; LATEST_MAIN_MODE=0
  resolve_source_provider
  assert_eq "github" "$BUNDLE_PROVIDER" "explicit source mode remains GitHub"
)

# Build a latest/v1 fixture from exact bytes, then prove metadata and the
# selected asset both use /dl and the asset is size/SHA verified.
(
  fakebin="$tmp/latest-route-fakebin"
  setup_fake_curl "$fakebin"
  export PATH="$fakebin:/usr/bin:/bin"
  export FAKE_CURL_LOG="$tmp/latest-route.log"
  : > "$FAKE_CURL_LOG"
  LINGTAI_WEB_BASE="https://lingtai.ai"
  asset="$tmp/latest-route.asset"
  printf 'selected-mirror-bytes' > "$asset"
  sha="$(shasum -a 256 "$asset" | cut -d' ' -f1)"
  size="$(wc -c < "$asset" | tr -d '[:space:]')"
  latest="$(printf '{"schema":"lingtai.release_mirror.latest/v1","source_repo":"Lingtai-AI/lingtai","release_id":7,"tag":"v1.2.3","assets":[{"name":"selected.bin","sha256":"%s","size":%s}]}' "$sha" "$size")"
  register_response_text "https://lingtai.ai/dl/Lingtai-AI/lingtai/latest.json" "$latest"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai/v1.2.3/selected.bin" "$asset"
  MIRROR_TUI_LATEST_JSON=""; MIRROR_TUI_LATEST_TAG=""
  fetch_mirror_latest "$REPO_SLUG" || fail "valid latest/v1 metadata should resolve"
  assert_eq "v1.2.3" "$MIRROR_TUI_LATEST_TAG" "mirror latest tag"
  out="$tmp/latest-route.out"
  download_mirror_asset "$REPO_SLUG" v1.2.3 selected.bin "$out" || fail "selected mirror asset should download and verify"
  cmp "$asset" "$out" || fail "selected mirror bytes changed"
  grep -qx 'https://lingtai.ai/dl/Lingtai-AI/lingtai/latest.json' "$FAKE_CURL_LOG" || fail "latest metadata route missing"
  grep -qx 'https://lingtai.ai/dl/Lingtai-AI/lingtai/v1.2.3/selected.bin' "$FAKE_CURL_LOG" || fail "selected asset route missing"
  ! grep -q 'github.com' "$FAKE_CURL_LOG" || fail "healthy mirror helper route made a GitHub request"
)

# Default bundle resolution consumes mirror latest.json and its selected,
# independently verified bundle manifest without crossing providers.
(
  fakebin="$tmp/bundle-mirror-fakebin"
  setup_fake_curl "$fakebin"
  export PATH="$fakebin:/usr/bin:/bin"
  export FAKE_CURL_LOG="$tmp/bundle-mirror.log"
  : > "$FAKE_CURL_LOG"
  LINGTAI_WEB_BASE="https://lingtai.ai"
  bundle_file="$tmp/bundle-mirror.json"
  archive="lingtai-v0.11.0-$(detect_os)-$(detect_arch).tar.gz"
  printf '%s' '{"schema":"lingtai.tui.bundle/v1","bundle_id":"v0.11.0","tui_tag":"v0.11.0","tui_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generated_at":"2026-07-15T00:00:00Z","kernel_tag":"v0.16.4","kernel_version":"0.16.4","kernel_manifest_filename":"lingtai-kernel-release-manifest.json","archives":[{"filename":"ARCHIVE","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"providers":{"github":{"repo":"Lingtai-AI/lingtai"},"gitee":{"owner":"huangzesen1997","repo":"lingtai"}}}' | sed "s/ARCHIVE/$archive/" > "$bundle_file"
  bundle_sha="$(shasum -a 256 "$bundle_file" | cut -d' ' -f1)"
  bundle_size="$(wc -c < "$bundle_file" | tr -d '[:space:]')"
  latest="$(printf '{"schema":"lingtai.release_mirror.latest/v1","source_repo":"Lingtai-AI/lingtai","release_id":8,"tag":"v0.11.0","assets":[{"name":"lingtai-bundle-manifest.json","sha256":"%s","size":%s}]}' "$bundle_sha" "$bundle_size")"
  register_response_text "https://lingtai.ai/dl/Lingtai-AI/lingtai/latest.json" "$latest"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai/v0.11.0/lingtai-bundle-manifest.json" "$bundle_file"
  SOURCE_ARG="auto"; VERSION=""; REF=""; FROM_SOURCE=0; UPDATE_MODE=0; LATEST_MAIN_MODE=0
  BUNDLE_TAG=""; BUNDLE_MANIFEST_JSON=""; MIRROR_TUI_LATEST_JSON=""; MIRROR_TUI_LATEST_TAG=""
  resolve_source_provider
  fetch_bundle_manifest || fail "default mirror bundle should resolve"
  assert_eq "mirror" "$BUNDLE_PROVIDER" "default bundle provider remains mirror"
  assert_eq "v0.11.0" "$BUNDLE_TAG" "default mirror bundle uses latest tag"
  ! grep -q 'github.com' "$FAKE_CURL_LOG" || fail "default mirror bundle made a GitHub request"
)

# Explicit old-version and explicit-GitHub requests use only existing GitHub
# metadata/assets. No /dl request is permitted for either route.
exercise_github_bundle_route() (
  local source="$1" version="$2" label="$3" fakebin manifest archive
  fakebin="$tmp/${label}-fakebin"
  setup_fake_curl "$fakebin"
  export PATH="$fakebin:/usr/bin:/bin"
  export FAKE_CURL_LOG="$tmp/${label}.log"
  : > "$FAKE_CURL_LOG"
  archive="lingtai-v0.11.0-$(detect_os)-$(detect_arch).tar.gz"
  manifest="$(printf '%s' '{"schema":"lingtai.tui.bundle/v1","bundle_id":"v0.11.0","tui_tag":"v0.11.0","tui_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generated_at":"2026-07-15T00:00:00Z","kernel_tag":"v0.16.4","kernel_version":"0.16.4","kernel_manifest_filename":"lingtai-kernel-release-manifest.json","archives":[{"filename":"ARCHIVE","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}],"providers":{"github":{"repo":"Lingtai-AI/lingtai"},"gitee":{"owner":"huangzesen1997","repo":"lingtai"}}}' | sed "s/ARCHIVE/$archive/")"
  register_response_text "https://api.github.com/repos/Lingtai-AI/lingtai/releases/latest" '{"tag_name":"v0.11.0"}'
  register_response_text "https://api.github.com/repos/Lingtai-AI/lingtai/releases/tags/v0.11.0" '{"tag_name":"v0.11.0","assets":[{"name":"lingtai-bundle-manifest.json"}]}'
  register_response_text "https://github.com/Lingtai-AI/lingtai/releases/download/v0.11.0/lingtai-bundle-manifest.json" "$manifest"
  API_BASE="https://api.github.com/repos/Lingtai-AI/lingtai"
  DOWNLOAD_BASE="https://github.com/Lingtai-AI/lingtai/releases/download"
  SOURCE_ARG="$source"; VERSION="$version"; REF=""; FROM_SOURCE=0; UPDATE_MODE=0; LATEST_MAIN_MODE=0
  BUNDLE_TAG=""; BUNDLE_MANIFEST_JSON=""
  resolve_source_provider
  fetch_bundle_manifest || fail "$label should resolve through GitHub"
  assert_eq "github" "$BUNDLE_PROVIDER" "$label provider"
  ! grep -q '/dl/' "$FAKE_CURL_LOG" || fail "$label made a mirror request"
)
exercise_github_bundle_route auto v0.11.0 explicit-old-version
exercise_github_bundle_route github '' explicit-github

# A selected mirror asset failure is terminal, points to the explicit switch,
# and makes no GitHub request.
(
  fakebin="$tmp/mirror-failure-fakebin"
  setup_fake_curl "$fakebin"
  export PATH="$fakebin:/usr/bin:/bin"
  export FAKE_CURL_LOG="$tmp/mirror-failure.log"
  : > "$FAKE_CURL_LOG"
  LINGTAI_WEB_BASE="https://lingtai.ai"
  missing_sha="$(printf missing | shasum -a 256 | cut -d' ' -f1)"
  MIRROR_TUI_LATEST_TAG="v1.2.3"
  MIRROR_TUI_LATEST_JSON="$(printf '{"schema":"lingtai.release_mirror.latest/v1","source_repo":"Lingtai-AI/lingtai","release_id":9,"tag":"v1.2.3","assets":[{"name":"missing.bin","sha256":"%s","size":7}]}' "$missing_sha")"
  register_response_text "https://lingtai.ai/dl/Lingtai-AI/lingtai/v1.2.3/missing.bin" '' 22
  if out="$(download_mirror_asset "$REPO_SLUG" v1.2.3 missing.bin "$tmp/missing.out" 2>&1)"; then
    fail "selected mirror asset failure should be terminal"
  fi
  echo "$out" | grep -q -- '--source github' || fail "mirror failure lacks explicit GitHub-switch guidance: $out"
  ! grep -q 'github.com' "$FAKE_CURL_LOG" || fail "mirror failure made a GitHub request"
)

# End-to-end selected-byte journey through the existing TUI and kernel install
# functions. Every release request must be one of the seven canonical /dl URLs.
(
  case_dir="$tmp/default-mirror-journey"
  fakebin="$case_dir/fakebin"
  mkdir -p "$case_dir/archive" "$case_dir/bin" "$case_dir/venv/bin"
  setup_fake_curl "$fakebin"
  export PATH="$fakebin:/usr/bin:/bin"
  export FAKE_CURL_LOG="$case_dir/requests.log"
  : > "$FAKE_CURL_LOG"
  LINGTAI_WEB_BASE="https://lingtai.ai"
  tag="v8.8.8"
  kernel_tag="v0.18.0"
  os="$(detect_os)"; arch="$(detect_arch)"
  archive_name="$(asset_name "$tag" "$os" "$arch")"

  cat > "$case_dir/archive/lingtai-tui" <<'EOF'
#!/usr/bin/env bash
[[ "${1:-}" == version ]] && echo 'lingtai-tui v8.8.8' || echo 'tui-runnable'
EOF
  cat > "$case_dir/archive/lingtai-portal" <<'EOF'
#!/usr/bin/env bash
echo 'portal-runnable'
EOF
  chmod +x "$case_dir/archive/lingtai-tui" "$case_dir/archive/lingtai-portal"
  tar -czf "$case_dir/$archive_name" -C "$case_dir/archive" lingtai-tui lingtai-portal
  archive_sha="$(shasum -a 256 "$case_dir/$archive_name" | cut -d' ' -f1)"
  printf '%s  %s\n' "$archive_sha" "$archive_name" > "$case_dir/$archive_name.sha256"

  bundle_file="$case_dir/lingtai-bundle-manifest.json"
  printf '%s' '{"schema":"lingtai.tui.bundle/v1","bundle_id":"v8.8.8","tui_tag":"v8.8.8","tui_commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generated_at":"2026-07-15T00:00:00Z","kernel_tag":"v0.17.0","kernel_version":"0.17.0","kernel_manifest_filename":"lingtai-kernel-release-manifest.json","archives":[{"filename":"ARCHIVE","sha256":"ARCHIVE_SHA"}],"providers":{"github":{"repo":"Lingtai-AI/lingtai"},"gitee":{"owner":"huangzesen1997","repo":"lingtai"}}}' \
    | sed "s/ARCHIVE_SHA/$archive_sha/; s/ARCHIVE/$archive_name/" > "$bundle_file"

  wheel_name="lingtai-0.18.0-cp312-cp312-macosx_11_0_arm64.whl"
  printf 'fake-wheel-selected-by-mirror' > "$case_dir/$wheel_name"
  wheel_sha="$(shasum -a 256 "$case_dir/$wheel_name" | cut -d' ' -f1)"
  kernel_file="$case_dir/lingtai-kernel-release-manifest.json"
  printf '{"schema":"lingtai.kernel.release/v1","kernel_version":"0.18.0","kernel_tag":"v0.18.0","commit":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","generated_at":"2026-07-15T00:00:00Z","artifacts":[{"filename":"%s","sha256":"%s","kind":"wheel","python_tag":"cp312","abi_tag":"cp312","platform_tag":"macosx_11_0_arm64"}],"sdist_fallback":""}' "$wheel_name" "$wheel_sha" > "$kernel_file"

  asset_json() {
    local name="$1" file="$2" sha size
    sha="$(shasum -a 256 "$file" | cut -d' ' -f1)"
    size="$(wc -c < "$file" | tr -d '[:space:]')"
    printf '{"name":"%s","sha256":"%s","size":%s}' "$name" "$sha" "$size"
  }
  tui_latest="$(printf '{"schema":"lingtai.release_mirror.latest/v1","source_repo":"Lingtai-AI/lingtai","release_id":88,"tag":"%s","assets":[%s,%s,%s,%s]}' "$tag" \
    "$(asset_json lingtai-bundle-manifest.json "$bundle_file")" \
    "$(asset_json "$archive_name" "$case_dir/$archive_name")" \
    "$(asset_json "$archive_name.sha256" "$case_dir/$archive_name.sha256")" \
    "$(asset_json unused.txt "$case_dir/$archive_name.sha256")")"
  kernel_latest="$(printf '{"schema":"lingtai.release_mirror.latest/v1","source_repo":"Lingtai-AI/lingtai-kernel","release_id":89,"tag":"%s","assets":[%s,%s]}' "$kernel_tag" \
    "$(asset_json lingtai-kernel-release-manifest.json "$kernel_file")" \
    "$(asset_json "$wheel_name" "$case_dir/$wheel_name")")"

  register_response_text "https://lingtai.ai/dl/Lingtai-AI/lingtai/latest.json" "$tui_latest"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai/$tag/lingtai-bundle-manifest.json" "$bundle_file"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai/$tag/$archive_name" "$case_dir/$archive_name"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai/$tag/$archive_name.sha256" "$case_dir/$archive_name.sha256"
  register_response_text "https://lingtai.ai/dl/Lingtai-AI/lingtai-kernel/latest.json" "$kernel_latest"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai-kernel/$kernel_tag/lingtai-kernel-release-manifest.json" "$kernel_file"
  register_response "https://lingtai.ai/dl/Lingtai-AI/lingtai-kernel/$kernel_tag/$wheel_name" "$case_dir/$wheel_name"

  py="$case_dir/venv/bin/python"
  cat > "$py" <<EOF
#!/usr/bin/env bash
case "\${1:-}" in
  -) cat >/dev/null; echo cp312-cp312-macosx_11_0_arm64 ;;
  -c) printf 'kernel-runnable\n' > "$case_dir/kernel-runnable" ;;
esac
exit 0
EOF
  uv="$case_dir/uv"
  cat > "$uv" <<EOF
#!/usr/bin/env bash
printf '%s\n' "\$@" > "$case_dir/kernel-install-argv"
exit 0
EOF
  chmod +x "$py" "$uv"

  SOURCE_ARG="auto"; VERSION=""; REF=""; FROM_SOURCE=0; UPDATE_MODE=0; LATEST_MAIN_MODE=0
  BUNDLE_PROVIDER=""; BUNDLE_REQUIRED=1; BUNDLE_TAG=""; BUNDLE_MANIFEST_JSON=""
  MIRROR_TUI_LATEST_JSON=""; MIRROR_TUI_LATEST_TAG=""; MIRROR_KERNEL_LATEST_JSON=""; MIRROR_KERNEL_LATEST_TAG=""
  BIN_DIR="$case_dir/bin"; BUILD_DIR="$case_dir/build"; SKIP_PORTAL=0; PORTAL_PATH=""
  KERNEL_SOURCE=""; KERNEL_LATEST_TAG=""; KERNEL_MANIFEST_JSON=""; KERNEL_MANIFEST_PROVIDER=""
  resolve_source_provider
  fetch_bundle_manifest || fail "default journey could not resolve the mirror bundle"
  try_release_asset "$BUNDLE_TAG" || fail "default journey could not install mirror TUI/Portal"
  install_kernel_from_bundle "$py" "$uv" || fail "default journey could not install mirror kernel"

  [[ "$($BIN_DIR/lingtai-tui)" == tui-runnable ]] || fail "installed TUI is not runnable"
  [[ "$($BIN_DIR/lingtai-portal)" == portal-runnable ]] || fail "installed Portal is not runnable"
  [[ -f "$case_dir/kernel-runnable" && -s "$case_dir/kernel-install-argv" ]] || fail "kernel install/import path was not runnable"
  assert_eq "7" "$(wc -l < "$FAKE_CURL_LOG" | tr -d '[:space:]')" "default mirror journey request count"
  if grep -v '^https://lingtai.ai/dl/' "$FAKE_CURL_LOG" >/dev/null; then
    fail "default mirror journey left canonical /dl routes: $(cat "$FAKE_CURL_LOG")"
  fi
  ! grep -q 'github' "$FAKE_CURL_LOG" || fail "default mirror journey made a GitHub request"
)

# --- kernel manifest handoff survives provider selection in the same shell ---

(
  BUNDLE_PROVIDER="mirror"
  KERNEL_MANIFEST_PROVIDER=""
  KERNEL_MANIFEST_JSON=""
  mirror_asset_text() {
    printf '%s' '{"schema":"lingtai.kernel.release/v1","kernel_version":"0.16.4","kernel_tag":"v0.16.4","commit":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","generated_at":"2026-07-15T00:00:00Z","sdist_fallback":"lingtai-0.16.4.tar.gz","artifacts":[{"filename":"lingtai-0.16.4.tar.gz","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","kind":"sdist","python_tag":null,"abi_tag":null,"platform_tag":null}]}'
  }

  fetch_kernel_manifest v0.16.4 || fail "fetch_kernel_manifest should populate explicit state"
  assert_eq "mirror" "$KERNEL_MANIFEST_PROVIDER" "kernel manifest provider survives fetch"
  [[ "$KERNEL_MANIFEST_JSON" == *'"kernel_version":"0.16.4"'* ]] ||
    fail "kernel manifest JSON survives fetch"
)

# --- select_kernel_wheel: exact match preferred, no match -> empty ----------

(
  # A real fresh uv venv has neither packaging nor pip. The dependency-free
  # fallback must still emit at least one usable CPython platform tag.
  no_pip_venv="$tmp/no-pip-venv"
  python3 -m venv --without-pip "$no_pip_venv"
  tags="$(python_platform_tags "$no_pip_venv/bin/python")"
  [[ -n "$tags" ]] || fail "python_platform_tags should work without packaging or pip"
  first_tag="$(printf '%s\n' "$tags" | sed -n '1p')"
  python_tag="${first_tag%%-*}"
  remainder="${first_tag#*-}"
  abi_tag="${remainder%%-*}"
  platform_tag="${remainder#*-}"
  kernel_manifest="$(python3 - "$python_tag" "$abi_tag" "$platform_tag" <<'PY'
import json, sys
python_tag, abi_tag, platform_tag = sys.argv[1:]
filename = f"lingtai-0.16.4-{python_tag}-{abi_tag}-{platform_tag}.whl"
print(json.dumps({
    "schema": "lingtai.kernel.release/v1",
    "artifacts": [{
        "filename": filename,
        "sha256": "fallbacksha",
        "kind": "wheel",
        "python_tag": python_tag,
        "abi_tag": abi_tag,
        "platform_tag": platform_tag,
    }],
    "sdist_fallback": "",
}))
PY
)"
  hit="$(select_kernel_wheel "$kernel_manifest" "$no_pip_venv/bin/python")" ||
    fail "select_kernel_wheel should use the dependency-free fresh-venv tags"
  assert_eq "lingtai-0.16.4-${first_tag}.whl fallbacksha" "$hit" \
    "fresh-venv fallback selects the compatible wheel"
)

(
  kernel_manifest='{"schema":"lingtai.kernel.release/v1","artifacts":[
    {"filename":"lingtai-0.16.4-cp312-cp312-macosx_11_0_arm64.whl","sha256":"aaaa","kind":"wheel","python_tag":"cp312","abi_tag":"cp312","platform_tag":"macosx_11_0_arm64"},
    {"filename":"lingtai-0.16.4-cp311-cp311-manylinux_2_28_x86_64.whl","sha256":"bbbb","kind":"wheel","python_tag":"cp311","abi_tag":"cp311","platform_tag":"manylinux_2_28_x86_64"},
    {"filename":"lingtai-0.16.4.tar.gz","sha256":"cccc","kind":"sdist","python_tag":null,"abi_tag":null,"platform_tag":null}
  ],"sdist_fallback":"lingtai-0.16.4.tar.gz"}'

  fake_py="$tmp/fake-python-matching"
  cat > "$fake_py" <<'PYEOF'
#!/usr/bin/env bash
cat <<'TAGS'
cp312-cp312-macosx_11_0_arm64
cp312-abi3-macosx_11_0_arm64
TAGS
PYEOF
  chmod +x "$fake_py"

  hit="$(select_kernel_wheel "$kernel_manifest" "$fake_py")" || fail "select_kernel_wheel should find the matching cp312 wheel"
  assert_eq "lingtai-0.16.4-cp312-cp312-macosx_11_0_arm64.whl aaaa" "$hit" "select_kernel_wheel returns filename+sha256 for the matching tag"
)

(
  kernel_manifest='{"schema":"lingtai.kernel.release/v1","artifacts":[
    {"filename":"lingtai-0.16.4-cp312-cp312-macosx_11_0_arm64.whl","sha256":"aaaa","kind":"wheel","python_tag":"cp312","abi_tag":"cp312","platform_tag":"macosx_11_0_arm64"},
    {"filename":"lingtai-0.16.4.tar.gz","sha256":"cccc","kind":"sdist","python_tag":null,"abi_tag":null,"platform_tag":null}
  ],"sdist_fallback":"lingtai-0.16.4.tar.gz"}'

  fake_py="$tmp/fake-python-nomatch"
  cat > "$fake_py" <<'PYEOF'
#!/usr/bin/env bash
cat <<'TAGS'
cp313-cp313-win_amd64
TAGS
PYEOF
  chmod +x "$fake_py"

  if hit="$(select_kernel_wheel "$kernel_manifest" "$fake_py")"; then
    fail "select_kernel_wheel should find nothing for an incompatible interpreter, got '$hit'"
  fi

  fallback="$(kernel_sdist_fallback "$kernel_manifest")" || fail "kernel_sdist_fallback should succeed"
  assert_eq "lingtai-0.16.4.tar.gz cccc" "$fallback" "kernel_sdist_fallback returns the declared sdist artifact"
)

# --- install_kernel_from_bundle: checksum mismatch fails loud and retains evidence ---

(
  fakebin="$tmp/install-checksum-fail-fakebin"
  setup_fake_curl "$fakebin"

  kernel_manifest='{"schema":"lingtai.kernel.release/v1","kernel_version":"0.16.4","artifacts":[
    {"filename":"lingtai-0.16.4-cp312-cp312-macosx_11_0_arm64.whl","sha256":"deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef","kind":"wheel","python_tag":"cp312","abi_tag":"cp312","platform_tag":"macosx_11_0_arm64"},
    {"filename":"lingtai-0.16.4.tar.gz","sha256":"cccc","kind":"sdist","python_tag":null,"abi_tag":null,"platform_tag":null}
  ],"sdist_fallback":"lingtai-0.16.4.tar.gz"}'

  register_response_text \
    "https://api.github.com/repos/Lingtai-AI/lingtai-kernel/releases/tags/v0.16.4" \
    '{"tag_name":"v0.16.4","assets":[{"name":"lingtai-kernel-release-manifest.json"}]}'
  register_response_text \
    "https://github.com/Lingtai-AI/lingtai-kernel/releases/download/v0.16.4/lingtai-kernel-release-manifest.json" \
    "$kernel_manifest"
  # The wheel download succeeds but its bytes will NOT hash to the manifest's
  # sha256 above — this is the checksum-mismatch fail-loud path.
  register_response_text \
    "https://github.com/Lingtai-AI/lingtai-kernel/releases/download/v0.16.4/lingtai-0.16.4-cp312-cp312-macosx_11_0_arm64.whl" \
    "not-the-real-wheel-bytes"

  export PATH="$fakebin:/usr/bin:/bin"
  KERNEL_GH_API_BASE="https://api.github.com/repos/Lingtai-AI/lingtai-kernel"

  fake_py="$tmp/fake-python-checksum-test"
  cat > "$fake_py" <<'PYEOF'
#!/usr/bin/env bash
cat <<'TAGS'
cp312-cp312-macosx_11_0_arm64
TAGS
PYEOF
  chmod +x "$fake_py"

  BUNDLE_PROVIDER="github"
  BUNDLE_MANIFEST_JSON='{"schema":"lingtai.tui.bundle/v1","bundle_id":"v0.11.0","kernel_tag":"v0.16.4"}'
  BUILD_DIR="$tmp/install-checksum-fail-build"
  KERNEL_SOURCE=""
  if install_kernel_from_bundle "$fake_py" ""; then
    fail "install_kernel_from_bundle must fail loud on checksum mismatch, not succeed"
  fi
  assert_eq "" "$KERNEL_SOURCE" "KERNEL_SOURCE must stay unset when install_kernel_from_bundle fails"
  if [[ ! -f "$BUILD_DIR/kernel-artifact/lingtai-0.16.4-cp312-cp312-macosx_11_0_arm64.whl" ]]; then
    fail "the tampered/mismatched artifact must be retained for diagnosis after checksum failure"
  fi
)

# --- command path: the dependency index follows the FINAL bundle provider ---
#
# These run install_kernel_from_bundle for real (fake curl + stub
# interpreter/uv) and assert on the EXACT argv the install command receives.
# Deliberately command-path rather than helper-level: what decides whether a
# mainland-China install can resolve lingtai's third-party dependencies is what
# pip/uv is actually asked to do, not what a helper returns in isolation.

KERNEL_ARTIFACT_NAME="lingtai-0.16.4-cp312-cp312-macosx_11_0_arm64.whl"

# argv_count echoes how many recorded argv elements exactly equal $2.
argv_count() {
  local n
  n="$(grep -cxF -- "$2" "$1" 2>/dev/null || true)"
  printf '%s' "${n:-0}"
}

# argv_value_after echoes the argv element immediately following the first
# element that exactly equals $2. Position-independent on purpose: the uv form
# interleaves `-p <venv>` between the index URL and the local artifact.
argv_value_after() {
  local file="$1" flag="$2" idx
  idx="$(grep -nxF -- "$flag" "$file" 2>/dev/null | head -1 | cut -d: -f1)"
  [[ -n "$idx" ]] || return 1
  sed -n "$((idx + 1))p" "$file"
}

# capture_bundle_install_argv <case-dir> <uv|pip> <bundle-provider> <kernel-provider>
# Drives install_kernel_from_bundle end to end and leaves the exact argv of the
# install command in <case-dir>/argv.txt, one element per line. Must be called
# inside a subshell: it mutates PATH and installer globals.
capture_bundle_install_argv() {
  local case_dir="$1" installer="$2" bundle_provider="$3" kernel_provider="$4"
  local venv_bin="$case_dir/venv/bin" fakebin="$case_dir/fakebin"
  local argv_log="$case_dir/argv.txt"
  mkdir -p "$venv_bin"
  : > "$argv_log"

  # The stub interpreter MUST dispatch on its first argument. install.sh calls
  # it three different ways — `$py - <<PY` (platform tags), `$py -m pip install
  # ...` (the install), `$py -c 'import lingtai...'` (post-install check). A
  # stub that answers every call with the tag list would record no argv at all
  # and make every assertion below pass vacuously.
  local py="$venv_bin/python"
  cat > "$py" <<STUB_PY
#!/usr/bin/env bash
case "\${1:-}" in
  -)
    cat >/dev/null
    echo "cp312-cp312-macosx_11_0_arm64"
    ;;
  -m)
    printf '%s\n' "\$@" >> "$argv_log"
    ;;
esac
exit 0
STUB_PY
  chmod +x "$py"

  local uv=""
  if [[ "$installer" == "uv" ]]; then
    uv="$case_dir/uv"
    cat > "$uv" <<STUB_UV
#!/usr/bin/env bash
printf '%s\n' "\$@" >> "$argv_log"
exit 0
STUB_UV
    chmod +x "$uv"
  fi

  # The manifest digest must match the served bytes, otherwise the checksum
  # gate returns before the install command ever runs.
  local wheel_body="$case_dir/wheel.bin" wheel_sha
  printf 'stub-wheel-bytes-for-%s' "$(basename "$case_dir")" > "$wheel_body"
  wheel_sha="$(shasum -a 256 "$wheel_body" | cut -d' ' -f1)"

  local kernel_manifest
  kernel_manifest="$(printf '{"schema":"lingtai.kernel.release/v1","kernel_version":"0.16.4","artifacts":[{"filename":"%s","sha256":"%s","kind":"wheel","python_tag":"cp312","abi_tag":"cp312","platform_tag":"macosx_11_0_arm64"}],"sdist_fallback":""}' \
    "$KERNEL_ARTIFACT_NAME" "$wheel_sha")"

  setup_fake_curl "$fakebin"
  # setup_fake_curl reuses one response directory per test process; clear it so
  # a previous case's registration cannot answer this case's request.
  rm -rf "${FAKE_CURL_DIR:?}" && mkdir -p "$FAKE_CURL_DIR"

  local kernel_tags_gh="https://api.github.com/repos/Lingtai-AI/lingtai-kernel/releases/tags/v0.16.4"
  local gh_dl="https://github.com/Lingtai-AI/lingtai-kernel/releases/download/v0.16.4"
  local mirror_dl="https://lingtai.ai/dl/Lingtai-AI/lingtai-kernel/v0.16.4"

  if [[ "$kernel_provider" == "mirror" ]]; then
    local manifest_file="$case_dir/kernel-manifest.json" manifest_sha manifest_size wheel_size
    printf '%s' "$kernel_manifest" > "$manifest_file"
    manifest_sha="$(shasum -a 256 "$manifest_file" | cut -d' ' -f1)"
    manifest_size="$(wc -c < "$manifest_file" | tr -d '[:space:]')"
    wheel_size="$(wc -c < "$wheel_body" | tr -d '[:space:]')"
    MIRROR_KERNEL_LATEST_TAG="v0.16.4"
    MIRROR_KERNEL_LATEST_JSON="$(printf '{"schema":"lingtai.release_mirror.latest/v1","source_repo":"Lingtai-AI/lingtai-kernel","release_id":10,"tag":"v0.16.4","assets":[{"name":"lingtai-kernel-release-manifest.json","sha256":"%s","size":%s},{"name":"%s","sha256":"%s","size":%s}]}' "$manifest_sha" "$manifest_size" "$KERNEL_ARTIFACT_NAME" "$wheel_sha" "$wheel_size")"
    register_response "$mirror_dl/lingtai-kernel-release-manifest.json" "$manifest_file"
    register_response "$mirror_dl/$KERNEL_ARTIFACT_NAME" "$wheel_body"
  else
    # The mirror is probed first whenever the bundle came from the mirror;
    # make it miss so the KERNEL manifest provider can differ from the FINAL
    # BUNDLE provider.
    register_response_text "$mirror_dl/lingtai-kernel-release-manifest.json" "" 22
    register_response_text "$kernel_tags_gh" \
      '{"tag_name":"v0.16.4","assets":[{"name":"lingtai-kernel-release-manifest.json"}]}'
    register_response_text "$gh_dl/lingtai-kernel-release-manifest.json" "$kernel_manifest"
    register_response "$gh_dl/$KERNEL_ARTIFACT_NAME" "$wheel_body"
  fi

  export PATH="$fakebin:/usr/bin:/bin"
  LINGTAI_WEB_BASE="https://lingtai.ai"
  KERNEL_GH_API_BASE="https://api.github.com/repos/Lingtai-AI/lingtai-kernel"

  BUNDLE_PROVIDER="$bundle_provider"
  BUNDLE_MANIFEST_JSON='{"schema":"lingtai.tui.bundle/v1","bundle_id":"v0.11.0","kernel_tag":"v0.16.4"}'
  BUNDLE_MANIFEST_BUNDLE_ID="v0.11.0"
  BUNDLE_MANIFEST_KERNEL_TAG="v0.16.4"
  BUNDLE_MANIFEST_KERNEL_VERSION="0.16.4"
  BUNDLE_MANIFEST_KERNEL_FILENAME="lingtai-kernel-release-manifest.json"
  BUILD_DIR="$case_dir/build"
  KERNEL_SOURCE=""
  KERNEL_PROVIDER=""

  install_kernel_from_bundle "$py" "$uv" >/dev/null || return 1
  printf '%s' "$argv_log"
}

# assert_single_local_artifact_install checks the invariants that hold for EVERY
# provider/index combination: exactly one --index-url, never an
# --extra-index-url, and the install target is the verified local artifact
# rather than the package name "lingtai" requested from an index.
assert_single_local_artifact_install() {
  local log="$1" label="$2"
  [[ -s "$log" ]] || fail "$label: recorded no install argv at all (stub never reached)"
  assert_eq "1" "$(argv_count "$log" "--index-url")" "$label: exactly one --index-url"
  assert_eq "0" "$(argv_count "$log" "--extra-index-url")" "$label: no --extra-index-url"
  assert_eq "0" "$(argv_count "$log" "lingtai")" "$label: never requests the package name lingtai"
  local target
  target="$(tail -n 1 "$log")"
  [[ "$target" == */"$KERNEL_ARTIFACT_NAME" ]] ||
    fail "$label: install target should be the local artifact path, got '$target'"
  [[ -f "$target" ]] || fail "$label: install target '$target' is not an existing local file"
}

(
  # FINAL provider mirror + no override -> Tsinghua TUNA. This is the
  # availability defect: official PyPI is not reliably reachable from the
  # mainland-China environments the mirror exists to serve.
  unset LINGTAI_PYPI_INDEX_URL
  case_dir="$tmp/argv-mirror-uv"
  log="$(capture_bundle_install_argv "$case_dir" uv mirror mirror)" ||
    fail "install_kernel_from_bundle should succeed on the mirror/uv command path"
  assert_eq "https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple" \
    "$(argv_value_after "$log" "--index-url")" \
    "mirror bundle provider resolves third-party dependencies via Tsinghua TUNA (uv)"
  assert_single_local_artifact_install "$log" "mirror/uv"
)

(
  # Same contract on the pip path (no uv available).
  unset LINGTAI_PYPI_INDEX_URL
  case_dir="$tmp/argv-mirror-pip"
  log="$(capture_bundle_install_argv "$case_dir" pip mirror mirror)" ||
    fail "install_kernel_from_bundle should succeed on the mirror/pip command path"
  assert_eq "https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple" \
    "$(argv_value_after "$log" "--index-url")" \
    "mirror bundle provider resolves third-party dependencies via Tsinghua TUNA (pip)"
  assert_single_local_artifact_install "$log" "mirror/pip"
)


(
  # FINAL provider github + no override -> official PyPI, unchanged.
  unset LINGTAI_PYPI_INDEX_URL
  case_dir="$tmp/argv-github-uv"
  log="$(capture_bundle_install_argv "$case_dir" uv github github)" ||
    fail "install_kernel_from_bundle should succeed on the github/uv command path"
  assert_eq "https://pypi.org/simple" "$(argv_value_after "$log" "--index-url")" \
    "github bundle provider keeps resolving third-party dependencies via official PyPI"
  assert_single_local_artifact_install "$log" "github/uv"
)

(
  # An explicit non-empty override always wins, on either provider.
  export LINGTAI_PYPI_INDEX_URL="https://packages.example.invalid/simple"
  case_dir="$tmp/argv-override-mirror"
  log="$(capture_bundle_install_argv "$case_dir" uv mirror mirror)" ||
    fail "install_kernel_from_bundle should succeed with an explicit index override"
  assert_eq "https://packages.example.invalid/simple" "$(argv_value_after "$log" "--index-url")" \
    "explicit non-empty LINGTAI_PYPI_INDEX_URL overrides the mirror default"
  assert_single_local_artifact_install "$log" "override/mirror"
)

(
  export LINGTAI_PYPI_INDEX_URL="https://packages.example.invalid/simple"
  case_dir="$tmp/argv-override-github"
  log="$(capture_bundle_install_argv "$case_dir" pip github github)" ||
    fail "install_kernel_from_bundle should succeed with an explicit index override on github"
  assert_eq "https://packages.example.invalid/simple" "$(argv_value_after "$log" "--index-url")" \
    "explicit non-empty LINGTAI_PYPI_INDEX_URL overrides the GitHub default"
  assert_single_local_artifact_install "$log" "override/github"
)

(
  # An EMPTY override is not an override: it must not blank out the index.
  export LINGTAI_PYPI_INDEX_URL=""
  case_dir="$tmp/argv-empty-override-mirror"
  log="$(capture_bundle_install_argv "$case_dir" uv mirror mirror)" ||
    fail "install_kernel_from_bundle should succeed with an empty index override"
  assert_eq "https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple" \
    "$(argv_value_after "$log" "--index-url")" \
    "an empty LINGTAI_PYPI_INDEX_URL falls through to the mirror provider default"
  assert_single_local_artifact_install "$log" "empty-override/mirror"
)

# --- ensure_runtime_venv: fail-loud gate (Blocker 1 repair) -----------------
#
# These tests exercise ONLY the early-exit gate at the top of
# ensure_runtime_venv, which runs before any venv/python provisioning work —
# so a disposable $HOME is sufficient; no fake python3/uv is needed to reach
# the assertions below.

(
  # Default one-command path (BUNDLE_REQUIRED=1), no bundle manifest resolved
  # -> must fail loud, not silently proceed to any install.
  export HOME="$tmp/gate-required-no-bundle-home"
  mkdir -p "$HOME"
  BUNDLE_REQUIRED=1
  BUNDLE_MANIFEST_JSON=""
  SKIP_VENV=0
  BUNDLE_PROVIDER="github"
  # This case owns only the fail-loud gate. Keep the decoupled latest-kernel
  # resolver offline and deterministically unavailable instead of consulting
  # the real GitHub/mirror routes from an installer test.
  resolve_latest_kernel_release() { return 1; }
  if out="$(ensure_runtime_venv "$tmp/bin" 2>&1)"; then
    rc=0
  else
    rc=$?
  fi
  if [[ "$rc" -eq 0 ]]; then
    fail "ensure_runtime_venv must fail loud (nonzero) when BUNDLE_REQUIRED=1 and no bundle was resolved, got rc=0: $out"
  fi
  echo "$out" | grep -q "never installed from PyPI\|never.*from an index\|hard stop" \
    || fail "expected an explicit never-PyPI/hard-stop message in the fail-loud output, got: $out"
  echo "$out" | grep -q -- "--skip-python" \
    || fail "expected the fail-loud message to mention --skip-python as the opt-out, got: $out"
)

(
  # Same as above, but --skip-python (SKIP_VENV=1) must skip cleanly (rc=0),
  # never attempting any install.
  export HOME="$tmp/gate-required-skip-home"
  mkdir -p "$HOME"
  BUNDLE_REQUIRED=1
  BUNDLE_MANIFEST_JSON=""
  SKIP_VENV=1
  out="$(ensure_runtime_venv "$tmp/bin" 2>&1)" || fail "--skip-python must exit 0 even with no bundle resolved: $out"
  SKIP_VENV=0
)

(
  # --ref / source-ref build (BUNDLE_REQUIRED=0), no bundle manifest -> must
  # ALSO fail loud (a distinct message), never silently reach for PyPI.
  export HOME="$tmp/gate-ref-no-bundle-home"
  mkdir -p "$HOME"
  BUNDLE_REQUIRED=0
  BUNDLE_MANIFEST_JSON=""
  SKIP_VENV=0
  if out="$(ensure_runtime_venv "$tmp/bin" 2>&1)"; then
    rc=0
  else
    rc=$?
  fi
  if [[ "$rc" -eq 0 ]]; then
    fail "ensure_runtime_venv must fail loud for a --ref build with no bundle, got rc=0: $out"
  fi
  echo "$out" | grep -q -- "--skip-python" \
    || fail "expected the --ref fail-loud message to mention --skip-python, got: $out"
  echo "$out" | grep -q "no pinned kernel release bundle to install from\|never installed from PyPI\|never.*from an index" \
    || fail "expected an explicit no-bundle-for-source-ref explanation, got: $out"
)

(
  # --ref build + --skip-python must skip cleanly.
  export HOME="$tmp/gate-ref-skip-home"
  mkdir -p "$HOME"
  BUNDLE_REQUIRED=0
  BUNDLE_MANIFEST_JSON=""
  SKIP_VENV=1
  out="$(ensure_runtime_venv "$tmp/bin" 2>&1)" || fail "--skip-python must exit 0 for a --ref build too: $out"
  SKIP_VENV=0
)

# --- Regression: no new-install path ever runs `pip/uv ... install lingtai` by name ---
#
# Static assertion over the shipped script: grep for any install invocation
# that names the "lingtai" package directly (as opposed to installing an
# explicit local .whl/.tar.gz path). This is deliberately a text-level
# regression guard — it fails loud if a future edit reintroduces
# `pip install lingtai` / `uv pip install --upgrade lingtai` anywhere.
(
  matches="$(grep -nE '(pip install|pip3 install)[^|&;]*[[:space:]]lingtai([[:space:]]|$)' "$ROOT_DIR/install.sh" | grep -vE '^[0-9]*:[[:space:]]*#' || true)"
  if [[ -n "$matches" ]]; then
    fail "install.sh must never install the 'lingtai' package by name from an index; found:
$matches"
  fi
)

echo "install.sh mirror bundle installer tests passed"
