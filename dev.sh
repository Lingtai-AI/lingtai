#!/usr/bin/env bash
# For coding-agent maintainers:
# - Read this repository's root ANATOMY.md before changing this file.
# - Preserve this entrypoint's operation, ownership, consent, and mutation boundary;
#   do not turn one operation into an implicit install, update, repair, or deploy.
# - Keep executable behavior, CONTRACT.md, ANATOMY.md, and README guidance
#   in lockstep; do not add static, shim, fake-command, or hermetic acceptance tests.
# - Every dev.sh change must execute a real source build and editable install in
#   isolated non-root Linux with exact checkouts, then observe binary/runtime/import/
#   source-provenance/receipt postconditions or actual partial failure.
#   Source grep and fake CLI output are not acceptance.
# - Never publish a success receipt before every declared postcondition passes;
#   report partial state honestly and do not treat failure as cleanup authority.
# - These maintenance rules grant no merge, release, deploy, auth, config, or
#   deletion authority.
# This file owns only explicit editable development state after --yes; build writes
# stay in the named checkouts/runtime/target and never imply release or deployment.
# Explicit editable development installation. Standalone; never sources install.sh.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: dev.sh --tui-source DIR --kernel-source DIR --bin-dir DIR \
             [--runtime-python PATH] --yes [--skip-portal]

DIR values are absolute Git checkouts. The selected runtime may be a normal
venv launcher symlink, but its sys.prefix must resolve to a venv physically
under $HOME/.lingtai-tui/runtime. The complete v1 receipt is written only
after build, install, import, and provenance postconditions pass.
EOF
}
fail() { echo "dev.sh: error: $*" >&2; exit 1; }
partial_fail() { echo "dev.sh: error: $*" >&2; exit 1; }
abs() { [[ "$1" == /* && "$1" != *$'\n'* && "$1" != *$'\t'* && "$1" != */../* && "$1" != */./* ]]; }

# The TUI contract is one identity token: exactly one vX.Y.Z token or one
# standalone dev token. Garbage, duplicates, and mixed identities fail closed.
parse_tui_identity() {
  local output="$1" tokens token identity="" count=0 candidate
  tokens="$(printf '%s' "$output" | tr -s '[:space:]' '\n')" || return 1
  while IFS= read -r token; do
    [[ -n "$token" ]] || continue
    candidate=""
    if [[ "$token" == dev ]]; then
      candidate=dev
    elif [[ "$token" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
      candidate="$token"
    else
      continue
    fi
    identity="$candidate"
    count=$((count + 1))
  done <<EOF
$tokens
EOF
  [[ "$count" -eq 1 ]] || return 1
  printf '%s\n' "$identity"
}

source_dir= kernel_dir= bin_dir= runtime= yes=0 skip_portal=0
while (($#)); do
  case "$1" in
    --tui-source) (($# >= 2)) || fail "--tui-source requires DIR"; source_dir=$2; shift 2 ;;
    --kernel-source) (($# >= 2)) || fail "--kernel-source requires DIR"; kernel_dir=$2; shift 2 ;;
    --bin-dir) (($# >= 2)) || fail "--bin-dir requires DIR"; bin_dir=$2; shift 2 ;;
    --runtime-python) (($# >= 2)) || fail "--runtime-python requires PATH"; runtime=$2; shift 2 ;;
    --yes) yes=1; shift ;;
    --skip-portal) skip_portal=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done
[[ -n "$source_dir" && -n "$kernel_dir" && -n "$bin_dir" ]] || { usage >&2; exit 2; }
for path in "$source_dir" "$kernel_dir" "$bin_dir"; do abs "$path" || fail "path is not exact absolute: $path"; done
[[ "$yes" == 1 ]] || fail "development install mutates checkout/runtime/target; provide --yes"
[[ -d "$source_dir/.git" || -f "$source_dir/.git" ]] || fail "TUI source is not a Git checkout"
[[ -d "$kernel_dir/.git" || -f "$kernel_dir/.git" ]] || fail "kernel source is not a Git checkout"
[[ -d "$source_dir/tui" && -f "$source_dir/tui/go.mod" ]] || fail "TUI checkout lacks tui/go.mod"
[[ -f "$kernel_dir/pyproject.toml" || -f "$kernel_dir/setup.py" ]] || fail "kernel checkout lacks packaging metadata"
[[ ! -L "$bin_dir" ]] || fail "target bin directory is a symlink"

case "$runtime" in
  "") runtime="$HOME/.lingtai-tui/runtime/venv/bin/python" ;;
esac
abs "$runtime" || fail "runtime path is not exact absolute"
case "$runtime" in
  /usr|/usr/*|/usr/local|/usr/local/*|/opt/homebrew|/opt/homebrew/*|/System|/System/*|/Library|/Library/*) fail "system/Homebrew Python is forbidden" ;;
esac
case "$runtime" in
  "$HOME/.lingtai-tui/runtime/"*) ;;
  *) fail "runtime interpreter is not lexically under the canonical owned runtime root" ;;
esac
[[ ! -L "$HOME/.lingtai-tui" && ! -L "$HOME/.lingtai-tui/runtime" ]] || fail "owned installation/runtime root is a symlink"
runtime_root="$HOME/.lingtai-tui/runtime"
mkdir -p "$runtime_root"
venv_dir="$(dirname "$(dirname "$runtime")")"
if [[ ! -e "$runtime" ]]; then
  mkdir -p "$(dirname "$runtime")"
  if ! python3 -m venv "$venv_dir"; then
    partial_fail "runtime venv may be partially created at $venv_dir; it was not deleted or overwritten"
  fi
fi
[[ -f "$runtime" && -x "$runtime" ]] || partial_fail "selected runtime is missing; possible partial venv: $venv_dir"
owned_root="$(cd "$runtime_root" 2>/dev/null && pwd -P)" || fail "owned runtime root cannot be canonicalized"
selected_venv="$(cd "$(dirname "$runtime")/.." 2>/dev/null && pwd -P)" || fail "selected runtime venv cannot be canonicalized"
selected_parent="$(cd "$(dirname "$selected_venv")" 2>/dev/null && pwd -P)" || fail "selected runtime parent cannot be canonicalized"
[[ "$selected_parent" == "$owned_root" ]] || fail "runtime venv escapes the owned runtime root"
PYTHONPATH= "$runtime" - "$selected_venv" <<'PY' >/dev/null 2>&1 || fail "runtime interpreter prefix does not match selected venv"
import os, sys
if os.path.realpath(sys.prefix) != os.path.realpath(sys.argv[1]):
    raise SystemExit(1)
PY

metadata="$HOME/.lingtai-tui/install.json"
# Existing targets require a structurally valid owned receipt. This is checked
# through the selected runtime, never by grep or textual substitution.
if [[ -e "$bin_dir/lingtai-tui" || -L "$bin_dir/lingtai-tui" ]]; then
  [[ -f "$metadata" && ! -L "$metadata" ]] || fail "existing target has no owned metadata"
  "$runtime" - "$metadata" "$bin_dir" "$selected_venv" <<'PY' || fail "existing target metadata is not a valid v1 receipt owned by this target"
# LINGTAI_RECEIPT_PARSE_DEV_EXISTING
import json, os, sys
path, expected_bin, expected_venv = sys.argv[1:]
def pairs(items):
    out = {}
    for key, value in items:
        if key in out: raise ValueError("duplicate JSON key: " + key)
        out[key] = value
    return out
with open(path, encoding="utf-8") as stream:
    data = json.load(stream, object_pairs_hook=pairs)
if not isinstance(data, dict) or data.get("schema") != "lingtai.tui.install/v1" or data.get("schema_version") != 1: raise ValueError("schema")
if data.get("bin_dir") != expected_bin: raise ValueError("bin_dir")
if not isinstance(data.get("runtime_venv"), str) or os.path.realpath(data["runtime_venv"]) != os.path.realpath(expected_venv): raise ValueError("runtime_venv")
if data.get("install_kind") not in ("release-asset", "source-build", "dev-source"): raise ValueError("install_kind")
managed = data.get("managed_binaries")
if not isinstance(managed, list) or expected_bin + "/lingtai-tui" not in managed: raise ValueError("managed_binaries")
PY
fi

source_physical="$(cd "$source_dir" 2>/dev/null && pwd -P)" || fail "TUI source cannot be canonicalized"
kernel_physical="$(cd "$kernel_dir" 2>/dev/null && pwd -P)" || fail "kernel source cannot be canonicalized"
source_commit="$(git -C "$source_dir" rev-parse HEAD 2>/dev/null)" || fail "cannot resolve TUI source commit"
kernel_commit="$(git -C "$kernel_dir" rev-parse HEAD 2>/dev/null)" || fail "cannot resolve kernel source commit"
work="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-dev.XXXXXX")" || fail "could not create development scratch directory"
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/tui"
if ! (cd "$source_dir/tui" && CGO_ENABLED=0 go build -o "$work/tui/lingtai-tui" .); then
  partial_fail "TUI source build failed; target and receipt were not intentionally changed"
fi
if [[ "$skip_portal" != 1 && -d "$source_dir/portal" && -d "$source_dir/portal/web" ]]; then
  command -v npm >/dev/null || fail "npm is required for Portal; use --skip-portal explicitly"
  command -v go >/dev/null || fail "go is required for Portal"
  if ! (cd "$source_dir/portal/web" && npm ci --silent && npm run build --silent); then
    partial_fail "Portal source build failed; TUI target and receipt were not intentionally changed"
  fi
  if ! (cd "$source_dir/portal" && CGO_ENABLED=0 go build -o "$work/tui/lingtai-portal" .); then
    partial_fail "Portal source build failed; TUI target and receipt were not intentionally changed"
  fi
fi

if ! "$runtime" -m pip install --disable-pip-version-check --editable "$kernel_dir"; then
  partial_fail "kernel editable install may have partially changed the selected runtime; target and receipt were not intentionally changed"
fi
mkdir -p "$bin_dir"
if ! install -m 755 "$work/tui/lingtai-tui" "$bin_dir/lingtai-tui"; then
  partial_fail "TUI target may have changed partially; receipt was not intentionally changed"
fi
managed_tui="$bin_dir/lingtai-tui"
managed_portal=""
if [[ "$skip_portal" != 1 && -f "$work/tui/lingtai-portal" ]]; then
  if ! install -m 755 "$work/tui/lingtai-portal" "$bin_dir/lingtai-portal"; then
    partial_fail "TUI and possibly Portal targets may have changed; receipt was not intentionally changed"
  fi
  managed_portal="$bin_dir/lingtai-portal"
fi

observed_kernel_version="$(PYTHONPATH= "$runtime" - "$selected_venv" "$kernel_physical" <<'PY'
# LINGTAI_DEV_IMPORT_POSTCONDITION
import importlib, os, sys
venv, source = os.path.realpath(sys.argv[1]), os.path.realpath(sys.argv[2])
if os.path.realpath(sys.prefix) != venv: raise SystemExit(1)
package = importlib.import_module("lingtai")
kernel = importlib.import_module("lingtai.kernel")
for module in (package, kernel):
    path = os.path.realpath(getattr(module, "__file__", "") or "")
    if not path or not (path == source or path.startswith(source + os.sep)): raise SystemExit(1)
version = str(getattr(package, "__version__", ""))
if not version: raise SystemExit(1)
print(version)
PY
)" || partial_fail "editable runtime postcondition failed; TUI/Portal may have changed and no receipt was written"
tui_output="$("$bin_dir/lingtai-tui" version 2>/dev/null)" || partial_fail "TUI version postcondition failed; runtime and TUI may have changed and no receipt was written"
stamped_version="$(parse_tui_identity "$tui_output")" || partial_fail "TUI identity is not exactly one vX.Y.Z or dev token; runtime and TUI may have changed and no receipt was written"
prefix="$bin_dir"
[[ "$(basename "$bin_dir")" == bin ]] && prefix="$(dirname "$bin_dir")"

# JSON serialization, same-directory fsync, mode preservation, and atomic replace
# are performed by the selected runtime only after every postcondition above.
if ! "$runtime" - "$metadata" "$prefix" "$bin_dir" "$stamped_version" "$selected_venv" "$source_physical" "$kernel_physical" "$source_commit" "$kernel_commit" "$observed_kernel_version" "$managed_tui" "$managed_portal" <<'PY'
# LINGTAI_DEV_RECEIPT_WRITE
import datetime, json, os, stat, sys, tempfile
(path, prefix, bin_dir, stamped, venv, tui_source, kernel_source,
 tui_commit, kernel_commit, kernel_version, tui_binary, portal_binary) = sys.argv[1:]
receipt = {
    "schema": "lingtai.tui.install/v1",
    "schema_version": 1,
    "install_method": "source",
    "install_kind": "dev-source",
    "prefix": prefix,
    "bin_dir": bin_dir,
    "stamped_version": stamped,
    "installed_at": datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "managed_binaries": [tui_binary] + ([portal_binary] if portal_binary else []),
    "runtime_venv": os.path.realpath(venv),
    "tui_source": os.path.realpath(tui_source),
    "tui_commit": tui_commit,
    "kernel_source": "editable",
    "kernel_source_path": os.path.realpath(kernel_source),
    "kernel_commit": kernel_commit,
    "kernel_version": kernel_version,
}
directory = os.path.dirname(os.path.abspath(path))
os.makedirs(directory, exist_ok=True)
mode = stat.S_IMODE(os.stat(path).st_mode) if os.path.exists(path) else 0o600
fd, temp = tempfile.mkstemp(prefix=".install.json.", dir=directory)
try:
    os.fchmod(fd, mode)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        json.dump(receipt, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temp, path)
    dirfd = os.open(directory, os.O_RDONLY)
    try: os.fsync(dirfd)
    finally: os.close(dirfd)
except Exception:
    try: os.unlink(temp)
    except OSError: pass
    raise
PY
then
  partial_fail "runtime and TUI/Portal may have changed; complete dev receipt was not written"
fi
printf 'PASS: editable development install from TUI %s and kernel %s (%s).\n' "$source_commit" "$kernel_commit" "$observed_kernel_version"
