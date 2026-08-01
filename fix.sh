#!/usr/bin/env bash
# For coding-agent maintainers:
# - Read this repository's root ANATOMY.md before changing this file.
# - Preserve this entrypoint's operation, ownership, consent, and mutation boundary;
#   do not turn one operation into an implicit install, update, repair, or deploy.
# - Keep executable behavior, CONTRACT.md, ANATOMY.md, and README guidance
#   in lockstep; do not add static, shim, fake-command, or hermetic acceptance tests.
# - Every fix.sh change must execute real diagnosis in isolated non-root Linux and,
#   when mutation changes, real --apply --yes against owned broken runtime state;
#   observe the new-runtime/receipt postconditions or actual partial failure.
#   Source grep and fake CLI output are not acceptance.
# - Never publish a success receipt before every declared postcondition passes;
#   report partial state honestly and do not treat failure as cleanup authority.
# - These maintenance rules grant no merge, release, deploy, auth, config, or
#   deletion authority.
# This file is read-only by default; --apply --yes may create only the exact new
# owned runtime child and repoint a validated ordinary receipt, never delete old state.
# Explicit bounded repair. Defaults to a read-only plan; never sources install.sh.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: fix.sh --bin-dir DIR --runtime-dir DIR \
              [--kernel-artifact FILE_OR_HTTPS_URL --kernel-sha256 HEX] \
              [--apply --yes]

The diagnosis is read-only. --apply --yes requires one explicitly named free
runtime directory directly under $HOME/.lingtai-tui/runtime. The bootstrap
python3 command parses only the old receipt and creates the new venv; the old
runtime is never executed. Existing runtime pointers/provenance are checked
structurally before any repair directory is made.
EOF
}
fail() { echo "fix.sh: error: $*" >&2; exit 1; }
partial_fail() { echo "fix.sh: error: $*" >&2; exit 1; }
abs() { [[ "$1" == /* && "$1" != *$'\n'* && "$1" != *$'\t'* && "$1" != */../* && "$1" != */./* ]]; }
sha() { [[ "$1" =~ ^[0-9a-fA-F]{64}$ ]]; }
fetch() {
  case "$1" in
    https://github.com/Lingtai-AI/*|https://gitee.com/huangzesen1997/*) command -v curl >/dev/null || fail "curl is required"; curl -fsSL --max-time 300 -o "$2" "$1" || fail "kernel artifact download failed" ;;
    /*) cp -p "$1" "$2" || fail "kernel artifact copy failed" ;;
    *) fail "artifact must be an exact path or official HTTPS URL" ;;
  esac
}
verify_sha() {
  local got
  if command -v sha256sum >/dev/null; then got="$(sha256sum "$1" | cut -d' ' -f1)"
  elif command -v shasum >/dev/null; then got="$(shasum -a 256 "$1" | cut -d' ' -f1)"
  else fail "no SHA-256 utility is available"; fi
  [[ "$got" == "$(printf '%s' "$2" | tr 'A-F' 'a-f')" ]] || fail "kernel artifact SHA-256 mismatch"
}

bin_dir= runtime_dir= artifact= artifact_sha= apply=0 yes=0
while (($#)); do
  case "$1" in
    --bin-dir) (($# >= 2)) || fail "--bin-dir requires DIR"; bin_dir=$2; shift 2 ;;
    --runtime-dir) (($# >= 2)) || fail "--runtime-dir requires DIR"; runtime_dir=$2; shift 2 ;;
    --kernel-artifact) (($# >= 2)) || fail "--kernel-artifact requires FILE_OR_URL"; artifact=$2; shift 2 ;;
    --kernel-sha256) (($# >= 2)) || fail "--kernel-sha256 requires HEX"; artifact_sha=$2; shift 2 ;;
    --apply) apply=1; shift ;;
    --yes) yes=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done
[[ -n "$bin_dir" ]] || { usage >&2; exit 2; }
abs "$bin_dir" || fail "bin directory is not an exact absolute path"
if [[ -z "$runtime_dir" ]]; then
  [[ "$apply" != 1 ]] || fail "--runtime-dir is required for --apply"
  runtime_dir="$HOME/.lingtai-tui/runtime/diagnosis"
fi
abs "$runtime_dir" || fail "runtime directory is not an exact absolute path"
[[ "$yes" == 0 || "$apply" == 1 ]] || fail "--yes is meaningful only with --apply"

# This is the only interpreter selected before repair. It is used only for the
# strict read-only receipt parser and, below, for `-m venv`; it never imports the
# old runtime or installs the kernel.
bootstrap="$(command -v python3 2>/dev/null)" || fail "python3 is required to parse the receipt and create the repair venv"
[[ -x "$bootstrap" ]] || fail "python3 bootstrap is not executable"

metadata="$HOME/.lingtai-tui/install.json"
[[ ! -L "$HOME/.lingtai-tui" && ! -L "$HOME/.lingtai-tui/runtime" ]] || fail "owned installation/runtime root is a symlink"
[[ ! -L "$bin_dir" && -d "$bin_dir" && -f "$bin_dir/lingtai-tui" && ! -L "$bin_dir/lingtai-tui" ]] || fail "target is not one exact existing installation"
[[ -f "$metadata" && ! -L "$metadata" ]] || fail "owned install metadata is missing or redirected"
runtime_root="$HOME/.lingtai-tui/runtime"
owned_root="$(cd "$runtime_root" 2>/dev/null && pwd -P)" || fail "owned runtime root is absent"

# Both the old pointer and the new requested directory must be one lexical,
# normalized direct child. The old child may be missing; if present it must be a
# real directory, never a symlink or file. Physical-parent checks are separate.
valid_child() {
  local path="$1" parent name
  [[ "$path" == "$runtime_root/"* ]] || return 1
  [[ "$path" != */ ]] || return 1
  parent="$(dirname "$path")"
  name="$(basename "$path")"
  [[ "$parent" == "$runtime_root" ]] || return 1
  [[ "$name" =~ ^[A-Za-z0-9._-]+$ ]] || return 1
  [[ "$path" == "$runtime_root/$name" ]] || return 1
}
valid_child "$runtime_dir" || fail "repair directory must be one exact direct child of the canonical runtime root"
repair_parent_physical="$(cd "$runtime_root" 2>/dev/null && pwd -P)" || fail "repair directory parent cannot be canonicalized"
[[ "$repair_parent_physical" == "$owned_root" ]] || fail "repair directory parent is not physically the owned runtime root"

# LINGTAI_RECEIPT_PARSE_FIX: the bootstrap parses a strict v1 receipt and emits
# stamp<TAB>old-pointer. It does not execute the old pointer.
prior_record="$("$bootstrap" - "$metadata" "$bin_dir" "$runtime_root" <<'PY'
# LINGTAI_RECEIPT_PARSE_FIX
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
if data.get("install_kind") not in ("release-asset", "source-build"): raise SystemExit("receipt is not ordinary provenance")
if data.get("kernel_source") == "editable" or data.get("install_kind") == "dev-source": raise SystemExit("dev-source receipt cannot be repaired as ordinary")
stamp = data.get("stamped_version")
if not isinstance(stamp, str) or not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", stamp): raise SystemExit("stamped_version is not an exact release")
kernel_version = data.get("kernel_version")
if not isinstance(kernel_version, str) or not kernel_version or kernel_version != kernel_version.strip() or any(ch in kernel_version for ch in "\x00\n\r\t"):
    raise SystemExit("kernel_version is missing or unsafe")
old = data.get("runtime_venv")
if not isinstance(old, str) or not os.path.isabs(old): raise SystemExit("runtime_venv is not absolute")
if "\x00" in old or "\n" in old or "\r" in old or "\t" in old: raise SystemExit("runtime_venv contains unsafe characters")
if os.path.normpath(old) != old or os.path.dirname(old) != runtime_root: raise SystemExit("runtime_venv is not a normalized direct child")
name = os.path.basename(old)
if not re.fullmatch(r"[A-Za-z0-9._-]+", name) or old != runtime_root + "/" + name: raise SystemExit("runtime_venv is not a safe direct child")
if os.path.lexists(old) and (os.path.islink(old) or not os.path.isdir(old)): raise SystemExit("existing runtime_venv is not a real directory")
print(stamp + "\t" + old + "\t" + kernel_version)
PY
)" || fail "prior receipt is not a strict ordinary v1 receipt"
IFS="$(printf '\t')" read -r prior_stamp prior_venv prior_kernel_version <<EOF
$prior_record
EOF
[[ -n "$prior_stamp" && -n "$prior_venv" && -n "$prior_kernel_version" ]] || fail "prior receipt parser emitted incomplete runtime/kernel provenance"
valid_child "$prior_venv" || fail "prior runtime pointer is not one exact direct child of the canonical runtime root"
prior_parent_physical="$(cd "$runtime_root" 2>/dev/null && pwd -P)" || fail "prior runtime parent cannot be canonicalized"
[[ "$prior_parent_physical" == "$owned_root" ]] || fail "prior runtime parent is not physically the owned runtime root"
if [[ -e "$prior_venv" || -L "$prior_venv" ]]; then
  [[ ! -L "$prior_venv" && -d "$prior_venv" ]] || fail "existing prior runtime pointer is not a real directory"
fi

state=free
[[ -e "$runtime_dir" || -L "$runtime_dir" ]] && state=occupied
printf 'Diagnosis: bin=%s; metadata=%s; prior-runtime=%s; prior-stamp=%s; kernel=%s; repair-runtime=%s (%s)\n' "$bin_dir" "$metadata" "$prior_venv" "$prior_stamp" "$prior_kernel_version" "$runtime_dir" "$state"
if [[ "$apply" != 1 ]]; then
  printf 'Read-only plan: no state changed. Re-run with --apply --yes and an exact free runtime-dir to repair.\n'
  exit 0
fi
[[ "$yes" == 1 ]] || fail "--apply is mutating; provide --yes after reviewing the plan"
[[ ! -e "$runtime_dir" && ! -L "$runtime_dir" ]] || fail "repair target is occupied; choose one exact free runtime-dir; no existing directory was overwritten"
[[ -n "$artifact" ]] || fail "--kernel-artifact is required for --apply"
sha "$artifact_sha" || fail "--kernel-sha256 must be 64 hex"
work="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-fix.XXXXXX")" || fail "could not create repair scratch directory"
trap 'rm -rf "$work"' EXIT
# pip validates the wheel FILENAME itself (name-version-pytag-abitag-platformtag.whl)
# before it will install it, so the staged copy must keep the artifact's own
# basename rather than a fixed name like kernel.whl — pip rejects that outright
# ("Invalid wheel filename") regardless of the file's actual contents.
kernel_basename="$(basename -- "$artifact")"
case "$kernel_basename" in
  *.whl) ;;
  *) fail "--kernel-artifact must be a .whl file: $artifact" ;;
esac
fetch "$artifact" "$work/$kernel_basename"
verify_sha "$work/$kernel_basename" "$artifact_sha"
# The repair directory is intentionally never removed after this point. Any
# failure names it as possibly partial instead of claiming rollback.
if ! "$bootstrap" -m venv "$runtime_dir"; then
  partial_fail "repair venv creation failed; partial directory may exist at $runtime_dir and was not deleted or overwritten"
fi
new_runtime="$runtime_dir/bin/python"
if [[ ! -f "$new_runtime" || ! -x "$new_runtime" ]]; then
  partial_fail "repair venv is incomplete; partial directory may exist at $runtime_dir and was not deleted or overwritten"
fi
new_venv="$(cd "$runtime_dir" 2>/dev/null && pwd -P)" || partial_fail "repair venv cannot be canonicalized; partial directory may exist at $runtime_dir"
new_parent="$(cd "$(dirname "$new_venv")" 2>/dev/null && pwd -P)" || partial_fail "repair venv parent cannot be canonicalized; partial directory may exist at $runtime_dir"
[[ "$new_parent" == "$owned_root" ]] || partial_fail "repair venv escaped its owned root; partial directory may exist at $runtime_dir"
if ! "$new_runtime" -m pip install --disable-pip-version-check --no-deps --force-reinstall "$work/$kernel_basename"; then
  partial_fail "kernel install failed; partial directory may exist at $runtime_dir and was not deleted or overwritten"
fi
if ! PYTHONPATH= "$new_runtime" - "$new_venv" "$prior_kernel_version" <<'PY'
# LINGTAI_FIX_IMPORT_POSTCONDITION
import importlib, os, sys
root, expected = os.path.realpath(sys.argv[1]), sys.argv[2]
if os.path.realpath(sys.prefix) != root: raise SystemExit(1)
package = importlib.import_module("lingtai")
kernel = importlib.import_module("lingtai.kernel")
if str(getattr(package, "__version__", "")) != expected: raise SystemExit(1)
for module in (package, kernel):
    path = os.path.realpath(getattr(module, "__file__", "") or "")
    if not path.startswith(root + os.sep): raise SystemExit(1)
PY
then
  partial_fail "repaired runtime postcondition failed; partial directory may exist at $runtime_dir and was not deleted or overwritten"
fi
if ! "$new_runtime" - "$metadata" "$bin_dir" "$prior_venv" "$new_venv" "$prior_stamp" "$prior_kernel_version" <<'PY'
# LINGTAI_RECEIPT_REVALIDATE_FIX
import datetime, json, os, stat, sys, tempfile
path, expected_bin, old_venv, new_venv, prior_stamp, prior_kernel_version = sys.argv[1:]
def pairs(items):
    out = {}
    for key, value in items:
        if key in out: raise ValueError("duplicate JSON key: " + key)
        out[key] = value
    return out
with open(path, encoding="utf-8") as stream:
    data = json.load(stream, object_pairs_hook=pairs)
if not isinstance(data, dict) or data.get("schema") != "lingtai.tui.install/v1" or type(data.get("schema_version")) is not int or data["schema_version"] != 1:
    raise ValueError("metadata shape changed")
if data.get("bin_dir") != expected_bin or data.get("runtime_venv") != old_venv:
    raise ValueError("prior ownership changed")
if data.get("stamped_version") != prior_stamp:
    raise ValueError("prior receipt stamp changed")
if data.get("kernel_version") != prior_kernel_version:
    raise ValueError("prior receipt kernel_version changed")
if data.get("install_kind") not in ("release-asset", "source-build") or data.get("kernel_source") == "editable":
    raise ValueError("ordinary provenance changed")
if not isinstance(data.get("managed_binaries"), list) or expected_bin + "/lingtai-tui" not in data["managed_binaries"]:
    raise ValueError("managed TUI target changed")
data["runtime_venv"] = os.path.realpath(new_venv)
data["updated_at"] = datetime.datetime.now(datetime.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
mode = stat.S_IMODE(os.stat(path).st_mode)
directory = os.path.dirname(os.path.abspath(path))
fd, temp = tempfile.mkstemp(prefix=".install.json.", dir=directory)
try:
    os.fchmod(fd, mode)
    with os.fdopen(fd, "w", encoding="utf-8") as stream:
        json.dump(data, stream, ensure_ascii=False, indent=2)
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
  partial_fail "repair runtime may exist at $runtime_dir; metadata was not intentionally changed because its atomic update failed"
fi
printf 'PASS: one exact runtime repair created at %s; prior TUI target/provenance were preserved.\n' "$runtime_dir"
