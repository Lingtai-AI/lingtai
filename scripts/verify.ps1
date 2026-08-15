#requires -Version 5.1
<#
.SYNOPSIS
    LingTai native Windows (PowerShell) installation verification.

.DESCRIPTION
    The PowerShell counterpart to verify.sh: strictly read-only proof for one
    exact target/runtime/receipt. PASS is not mutation, migration, refresh,
    release, deploy, or another install's proof. It never changes state.

    Verifies the receipt at %USERPROFILE%\.lingtai-tui\install.json owns the
    requested -BinDir and -RuntimePython, that the runtime interpreter is a
    real executable under the canonical owned runtime root, that its sys.prefix
    matches the selected venv, that the TUI binary reports exactly one identity
    token (vX.Y.Z or dev), and -- via the selected runtime's own
    import/provenance probe -- that the installed lingtai kernel resolves from
    the receipt-declared source and its version exactly matches the receipt.

    Install kinds are the Windows-native set install.ps1 writes
    (powershell-release-asset, powershell-local-artifact,
    powershell-source-build, powershell-dev-source). Receipts written by a
    -Latest or -Ref install (powershell-latest-main, powershell-source-ref)
    stamp a main-<sha>/ref identity that this verify cannot prove as a release
    or dev receipt, exactly like verify.sh rejects a non-vX.Y.Z/dev stamp on
    POSIX; such receipts fail closed.

.PARAMETER BinDir
    Directory the managed binaries live in. Required.

.PARAMETER RuntimePython
    Absolute path to the venv's Scripts\python.exe. Required.

.PARAMETER Metadata
    Path to the install receipt. Default:
    %USERPROFILE%\.lingtai-tui\install.json.

.EXAMPLE
    .\verify.ps1 -BinDir "$env:LOCALAPPDATA\Programs\lingtai\bin" `
        -RuntimePython "$env:USERPROFILE\.lingtai-tui\runtime\venv\Scripts\python.exe"

.NOTES
    Requires PowerShell 5.1 or later. Read-only: PASS means every declared
    postcondition held; any failure exits non-zero and names the failed check.
    The selected runtime is the ONLY interpreter that parses the receipt or
    imports lingtai -- no textual JSON substitution, no shell-level parsing.
#>
[CmdletBinding()]
param(
    [string]$BinDir,
    [string]$RuntimePython,
    [string]$Metadata = ''
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

# Parse-TuiIdentity: the TUI contract is one identity token -- exactly one
# vX.Y.Z token or one standalone dev token. Garbage, duplicates, and mixed
# identities fail closed (returns $false).
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

function Get-JsonProperty {
    param($Data, [string]$Name)
    if ($Data.PSObject.Properties.Name -contains $Name) { return $Data.$Name }
    return $null
}

# Read-JsonNoDuplicateKeys: ConvertFrom-Json keeps the LAST value for a
# duplicate top-level key silently. install.json is a flat object; a
# single-depth scan is sufficient (same as remove.ps1).
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
    Fail "verify.ps1 supports native Windows only. On macOS/Linux/WSL, use verify.sh instead."
}

if (-not $BinDir) { Fail "-BinDir is required. Usage: verify.ps1 -BinDir DIR -RuntimePython PATH" }
if (-not (Test-AbsolutePath $BinDir)) { Fail "-BinDir is not an exact absolute path: $BinDir" }
if (-not $RuntimePython) { Fail "-RuntimePython is required. Usage: verify.ps1 -BinDir DIR -RuntimePython PATH" }
if (-not (Test-AbsolutePath $RuntimePython)) { Fail "-RuntimePython is not an exact absolute path: $RuntimePython" }
if (-not $Metadata) { $Metadata = Join-Path $env:USERPROFILE '.lingtai-tui\install.json' }
if (-not (Test-AbsolutePath $Metadata)) { Fail "-Metadata is not an exact absolute path: $Metadata" }

$globalDir = Join-Path $env:USERPROFILE '.lingtai-tui'
$runtimeRoot = Join-Path $globalDir 'runtime'

# --- Read-only structural checks ---------------------------------------------

if (Test-ReparsePoint $BinDir) { Fail "bin directory is a reparse point/symlink: $BinDir" }
if (-not (Test-Path -LiteralPath $BinDir -PathType Container)) { Fail "bin directory is missing: $BinDir" }
$tuiExe = Join-Path $BinDir 'lingtai-tui.exe'
if (Test-ReparsePoint $tuiExe) { Fail "TUI binary is a reparse point/symlink: $tuiExe" }
if (-not (Test-Path -LiteralPath $tuiExe -PathType Leaf)) { Fail "TUI binary is missing or not an owned regular file: $tuiExe" }
if (Test-ReparsePoint $Metadata) { Fail "install metadata is a reparse point/symlink: $Metadata" }
if (-not (Test-Path -LiteralPath $Metadata -PathType Leaf)) { Fail "install metadata is missing: $Metadata" }
if (Test-ReparsePoint $globalDir) { Fail "owned installation root is a reparse point/symlink: $globalDir" }
if (Test-ReparsePoint $runtimeRoot) { Fail "owned runtime root is a reparse point/symlink: $runtimeRoot" }

# System-managed Python is never an installation runtime (mirrors verify.sh's
# system/Homebrew refusal). Windows system roots: %WINDIR%, Program Files.
$systemRoots = @("$env:WINDIR", "$env:ProgramFiles", "${env:ProgramFiles(x86)}")
foreach ($root in $systemRoots) {
    if ($root -and ($RuntimePython -eq $root -or $RuntimePython.StartsWith($root + [System.IO.Path]::DirectorySeparatorChar, [System.StringComparison]::OrdinalIgnoreCase))) {
        Fail "system-managed Python is not an installation runtime: $RuntimePython"
    }
}

$runtimeRootPrefix = $runtimeRoot + [System.IO.Path]::DirectorySeparatorChar
if (-not $RuntimePython.StartsWith($runtimeRootPrefix, [System.StringComparison]::OrdinalIgnoreCase)) {
    Fail "runtime interpreter is not lexically under the canonical owned runtime root: $RuntimePython"
}
if (Test-ReparsePoint $RuntimePython) { Fail "runtime interpreter is a reparse point/symlink: $RuntimePython" }
if (-not (Test-Path -LiteralPath $RuntimePython -PathType Leaf)) { Fail "runtime interpreter is missing: $RuntimePython" }

# Canonicalize: owned root, selected venv (parent of the Scripts dir), and its
# parent must equal the owned runtime root.
$ownedRootPhysical = (Get-Item -LiteralPath $runtimeRoot -Force).FullName
$scriptsDir = Split-Path -Path $RuntimePython -Parent
$selectedVenv = Split-Path -Path $scriptsDir -Parent
$selectedParent = Split-Path -Path $selectedVenv -Parent
if ($selectedParent -ne $ownedRootPhysical) { Fail "runtime interpreter is outside the owned runtime root: $RuntimePython" }

# The selected runtime's sys.prefix must match the selected venv.
$prefixOk = $true
try {
    & $RuntimePython -c "import os,sys; raise SystemExit(0 if os.path.realpath(sys.prefix)==os.path.realpath(r'$selectedVenv') else 1)" 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { $prefixOk = $false }
} catch { $prefixOk = $false }
if (-not $prefixOk) { Fail "runtime interpreter prefix does not match selected venv: $RuntimePython" }

# --- TUI version probe ---------------------------------------------------------

$versionOutput = $null
$versionCode = 0
try {
    $versionOutput = (& $tuiExe 'version' 2>&1 | Out-String)
    $versionCode = $LASTEXITCODE
} catch {
    Fail "TUI version probe failed: $($_.Exception.Message)"
}
if ($versionCode -ne 0) { Fail "TUI version probe failed (exit $versionCode): $versionOutput" }
$version = Parse-TuiIdentity -Output $versionOutput
if ($version -eq $false -or [string]::IsNullOrEmpty($version)) {
    Fail "TUI identity is not exactly one vX.Y.Z or dev token: $versionOutput"
}

# --- Runtime receipt/import/provenance probe -----------------------------------

# The selected runtime parses the receipt and performs the provenance checks.
# A release receipt must import from its venv; a dev-source receipt must import
# from its metadata-declared checkout. Both remain bound to this venv's
# sys.prefix. Accepts the Windows-native install kinds install.ps1 writes for
# receipts this verifier can prove; latest-main/source-ref receipts fail closed.
$probe = $null
$probeCode = 0
$probeScript = @'
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
if kind not in ("powershell-release-asset", "powershell-local-artifact", "powershell-source-build", "powershell-dev-source"): raise SystemExit("unknown install_kind for verifiable receipt")
managed = data.get("managed_binaries")
if not isinstance(managed, list) or expected_bin + "\\lingtai-tui.exe" not in managed: raise SystemExit("managed_binaries does not own lingtai-tui.exe")
if kind == "powershell-dev-source":
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
'@
try {
    $probe = (& $RuntimePython -c $probeScript $Metadata $BinDir $selectedVenv $version 2>&1 | Out-String)
    $probeCode = $LASTEXITCODE
} catch {
    Fail "metadata/runtime/import/provenance probe failed: $($_.Exception.Message)"
}
if ($probeCode -ne 0) { Fail "metadata/runtime/import/provenance postcondition failed: $probe" }

Write-Host "PASS"
Write-Host "TUI: $version"
Write-Host "Runtime: $RuntimePython"
Write-Host "Runtime provenance: $($probe.Trim())"
