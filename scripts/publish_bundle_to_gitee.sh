#!/usr/bin/env bash
# Publish a TUI release bundle (archives + checksums + bundle manifest) to
# Gitee, using the exact bytes already uploaded to the GitHub release by
# release.yml's release-assets/publish-bundle jobs. Never rebuilds anything.
#
# Safety: every mutating action requires --execute. Without it (the default)
# this script only prints its plan and exits 0. release.yml never passes
# --execute — actual Gitee publication requires a separate authorized run.
#
# Auth: GITEE_ACCESS_TOKEN env var, never echoed/logged. If unset, this script
# prints why it is skipping and exits 0 (not a failure — Gitee credentials are
# a separate authorization step from shipping the GitHub release).
#
# Gitee v5 REST contract used:
#   GET  /v5/repos/{owner}/{repo}/tags/{tag}                (verify sync)
#   GET  /v5/repos/{owner}/{repo}/releases/tags/{tag}        (find release)
#   POST /v5/repos/{owner}/{repo}/releases                   (create release)
#   GET  /v5/repos/{owner}/{repo}/releases/{id}               (list attachments)
#   POST /v5/repos/{owner}/{repo}/releases/{id}/attach_files (upload attachment)
set -euo pipefail

GITEE_API_BASE="https://gitee.com/api/v5"
GITEE_OWNER="${GITEE_OWNER:-huangzesen1997}"
GITEE_REPO="${GITEE_REPO:-lingtai}"
EXECUTE=0
BUNDLE_DIR=""
TAG=""

usage() {
  cat <<'EOF'
Usage: publish_bundle_to_gitee.sh --tag vX.Y.Z --bundle-dir <dir> [--execute]

--bundle-dir must contain: the bundle manifest (lingtai-bundle-manifest.json),
every lingtai-<tag>-<os>-<arch>.tar.gz archive it references, and each
archive's .sha256 sidecar.

Without --execute, prints the upload plan and exits 0 (dry run, the default
and the only mode release.yml currently invokes).
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag) TAG="${2:?}"; shift 2 ;;
    --bundle-dir) BUNDLE_DIR="${2:?}"; shift 2 ;;
    --execute) EXECUTE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown flag: $1" >&2; usage >&2; exit 1 ;;
  esac
done

[[ -n "$TAG" ]] || { echo "error: --tag is required" >&2; exit 1; }
[[ -n "$BUNDLE_DIR" ]] || { echo "error: --bundle-dir is required" >&2; exit 1; }
[[ -d "$BUNDLE_DIR" ]] || { echo "error: --bundle-dir not found: $BUNDLE_DIR" >&2; exit 1; }

MANIFEST="$BUNDLE_DIR/lingtai-bundle-manifest.json"
[[ -f "$MANIFEST" ]] || { echo "error: bundle manifest not found: $MANIFEST" >&2; exit 1; }

# --- local asset verification: publish only bytes that match the manifest ---

verify_local_assets() {
  python3 - "$MANIFEST" "$BUNDLE_DIR" <<'PY'
import hashlib, json, sys
from pathlib import Path

manifest_path, bundle_dir = sys.argv[1], Path(sys.argv[2])
data = json.loads(Path(manifest_path).read_text())
if data.get("schema") != "lingtai.tui.bundle/v1":
    sys.exit(f"error: unexpected bundle manifest schema: {data.get('schema')!r}")
for archive in data.get("archives", []):
    if not isinstance(archive, dict) or not archive.get("filename") or not archive.get("sha256"):
        sys.exit("error: malformed archive entry")
    path = bundle_dir / archive["filename"]
    if not path.is_file():
        sys.exit(f"error: archive listed in bundle manifest is missing on disk: {path}")
    h = hashlib.sha256(path.read_bytes()).hexdigest()
    if h != archive["sha256"]:
        sys.exit(
            f"error: {archive['filename']} sha256 mismatch: "
            f"manifest={archive['sha256']} on-disk={h}"
        )
    sidecar = bundle_dir / (archive["filename"] + ".sha256")
    if not sidecar.is_file():
        sys.exit(f"error: archive sidecar is missing on disk: {sidecar}")
    parts = sidecar.read_text().split()
    if not parts or parts[0] != h:
        sys.exit(f"error: {sidecar.name} disagrees with archive bytes")
print(f"local assets OK: {len(data.get('archives', []))} archive(s) match the bundle manifest")
PY
}

verify_local_assets

if [[ -z "${GITEE_ACCESS_TOKEN:-}" ]]; then
  echo "[gitee] GITEE_ACCESS_TOKEN is not set; skipping Gitee publish."
  exit 0
fi

# --- Gitee v5 helpers -------------------------------------------------------

gitee_get() {
  local path="$1"
  curl -fsSL --max-time 15 "${GITEE_API_BASE}${path}?access_token=${GITEE_ACCESS_TOKEN}"
}

# Fail loud unless Gitee's tag already points at the exact commit the bundle
# was built from. Never force-pushes or otherwise mutates the Gitee git repo —
# synchronizing the mirror is an out-of-band precondition this script checks.
verify_tag_synchronized() {
  local expected_commit body sha
  expected_commit="$(python3 -c "import json; print(json.load(open('$MANIFEST'))['tui_commit'])")"
  if ! body="$(gitee_get "/repos/${GITEE_OWNER}/${GITEE_REPO}/tags/${TAG}")"; then
    echo "error: Gitee tag ${TAG} not found on ${GITEE_OWNER}/${GITEE_REPO} (or lookup failed)." >&2
    echo "       The TUI Gitee mirror must be synchronized to commit ${expected_commit} and tag ${TAG}" >&2
    echo "       before publishing there. This script will not push or force-sync the mirror." >&2
    return 1
  fi
  sha="$(printf '%s' "$body" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("commit",{}).get("sha",""))' 2>/dev/null || true)"
  if [[ "$sha" != "$expected_commit" ]]; then
    echo "error: Gitee tag ${TAG} points at commit ${sha:-<unknown>}, expected ${expected_commit}." >&2
    echo "       Refusing to publish assets against a mismatched tag." >&2
    return 1
  fi
  echo "[gitee] tag ${TAG} is synchronized to ${expected_commit:0:12}"
}

find_release_id() {
  local body
  body="$(gitee_get "/repos/${GITEE_OWNER}/${GITEE_REPO}/releases/tags/${TAG}" 2>/dev/null || true)"
  [[ -n "$body" ]] || { echo ""; return 0; }
  printf '%s' "$body" | python3 -c 'import json,sys
try:
    print(json.load(sys.stdin).get("id",""))
except Exception:
    print("")' 2>/dev/null || echo ""
}

existing_attachments() {
  local release_id="$1" body
  body="$(gitee_get "/repos/${GITEE_OWNER}/${GITEE_REPO}/releases/${release_id}")"
  printf '%s' "$body" | python3 -c 'import json,sys
data = json.load(sys.stdin)
for a in data.get("attach_files", []):
    url = a.get("browserDownloadUrl") or a.get("browser_download_url") or ""
    print(f"{a.get('name', '')}\t{url}")'
}

# --- plan --------------------------------------------------------------------

echo "[gitee] target: ${GITEE_OWNER}/${GITEE_REPO} tag ${TAG}"
verify_tag_synchronized

release_id="$(find_release_id)"
files_to_upload=("$MANIFEST")
while IFS= read -r name; do
  files_to_upload+=("$BUNDLE_DIR/$name" "$BUNDLE_DIR/$name.sha256")
done < <(python3 - "$MANIFEST" <<'PY'
import json, sys
data = json.load(open(sys.argv[1]))
for archive in data["archives"]:
    print(archive["filename"])
PY
)

if [[ -z "$release_id" ]]; then
  echo "[gitee] release ${TAG} does not exist yet"
  if [[ "$EXECUTE" == "1" ]]; then
    body="$(curl -fsSL --max-time 15 -X POST \
      -H 'Content-Type: application/json;charset=UTF-8' \
      -d "{\"tag_name\":\"${TAG}\",\"name\":\"${TAG}\",\"body\":\"Release ${TAG}\",\"prerelease\":false}" \
      "${GITEE_API_BASE}/repos/${GITEE_OWNER}/${GITEE_REPO}/releases?access_token=${GITEE_ACCESS_TOKEN}")"
    release_id="$(printf '%s' "$body" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])')"
  else
    echo "[gitee] DRY RUN: would create release ${TAG}"
    echo "[gitee] DRY RUN: cannot plan attachment uploads without a release id (would create first)"
    exit 0
  fi
fi

existing="$(existing_attachments "$release_id")"
for f in "${files_to_upload[@]}"; do
  name="$(basename "$f")"
  attachment_url="$(printf '%s\n' "$existing" | awk -F '\t' -v name="$name" '$1 == name {print $2; exit}')"
  if [[ -n "$attachment_url" ]]; then
    if ! remote="$(mktemp "${BUNDLE_DIR}/.remote-${name}.XXXXXXXX")"; then
      echo "error: could not allocate a unique retained comparison path for $name" >&2
      exit 1
    fi
    if ! curl -fsSL --max-time 60 -o "$remote" "$attachment_url"; then
      echo "error: could not download existing Gitee attachment $name for byte verification" >&2
      exit 1
    fi
    local_sha="$(sha256sum "$f" | cut -d' ' -f1)"
    remote_sha="$(sha256sum "$remote" | cut -d' ' -f1)"
    if [[ "$local_sha" != "$remote_sha" ]]; then
      echo "error: existing Gitee attachment $name has different bytes; refusing clobber" >&2
      exit 1
    fi
    echo "[gitee] ${name}: existing bytes match; skipping (idempotent, no delete-and-replace)"
    continue
  fi
  if printf '%s\n' "$existing" | awk -F '\t' -v name="$name" '$1 == name {found=1} END {exit !found}'; then
    echo "error: existing Gitee attachment $name has no usable download URL; refusing unverified skip" >&2
    exit 1
  fi
  if [[ "$EXECUTE" == "1" ]]; then
    curl -fsSL --max-time 60 -X POST \
      -F "file=@${f}" \
      "${GITEE_API_BASE}/repos/${GITEE_OWNER}/${GITEE_REPO}/releases/${release_id}/attach_files?access_token=${GITEE_ACCESS_TOKEN}" \
      -o /dev/null
    echo "[gitee] uploaded ${name}"
  else
    echo "[gitee] DRY RUN: would upload ${name}"
  fi
done
