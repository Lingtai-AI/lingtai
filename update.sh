#!/usr/bin/env bash
# For coding-agent maintainers:
# - Read this repository's root ANATOMY.md before changing this file.
# - Preserve this entrypoint's operation, ownership, consent, and mutation boundary;
#   do not turn one operation into an implicit install, update, repair, or deploy.
# - Keep executable behavior, CONTRACT.md, ANATOMY.md, and README guidance
#   in lockstep; do not add static, shim, fake-command, or hermetic acceptance tests.
# - Every update.sh change must execute this exact final candidate in isolated
#   non-root Linux against real owned ordinary state and exact artifacts, then
#   observe the actual kernel/TUI/receipt postconditions or actual partial failure.
#   Source grep and fake CLI output are not acceptance.
# - Never publish a success receipt before every declared postcondition passes;
#   report partial state honestly and do not treat failure as cleanup authority.
# - These maintenance rules grant no merge, release, deploy, auth, config, or
#   deletion authority.
# This file owns only an exact-artifact ordinary update after explicit --yes;
# it neither selects latest nor changes provenance, repairs state, or updates Portal.
# Explicit exact-artifact update. Standalone; never sources install.sh.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage: update.sh --bin-dir DIR --runtime-python PATH --tui-archive FILE_OR_HTTPS_URL \
                 --tui-sha256 HEX --kernel-artifact FILE_OR_HTTPS_URL \
                 --kernel-sha256 HEX --tui-tag vX.Y.Z --kernel-version VERSION --yes

Inputs are exact artifacts. Every download, checksum, archive, binary, version,
and metadata check completes before mutation. --yes authorizes mutation.
EOF
}
fail() { echo "update.sh: error: $*" >&2; exit 1; }
partial_fail() { echo "update.sh: error: $*" >&2; exit 1; }
abs() { [[ "$1" == /* && "$1" != *$'\n'* && "$1" != *$'\t'* && "$1" != */../* && "$1" != */./* ]]; }
sha() { [[ "$1" =~ ^[0-9a-fA-F]{64}$ ]]; }

# The TUI contract is one identity token: exactly one vX.Y.Z token or one
# standalone dev token. Other prose is harmless, but no identity may repeat or mix.
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

fetch() {
  local src="$1" dst="$2"
  case "$src" in
    https://github.com/Lingtai-AI/*|https://gitee.com/huangzesen1997/*)
      command -v curl >/dev/null || fail "curl is required for HTTPS artifacts"
      curl -fsSL --max-time 300 -o "$dst" "$src" || fail "artifact download failed: $src" ;;
    /*) cp -p "$src" "$dst" || fail "artifact copy failed: $src" ;;
    *) fail "artifact must be an absolute path or official HTTPS URL: $src" ;;
  esac
}
verify_sha() {
  local file="$1" want="$2" got
  if command -v sha256sum >/dev/null; then
    got="$(sha256sum "$file" | cut -d' ' -f1)"
  elif command -v shasum >/dev/null; then
    got="$(shasum -a 256 "$file" | cut -d' ' -f1)"
  else
    fail "no SHA-256 utility is available"
  fi
  [[ "$got" == "$(printf '%s' "$want" | tr 'A-F' 'a-f')" ]] || fail "SHA-256 mismatch for $file"
}

bin_dir= runtime= tui_archive= tui_sha= kernel_artifact= kernel_sha= tui_tag= kernel_version= yes=0
while (($#)); do
  case "$1" in
    --bin-dir) (($# >= 2)) || fail "--bin-dir requires DIR"; bin_dir=$2; shift 2 ;;
    --runtime-python) (($# >= 2)) || fail "--runtime-python requires PATH"; runtime=$2; shift 2 ;;
    --tui-archive) (($# >= 2)) || fail "--tui-archive requires FILE_OR_URL"; tui_archive=$2; shift 2 ;;
    --tui-sha256) (($# >= 2)) || fail "--tui-sha256 requires HEX"; tui_sha=$2; shift 2 ;;
    --kernel-artifact) (($# >= 2)) || fail "--kernel-artifact requires FILE_OR_URL"; kernel_artifact=$2; shift 2 ;;
    --kernel-sha256) (($# >= 2)) || fail "--kernel-sha256 requires HEX"; kernel_sha=$2; shift 2 ;;
    --tui-tag) (($# >= 2)) || fail "--tui-tag requires TAG"; tui_tag=$2; shift 2 ;;
    --kernel-version) (($# >= 2)) || fail "--kernel-version requires VERSION"; kernel_version=$2; shift 2 ;;
    --yes) yes=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) fail "unknown argument: $1" ;;
  esac
done
[[ -n "$bin_dir" && -n "$runtime" && -n "$tui_archive" && -n "$kernel_artifact" && -n "$tui_tag" && -n "$kernel_version" ]] || { usage >&2; exit 2; }
abs "$bin_dir" || fail "bin directory is not an exact absolute path"
abs "$runtime" || fail "runtime path is not an exact absolute path"
[[ "$tui_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "TUI tag is not exact vX.Y.Z"
sha "$tui_sha" || fail "TUI SHA-256 is not 64 hex"
sha "$kernel_sha" || fail "kernel SHA-256 is not 64 hex"
[[ "$yes" == 1 ]] || fail "healthy update is mutating; provide --yes after reviewing exact inputs"

metadata="$HOME/.lingtai-tui/install.json"
[[ ! -L "$HOME/.lingtai-tui" && ! -L "$HOME/.lingtai-tui/runtime" ]] || fail "owned installation/runtime root is a symlink"
[[ ! -L "$bin_dir" && -d "$bin_dir" && -f "$bin_dir/lingtai-tui" && ! -L "$bin_dir/lingtai-tui" ]] || fail "target is not one owned existing installation"
[[ -f "$metadata" && ! -L "$metadata" ]] || fail "install metadata is missing or redirected"
case "$runtime" in
  /usr|/usr/*|/usr/local|/usr/local/*|/opt/homebrew|/opt/homebrew/*|/System|/System/*|/Library|/Library/*) fail "system/Homebrew Python is forbidden" ;;
esac
case "$runtime" in
  "$HOME/.lingtai-tui/runtime/"*) ;;
  *) fail "runtime interpreter is not lexically under the canonical owned runtime root" ;;
esac
[[ -f "$runtime" && -x "$runtime" ]] || fail "runtime interpreter is missing or not executable"
owned_root="$(cd "$HOME/.lingtai-tui/runtime" 2>/dev/null && pwd -P)" || fail "owned runtime root is absent"
selected_venv="$(cd "$(dirname "$runtime")/.." 2>/dev/null && pwd -P)" || fail "selected runtime venv cannot be canonicalized"
selected_parent="$(cd "$(dirname "$selected_venv")" 2>/dev/null && pwd -P)" || fail "selected runtime parent cannot be canonicalized"
[[ "$selected_parent" == "$owned_root" ]] || fail "runtime venv escapes the owned runtime root"
PYTHONPATH= "$runtime" - "$selected_venv" <<'PY' >/dev/null 2>&1 || fail "runtime interpreter prefix does not match selected venv"
# LINGTAI_RUNTIME_PREFIX
import os, sys
selected = os.path.realpath(sys.argv[1])
if os.path.realpath(sys.prefix) != selected:
    raise SystemExit(1)
PY

# The selected runtime is the only parser for the receipt. It emits the prior
# stamp for the installed-TUI probe; no line-oriented JSON substitution occurs.
prior_record="$("$runtime" - "$metadata" "$bin_dir" "$selected_venv" <<'PY'
# LINGTAI_RECEIPT_PARSE_UPDATE
import json, os, sys
path, expected_bin, expected_venv = sys.argv[1:]
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
if data.get("bin_dir") != expected_bin: raise SystemExit("bin_dir does not own the requested target")
runtime_pointer = data.get("runtime_venv")
if not isinstance(runtime_pointer, str) or not os.path.isabs(runtime_pointer) or "\x00" in runtime_pointer or "\n" in runtime_pointer or "\t" in runtime_pointer or os.path.realpath(runtime_pointer) != os.path.realpath(expected_venv): raise SystemExit("runtime_venv does not own the selected venv")
if not isinstance(data.get("stamped_version"), str): raise SystemExit("stamped_version is missing")
import re
if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", data["stamped_version"]): raise SystemExit("stamped_version is not an exact release")
if data.get("install_kind") not in ("release-asset", "source-build"): raise SystemExit("receipt is not ordinary install provenance")
if data.get("kernel_source") == "editable" or data.get("install_kind") == "dev-source": raise SystemExit("dev-source receipt cannot be updated as an ordinary install")
managed = data.get("managed_binaries")
target = expected_bin + "/lingtai-tui"
if not isinstance(managed, list) or target not in managed: raise SystemExit("managed_binaries does not own lingtai-tui")
print(data["stamped_version"] + "\t" + runtime_pointer)
PY
)" || fail "install metadata is not a valid ordinary v1 receipt"
IFS="$(printf '\t')" read -r prior_stamp prior_venv <<EOF
$prior_record
EOF
[[ -n "$prior_stamp" && -n "$prior_venv" ]] || fail "install metadata parser emitted no prior stamp/runtime pointer"
current_output="$("$bin_dir/lingtai-tui" version 2>/dev/null)" || fail "installed TUI version probe failed before update"
current_identity="$(parse_tui_identity "$current_output")" || fail "installed TUI identity is not exactly one vX.Y.Z or dev token"
[[ "$current_identity" == "$prior_stamp" ]] || fail "installed TUI identity does not match the receipt stamped_version"

work="$(mktemp -d "${TMPDIR:-/tmp}/lingtai-update.XXXXXX")" || fail "could not create update scratch directory"
trap 'rm -rf "$work"' EXIT
fetch "$tui_archive" "$work/tui.tar.gz"
# pip validates the wheel FILENAME itself (name-version-pytag-abitag-platformtag.whl)
# before it will install it, so the staged copy must keep the artifact's own
# basename rather than a fixed name like kernel.whl — pip rejects that outright
# ("Invalid wheel filename") regardless of the file's actual contents.
kernel_basename="$(basename -- "$kernel_artifact")"
case "$kernel_basename" in
  *.whl) ;;
  *) fail "--kernel-artifact must be a .whl file: $kernel_artifact" ;;
esac
fetch "$kernel_artifact" "$work/$kernel_basename"
verify_sha "$work/tui.tar.gz" "$tui_sha"
verify_sha "$work/$kernel_basename" "$kernel_sha"
mkdir "$work/tui"
while IFS= read -r member; do
  case "$member" in
    /*|../*|*/../*|*/..|..|./*|*/./*) fail "TUI archive has unsafe path: $member" ;;
  esac
done < <(tar -tzf "$work/tui.tar.gz" 2>/dev/null) || fail "TUI archive listing failed"
tar -xzf "$work/tui.tar.gz" -C "$work/tui" || fail "TUI archive extraction failed"
candidates="$(find "$work/tui" -type f -name lingtai-tui -perm -u+x -print)"
count="$(printf '%s\n' "$candidates" | sed '/^$/d' | wc -l | tr -d ' ')"
[[ "$count" == 1 ]] || fail "TUI archive must contain exactly one executable lingtai-tui (found $count)"
tui="$candidates"
tui_output="$("$tui" version 2>/dev/null)" || fail "TUI archive binary probe failed"
candidate_identity="$(parse_tui_identity "$tui_output")" || fail "TUI archive identity is not exactly one vX.Y.Z or dev token"
[[ "$candidate_identity" == "$tui_tag" ]] || fail "TUI archive identity does not equal the requested tag"
printf 'Preflight complete: exact TUI %s, kernel %s, target %s\n' "$tui_tag" "$kernel_version" "$bin_dir"

# Mutation phases are deliberately explicit. Cross-component rollback is not
# possible: every failure names which components may have changed.
if ! "$runtime" -m pip install --disable-pip-version-check --no-deps --force-reinstall "$work/$kernel_basename"; then
  partial_fail "kernel component may have changed; TUI and metadata were not intentionally changed"
fi
if ! PYTHONPATH= "$runtime" - "$selected_venv" "$kernel_version" <<'PY'
# LINGTAI_KERNEL_POSTCONDITION
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
  partial_fail "kernel component may have changed; TUI and metadata were not intentionally changed (runtime postcondition failed)"
fi
new_tui="$bin_dir/.lingtai-tui.update.$$"
if ! install -m 755 "$tui" "$new_tui" || ! mv -f "$new_tui" "$bin_dir/lingtai-tui"; then
  rm -f "$new_tui" 2>/dev/null || true
  partial_fail "kernel and possibly TUI components may have changed; metadata was not intentionally changed"
fi
if ! output="$("$bin_dir/lingtai-tui" version 2>/dev/null)"; then
  partial_fail "kernel and TUI components may have changed; metadata was not intentionally changed (TUI postcondition failed)"
fi
if ! installed_identity="$(parse_tui_identity "$output")" || [[ "$installed_identity" != "$tui_tag" ]]; then
  partial_fail "kernel and TUI components may have changed; metadata was not intentionally changed (TUI identity postcondition failed)"
fi

if ! "$runtime" - "$metadata" "$bin_dir" "$selected_venv" "$prior_stamp" "$prior_venv" "$tui_tag" "$kernel_version" <<'PY'
# LINGTAI_RECEIPT_REVALIDATE_UPDATE
import datetime, json, os, stat, sys, tempfile
path, expected_bin, expected_venv, prior_stamp, prior_venv, tui_tag, kernel_version = sys.argv[1:]
def pairs(items):
    out = {}
    for key, value in items:
        if key in out: raise ValueError("duplicate JSON key: " + key)
        out[key] = value
    return out
with open(path, encoding="utf-8") as stream:
    data = json.load(stream, object_pairs_hook=pairs)
if not isinstance(data, dict) or data.get("schema") != "lingtai.tui.install/v1" or type(data.get("schema_version")) is not int or data["schema_version"] != 1:
    raise ValueError("metadata changed shape before update")
if data.get("bin_dir") != expected_bin or data.get("runtime_venv") != prior_venv or os.path.realpath(data.get("runtime_venv", "")) != os.path.realpath(expected_venv):
    raise ValueError("bin_dir or runtime_venv changed before update")
if data.get("stamped_version") != prior_stamp:
    raise ValueError("receipt stamp changed before update")
if data.get("install_kind") not in ("release-asset", "source-build") or data.get("kernel_source") == "editable":
    raise ValueError("ordinary provenance changed before update")
if not isinstance(data.get("managed_binaries"), list) or expected_bin + "/lingtai-tui" not in data["managed_binaries"]:
    raise ValueError("managed TUI target changed before update")
data["stamped_version"] = tui_tag
data["kernel_version"] = kernel_version
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
  partial_fail "kernel and TUI components may have changed; metadata may be stale because its atomic receipt update failed"
fi
printf 'PASS: exact TUI %s and kernel %s updated; kernel/TUI/metadata phases completed.\n' "$tui_tag" "$kernel_version"
