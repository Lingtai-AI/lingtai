#requires -Version 5.1
<#
.SYNOPSIS
    LingTai native Windows (PowerShell) bounded runtime repair.

.DESCRIPTION
    The PowerShell counterpart to fix.sh. Defaults to a read-only plan; never
    sources install.ps1. Read-only by default; --apply --yes may create only the
    exact new owned runtime child and repoint a validated ordinary receipt,
    never delete old state.

    The diagnosis is read-only. --apply --yes requires one explicitly named free
    runtime directory directly under %USERPROFILE%\.lingtai-tui\runtime. The
    bootstrap python command parses only the old receipt and creates the new
    venv; the old runtime is never executed. Existing runtime
    pointers/provenance are checked structurally before any repair directory is
    made.

.PARAMETER BinDir
    Directory the managed binaries live in. Required.

.PARAMETER RuntimeDir
    Exact repair runtime directory (direct child of
    %USERPROFILE%\.lingtai-tui\runtime). Required for --apply.

.PARAMETER KernelArtifact
    .whl file path or official HTTPS URL for the replacement kernel.

.PARAMETER KernelSha256
    SHA-256 hex of the kernel artifact (64 hex chars).

.PARAMETER Apply
    Authorize the repair mutation (without it the script only diagnoses).

.PARAMETER Yes
    Required together with --apply after reviewing the plan.

.EXAMPLE
    .\fix.ps1 -BinDir "$env:LOCALAPPDATA\Programs\lingtai\bin" `
        -RuntimeDir "$env:USERPROFILE\.lingtai-tui\runtime\venv-repair-20260815" `
        -KernelArtifact kernel-1.0.0-py3-none-any.whl -KernelSha256 <64hex> -Apply -Yes

.NOTES
    Requires PowerShell 5.1 or later and native Windows. The repair directory is
    intentionally never removed after creation; any failure names it as
    possibly partial instead of claiming rollback. The bootstrap python parses
    the old receipt and creates the new venv; the old runtime is never executed.
#>
[CmdletBinding()]
param(
    [string]$BinDir,
    [string]$RuntimeDir = '',
    [string]$KernelArtifact = '',
    [string]$KernelSha256 = '',
    [switch]$Apply,
    [switch]$Yes
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Write-Info { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Step { param([string]$Message) Write-Host "  -> $Message" -ForegroundColor DarkGray }

function Fail {
    param([string]$Message)
    Write-Error $Message
    throw $Message
}

function Partial-Fail {
    param([string]$Message)
    Write-Error $Message
    throw $Message
}

function Test-AbsolutePath {
    param([string]$Path)
    if ([string]::IsNullOrWhiteSpace($Path)) { return $false }
    if (-not [System.IO.Path]::IsPathRooted($Path)) { return $false }
    if ($Path -match '[\r\n\t\x00]') { return $false }
    return $true
}

function Test-ReparsePoint {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return $false }
    $item = Get-Item -LiteralPath $Path -Force -ErrorAction SilentlyContinue
    return [bool]($item -and $item.LinkType)
}

function Get-JsonProperty {
    param($Data, [string]$Name)
    if ($Data.PSObject.Properties.Name -contains $Name) { return $Data.$Name }
    return $null
}

# --- Preconditions -----------------------------------------------------------

if ($PSVersionTable.PSVersion.Major -lt 5) {
    Fail "PowerShell 5.1 or later is required (found $($PSVersionTable.PSVersion))."
}

$onWindows = $false
if (Get-Variable -Name IsWindows -Scope Global -ErrorAction SilentlyContinue) {
    $onWindows = [bool]$IsWindows
} else {
    $onWindows = ($env:OS -eq 'Windows_NT') -or `
                 ([System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT)
}
if (-not $onWindows) {
    Fail "fix.ps1 supports native Windows only. On macOS/Linux/WSL, use fix.sh instead."
}

if (-not $BinDir) { Fail "-BinDir is required. Usage: fix.ps1 -BinDir DIR [-RuntimeDir DIR] [--Apply --Yes]" }
if (-not (Test-AbsolutePath $BinDir)) { Fail "bin directory is not an exact absolute path: $BinDir" }
if (-not $RuntimeDir) {
    if ($Apply) { Fail "-RuntimeDir is required for -Apply" }
    $RuntimeDir = Join-Path $env:USERPROFILE '.lingtai-tui\runtime\diagnosis'
}
if (-not (Test-AbsolutePath $RuntimeDir)) { Fail "runtime directory is not an exact absolute path: $RuntimeDir" }
if ($Yes -and -not $Apply) { Fail "-Yes is meaningful only with -Apply" }

$globalDir = Join-Path $env:USERPROFILE '.lingtai-tui'
$runtimeRoot = Join-Path $globalDir 'runtime'
$metadata = Join-Path $globalDir 'install.json'

# This is the only interpreter selected before repair. It is used only for the
# strict read-only receipt parser and, below, for `-m venv`; it never imports
# the old runtime or installs the kernel.
$bootstrap = (Get-Command python -ErrorAction SilentlyContinue)
if (-not $bootstrap) { $bootstrap = (Get-Command py -ErrorAction SilentlyContinue) }
if (-not $bootstrap) { Fail "python is required to parse the receipt and create the repair venv" }
$bootstrapPath = $bootstrap.Source
if (-not (Test-Path -LiteralPath $bootstrapPath)) { Fail "python bootstrap is not executable" }

if (Test-ReparsePoint $globalDir) { Fail "owned installation root is a reparse point/symlink: $globalDir" }
if (Test-ReparsePoint $runtimeRoot) { Fail "owned runtime root is a reparse point/symlink: $runtimeRoot" }
if (Test-ReparsePoint $BinDir) { Fail "target bin directory is a reparse point/symlink: $BinDir" }
if (-not (Test-Path -LiteralPath $BinDir -PathType Container)) { Fail "target is not one exact existing installation: $BinDir" }
$tuiExe = Join-Path $BinDir 'lingtai-tui.exe'
if (Test-ReparsePoint $tuiExe) { Fail "target TUI binary is a reparse point/symlink: $tuiExe" }
if (-not (Test-Path -LiteralPath $tuiExe -PathType Leaf)) { Fail "target is not one exact existing installation: $tuiExe" }
if (Test-ReparsePoint $metadata) { Fail "owned install metadata is missing or redirected: $metadata" }
if (-not (Test-Path -LiteralPath $metadata -PathType Leaf)) { Fail "owned install metadata is missing or redirected: $metadata" }
if (-not (Test-Path -LiteralPath $runtimeRoot -PathType Container)) { Fail "owned runtime root is absent: $runtimeRoot" }
$ownedRootPhysical = (Get-Item -LiteralPath $runtimeRoot -Force).FullName

# Both the old pointer and the new requested directory must be one lexical,
# normalized direct child. The old child may be missing; if present it must be
# a real directory, never a reparse point or file.
function Test-ValidChild {
    param([string]$Path)
    if (-not $Path.StartsWith($runtimeRoot + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase)) { return $false }
    if ($Path.EndsWith([System.IO.Path]::DirectorySeparatorChar)) { return $false }
    $parent = Split-Path -Path $Path -Parent
    $name = Split-Path -Path $Path -Leaf
    if ($parent -ne $runtimeRoot) { return $false }
    if ($name -notmatch '^[A-Za-z0-9._-]+$') { return $false }
    $expected = Join-Path $runtimeRoot $name
    if (-not $Path.Equals($expected, [System.StringComparison]::OrdinalIgnoreCase)) { return $false }
    return $true
}

if (-not (Test-ValidChild $RuntimeDir)) { Fail "repair directory must be one exact direct child of the canonical runtime root: $RuntimeDir" }
$repairParentPhysical = (Get-Item -LiteralPath $runtimeRoot -Force).FullName
if ($repairParentPhysical -ne $ownedRootPhysical) { Fail "repair directory parent is not physically the owned runtime root" }

# LINGTAI_RECEIPT_PARSE_FIX: the bootstrap parses a strict v1 receipt and emits
# stamp<TAB>old-pointer. It does not execute the old pointer.
$priorRecord = $null
$parseScript = @'
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
target = expected_bin + "\\lingtai-tui.exe"
managed = data.get("managed_binaries")
if not isinstance(managed, list) or target not in managed: raise SystemExit("managed_binaries does not own lingtai-tui.exe")
if data.get("install_kind") not in ("release-asset", "source-build", "powershell-release-asset", "powershell-local-artifact", "powershell-source-build"): raise SystemExit("receipt is not ordinary provenance")
if data.get("kernel_source") == "editable" or data.get("install_kind") in ("dev-source", "powershell-dev-source"): raise SystemExit("dev-source receipt cannot be repaired as ordinary")
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
if not re.fullmatch(r"[A-Za-z0-9._-]+", name) or old != runtime_root + "\\" + name: raise SystemExit("runtime_venv is not a safe direct child")
if os.path.lexists(old) and (os.path.islink(old) or not os.path.isdir(old)): raise SystemExit("existing runtime_venv is not a real directory")
print(stamp + "\t" + old + "\t" + kernel_version)
'@
try {
    $priorOut = (& $bootstrapPath -c $parseScript $metadata $BinDir $runtimeRoot 2>&1 | Out-String)
    if ($LASTEXITCODE -ne 0) { Fail "prior receipt is not a strict ordinary v1 receipt: $priorOut" }
    $priorRecord = $priorOut.Trim()
} catch {
    Fail "prior receipt is not a strict ordinary v1 receipt: $($_.Exception.Message)"
}
$priorParts = $priorRecord -split "`t"
if ($priorParts.Count -ne 3 -or -not $priorParts[0] -or -not $priorParts[1] -or -not $priorParts[2]) {
    Fail "prior receipt parser emitted incomplete runtime/kernel provenance"
}
$priorStamp = $priorParts[0]
$priorVenv = $priorParts[1]
$priorKernelVersion = $priorParts[2]
if (-not (Test-ValidChild $priorVenv)) { Fail "prior runtime pointer is not one exact direct child of the canonical runtime root: $priorVenv" }
$priorParentPhysical = (Get-Item -LiteralPath $runtimeRoot -Force).FullName
if ($priorParentPhysical -ne $ownedRootPhysical) { Fail "prior runtime parent is not physically the owned runtime root" }
if (Test-Path -LiteralPath $priorVenv) {
    if (Test-ReparsePoint $priorVenv) { Fail "existing prior runtime pointer is not a real directory: $priorVenv" }
    if (-not (Test-Path -LiteralPath $priorVenv -PathType Container)) { Fail "existing prior runtime pointer is not a real directory: $priorVenv" }
}

$state = 'free'
if (Test-Path -LiteralPath $RuntimeDir) { $state = 'occupied' }
Write-Host "Diagnosis: bin=$BinDir; metadata=$metadata; prior-runtime=$priorVenv; prior-stamp=$priorStamp; kernel=$priorKernelVersion; repair-runtime=$RuntimeDir ($state)"
if (-not $Apply) {
    Write-Host "Read-only plan: no state changed. Re-run with -Apply -Yes and an exact free runtime-dir to repair."
    exit 0
}
if (-not $Yes) { Fail "-Apply is mutating; provide -Yes after reviewing the plan" }
if (Test-Path -LiteralPath $RuntimeDir) { Fail "repair target is occupied; choose one exact free runtime-dir; no existing directory was overwritten" }
if (-not $KernelArtifact) { Fail "-KernelArtifact is required for -Apply" }
if ($KernelSha256 -notmatch '^[0-9a-fA-F]{64}$') { Fail "-KernelSha256 must be 64 hex" }

# Fetch/verify the kernel artifact.
$kernelBasename = Split-Path -Path $KernelArtifact -Leaf
if ($kernelBasename -notmatch '\.whl$') { Fail "-KernelArtifact must be a .whl file: $KernelArtifact" }
$work = Join-Path $env:TEMP ("lingtai-fix-" + [System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Force -Path $work | Out-Null
try {
    $kernelPath = Join-Path $work $kernelBasename
    if ($KernelArtifact.StartsWith('https://github.com/Lingtai-AI/') -or $KernelArtifact.StartsWith('https://gitee.com/huangzesen1997/')) {
        try { Invoke-WebRequest -Uri $KernelArtifact -OutFile $kernelPath -UseBasicParsing -TimeoutSec 300 }
        catch { Fail "kernel artifact download failed: $($_.Exception.Message)" }
    } elseif ($KernelArtifact.StartsWith([System.IO.Path]::DirectorySeparatorChar)) {
        try { Copy-Item -LiteralPath $KernelArtifact -Destination $kernelPath -Force -ErrorAction Stop }
        catch { Fail "kernel artifact copy failed: $($_.Exception.Message)" }
    } else {
        Fail "artifact must be an exact path or official HTTPS URL: $KernelArtifact"
    }
    $got = (Get-FileHash -LiteralPath $kernelPath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($got -ne $KernelSha256.ToLowerInvariant()) { Fail "kernel artifact SHA-256 mismatch" }

    # The repair directory is intentionally never removed after this point. Any
    # failure names it as possibly partial instead of claiming rollback.
    & $bootstrapPath -m venv $RuntimeDir 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Partial-Fail "repair venv creation failed; partial directory may exist at $RuntimeDir and was not deleted or overwritten" }
    $newRuntime = Join-Path $RuntimeDir 'Scripts\python.exe'
    if (-not (Test-Path -LiteralPath $newRuntime -PathType Leaf)) { Partial-Fail "repair venv is incomplete; partial directory may exist at $RuntimeDir and was not deleted or overwritten" }
    $newVenvPhysical = (Get-Item -LiteralPath $RuntimeDir -Force).FullName
    $newParent = Split-Path -Path $RuntimeDir -Parent
    $newParentPhysical = (Get-Item -LiteralPath $newParent -Force).FullName
    if ($newParentPhysical -ne $ownedRootPhysical) { Partial-Fail "repair venv escaped its owned root; partial directory may exist at $RuntimeDir" }

    & $newRuntime -m pip install --disable-pip-version-check --no-deps --force-reinstall $kernelPath 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Partial-Fail "kernel install failed; partial directory may exist at $RuntimeDir and was not deleted or overwritten" }

    # Import postcondition on the repaired runtime.
    $importScript = @'
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
'@
    & $newRuntime -c $importScript $newVenvPhysical $priorKernelVersion 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Partial-Fail "repaired runtime postcondition failed; partial directory may exist at $RuntimeDir and was not deleted or overwritten" }

    # Receipt revalidation + repoint (atomic).
    $revalidateScript = @'
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
if data.get("install_kind") not in ("release-asset", "source-build", "powershell-release-asset", "powershell-local-artifact", "powershell-source-build") or data.get("kernel_source") == "editable":
    raise ValueError("ordinary provenance changed")
if not isinstance(data.get("managed_binaries"), list) or expected_bin + "\\lingtai-tui.exe" not in data["managed_binaries"]:
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
'@
    & $newRuntime -c $revalidateScript $metadata $BinDir $priorVenv $newVenvPhysical $priorStamp $priorKernelVersion 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Partial-Fail "repair runtime may exist at $RuntimeDir; metadata was not intentionally changed because its atomic update failed" }

    Write-Host "PASS: one exact runtime repair created at $RuntimeDir; prior TUI target/provenance were preserved."
}
finally {
    if (Test-Path -LiteralPath $work) { Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue }
}
