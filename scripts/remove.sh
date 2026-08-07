#!/usr/bin/env bash
# For coding-agent maintainers:
# - Read this repository's root ANATOMY.md before changing this file.
# - Preserve this entrypoint's operation, ownership, consent, and mutation boundary;
#   do not turn one operation into an implicit install, update, repair, or deploy.
# - Keep executable behavior, CONTRACT.md, ANATOMY.md, and README guidance
#   in lockstep; do not add static, shim, fake-command, or hermetic acceptance tests.
# - Every remove.sh change must execute this exact final candidate as a real
#   subprocess against real owned installation state (real venv, real installed
#   "lingtai" distribution, real install.json receipt) in isolated non-root Linux,
#   then observe the actual deletion/receipt postconditions or actual partial
#   failure. Source grep and fake CLI output are not acceptance.
# - Never publish a success receipt before every declared postcondition passes;
#   report partial state honestly and do not treat failure as cleanup authority.
# - These maintenance rules grant no merge, release, deploy, auth, config, or
#   deletion authority.
# This file is the first lifecycle asset whose purpose is deletion. It deletes
# ONLY the exact artifact set the receipt proves this installation owns
# (binaries, the receipt-pointed runtime venv, and finally the receipt
# itself); it never touches config.json, .env, presets/saved/, auth material,
# per-project .lingtai/ state, a Homebrew-managed installation, or any
# filename-pattern-matched directory the receipt does not itself name. The
# receipt is the only deletion oracle: a leftover runtime/venv-repair-* (or
# any other) directory that install.sh/fix.sh may retain but the receipt does
# not point at is reported as a survivor, never swept by name pattern alone.
# Deletion is refused, not attempted, whenever ownership cannot be proven.
# Explicit owned-installation removal. Standalone; never sources install.sh.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: remove.sh --bin-dir DIR --yes

Deletes only the exact installation the lingtai.tui.install/v1 receipt at
$HOME/.lingtai-tui/install.json proves --bin-dir owns: the managed binaries
(lingtai-tui, lingtai-portal, the lingtai/lingtai-agent symlinks), the
receipt-pointed runtime venv, and finally the receipt itself. The receipt is
the only deletion oracle; a leftover runtime directory the receipt does not
point at (e.g. an old runtime/venv retained by a prior repair) is reported as
a survivor, never swept by filename pattern. It never touches user config,
secrets, presets, auth, per-project state, or a Homebrew-managed installation
(refused; run `brew uninstall` instead). Deletes the receipt last so a
partial failure never claims full removal. Running remove.sh twice is safe:
the second run reports nothing to remove.
EOF
}
fail() { echo "remove.sh: error: $*" >&2; exit 1; }
partial_fail() { echo "remove.sh: error: $*" >&2; exit 1; }
abs() { [[ "$1" == /* && "$1" != *$'\n'* && "$1" != *$'\t'* && "$1" != */../* && "$1" != */./* ]]; }

bin_dir= yes=0
while (($#)); do
  case "$1" in
    --bin-dir) (($# >= 2)) || fail "--bin-dir requires DIR"; bin_dir=$2; shift 2 ;;
    --yes) yes=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done
[[ -n "$bin_dir" ]] || { usage >&2; exit 2; }
abs "$bin_dir" || fail "bin directory is not an exact absolute path"
[[ "$yes" == 1 ]] || fail "removal is mutating; provide --yes after reviewing the plan"

state_root="$HOME/.lingtai-tui"
runtime_root="$state_root/runtime"
metadata="$state_root/install.json"

# Idempotent second run: no receipt at all means nothing owned to remove.
if [[ ! -e "$metadata" && ! -L "$metadata" ]]; then
  printf 'PASS: nothing to remove; no install receipt at %s.\n' "$metadata"
  exit 0
fi

[[ ! -L "$state_root" && ! -L "$runtime_root" ]] || fail "owned installation/runtime root is a symlink"
[[ -f "$metadata" && ! -L "$metadata" ]] || fail "install metadata is missing or redirected"

bootstrap="$(command -v python3 2>/dev/null)" || fail "python3 is required to parse the receipt"
[[ -x "$bootstrap" ]] || fail "python3 bootstrap is not executable"

# Homebrew-managed installs are never touched here; refuse and point at brew.
# Mirrors tui/internal/config/venv.go's detectHomebrewTUIInstall: an env-var
# prefix match, a literal /Cellar/ path component, or one of the well-known
# Homebrew root prefixes.
is_homebrew_path() {
  local path="$1" prefix
  for prefix in "${HOMEBREW_PREFIX:-}" "${HOMEBREW_CELLAR:-}" "${HOMEBREW_REPOSITORY:-}"; do
    [[ -n "$prefix" ]] || continue
    [[ "$path" == "$prefix" || "$path" == "$prefix"/* ]] && return 0
  done
  case "$path" in
    */Cellar/*|/opt/homebrew|/opt/homebrew/*|/home/linuxbrew/.linuxbrew|/home/linuxbrew/.linuxbrew/*|/usr/local/Homebrew|/usr/local/Homebrew/*|/usr/local/Cellar|/usr/local/Cellar/*) return 0 ;;
  esac
  return 1
}
# Path-based only (mirrors detectHomebrewTUIInstall in tui/internal/config/venv.go
# exactly): a name match via "brew list lingtai-tui" would refuse removal of an
# unrelated --bin-dir just because a Homebrew-managed lingtai-tui happens to be
# installed somewhere else on the same machine.
if [[ -f "$bin_dir/lingtai-tui" && ! -L "$bin_dir/lingtai-tui" ]]; then
  resolved_tui="$(cd "$bin_dir" 2>/dev/null && pwd -P)/lingtai-tui"
  if is_homebrew_path "$resolved_tui"; then
    fail "target is a Homebrew-managed installation ($resolved_tui); use 'brew uninstall lingtai-tui' instead, remove.sh will not touch it"
  fi
fi

# LINGTAI_RECEIPT_PARSE_REMOVE: strict v1 receipt parse, ownership match against
# --bin-dir, and (if present) a physically-contained runtime_venv pointer. Does
# not execute or import the installed runtime; only reads/validates the receipt.
plan="$("$bootstrap" - "$metadata" "$bin_dir" "$runtime_root" <<'PY'
# LINGTAI_RECEIPT_PARSE_REMOVE
import json, os, re, sys
path, expected_bin, runtime_root = sys.argv[1:]
def pairs(items):
    out = {}
    for key, value in items:
        if key in out:
            raise ValueError("duplicate JSON key: " + key)
        out[key] = value
    return out
try:
    with open(path, encoding="utf-8") as stream:
        data = json.load(stream, object_pairs_hook=pairs)
except Exception as exc:
    raise SystemExit("metadata JSON: %s" % exc)
if not isinstance(data, dict): raise SystemExit("metadata is not an object")
if data.get("schema") != "lingtai.tui.install/v1": raise SystemExit("unexpected schema")
if type(data.get("schema_version")) is not int or data["schema_version"] != 1: raise SystemExit("unexpected schema_version")
if data.get("bin_dir") != expected_bin: raise SystemExit("bin_dir does not own this target")
target = expected_bin + "/lingtai-tui"
managed = data.get("managed_binaries")
if not isinstance(managed, list) or target not in managed: raise SystemExit("managed_binaries does not own lingtai-tui")
if data.get("install_kind") not in ("release-asset", "source-build", "dev-source"): raise SystemExit("receipt install_kind is not recognized")

portal_target = expected_bin + "/lingtai-portal"
has_portal = portal_target in managed

venv = data.get("runtime_venv")
has_venv = False
if venv is not None:
    if not isinstance(venv, str) or not os.path.isabs(venv): raise SystemExit("runtime_venv is not absolute")
    if "\x00" in venv or "\n" in venv or "\r" in venv or "\t" in venv: raise SystemExit("runtime_venv contains unsafe characters")
    if os.path.normpath(venv) != venv or os.path.dirname(venv) != runtime_root: raise SystemExit("runtime_venv is not a normalized direct child of the runtime root")
    name = os.path.basename(venv)
    if not re.fullmatch(r"[A-Za-z0-9._-]+", name) or venv != runtime_root + "/" + name: raise SystemExit("runtime_venv is not a safe direct child")
    if os.path.lexists(venv):
        if os.path.islink(venv) or not os.path.isdir(venv): raise SystemExit("runtime_venv exists but is not a real directory")
        has_venv = True

# "-" is a sentinel for "absent" (never a valid value: every real field here
# is an absolute path or a 0/1 flag) so the read below never has to parse an
# empty field between two tabs: a tab-delimited read still squeezes adjacent
# whitespace-class delimiters exactly like word-splitting does, which
# silently drops empty fields and shifts every field after it.
out = [target, "1" if has_portal else "0", portal_target if has_portal else "-", venv or "-", "1" if has_venv else "0"]
print("\t".join(out))
PY
)" || {
  # On any refusal, best-effort surface the receipt's own bin_dir (if the
  # receipt is at least readable JSON) so a user who guessed wrong has an
  # actionable next step instead of having to read install.json by hand.
  receipt_bin_dir="$("$bootstrap" -c '
import json, sys
try:
    with open(sys.argv[1], encoding="utf-8") as stream:
        data = json.load(stream)
    value = data.get("bin_dir") if isinstance(data, dict) else None
    print(value if isinstance(value, str) and value else "")
except Exception:
    print("")
' "$metadata" 2>/dev/null)" || receipt_bin_dir=""
  if [[ -n "$receipt_bin_dir" && "$receipt_bin_dir" != "$bin_dir" ]]; then
    fail "install metadata is not a strict, owned v1 receipt for --bin-dir $bin_dir; this receipt owns --bin-dir $receipt_bin_dir -- re-run with: remove.sh --bin-dir $receipt_bin_dir --yes"
  fi
  fail "install metadata is not a strict, owned v1 receipt for --bin-dir $bin_dir"
}
IFS="$(printf '\t')" read -r tui_target has_portal portal_target venv_path has_venv <<EOF
$plan
EOF
[[ -n "$tui_target" ]] || fail "receipt parser emitted no managed TUI target"

owned_root=""
if [[ -d "$runtime_root" && ! -L "$runtime_root" ]]; then
  owned_root="$(cd "$runtime_root" 2>/dev/null && pwd -P)" || fail "owned runtime root cannot be canonicalized"
fi

# Every deletion candidate is re-verified as real (not a symlink where a real
# path is expected, not escaped from the owned root) immediately before this
# plan is printed, and again immediately before each individual deletion
# below (TOCTOU-safe: stat right before unlink, not only here).
plan_lines=()
plan_lines+=("$tui_target")
[[ "$has_portal" == 1 ]] && plan_lines+=("$portal_target")
[[ -L "$bin_dir/lingtai" ]] && [[ "$(readlink "$bin_dir/lingtai")" == "$bin_dir/lingtai-tui" ]] && plan_lines+=("$bin_dir/lingtai (alias symlink)")
if [[ "$has_venv" == 1 ]]; then
  if [[ -x "$venv_path/bin/lingtai-agent" ]] && [[ -L "$bin_dir/lingtai-agent" ]] && [[ "$(readlink "$bin_dir/lingtai-agent")" == "$venv_path/bin/lingtai-agent" ]]; then
    plan_lines+=("$bin_dir/lingtai-agent (agent symlink)")
  fi
  plan_lines+=("$venv_path (runtime venv)")
fi
plan_lines+=("$metadata (receipt, removed last)")

printf 'Plan: the following owned artifacts will be removed:\n'
for line in "${plan_lines[@]}"; do printf '  - %s\n' "$line"; done

# --- deletion (order matters: receipt last, so a partial failure never
# leaves a receipt that falsely claims full removal) -------------------------

removed=() failed=()

remove_symlink_if_exact() {
  local path="$1" want_target="$2" label="$3"
  if [[ -L "$path" ]]; then
    local current
    current="$(readlink "$path" 2>/dev/null || true)"
    if [[ "$current" == "$want_target" ]]; then
      if rm -f -- "$path"; then removed+=("$label"); else failed+=("$label (rm failed)"); fi
    fi
  fi
}

remove_symlink_if_exact "$bin_dir/lingtai-agent" "$venv_path/bin/lingtai-agent" "lingtai-agent symlink"
remove_symlink_if_exact "$bin_dir/lingtai" "$bin_dir/lingtai-tui" "lingtai alias symlink"

if [[ "$has_portal" == 1 && ( -e "$portal_target" || -L "$portal_target" ) ]]; then
  if [[ ! -L "$portal_target" && -f "$portal_target" ]]; then
    if rm -f -- "$portal_target"; then removed+=("lingtai-portal binary"); else failed+=("lingtai-portal binary (rm failed)"); fi
  else
    failed+=("lingtai-portal binary (not an owned regular file, left in place)")
  fi
fi

if [[ -e "$tui_target" || -L "$tui_target" ]]; then
  if [[ ! -L "$tui_target" && -f "$tui_target" ]]; then
    if rm -f -- "$tui_target"; then removed+=("lingtai-tui binary"); else failed+=("lingtai-tui binary (rm failed)"); fi
  else
    failed+=("lingtai-tui binary (not an owned regular file, left in place)")
  fi
fi

if [[ "$has_venv" == 1 ]]; then
  # Re-validate physical containment at delete-time, not from the earlier
  # precondition read: the kernel's own venv-mismatch recovery can delete and
  # recreate this venv between the plan read and this deletion.
  if [[ -n "$owned_root" && -d "$venv_path" && ! -L "$venv_path" ]]; then
    venv_physical="$(cd "$venv_path" 2>/dev/null && pwd -P)" || venv_physical=""
    venv_parent_physical="$(cd "$(dirname "$venv_path")" 2>/dev/null && pwd -P)" || venv_parent_physical=""
    if [[ -n "$venv_physical" && "$venv_parent_physical" == "$owned_root" ]]; then
      if rm -rf -- "$venv_path"; then removed+=("runtime venv"); else failed+=("runtime venv (rm failed)"); fi
    else
      failed+=("runtime venv (failed physical containment re-check, left in place)")
    fi
  elif [[ -e "$venv_path" || -L "$venv_path" ]]; then
    failed+=("runtime venv (no longer a real contained directory, left in place)")
  fi
fi

if ((${#failed[@]} > 0)); then
  printf 'PARTIAL: the following owned artifacts were removed:\n' >&2
  for line in "${removed[@]:-}"; do [[ -n "$line" ]] && printf '  - %s\n' "$line" >&2; done
  joined_failed="$(printf '%s; ' "${failed[@]}")"
  partial_fail "the following owned artifacts could NOT be removed and the receipt was left in place so this state is not silently claimed as fully removed: ${joined_failed%; }"
fi

# Only after every above step succeeds is the receipt itself removed.
if ! rm -f -- "$metadata"; then
  partial_fail "every other owned artifact was removed, but the receipt at $metadata could not be deleted; re-run remove.sh to retry"
fi

# Best-effort: remove the owned roots only if now empty. Never a recursive
# removal of the state root -- it may still legitimately contain NOT-owned
# files (config.json, .env, presets/saved/, codex-auth*.json, meta.json,
# registry.jsonl). rmdir fails loudly (silently, here, since it is
# best-effort informational only) on a non-empty directory rather than
# following it.
survivors=()
if [[ -n "$owned_root" ]]; then
  rmdir "$runtime_root" 2>/dev/null || true
fi
if [[ -d "$state_root" && ! -L "$state_root" ]]; then
  if ! rmdir "$state_root" 2>/dev/null; then
    # -printf is a GNU find extension unavailable on macOS/BSD find; basename
    # every entry from a plain -mindepth/-maxdepth listing instead, which both
    # find implementations support identically.
    while IFS= read -r entry; do
      [[ -n "$entry" ]] && survivors+=("$(basename -- "$entry")")
    done < <(find "$state_root" -mindepth 1 -maxdepth 1 2>/dev/null)
  fi
fi

if ((${#survivors[@]} > 0)); then
  printf 'PASS: owned installation removed (%d artifact(s)); %s left in place because it still contains: %s\n' \
    "${#removed[@]}" "$state_root" "$(printf '%s, ' "${survivors[@]}" | sed 's/, $//')"
else
  printf 'PASS: owned installation removed (%d artifact(s)); no unowned state remained under %s.\n' "${#removed[@]}" "$state_root"
fi
