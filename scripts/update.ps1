#requires -Version 5.1
<#
.SYNOPSIS
    LingTai native Windows (PowerShell) exact-artifact update.

.DESCRIPTION
    The PowerShell counterpart to update.sh. Owns only an exact-artifact
    ordinary update after explicit -Yes; it neither selects latest nor changes
    provenance, repairs state, or updates Portal. It never sources install.ps1.

    Inputs are exact artifacts. Every download, checksum, archive, binary,
    version, and metadata check completes before mutation. -Yes authorizes
    mutation. The TUI archive is the Windows release .zip
    (lingtai-<tag>-windows-amd64.zip); the kernel artifact is a .whl.

.PARAMETER BinDir
    Directory the managed binaries live in. Required.

.PARAMETER RuntimePython
    Absolute path to the venv's Scripts\python.exe. Required.

.PARAMETER TuiArchive
    .zip file path or official HTTPS URL for the TUI release archive.

.PARAMETER TuiSha256
    SHA-256 hex of the TUI archive (64 hex chars).

.PARAMETER KernelArtifact
    .whl file path or official HTTPS URL for the kernel wheel.

.PARAMETER KernelSha256
    SHA-256 hex of the kernel wheel (64 hex chars).

.PARAMETER TuiTag
    Exact vX.Y.Z tag the TUI archive must report.

.PARAMETER KernelVersion
    Exact kernel version the installed wheel must report.

.PARAMETER Yes
    Required. Healthy update is mutating; -Yes authorizes it after reviewing
    exact inputs.

.EXAMPLE
    .\update.ps1 -BinDir "$env:LOCALAPPDATA\Programs\lingtai\bin" `
        -RuntimePython "$env:USERPROFILE\.lingtai-tui\runtime\venv\Scripts\python.exe" `
        -TuiArchive lingtai-v0.19.1-windows-amd64.zip -TuiSha256 <64hex> `
        -KernelArtifact kernel-0.19.1-py3-none-any.whl -KernelSha256 <64hex> `
        -TuiTag v0.19.1 -KernelVersion 0.19.1 -Yes

.NOTES
    Requires PowerShell 5.1 or later and native Windows. Cross-component
    rollback is not possible: every failure names which components may have
    changed. The selected runtime is the ONLY interpreter that parses/writes
    receipts or imports lingtai.
#>
[CmdletBinding()]
param(
    [string]$BinDir,
    [string]$RuntimePython,
    [string]$TuiArchive,
    [string]$TuiSha256,
    [string]$KernelArtifact,
    [string]$KernelSha256,
    [string]$TuiTag,
    [string]$KernelVersion,
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

function Read-JsonNoDuplicateKeys {
    param([string]$RawJson)
    $seen = New-Object 'System.Collections.Generic.HashSet[string]'
    foreach ($m in [regex]::Matches($RawJson, '"([A-Za-z_]+)"\s*:')) {
        $key = $m.Groups[1].Value
        if (-not $seen.Add($key)) {
            Fail "install metadata has a duplicate JSON key: $key"
        }
    }
    try {
        return $RawJson | ConvertFrom-Json
    } catch {
        Fail "install metadata JSON could not be parsed ($($_.Exception.Message))"
    }
}

# Parse-TuiIdentity: exactly one vX.Y.Z token or one dev token; otherwise $false.
function Parse-TuiIdentity {
    param([string]$Output)
    $identity = ''
    $count = 0
    foreach ($token in ($Output -split '\s+' | Where-Object { $_ -ne '' })) {
        $candidate = ''
        if ($token -ceq 'dev') { $candidate = 'dev' }
        elseif ($token -match '^v[0-9]+\.[0-9]+\.[0-9]+$') { $candidate = $token }
        else { continue }
        $identity = $candidate
        $count += 1
    }
    if ($count -ne 1) { return $false }
    return $identity
}

function Invoke-Fetch {
    param([string]$Source, [string]$Destination)
    if ($Source.StartsWith('https://github.com/Lingtai-AI/') -or $Source.StartsWith('https://gitee.com/huangzesen1997/')) {
        try { Invoke-WebRequest -Uri $Source -OutFile $Destination -UseBasicParsing -TimeoutSec 300 }
        catch { Fail "artifact download failed: $Source ($($_.Exception.Message))" }
    } elseif ($Source.StartsWith([System.IO.Path]::DirectorySeparatorChar)) {
        try { Copy-Item -LiteralPath $Source -Destination $Destination -Force -ErrorAction Stop }
        catch { Fail "artifact copy failed: $Source ($($_.Exception.Message))" }
    } else {
        Fail "artifact must be an absolute path or official HTTPS URL: $Source"
    }
}

function Invoke-Sha256 {
    param([string]$Path)
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
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
    Fail "update.ps1 supports native Windows only. On macOS/Linux/WSL, use update.sh instead."
}

if (-not $BinDir) { Fail "-BinDir is required." }
if (-not $RuntimePython) { Fail "-RuntimePython is required." }
if (-not $TuiArchive) { Fail "-TuiArchive is required." }
if (-not $KernelArtifact) { Fail "-KernelArtifact is required." }
if (-not $TuiTag) { Fail "-TuiTag is required." }
if (-not $KernelVersion) { Fail "-KernelVersion is required." }
if (-not (Test-AbsolutePath $BinDir)) { Fail "bin directory is not an exact absolute path: $BinDir" }
if (-not (Test-AbsolutePath $RuntimePython)) { Fail "runtime path is not an exact absolute path: $RuntimePython" }
if ($TuiTag -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+$') { Fail "TUI tag is not exact vX.Y.Z: $TuiTag" }
if ($TuiSha256 -notmatch '^[0-9a-fA-F]{64}$') { Fail "TUI SHA-256 is not 64 hex" }
if ($KernelSha256 -notmatch '^[0-9a-fA-F]{64}$') { Fail "kernel SHA-256 is not 64 hex" }
if (-not $Yes) { Fail "healthy update is mutating; provide -Yes after reviewing exact inputs" }

$globalDir = Join-Path $env:USERPROFILE '.lingtai-tui'
$runtimeRoot = Join-Path $globalDir 'runtime'
$metadata = Join-Path $globalDir 'install.json'

if (Test-ReparsePoint $globalDir) { Fail "owned installation root is a reparse point/symlink: $globalDir" }
if (Test-ReparsePoint $runtimeRoot) { Fail "owned runtime root is a reparse point/symlink: $runtimeRoot" }
if (Test-ReparsePoint $BinDir) { Fail "target bin directory is a reparse point/symlink: $BinDir" }
if (-not (Test-Path -LiteralPath $BinDir -PathType Container)) { Fail "target is not one owned existing installation: $BinDir" }
$tuiExe = Join-Path $BinDir 'lingtai-tui.exe'
if (Test-ReparsePoint $tuiExe) { Fail "target TUI binary is a reparse point/symlink: $tuiExe" }
if (-not (Test-Path -LiteralPath $tuiExe -PathType Leaf)) { Fail "target is not one owned existing installation: $tuiExe" }
if (Test-ReparsePoint $metadata) { Fail "install metadata is missing or redirected: $metadata" }
if (-not (Test-Path -LiteralPath $metadata -PathType Leaf)) { Fail "install metadata is missing or redirected: $metadata" }

$systemRoots = @("$env:WINDIR", "$env:ProgramFiles", "${env:ProgramFiles(x86)}")
foreach ($root in $systemRoots) {
    if ($root -and ($RuntimePython -eq $root -or $RuntimePython.StartsWith($root + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase))) {
        Fail "system-managed Python is forbidden as a runtime: $RuntimePython"
    }
}
$runtimeRootPrefix = $runtimeRoot + [System.IO.Path]::DirectorySeparatorChar
if (-not $RuntimePython.StartsWith($runtimeRootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    Fail "runtime interpreter is not lexically under the canonical owned runtime root: $RuntimePython"
}
if (Test-ReparsePoint $RuntimePython) { Fail "runtime interpreter is a reparse point/symlink: $RuntimePython" }
if (-not (Test-Path -LiteralPath $RuntimePython -PathType Leaf)) { Fail "runtime interpreter is missing: $RuntimePython" }

$ownedRootPhysical = (Get-Item -LiteralPath $runtimeRoot -Force).FullName
$scriptsDir = Split-Path -Path $RuntimePython -Parent
$selectedVenv = Split-Path -Path $scriptsDir -Parent
$selectedParent = Split-Path -Path $selectedVenv -Parent
if ($selectedParent -ne $ownedRootPhysical) { Fail "runtime venv escapes the owned runtime root: $RuntimePython" }
$prefixOk = $true
try {
    & $RuntimePython -c "import os,sys; raise SystemExit(0 if os.path.realpath(sys.prefix)==os.path.realpath(r'$selectedVenv') else 1)" 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { $prefixOk = $false }
} catch { $prefixOk = $false }
if (-not $prefixOk) { Fail "runtime interpreter prefix does not match selected venv: $RuntimePython" }

# --- Prior receipt parse + current TUI identity -------------------------------

$parseScript = @'
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
if data.get("install_kind") not in ("release-asset", "source-build", "powershell-release-asset", "powershell-local-artifact", "powershell-source-build"): raise SystemExit("receipt is not ordinary install provenance")
if data.get("kernel_source") == "editable" or data.get("install_kind") in ("dev-source", "powershell-dev-source"): raise SystemExit("dev-source receipt cannot be updated as an ordinary install")
managed = data.get("managed_binaries")
target = expected_bin + "\\lingtai-tui.exe"
if not isinstance(managed, list) or target not in managed: raise SystemExit("managed_binaries does not own lingtai-tui.exe")
print(data["stamped_version"] + "\t" + runtime_pointer)
'@
$priorRecord = $null
try {
    $priorOut = (& $RuntimePython -c $parseScript $metadata $BinDir $selectedVenv 2>&1 | Out-String)
    if ($LASTEXITCODE -ne 0) { Fail "install metadata is not a valid ordinary v1 receipt: $priorOut" }
    $priorRecord = $priorOut.Trim()
} catch {
    Fail "install metadata is not a valid ordinary v1 receipt: $($_.Exception.Message)"
}
$priorParts = $priorRecord -split "`t"
if ($priorParts.Count -ne 2 -or -not $priorParts[0] -or -not $priorParts[1]) {
    Fail "install metadata parser emitted no prior stamp/runtime pointer"
}
$priorStamp = $priorParts[0]
$priorVenv = $priorParts[1]

$currentOutput = $null
try {
    $currentOutput = (& $tuiExe 'version' 2>&1 | Out-String)
    if ($LASTEXITCODE -ne 0) { Fail "installed TUI version probe failed before update" }
} catch {
    Fail "installed TUI version probe failed before update: $($_.Exception.Message)"
}
$currentIdentity = Parse-TuiIdentity -Output $currentOutput
if ($currentIdentity -eq $false -or [string]::IsNullOrEmpty($currentIdentity)) { Fail "installed TUI identity is not exactly one vX.Y.Z or dev token: $currentOutput" }
if ($currentIdentity -ne $priorStamp) { Fail "installed TUI identity does not match the receipt stamped_version" }

# --- Download/verify/stage exact artifacts ------------------------------------

$work = Join-Path $env:TEMP ("lingtai-update-" + [System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Force -Path $work | Out-Null
try {
    $tuiZip = Join-Path $work 'tui.zip'
    Invoke-Fetch -Source $TuiArchive -Destination $tuiZip
    $kernelBasename = Split-Path -Path $KernelArtifact -Leaf
    if ($kernelBasename -notmatch '\.whl$') { Fail "--kernel-artifact must be a .whl file: $KernelArtifact" }
    $kernelPath = Join-Path $work $kernelBasename
    Invoke-Fetch -Source $KernelArtifact -Destination $kernelPath
    $gotTui = Invoke-Sha256 -Path $tuiZip
    if ($gotTui -ne $TuiSha256.ToLowerInvariant()) { Fail "SHA-256 mismatch for TUI archive" }
    $gotKernel = Invoke-Sha256 -Path $kernelPath
    if ($gotKernel -ne $KernelSha256.ToLowerInvariant()) { Fail "SHA-256 mismatch for kernel artifact" }

    # Expand the TUI archive into a fresh staging dir and require exactly one
    # executable lingtai-tui.exe. System.IO.Compression rejects entries with
    # unsafe traversal, and we additionally refuse absolute/..-style entries.
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $tuiStage = Join-Path $work 'tui'
    New-Item -ItemType Directory -Force -Path $tuiStage | Out-Null
    try {
        $zip = [System.IO.Compression.ZipFile]::OpenRead($tuiZip)
        try {
            foreach ($entry in $zip.Entries) {
                $name = $entry.FullName
                if ($name.StartsWith('/') -or $name -match '(^|[\\/])\.\.([\\/]|$)' -or $name -match '^[A-Za-z]:') {
                    Fail "TUI archive has unsafe path: $name"
                }
            }
        } finally { $zip.Dispose() }
        [System.IO.Compression.ZipFile]::ExtractToDirectory($tuiZip, $tuiStage)
    } catch {
        Fail "TUI archive extraction failed: $($_.Exception.Message)"
    }
    $candidates = @(Get-ChildItem -LiteralPath $tuiStage -Recurse -Filter 'lingtai-tui.exe' -File -ErrorAction SilentlyContinue)
    if ($candidates.Count -ne 1) { Fail "TUI archive must contain exactly one executable lingtai-tui.exe (found $($candidates.Count))" }
    $stagedTui = $candidates[0].FullName
    $stagedOutput = $null
    try {
        $stagedOutput = (& $stagedTui 'version' 2>&1 | Out-String)
        if ($LASTEXITCODE -ne 0) { Fail "TUI archive binary probe failed" }
    } catch {
        Fail "TUI archive binary probe failed: $($_.Exception.Message)"
    }
    $candidateIdentity = Parse-TuiIdentity -Output $stagedOutput
    if ($candidateIdentity -eq $false -or [string]::IsNullOrEmpty($candidateIdentity)) { Fail "TUI archive identity is not exactly one vX.Y.Z or dev token: $stagedOutput" }
    if ($candidateIdentity -ne $TuiTag) { Fail "TUI archive identity does not equal the requested tag" }
    Write-Host "Preflight complete: exact TUI $TuiTag, kernel $KernelVersion, target $BinDir"

    # --- Mutation phases (explicit; cross-component rollback is not possible) ---

    & $RuntimePython -m pip install --disable-pip-version-check --no-deps --force-reinstall $kernelPath 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Partial-Fail "kernel component may have changed; TUI and metadata were not intentionally changed" }

    $kernelProbe = @'
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
'@
    & $RuntimePython -c $kernelProbe $selectedVenv $KernelVersion 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Partial-Fail "kernel component may have changed; TUI and metadata were not intentionally changed (runtime postcondition failed)" }

    $newTui = Join-Path $BinDir (".lingtai-tui.update." + $PID)
    try {
        Copy-Item -LiteralPath $stagedTui -Destination $newTui -Force -ErrorAction Stop
        Move-Item -LiteralPath $newTui -Destination $tuiExe -Force -ErrorAction Stop
    } catch {
        if (Test-Path -LiteralPath $newTui) { Remove-Item -LiteralPath $newTui -Force -ErrorAction SilentlyContinue }
        Partial-Fail "kernel and possibly TUI components may have changed; metadata was not intentionally changed ($($_.Exception.Message))"
    }
    $installedOutput = $null
    try {
        $installedOutput = (& $tuiExe 'version' 2>&1 | Out-String)
        if ($LASTEXITCODE -ne 0) { Partial-Fail "kernel and TUI components may have changed; metadata was not intentionally changed (TUI postcondition failed)" }
    } catch {
        Partial-Fail "kernel and TUI components may have changed; metadata was not intentionally changed (TUI postcondition failed): $($_.Exception.Message)"
    }
    $installedIdentity = Parse-TuiIdentity -Output $installedOutput
    if ($installedIdentity -eq $false -or $installedIdentity -ne $TuiTag) {
        Partial-Fail "kernel and TUI components may have changed; metadata was not intentionally changed (TUI identity postcondition failed)"
    }

    $revalidateScript = @'
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
if data.get("install_kind") not in ("release-asset", "source-build", "powershell-release-asset", "powershell-local-artifact", "powershell-source-build") or data.get("kernel_source") == "editable":
    raise ValueError("ordinary provenance changed before update")
if not isinstance(data.get("managed_binaries"), list) or expected_bin + "\\lingtai-tui.exe" not in data["managed_binaries"]:
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
'@
    & $RuntimePython -c $revalidateScript $metadata $BinDir $selectedVenv $priorStamp $priorVenv $TuiTag $KernelVersion 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Partial-Fail "kernel and TUI components may have changed; metadata may be stale because its atomic receipt update failed" }

    Write-Host "PASS: exact TUI $TuiTag and kernel $KernelVersion updated; kernel/TUI/metadata phases completed."
}
finally {
    if (Test-Path -LiteralPath $work) { Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue }
}
