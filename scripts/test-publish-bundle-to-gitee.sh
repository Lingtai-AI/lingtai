#!/usr/bin/env bash
# Focused tests for scripts/publish_bundle_to_gitee.sh: local asset
# verification (pass/fail-loud on tamper), safe no-token skip, and
# --bundle-dir/--tag argument validation. Deliberately does not exercise the
# real Gitee network path (that would require live credentials); the Gitee
# HTTP calls themselves are covered indirectly by install.sh's own Gitee
# helpers in scripts/test-install-sh-gitee-bundle.sh, which share the same
# v5 API shapes.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/publish_bundle_to_gitee.sh"

fail() {
  echo "test-publish-bundle-to-gitee: $*" >&2
  exit 1
}

tmp="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-publish-gitee-test.XXXXXX")"
make_bundle() {
  local dir="$1" tag="$2" archive_bytes="${3:-fake-archive-bytes}" archive_present="${4:-1}"
  mkdir -p "$dir"
  local sha
  if [[ "$archive_present" == "1" ]]; then
    printf '%s' "$archive_bytes" > "$dir/lingtai-${tag}-darwin-arm64.tar.gz"
    sha="$(shasum -a 256 "$dir/lingtai-${tag}-darwin-arm64.tar.gz" | cut -d' ' -f1)"
    printf '%s  lingtai-%s-darwin-arm64.tar.gz\n' "$sha" "$tag" > "$dir/lingtai-${tag}-darwin-arm64.tar.gz.sha256"
  else
    sha="$(printf '0%.0s' {1..64})"
  fi
  cat > "$dir/lingtai-bundle-manifest.json" <<EOF
{
  "schema": "lingtai.tui.bundle/v1",
  "bundle_id": "$tag",
  "tui_tag": "$tag",
  "tui_commit": "$(printf 'a%.0s' $(seq 1 40))",
  "generated_at": "2026-07-15T00:00:00Z",
  "kernel_tag": "v0.16.4",
  "kernel_version": "0.16.4",
  "kernel_manifest_filename": "lingtai-kernel-release-manifest.json",
  "archives": [{"filename": "lingtai-${tag}-darwin-arm64.tar.gz", "sha256": "$sha"}],
  "providers": {"github": {"repo": "Lingtai-AI/lingtai"}, "gitee": {"owner": "huangzesen1997", "repo": "lingtai"}}
}
EOF
}

# --- missing required flags ---------------------------------------------------

if bash "$SCRIPT" --bundle-dir "$tmp" >/dev/null 2>&1; then
  fail "missing --tag should be rejected"
fi
if bash "$SCRIPT" --tag v9.9.9 >/dev/null 2>&1; then
  fail "missing --bundle-dir should be rejected"
fi
if bash "$SCRIPT" --tag v9.9.9 --bundle-dir "$tmp/does-not-exist" >/dev/null 2>&1; then
  fail "nonexistent --bundle-dir should be rejected"
fi

# --- matching bytes: verifies OK, then safely skips with no token -----------

(
  bundle_dir="$tmp/ok-bundle"
  make_bundle "$bundle_dir" "v9.9.1"
  unset GITEE_ACCESS_TOKEN 2>/dev/null || true
  out="$(bash "$SCRIPT" --tag v9.9.1 --bundle-dir "$bundle_dir" 2>&1)" || fail "script should exit 0 when assets match and no token is set: $out"
  echo "$out" | grep -q "local assets OK" || fail "expected local-assets-OK line, got: $out"
  echo "$out" | grep -q "GITEE_ACCESS_TOKEN is not set" || fail "expected the no-token skip line, got: $out"
)

# --- tampered archive: fails loud, never reaches the Gitee/token step -------

(
  bundle_dir="$tmp/tampered-bundle"
  make_bundle "$bundle_dir" "v9.9.2"
  # Tamper AFTER the manifest recorded the original (correct) checksum.
  printf 'tampered-bytes-do-not-match' > "$bundle_dir/lingtai-v9.9.2-darwin-arm64.tar.gz"
  export GITEE_ACCESS_TOKEN="unused-should-never-be-reached"
  if out="$(bash "$SCRIPT" --tag v9.9.2 --bundle-dir "$bundle_dir" 2>&1)"; then
    fail "script must fail loud on a checksum mismatch, got success: $out"
  fi
  unset GITEE_ACCESS_TOKEN
)

# --- missing archive referenced by the manifest -------------------------------

(
  bundle_dir="$tmp/missing-archive-bundle"
  make_bundle "$bundle_dir" "v9.9.3" "" 0
  if out="$(bash "$SCRIPT" --tag v9.9.3 --bundle-dir "$bundle_dir" 2>&1)"; then
    fail "script must fail loud when a manifest-listed archive is missing on disk, got success: $out"
  fi
)

# --- wrong schema in the manifest --------------------------------------------

(
  bundle_dir="$tmp/wrong-schema-bundle"
  make_bundle "$bundle_dir" "v9.9.4"
  python3 -c "
import json
p = '$bundle_dir/lingtai-bundle-manifest.json'
data = json.load(open(p))
data['schema'] = 'lingtai.tui.bundle/v2'
json.dump(data, open(p, 'w'))
"
  if out="$(bash "$SCRIPT" --tag v9.9.4 --bundle-dir "$bundle_dir" 2>&1)"; then
    fail "script must reject an unexpected bundle manifest schema, got success: $out"
  fi
)

# --- retained remote comparison paths must be collision-proof ----------------

grep -Fq 'mktemp "${BUNDLE_DIR}/.remote-${name}.XXXXXXXX"' "$SCRIPT" ||
  fail "publisher must allocate retained remote comparisons with an exclusive unique template"
if grep -Fq '.remote-${name}-$$' "$SCRIPT"; then
  fail "publisher must not reuse a predictable PID-based comparison path"
fi

echo "publish_bundle_to_gitee.sh tests passed"
