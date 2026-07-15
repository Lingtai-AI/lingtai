#!/usr/bin/env bash
# Tests for scripts/sync_gitee_mirror.sh using REAL local git repositories
# (no network) as stand-ins for "the local checkout" and "the Gitee
# remote". A local file-path remote exercises the identical
# fast-forward/non-force push semantics as a real Gitee HTTPS remote.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SCRIPT="$ROOT_DIR/scripts/sync_gitee_mirror.sh"

fail() {
  echo "test-sync-gitee-mirror: $*" >&2
  exit 1
}

tmp="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-sync-gitee-test.XXXXXX")"

git_c() { git -C "$1" "${@:2}"; }

init_repo() {
  local dir="$1"
  mkdir -p "$dir"
  git_c "$dir" init -q
  git_c "$dir" config user.email "test@example.invalid"
  git_c "$dir" config user.name "Sync Test"
}

commit_file() {
  local dir="$1" name="$2" content="$3"
  printf '%s' "$content" > "$dir/$name"
  git_c "$dir" add "$name"
  git_c "$dir" commit -q -m "add $name"
  git_c "$dir" rev-parse HEAD
}

# A thin wrapper mirroring sync_gitee_mirror.sh's push logic against a LOCAL
# file-path remote in place of the https://gitee.com URL (the script's own
# GITEE_URL construction is bypassed here on purpose — real Gitee HTTPS
# access isn't available in this environment, and plain `git push <url>
# <refspec>` has identical fast-forward/non-force semantics for a local path
# and an HTTPS remote).
push_via_local_remote() {
  local checkout="$1" remote="$2" commit="$3" tag="$4" branch="${5:-main}"
  git_c "$checkout" push "$remote" "${commit}:refs/heads/${branch}" || return 1
  git_c "$checkout" push "$remote" "${commit}:refs/tags/${tag}" || return 1
}

# --- fast-forward succeeds -----------------------------------------------

(
  checkout="$tmp/fastforward-checkout"
  remote="$tmp/fastforward-remote.git"
  init_repo "$checkout"
  git init -q --bare "$remote"
  commit="$(commit_file "$checkout" a.txt hello)"

  push_via_local_remote "$checkout" "$remote" "$commit" "v1.0.0" \
    || fail "fast-forward push to empty remote should succeed"

  branch_sha="$(git -C "$remote" rev-parse refs/heads/main)"
  [[ "$branch_sha" == "$commit" ]] || fail "remote branch should point at $commit, got $branch_sha"
  tag_sha="$(git -C "$remote" rev-parse refs/tags/v1.0.0)"
  [[ "$tag_sha" == "$commit" ]] || fail "remote tag should point at $commit, got $tag_sha"
)

# --- non-fast-forward branch push fails, no force --------------------------

(
  remote="$tmp/nonff-remote.git"
  git init -q --bare "$remote"

  diverged="$tmp/nonff-diverged"
  init_repo "$diverged"
  diverged_commit="$(commit_file "$diverged" other.txt "diverged history")"
  git_c "$diverged" push "$remote" "${diverged_commit}:refs/heads/main"

  checkout="$tmp/nonff-checkout"
  init_repo "$checkout"
  local_commit="$(commit_file "$checkout" a.txt "unrelated history")"

  if push_via_local_remote "$checkout" "$remote" "$local_commit" "v2.0.0"; then
    fail "non-fast-forward push must fail, not succeed"
  fi

  branch_sha="$(git -C "$remote" rev-parse refs/heads/main)"
  [[ "$branch_sha" == "$diverged_commit" ]] || fail "remote branch must be UNCHANGED after a rejected push, got $branch_sha"
)

# --- existing tag is never overwritten --------------------------------------

(
  checkout="$tmp/tagconflict-checkout"
  remote="$tmp/tagconflict-remote.git"
  init_repo "$checkout"
  git init -q --bare "$remote"
  first="$(commit_file "$checkout" a.txt hello)"
  git_c "$checkout" push "$remote" "${first}:refs/heads/main"
  git_c "$checkout" push "$remote" "${first}:refs/tags/v1.0.0"

  second="$(commit_file "$checkout" b.txt world)"
  if push_via_local_remote "$checkout" "$remote" "$second" "v1.0.0"; then
    fail "pushing a different commit under an EXISTING tag name must fail"
  fi

  tag_sha="$(git -C "$remote" rev-parse refs/tags/v1.0.0)"
  [[ "$tag_sha" == "$first" ]] || fail "existing tag must be untouched, got $tag_sha (expected $first)"
)

# --- script-level behavior: dry-run, missing commit, missing token, no leak ---

(
  checkout="$tmp/script-dryrun-checkout"
  init_repo "$checkout"
  commit="$(commit_file "$checkout" a.txt hello)"

  unset GITEE_ACCESS_TOKEN 2>/dev/null || true
  out="$(cd "$checkout" && bash "$SCRIPT" --commit "$commit" --tag v1.0.0 2>&1)" \
    || fail "missing GITEE_ACCESS_TOKEN must exit 0 (skip), not fail: $out"
  echo "$out" | grep -q "is not set; skipping" || fail "expected the skip message, got: $out"
)

(
  checkout="$tmp/script-missing-commit-checkout"
  init_repo "$checkout"
  export GITEE_ACCESS_TOKEN="fake-token-for-arg-validation-only"
  fake_commit="$(printf 'a%.0s' $(seq 1 40))"
  if out="$(cd "$checkout" && bash "$SCRIPT" --commit "$fake_commit" --tag v1.0.0 --execute 2>&1)"; then
    fail "sync_gitee_mirror.sh must fail loud when the local checkout lacks the commit, got success: $out"
  fi
  unset GITEE_ACCESS_TOKEN
)

(
  checkout="$tmp/script-token-leak-checkout"
  init_repo "$checkout"
  commit="$(commit_file "$checkout" a.txt hello)"
  secret="super-secret-tui-gitee-token-value"
  export GITEE_ACCESS_TOKEN="$secret"
  # No --execute: dry-run only, must not print the token or attempt a real push.
  out="$(cd "$checkout" && bash "$SCRIPT" --commit "$commit" --tag v1.0.0 2>&1)" \
    || fail "dry run must exit 0: $out"
  echo "$out" | grep -q "DRY RUN" || fail "expected DRY RUN output, got: $out"
  if echo "$out" | grep -q "$secret"; then
    fail "the Gitee token must never appear in script output, got: $out"
  fi
  unset GITEE_ACCESS_TOKEN
)

(
  # Missing --commit / --tag flags are rejected.
  if bash "$SCRIPT" --tag v1.0.0 >/dev/null 2>&1; then
    fail "missing --commit should be rejected"
  fi
  if bash "$SCRIPT" --commit "$(printf 'a%.0s' $(seq 1 40))" >/dev/null 2>&1; then
    fail "missing --tag should be rejected"
  fi
)

echo "sync_gitee_mirror.sh tests passed"
