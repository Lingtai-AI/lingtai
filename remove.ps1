#requires -Version 5.1
<#
.SYNOPSIS
    LingTai native Windows (PowerShell) installation removal.

.DESCRIPTION
    The PowerShell counterpart to remove.sh. Deletes ONLY the exact
    installation the lingtai.tui.install/v1 receipt at
    %USERPROFILE%\.lingtai-tui\install.json proves -BinDir owns: the managed
    binaries (lingtai-tui.exe, lingtai-portal.exe), the receipt-pointed
    runtime venv, and finally the receipt itself. It never touches user
    config, secrets, presets, auth material, per-project state, or a
    Homebrew-managed installation (there is no Homebrew concept on native
    Windows, but a receipt pointing outside the owned roots is refused the
    same way install.sh refuses a Homebrew-shaped target). Deletes the
    receipt last so a partial failure never claims full removal. Running
    remove.ps1 twice is safe: the second run reports nothing to remove.

.PARAMETER BinDir
    Directory the managed binaries live in. Required.

.PARAMETER Yes
    Required. Removal is mutating; -Yes authorizes it after the plan is
    printed.

.EXAMPLE
    .\remove.ps1 -BinDir "$env:LOCALAPPDATA\Programs\lingtai\bin" -Yes

.NOTES
    Requires PowerShell 5.1 or later. Does not require administrator.
    Exit 0 => success (including "nothing to remove"). Non-zero => a
    fail-loud error, or a partial removal that left the receipt intact so
    the state is never silently claimed as fully removed.

    Exit-code asymmetry with remove.sh (intentional, not a defect): POSIX
    remove.sh distinguishes a usage error (missing --bin-dir, exit 2) from a
    runtime/ownership failure (exit 1) because it parses argv itself in a
    manual loop. remove.ps1 relies on PowerShell's own param() binding plus a
    single Fail helper for every refusal path, so a missing -BinDir and every
    other refusal both exit 1. This is the smallest truthful shape for this
    script's structure; splitting a distinct usage-exit-2 path would require
    parsing $args manually instead of using param(), which is not otherwise
    justified here.
#>
[CmdletBinding()]
param(
    [string]$BinDir,
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

function Test-AbsolutePath {
    param([string]$Path)
    if ([string]::IsNullOrWhiteSpace($Path)) { return $false }
    if (-not [System.IO.Path]::IsPathRooted($Path)) { return $false }
    if ($Path -match '[\r\n\t\x00]') { return $false }
    return $true
}

# Get-JsonProperty: Set-StrictMode -Version Latest throws when a PSCustomObject
# from ConvertFrom-Json is accessed for a property it does not have (a
# truncated/tampered receipt missing a required field). $null is a safe
# "absent" sentinel here because every property this script reads is either a
# non-empty string or an array; a genuinely-present-but-null JSON value would
# fail its own shape check the same way an absent one does.
function Get-JsonProperty {
    param($Data, [string]$Name)
    if ($Data.PSObject.Properties.Name -contains $Name) { return $Data.$Name }
    return $null
}

# Read-JsonNoDuplicateKeys: PowerShell's ConvertFrom-Json keeps the LAST value
# for a duplicate top-level key silently. install.json is a flat object (no
# nesting to track), so a single-depth key scan is sufficient here, unlike
# install.ps1's Confirm-BundleManifest which must track nested provider
# blocks.
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
    Fail "remove.ps1 supports native Windows only. On macOS/Linux/WSL, use remove.sh instead."
}

if (-not $BinDir) { Fail "-BinDir is required. Usage: remove.ps1 -BinDir DIR -Yes" }
if (-not (Test-AbsolutePath $BinDir)) { Fail "-BinDir is not an exact absolute path: $BinDir" }
if (-not $Yes) { Fail "removal is mutating; provide -Yes after reviewing the plan." }

$globalDir = Join-Path $env:USERPROFILE '.lingtai-tui'
$runtimeRoot = Join-Path $globalDir 'runtime'
$metadataPath = Join-Path $globalDir 'install.json'

function Invoke-Main {
    # Idempotent second run: no receipt at all means nothing owned to remove.
    if (-not (Test-Path -LiteralPath $metadataPath)) {
        Write-Host "PASS: nothing to remove; no install receipt at $metadataPath."
        return
    }

    $rawJson = Get-Content -LiteralPath $metadataPath -Raw
    $data = Read-JsonNoDuplicateKeys -RawJson $rawJson

    $schema = Get-JsonProperty $data 'schema'
    if ($schema -ne 'lingtai.tui.install/v1') { Fail "install metadata has an unexpected or missing schema." }
    $schemaVersion = Get-JsonProperty $data 'schema_version'
    if ($schemaVersion -ne 1) { Fail "install metadata has an unexpected or missing schema_version." }
    $receiptBinDir = Get-JsonProperty $data 'bin_dir'
    if ($receiptBinDir -ne $BinDir) {
        if ($receiptBinDir) {
            Fail "install metadata bin_dir does not match -BinDir; this receipt owns -BinDir $receiptBinDir -- re-run with: remove.ps1 -BinDir `"$receiptBinDir`" -Yes"
        }
        Fail "install metadata bin_dir does not match -BinDir; refusing to remove a target this receipt does not own."
    }
    $installKind = Get-JsonProperty $data 'install_kind'
    $validKinds = @('release-asset', 'source-build', 'dev-source', 'powershell-release-asset', 'powershell-local-artifact')
    if ($validKinds -notcontains $installKind) { Fail "install metadata install_kind is not recognized or missing." }

    $tuiTarget = Join-Path $BinDir 'lingtai-tui.exe'
    $portalTarget = Join-Path $BinDir 'lingtai-portal.exe'
    $managed = @(Get-JsonProperty $data 'managed_binaries')
    if ($managed -notcontains $tuiTarget) { Fail "install metadata managed_binaries does not own $tuiTarget." }
    $hasPortal = $managed -contains $portalTarget

    $venvPath = $null
    $hasVenv = $false
    if ($data.PSObject.Properties.Name -contains 'runtime_venv' -and $data.runtime_venv) {
        $venv = [string]$data.runtime_venv
        if (-not (Test-AbsolutePath $venv)) { Fail "install metadata runtime_venv is not an exact absolute path." }
        $venvParent = Split-Path -Path $venv -Parent
        if ($venvParent -ne $runtimeRoot) { Fail "install metadata runtime_venv is not a direct child of the owned runtime root." }
        $venvName = Split-Path -Path $venv -Leaf
        if ($venvName -notmatch '^[A-Za-z0-9._-]+$') { Fail "install metadata runtime_venv has an unsafe name." }
        $venvPath = $venv
        if (Test-Path -LiteralPath $venvPath) {
            $item = Get-Item -LiteralPath $venvPath -Force
            if ($item.LinkType) { Fail "install metadata runtime_venv exists but is a reparse point/symlink, not a real directory." }
            if (-not $item.PSIsContainer) { Fail "install metadata runtime_venv exists but is not a real directory." }
            $hasVenv = $true
        }
    }

    $ownedRootPhysical = $null
    if (Test-Path -LiteralPath $runtimeRoot) {
        $ownedRootPhysical = (Get-Item -LiteralPath $runtimeRoot -Force).FullName
    }

    # Plan (printed before any mutation).
    $planLines = New-Object System.Collections.Generic.List[string]
    $planLines.Add($tuiTarget)
    if ($hasPortal) { $planLines.Add($portalTarget) }
    if ($hasVenv) { $planLines.Add("$venvPath (runtime venv)") }
    $planLines.Add("$metadataPath (receipt, removed last)")
    Write-Host "Plan: the following owned artifacts will be removed:"
    foreach ($line in $planLines) { Write-Host "  - $line" }

    $removed = New-Object System.Collections.Generic.List[string]
    $failed = New-Object System.Collections.Generic.List[string]

    if ($hasPortal -and (Test-Path -LiteralPath $portalTarget)) {
        $item = Get-Item -LiteralPath $portalTarget -Force
        if (-not $item.LinkType -and -not $item.PSIsContainer) {
            try { Remove-Item -LiteralPath $portalTarget -Force; $removed.Add("lingtai-portal.exe") }
            catch { $failed.Add("lingtai-portal.exe (remove failed: $($_.Exception.Message))") }
        } else {
            $failed.Add("lingtai-portal.exe (not an owned regular file, left in place)")
        }
    }

    if (Test-Path -LiteralPath $tuiTarget) {
        $item = Get-Item -LiteralPath $tuiTarget -Force
        if (-not $item.LinkType -and -not $item.PSIsContainer) {
            try { Remove-Item -LiteralPath $tuiTarget -Force; $removed.Add("lingtai-tui.exe") }
            catch { $failed.Add("lingtai-tui.exe (remove failed: $($_.Exception.Message))") }
        } else {
            $failed.Add("lingtai-tui.exe (not an owned regular file, left in place)")
        }
    }

    if ($hasVenv) {
        # Re-validate physical containment at delete-time, not from the earlier
        # precondition read.
        if ((Test-Path -LiteralPath $venvPath) -and $ownedRootPhysical) {
            $item = Get-Item -LiteralPath $venvPath -Force
            if (-not $item.LinkType -and $item.PSIsContainer) {
                $venvParentPhysical = (Get-Item -LiteralPath (Split-Path -Path $venvPath -Parent) -Force).FullName
                if ($venvParentPhysical -eq $ownedRootPhysical) {
                    try { Remove-Item -LiteralPath $venvPath -Recurse -Force; $removed.Add("runtime venv") }
                    catch { $failed.Add("runtime venv (remove failed: $($_.Exception.Message))") }
                } else {
                    $failed.Add("runtime venv (failed physical containment re-check, left in place)")
                }
            } else {
                $failed.Add("runtime venv (no longer a real contained directory, left in place)")
            }
        } elseif (Test-Path -LiteralPath $venvPath) {
            $failed.Add("runtime venv (owned runtime root missing, left in place)")
        }
    }

    if ($failed.Count -gt 0) {
        Write-Host "PARTIAL: the following owned artifacts were removed:" -ForegroundColor Yellow
        foreach ($line in $removed) { Write-Host "  - $line" }
        $joined = ($failed -join '; ')
        Fail "the following owned artifacts could NOT be removed and the receipt was left in place so this state is not silently claimed as fully removed: $joined"
    }

    # Only after every above step succeeds is the receipt itself removed.
    try {
        Remove-Item -LiteralPath $metadataPath -Force
    } catch {
        Fail "every other owned artifact was removed, but the receipt at $metadataPath could not be deleted ($($_.Exception.Message)); re-run remove.ps1 to retry."
    }

    # Best-effort: remove the owned roots only if now empty. Never a
    # recursive removal of the state root -- it may still legitimately
    # contain NOT-owned files (tui_config.json, .env, presets\saved\,
    # kernel-provenance.json only if orphaned, etc).
    $survivors = @()
    if (Test-Path -LiteralPath $runtimeRoot) {
        if (@(Get-ChildItem -LiteralPath $runtimeRoot -Force -ErrorAction SilentlyContinue).Count -eq 0) {
            try { Remove-Item -LiteralPath $runtimeRoot -Force -ErrorAction Stop } catch { }
        }
    }
    if (Test-Path -LiteralPath $globalDir) {
        $entries = @(Get-ChildItem -LiteralPath $globalDir -Force -ErrorAction SilentlyContinue)
        if ($entries.Count -eq 0) {
            try { Remove-Item -LiteralPath $globalDir -Force -ErrorAction Stop } catch { }
        } else {
            $survivors = $entries | ForEach-Object { $_.Name }
        }
    }

    if ($survivors.Count -gt 0) {
        Write-Host "PASS: owned installation removed ($($removed.Count) artifact(s)); $globalDir left in place because it still contains: $($survivors -join ', ')"
    } else {
        Write-Host "PASS: owned installation removed ($($removed.Count) artifact(s)); no unowned state remained under $globalDir."
    }
}

try {
    Invoke-Main
    exit 0
} catch {
    if ($_.Exception -and $_.Exception.Message) {
        Write-Host "error: $($_.Exception.Message)" -ForegroundColor Red
    }
    exit 1
}
