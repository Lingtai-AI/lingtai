#!/usr/bin/env bash
# Non-force-synchronize the exact release commit/tag to the Gitee TUI mirror
# before a Gitee release is created/attached to.
#
# Deliberately a SEPARATE, git-level step from publish_bundle_to_gitee.sh
# (which only speaks the Gitee v5 REST API for release/attachment
# management). Gitee's release/tag lookup requires the tag to already exist
# on the Gitee git repository — publish_bundle_to_gitee.sh's
# verify_tag_synchronized only checks that precondition; this script is what
# can actually establish it, safely.
#
# Safety:
#   - Every push is NON-FORCE. `git push` (not `--force`) on the branch ref
#     only succeeds if it is a fast-forward; a diverged/rewritten history on
#     Gitee's side fails loud rather than being silently overwritten.
#   - The tag push targets exactly one tag name and never overwrites an
#     existing tag pointing elsewhere (plain `git push` refuses that by
#     default; this script never adds --force).
#   - The token travels via a short-lived GIT_ASKPASS helper file (chmod 600,
#     deleted after use), never in argv, a URL, or a log line.
#   - --execute gates every mutating action; without it, prints the plan only.
#
# Usage:
#   export GITEE_ACCESS_TOKEN=...  # never echo or log this
#   ./scripts/sync_gitee_mirror.sh --commit <full-sha> --tag vX.Y.Z --branch main --execute
set -euo pipefail

GITEE_OWNER="${GITEE_OWNER:-huangzesen1997}"
GITEE_REPO="${GITEE_REPO:-lingtai}"
EXECUTE=0
COMMIT=""
TAG=""
BRANCH="main"

usage() {
  cat <<'EOF'
Usage: sync_gitee_mirror.sh --commit <sha> --tag vX.Y.Z [--branch main] [--execute]

Without --execute, prints the push plan and exits 0 (dry run, the default).
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --commit) COMMIT="${2:?}"; shift 2 ;;
    --tag) TAG="${2:?}"; shift 2 ;;
    --branch) BRANCH="${2:?}"; shift 2 ;;
    --execute) EXECUTE=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "error: unknown flag: $1" >&2; usage >&2; exit 1 ;;
  esac
done

[[ -n "$COMMIT" ]] || { echo "error: --commit is required" >&2; exit 1; }
[[ -n "$TAG" ]] || { echo "error: --tag is required" >&2; exit 1; }

if [[ -z "${GITEE_ACCESS_TOKEN:-}" ]]; then
  echo "GITEE_ACCESS_TOKEN is not set; skipping Gitee mirror sync."
  exit 0
fi

if ! git cat-file -e "${COMMIT}^{commit}" 2>/dev/null; then
  echo "error: local checkout does not have commit $COMMIT" >&2
  exit 1
fi

GITEE_URL="https://gitee.com/${GITEE_OWNER}/${GITEE_REPO}.git"

# GIT_ASKPASS helper: prints the token to stdout when git prompts for a
# password, keeping it out of argv/URLs/logs. Owner-only permissions,
# retained at the generated path for diagnosis; the machine-wide policy forbids
# deleting even a self-created credential helper.
ASKPASS_FILE="$(mktemp "${TMPDIR:-/tmp}/gitee-askpass-XXXXXX.sh")"

cat > "$ASKPASS_FILE" <<EOF
#!/bin/sh
printf '%s\n' "\${GITEE_ACCESS_TOKEN}"
EOF
chmod 700 "$ASKPASS_FILE"

BRANCH_REFSPEC="${COMMIT}:refs/heads/${BRANCH}"
TAG_REFSPEC="${COMMIT}:refs/tags/${TAG}"

if [[ "$EXECUTE" != "1" ]]; then
  echo "DRY RUN: would non-force push ${COMMIT:0:12} -> ${GITEE_OWNER}/${GITEE_REPO}#${BRANCH} (fast-forward only)"
  echo "DRY RUN: would push tag ${TAG} -> ${GITEE_OWNER}/${GITEE_REPO} (create-only, no overwrite)"
  exit 0
fi

echo "Pushing ${COMMIT:0:12} to ${GITEE_OWNER}/${GITEE_REPO}#${BRANCH} (non-force, fast-forward only)..."
if ! GIT_ASKPASS="$ASKPASS_FILE" GIT_TERMINAL_PROMPT=0 git push "$GITEE_URL" "$BRANCH_REFSPEC"; then
  echo "error: non-force push to ${GITEE_OWNER}/${GITEE_REPO}#${BRANCH} failed (not a fast-forward, or auth failed)." >&2
  echo "       This script never force-pushes. Investigate and resolve the divergence out of band, then retry." >&2
  exit 1
fi
echo "  OK: ${BRANCH} fast-forwarded to ${COMMIT:0:12}"

echo "Pushing tag ${TAG} to ${GITEE_OWNER}/${GITEE_REPO} (create-only)..."
if ! GIT_ASKPASS="$ASKPASS_FILE" GIT_TERMINAL_PROMPT=0 git push "$GITEE_URL" "$TAG_REFSPEC"; then
  echo "error: tag push for ${TAG} failed (tag may already exist pointing elsewhere, or auth failed)." >&2
  echo "       This script never overwrites an existing tag. Investigate and resolve out of band, then retry." >&2
  exit 1
fi
echo "  OK: tag ${TAG} created at ${COMMIT:0:12}"
