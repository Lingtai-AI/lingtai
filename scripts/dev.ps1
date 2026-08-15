#requires -Version 5.1
<#
.SYNOPSIS
    LingTai native Windows (PowerShell) editable development installation.

.DESCRIPTION
    The PowerShell counterpart to dev.sh. Owns only explicit editable
    development state after -Yes; build writes stay in the named
    checkouts/runtime/target and never imply release or deployment. It never
    sources install.ps1.

    Builds lingtai-tui.exe (and lingtai-portal.exe unless -SkipPortal) from the
    exact -TuiSource Git checkout, installs the kernel editable from the exact
    -KernelSource checkout into the selected runtime venv, installs the built
    binaries into -BinDir, verifies the import/source-provenance/TUI-identity
    postconditions through the selected runtime and the installed TUI, and only
    then writes the complete v1 receipt (install_kind
    powershell-dev-source, kernel_source editable) atomically.

.PARAMETER TuiSource
    Absolute path to a Git checkout of the TUI repository (must contain
    tui\go.mod). Required.

.PARAMETER KernelSource
    Absolute path to a Git checkout of the kernel repository (must contain
    pyproject.toml or setup.py). Required.

.PARAMETER BinDir
    Absolute directory the built binaries are installed into. Required.

.PARAMETER RuntimePython
    Absolute path to the venv's Scripts\python.exe. Default:
    %USERPROFILE%\.lingtai-tui\runtime\venv\Scripts\python.exe.

.PARAMETER Yes
    Required. Development install mutates checkout/runtime/target; -Yes
    authorizes it after the plan is validated.

.PARAMETER SkipPortal
    Skip building/installing lingtai-portal.exe (TUI-only install).

.EXAMPLE
    .\dev.ps1 -TuiSource C:\src\lingtai -KernelSource C:\src\lingtai-kernel `
        -BinDir "$env:LOCALAPPDATA\Programs\lingtai\bin" -Yes

.NOTES
    Requires PowerShell 5.1 or later and native Windows (go, npm for Portal).
    A partial failure names which components may have changed and never claims
    rollback. The selected runtime is the ONLY interpreter that parses/writes
    receipts or imports lingtai.
#>
[CmdletBinding()]
param(
    [string]$TuiSource,
    [string]$KernelSource,
    [string]$BinDir,
    [string]$RuntimePython = '',
    [switch]$Yes,
    [switch]$SkipPortal
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

function Write-Info { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Step { param([string]$Message) Write-Host "  -> $Message" -ForegroundColor DarkGray }
function Write-Ok { param([string]$Message) Write-Host "PASS: $Message" -ForegroundColor Green }

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

function Invoke-NativeChecked {
    param([string]$Tool, [string[]]$Arguments, [string]$Failure, [string]$WorkingDirectory = '')
    if ($WorkingDirectory) { Push-Location $WorkingDirectory }
    try {
        & $Tool @Arguments 2>&1 | Out-Null
        if ($LASTEXITCODE -ne 0) { Fail "$Failure (exit $LASTEXITCODE)" }
    } finally {
        if ($WorkingDirectory) { Pop-Location }
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
    Fail "dev.ps1 supports native Windows only. On macOS/Linux/WSL, use dev.sh instead."
}

if (-not $TuiSource) { Fail "-TuiSource is required." }
if (-not $KernelSource) { Fail "-KernelSource is required." }
if (-not $BinDir) { Fail "-BinDir is required." }
if (-not $Yes) { Fail "development install mutates checkout/runtime/target; provide -Yes" }
foreach ($path in @($TuiSource, $KernelSource, $BinDir)) {
    if (-not (Test-AbsolutePath $path)) { Fail "path is not an exact absolute path: $path" }
}
if (-not (Test-Path -LiteralPath (Join-Path $TuiSource '.git'))) { Fail "TUI source is not a Git checkout: $TuiSource" }
if (-not (Test-Path -LiteralPath (Join-Path $KernelSource '.git'))) { Fail "kernel source is not a Git checkout: $KernelSource" }
if (-not (Test-Path -LiteralPath (Join-Path $TuiSource 'tui\go.mod'))) { Fail "TUI checkout lacks tui\go.mod: $TuiSource" }
if (-not (Test-Path -LiteralPath (Join-Path $KernelSource 'pyproject.toml')) -and -not (Test-Path -LiteralPath (Join-Path $KernelSource 'setup.py'))) { Fail "kernel checkout lacks packaging metadata: $KernelSource" }
if (Test-ReparsePoint $BinDir) { Fail "target bin directory is a reparse point/symlink: $BinDir" }

if (-not $RuntimePython) { $RuntimePython = Join-Path $env:USERPROFILE '.lingtai-tui\runtime\venv\Scripts\python.exe' }
if (-not (Test-AbsolutePath $RuntimePython)) { Fail "runtime path is not an exact absolute path: $RuntimePython" }
$globalDir = Join-Path $env:USERPROFILE '.lingtai-tui'
$runtimeRoot = Join-Path $globalDir 'runtime'
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
if (Test-ReparsePoint $globalDir) { Fail "owned installation root is a reparse point/symlink: $globalDir" }
if (Test-ReparsePoint $runtimeRoot) { Fail "owned runtime root is a reparse point/symlink: $runtimeRoot" }

# Ensure the venv exists (create it from a bootstrap python if absent).
$scriptsDir = Split-Path -Path $RuntimePython -Parent
$venvDir = Split-Path -Path $scriptsDir -Parent
if (-not (Test-Path -LiteralPath $RuntimePython)) {
    New-Item -ItemType Directory -Force -Path (Split-Path $RuntimePython -Parent) | Out-Null
    $bootstrap = (Get-Command python -ErrorAction SilentlyContinue)
    if (-not $bootstrap) { $bootstrap = (Get-Command py -ErrorAction SilentlyContinue) }
    if (-not $bootstrap) { Partial-Fail "no bootstrap python found to create the runtime venv at $venvDir" }
    & $bootstrap.Source -m venv $venvDir 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Partial-Fail "runtime venv may be partially created at $venvDir; it was not deleted or overwritten" }
}
if (-not (Test-Path -LiteralPath $RuntimePython -PathType Leaf)) { Partial-Fail "selected runtime is missing; possible partial venv: $venvDir" }

$ownedRootPhysical = (Get-Item -LiteralPath $runtimeRoot -Force).FullName
$selectedVenv = $venvDir
$selectedParent = Split-Path -Path $selectedVenv -Parent
if ($selectedParent -ne $ownedRootPhysical) { Fail "runtime venv escapes the owned runtime root: $venvDir" }
$prefixOk = $true
try {
    & $RuntimePython -c "import os,sys; raise SystemExit(0 if os.path.realpath(sys.prefix)==os.path.realpath(r'$selectedVenv') else 1)" 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { $prefixOk = $false }
} catch { $prefixOk = $false }
if (-not $prefixOk) { Fail "runtime interpreter prefix does not match selected venv: $RuntimePython" }

# Existing targets require a structurally valid owned receipt.
$metadata = Join-Path $globalDir 'install.json'
$tuiTarget = Join-Path $BinDir 'lingtai-tui.exe'
if (Test-Path -LiteralPath $tuiTarget) {
    if (Test-ReparsePoint $metadata) { Fail "existing target has no owned metadata: $metadata" }
    if (-not (Test-Path -LiteralPath $metadata -PathType Leaf)) { Fail "existing target has no owned metadata: $metadata" }
    $rawJson = Get-Content -LiteralPath $metadata -Raw
    $data = Read-JsonNoDuplicateKeys -RawJson $rawJson
    $schema = Get-JsonProperty $data 'schema'
    $schemaVersion = Get-JsonProperty $data 'schema_version'
    if ($schema -ne 'lingtai.tui.install/v1' -or $schemaVersion -ne 1) { Fail "existing target metadata is not a valid v1 receipt" }
    if ((Get-JsonProperty $data 'bin_dir') -ne $BinDir) { Fail "existing target metadata does not own this bin_dir" }
    $rv = Get-JsonProperty $data 'runtime_venv'
    $rvPhysical = $null
    if (Test-Path -LiteralPath $rv) { $rvPhysical = (Get-Item -LiteralPath $rv -Force).FullName }
    $selPhysical = (Get-Item -LiteralPath $selectedVenv -Force).FullName
    if (-not $rv -or $rvPhysical -ne $selPhysical) { Fail "existing target metadata does not own the selected venv" }
    $kind = Get-JsonProperty $data 'install_kind'
    $validKinds = @('release-asset', 'source-build', 'dev-source', 'powershell-release-asset', 'powershell-local-artifact', 'powershell-source-build', 'powershell-source-ref', 'powershell-latest-main', 'powershell-dev-source')
    if ($validKinds -notcontains $kind) { Fail "existing target metadata install_kind is not recognized" }
    $managed = @(Get-JsonProperty $data 'managed_binaries')
    if ($managed -notcontains $tuiTarget) { Fail "existing target metadata managed_binaries does not own lingtai-tui.exe" }
}

# --- Builds -----------------------------------------------------------------

$work = Join-Path $env:TEMP ("lingtai-dev-" + [System.IO.Path]::GetRandomFileName())
New-Item -ItemType Directory -Force -Path (Join-Path $work 'tui') | Out-Null

try {
    # TUI source build.
    Invoke-NativeChecked -Tool 'go' -Arguments @('build', '-trimpath', '-o', (Join-Path $work 'tui\lingtai-tui.exe'), '.') -Failure 'TUI source build failed; target and receipt were not intentionally changed' -WorkingDirectory (Join-Path $TuiSource 'tui')

    # Optional Portal source build.
    if (-not $SkipPortal -and (Test-Path -LiteralPath (Join-Path $TuiSource 'portal\web'))) {
        if (-not (Get-Command npm -ErrorAction SilentlyContinue)) { Fail "npm is required for Portal; use -SkipPortal explicitly" }
        if (-not (Get-Command go -ErrorAction SilentlyContinue)) { Fail "go is required for Portal" }
        Push-Location (Join-Path $TuiSource 'portal\web')
        try {
            & npm ci 2>&1 | Out-Null
            if ($LASTEXITCODE -ne 0) { Partial-Fail "Portal web dependency install failed; TUI target and receipt were not intentionally changed" }
            & npm run build 2>&1 | Out-Null
            if ($LASTEXITCODE -ne 0) { Partial-Fail "Portal web build failed; TUI target and receipt were not intentionally changed" }
        } finally { Pop-Location }
        Invoke-NativeChecked -Tool 'go' -Arguments @('build', '-trimpath', '-o', (Join-Path $work 'tui\lingtai-portal.exe'), '.') -Failure 'Portal source build failed; TUI target and receipt were not intentionally changed' -WorkingDirectory (Join-Path $TuiSource 'portal')
    }

    # Kernel editable install into the selected runtime.
    & $RuntimePython -m pip install --disable-pip-version-check --editable $KernelSource 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) { Partial-Fail "kernel editable install may have partially changed the selected runtime; target and receipt were not intentionally changed" }

    # Install binaries into BinDir.
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    try { Copy-Item -LiteralPath (Join-Path $work 'tui\lingtai-tui.exe') -Destination $tuiTarget -Force -ErrorAction Stop }
    catch { Partial-Fail "TUI target may have changed partially; receipt was not intentionally changed ($($_.Exception.Message))" }
    $managedTui = $tuiTarget
    $managedPortal = ''
    $portalOut = Join-Path $work 'tui\lingtai-portal.exe'
    if (-not $SkipPortal -and (Test-Path -LiteralPath $portalOut)) {
        $portalTarget = Join-Path $BinDir 'lingtai-portal.exe'
        try { Copy-Item -LiteralPath $portalOut -Destination $portalTarget -Force -ErrorAction Stop }
        catch { Partial-Fail "TUI and possibly Portal targets may have changed; receipt was not intentionally changed ($($_.Exception.Message))" }
        $managedPortal = $portalTarget
    }

    # Kernel import/provenance postcondition through the selected runtime.
    $sourcePhysical = (Get-Item -LiteralPath $KernelSource -Force).FullName
    $kernelProbe = @'
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
'@
    $observedKernelVersion = $null
    try {
        $kernelOut = (& $RuntimePython -c $kernelProbe $selectedVenv $sourcePhysical 2>&1 | Out-String)
        if ($LASTEXITCODE -ne 0) { Partial-Fail "editable runtime postcondition failed; TUI/Portal may have changed and no receipt was written: $kernelOut" }
        $observedKernelVersion = $kernelOut.Trim()
    } catch {
        Partial-Fail "editable runtime postcondition failed; TUI/Portal may have changed and no receipt was written: $($_.Exception.Message)"
    }

    # TUI identity postcondition through the installed binary.
    $tuiOutput = $null
    try {
        $tuiOutput = (& $tuiTarget 'version' 2>&1 | Out-String)
    } catch {
        Partial-Fail "TUI version postcondition failed; runtime and TUI may have changed and no receipt was written: $($_.Exception.Message)"
    }
    $stampedVersion = Parse-TuiIdentity -Output $tuiOutput
    if ($stampedVersion -eq $false -or [string]::IsNullOrEmpty($stampedVersion)) {
        Partial-Fail "TUI identity is not exactly one vX.Y.Z or dev token; runtime and TUI may have changed and no receipt was written"
    }

    $prefix = $BinDir
    if ((Split-Path -Path $BinDir -Leaf) -ceq 'bin') { $prefix = Split-Path -Path $BinDir -Parent }
    $tuiPhysical = (Get-Item -LiteralPath $TuiSource -Force).FullName
    $tuiCommit = (& git -C $TuiSource rev-parse HEAD 2>$null | Out-String).Trim()
    if (-not $tuiCommit) { Fail "cannot resolve TUI source commit" }
    $kernelCommit = (& git -C $KernelSource rev-parse HEAD 2>$null | Out-String).Trim()
    if (-not $kernelCommit) { Fail "cannot resolve kernel source commit" }

    # Receipt write -- the selected runtime performs atomic JSON serialization.
    $receiptWriter = @'
# LINGTAI_DEV_RECEIPT_WRITE
import datetime, json, os, stat, sys, tempfile
(path, prefix, bin_dir, stamped, venv, tui_source, kernel_source,
 tui_commit, kernel_commit, kernel_version, tui_binary, portal_binary) = sys.argv[1:]
receipt = {
    "schema": "lingtai.tui.install/v1",
    "schema_version": 1,
    "install_method": "powershell",
    "install_kind": "powershell-dev-source",
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
'@
    $writerCode = 0
    try {
        & $RuntimePython -c $receiptWriter $metadata $prefix $BinDir $stampedVersion $selectedVenv $tuiPhysical $sourcePhysical $tuiCommit $kernelCommit $observedKernelVersion $managedTui $managedPortal 2>&1 | Out-Null
        $writerCode = $LASTEXITCODE
    } catch { $writerCode = 1 }
    if ($writerCode -ne 0) {
        Partial-Fail "runtime and TUI/Portal may have changed; complete dev receipt was not written"
    }

    Write-Ok "editable development install from TUI $tuiCommit and kernel $kernelCommit ($observedKernelVersion)."
}
finally {
    if (Test-Path -LiteralPath $work) { Remove-Item -LiteralPath $work -Recurse -Force -ErrorAction SilentlyContinue }
}
