#!/usr/bin/env bash
# For coding-agent maintainers:
# - Read this repository's root ANATOMY.md before changing this file.
# - Preserve this entrypoint's operation, ownership, consent, and mutation boundary;
#   do not turn one operation into an implicit install, update, repair, or deploy.
# - Keep executable behavior, CONTRACT.md, ANATOMY.md, and README guidance
#   in lockstep; do not add static, shim, fake-command, or hermetic acceptance tests.
# - Every verify.sh change must execute this exact final candidate against real
#   healthy and mismatched installation state in isolated non-root Linux, proving
#   actual PASS/failure output and byte-for-byte read-only postconditions.
#   Source grep and fake CLI output are not acceptance.
# - Never publish a success receipt before every declared postcondition passes;
#   report partial state honestly and do not treat failure as cleanup authority.
# - These maintenance rules grant no merge, release, deploy, auth, config, or
#   deletion authority.
# This file is strictly read-only proof for one exact target/runtime/receipt;
# PASS is not mutation, migration, refresh, release, deploy, or another install's proof.
# Read-only installation receipt. Standalone; never sources install.sh.
set -euo pipefail
usage() {
  cat <<'EOF'
Usage: verify.sh --bin-dir DIR --runtime-python PATH [--metadata PATH]

Checks one exact release or editable-development receipt without changing state.
EOF
}
fail() { echo "verify.sh: error: $*" >&2; exit 1; }
abs_path() { [[ "$1" == /* && "$1" != *$'\n'* && "$1" != *$'\t'* && "$1" != */../* && "$1" != */./* ]]; }

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

bin_dir=""; runtime=""; metadata="${HOME}/.lingtai-tui/install.json"
while (($#)); do
  case "$1" in
    --bin-dir) (($# >= 2)) || fail "--bin-dir requires DIR"; bin_dir=$2; shift 2 ;;
    --runtime-python) (($# >= 2)) || fail "--runtime-python requires PATH"; runtime=$2; shift 2 ;;
    --metadata) (($# >= 2)) || fail "--metadata requires PATH"; metadata=$2; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1 (use --help)" ;;
  esac
done
[[ -n "$bin_dir" && -n "$runtime" ]] || { usage >&2; exit 2; }
abs_path "$bin_dir" || fail "bin directory is not an exact absolute path: $bin_dir"
abs_path "$runtime" || fail "runtime interpreter is not an exact absolute path: $runtime"
abs_path "$metadata" || fail "metadata path is not an exact absolute path: $metadata"
[[ ! -L "$bin_dir" && -d "$bin_dir" ]] || fail "bin directory is missing or a symlink: $bin_dir"
[[ -f "$bin_dir/lingtai-tui" && -x "$bin_dir/lingtai-tui" && ! -L "$bin_dir/lingtai-tui" ]] || fail "TUI binary is not an owned regular executable"
[[ -f "$metadata" && ! -L "$metadata" ]] || fail "install metadata is missing or redirected"
[[ ! -L "${HOME}/.lingtai-tui" && ! -L "${HOME}/.lingtai-tui/runtime" ]] || fail "owned installation/runtime root is a symlink"
case "$runtime" in
  /usr|/usr/*|/usr/local|/usr/local/*|/opt/homebrew|/opt/homebrew/*|/System|/System/*|/Library|/Library/*) fail "system/Homebrew Python is not an installation runtime" ;;
esac
case "$runtime" in
  "$HOME/.lingtai-tui/runtime/"*) ;;
  *) fail "runtime interpreter is not lexically under the canonical owned runtime root" ;;
esac
[[ -f "$runtime" && -x "$runtime" ]] || fail "runtime interpreter is missing or not executable"
owned_root="$(cd "$HOME/.lingtai-tui/runtime" 2>/dev/null && pwd -P)" || fail "owned runtime root is absent"
selected_venv="$(cd "$(dirname "$runtime")/.." 2>/dev/null && pwd -P)" || fail "runtime venv cannot be canonicalized"
selected_parent="$(cd "$(dirname "$selected_venv")" 2>/dev/null && pwd -P)" || fail "runtime parent cannot be canonicalized"
[[ "$selected_parent" == "$owned_root" ]] || fail "runtime interpreter is outside the owned runtime root"
PYTHONPATH= "$runtime" - "$selected_venv" <<'PY' >/dev/null 2>&1 || fail "runtime interpreter prefix does not match selected venv"
import os, sys
if os.path.realpath(sys.prefix) != os.path.realpath(sys.argv[1]):
    raise SystemExit(1)
PY
version_output="$("$bin_dir/lingtai-tui" version 2>/dev/null)" || fail "TUI version probe failed"
version="$(parse_tui_identity "$version_output")" || fail "TUI identity is not exactly one vX.Y.Z or dev token"

# The selected runtime parses the receipt and performs the provenance checks. A
# release receipt must import from its venv; a dev-source receipt must import from
# its metadata-declared checkout. Both remain bound to this venv's sys.prefix.
probe="$("$runtime" - "$metadata" "$bin_dir" "$selected_venv" "$version" <<'PY'
# LINGTAI_VERIFY_PROBE
import importlib, json, os, re, sys
path, expected_bin, expected_venv, tui_version = sys.argv[1:]
def pairs(items):
    out = {}
    for key, value in items:
        if key in out: raise ValueError("duplicate JSON key: " + key)
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
if not isinstance(data.get("runtime_venv"), str) or os.path.realpath(data["runtime_venv"]) != os.path.realpath(expected_venv): raise SystemExit("runtime_venv does not own this venv")
if not isinstance(data.get("stamped_version"), str): raise SystemExit("stamped_version is missing")
kind = data.get("install_kind")
if kind not in ("release-asset", "source-build", "dev-source"): raise SystemExit("unknown install_kind")
managed = data.get("managed_binaries")
if not isinstance(managed, list) or expected_bin + "/lingtai-tui" not in managed: raise SystemExit("managed_binaries does not own lingtai-tui")
if kind == "dev-source":
    if data.get("kernel_source") != "editable": raise SystemExit("dev-source receipt lacks editable kernel_source")
    source = data.get("kernel_source_path")
    if not isinstance(source, str) or not os.path.isabs(source): raise SystemExit("dev-source receipt lacks kernel_source_path")
    source = os.path.realpath(source)
else:
    if data.get("kernel_source") == "editable": raise SystemExit("ordinary receipt claims editable provenance")
    source = os.path.realpath(expected_venv)
if os.path.realpath(sys.prefix) != os.path.realpath(expected_venv): raise SystemExit("sys.prefix is not the selected venv")
package = importlib.import_module("lingtai")
kernel = importlib.import_module("lingtai.kernel")
observed = str(getattr(package, "__version__", ""))
if not observed: raise SystemExit("lingtai has no observed version")
for module in (package, kernel):
    module_path = os.path.realpath(getattr(module, "__file__", "") or "")
    if not module_path or not (module_path == source or module_path.startswith(source + os.sep)):
        raise SystemExit("module provenance is outside the declared source")
stamped = data["stamped_version"]
if stamped != "dev" and not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", stamped):
    raise SystemExit("stamped_version is neither dev nor an exact release")
if not tui_version or tui_version != stamped:
    raise SystemExit("TUI identity does not match stamped_version")
receipt_kernel_version = data.get("kernel_version")
if not isinstance(receipt_kernel_version, str) or observed != receipt_kernel_version:
    raise SystemExit("lingtai.__version__ does not exactly match kernel_version")
print(observed)
PY
)" || fail "metadata/runtime/import/provenance postcondition failed"
[[ -n "$version" || "$(printf '%s' "$probe")" != "" ]] || fail "TUI version output is empty"
printf 'PASS\nTUI: %s\nRuntime: %s\nRuntime provenance: %s\n' "${version:-dev}" "$runtime" "$probe"
