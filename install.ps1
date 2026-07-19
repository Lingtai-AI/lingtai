#requires -Version 5.1
<#
.SYNOPSIS
    LingTai native Windows (PowerShell) installer -- EXPERIMENTAL.

.DESCRIPTION
    One-click installer for the LingTai TUI (and portal) on native Windows. It is
    the PowerShell counterpart to install.sh and parses/runs identically under
    Windows PowerShell 5.1 (Desktop) and PowerShell 7+ (Core).

    Two install sources are supported:

      * LOCAL ARTIFACT MODE (-ArchivePath + -ChecksumPath): install FROM an
        already-downloaded release archive plus its sha256 sidecar. No network.
        The archive is verified, expanded into an installer-owned staging
        directory, the staged lingtai-tui.exe is run to confirm it reports the
        requested -Version, and only THEN are the binaries copied into -BinDir.
        This is the seam the Windows contract suite exercises.

      * PUBLIC MODE (no -ArchivePath): resolve a release tag from GitHub and the
        Windows amd64 release asset for it. No such asset is published for the
        current source-only release workflow (see RELEASING.md), so this path
        fails loud with an actionable message rather than pretending an asset
        exists.

    Native Windows is EXPERIMENTAL. The TUI and portal render fine, but some
    agent capabilities run at reduced fidelity natively (daemon/subagents; the
    bash tool runs cmd.exe). For full parity use WSL2 and install.sh.

    The Python runtime venv (default, non -SkipVenv) is provisioned ONLY from a
    verified pinned kernel release bundle, exactly like install.sh -- LingTai is
    never installed from a package index by name. Because no such Windows release
    bundle is published today, the default runtime-provisioning path fails loud
    and directs you to -SkipVenv plus a manual runtime setup. -SkipVenv installs
    the binaries only and creates no venv.

.PARAMETER Version
    Release tag/version to install, e.g. v0.11.4. In local-artifact mode the
    staged lingtai-tui.exe MUST report exactly this version or the install aborts
    before touching BinDir. Defaults to $env:LINGTAI_VERSION.

.PARAMETER BinDir
    Directory the binaries install into. Defaults to a per-user, non-admin
    location: %LOCALAPPDATA%\Programs\lingtai\bin. Never requires administrator.

.PARAMETER GlobalDir
    Per-user global state directory (the ~/.lingtai-tui analogue). Defaults to
    %USERPROFILE%\.lingtai-tui.

.PARAMETER ArchivePath
    Local release archive (.zip) to install FROM. Enables local-artifact mode.
    Requires -ChecksumPath. No network is used in this mode.

.PARAMETER ChecksumPath
    sha256 sidecar for -ArchivePath (sha256sum-style "<hex>  <name>" or a bare
    hash). The archive's SHA-256 is verified case-insensitively against it.

.PARAMETER SkipVenv
    Skip Python runtime venv provisioning. No venv is created.

.PARAMETER NoModifyPath
    Do not persist PATH changes. Persistent user PATH is left untouched.

.PARAMETER DryRun
    Plan only: make no filesystem, PATH, or config writes. In local-artifact mode
    it may read and validate inputs (including the checksum) and print the plan,
    but it creates no staging/bin/global directories.

.EXAMPLE
    .\install.ps1 -ArchivePath .\lingtai-v0.11.4-windows-amd64.zip `
                  -ChecksumPath .\lingtai-v0.11.4-windows-amd64.zip.sha256 `
                  -Version v0.11.4 -SkipVenv

.NOTES
    Requires PowerShell 5.1 or later. Does not require administrator.
    Exit 0 => success. Non-zero => a fail-loud error. Validation and the known
    unsupported-runtime gate fail before BinDir writes; an unexpected OS-level
    copy/metadata failure may leave partial files and is reported honestly.
#>
[CmdletBinding()]
param(
    [string]$Version      = $env:LINGTAI_VERSION,
    [string]$BinDir       = $env:LINGTAI_BIN_DIR,
    [string]$GlobalDir    = $env:LINGTAI_GLOBAL_DIR,
    [string]$ArchivePath,
    [string]$ChecksumPath,
    [switch]$SkipVenv,
    [switch]$NoModifyPath,
    [switch]$DryRun
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# On Windows PowerShell 5.1 the per-request progress bar makes Invoke-WebRequest
# downloads dramatically slower and clutters piped output. Silence it.
$ProgressPreference = 'SilentlyContinue'

# --- Constants ---------------------------------------------------------------

$Repo    = 'Lingtai-AI/lingtai'
$RepoUrl = "https://github.com/$Repo"
$ApiBase = "https://api.github.com/repos/$Repo"

# --- Output helpers ----------------------------------------------------------

function Write-Info { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Warn { param([string]$Message) Write-Host "warn: $Message" -ForegroundColor Yellow }
function Write-Ok   { param([string]$Message) Write-Host "  ok: $Message" -ForegroundColor Green }
function Write-Step { param([string]$Message) Write-Host "  -> $Message" -ForegroundColor DarkGray }

# Fail loud: print an actionable message to the ERROR stream and throw so the
# outer catch turns it into a non-zero exit. Never swallow, never fake success.
function Fail {
    param([string]$Message)
    Write-Error $Message
    throw $Message
}

# --- Preconditions -----------------------------------------------------------

if ($PSVersionTable.PSVersion.Major -lt 5) {
    Fail "PowerShell 5.1 or later is required (found $($PSVersionTable.PSVersion)). Update Windows PowerShell or install PowerShell 7."
}

# OS guard: native Windows only. $IsWindows exists only on PowerShell 6+, so on
# Windows PowerShell 5.1 (where it is undefined) fall back to the platform enum
# and the OS env var, both reliably "Windows" there.
$onWindows = $false
if (Get-Variable -Name IsWindows -Scope Global -ErrorAction SilentlyContinue) {
    $onWindows = [bool]$IsWindows
} else {
    $onWindows = ($env:OS -eq 'Windows_NT') -or `
                 ([System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT)
}
if (-not $onWindows) {
    Fail @"
install.ps1 supports native Windows only.

On macOS or Linux, use the shell installer instead:
    curl -fsSL https://lingtai.ai/install.sh | bash
"@
}

# --- Path / arch helpers -----------------------------------------------------

function Get-DefaultBinDir {
    $base = $env:LOCALAPPDATA
    if ([string]::IsNullOrWhiteSpace($base)) {
        $base = Join-Path $env:USERPROFILE 'AppData\Local'
    }
    return Join-Path $base 'Programs\lingtai\bin'
}

function Get-DefaultGlobalDir {
    return Join-Path $env:USERPROFILE '.lingtai-tui'
}

function Get-Arch {
    # PROCESSOR_ARCHITECTURE reflects the host on 64-bit shells; the WOW64
    # variable covers a 32-bit host launching a 64-bit install.
    $raw = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $raw = $env:PROCESSOR_ARCHITEW6432 }
    switch ($raw) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        'x86'   { Fail "32-bit Windows (x86) is not supported. LingTai requires 64-bit Windows." }
        default { Fail "Unsupported processor architecture '$raw'. LingTai's Windows release artifact is amd64-only." }
    }
}

# --- Checksum handling -------------------------------------------------------

# Parse the first 64-hex SHA-256 digest from a sha256 sidecar. Handles the
# shasum/sha256sum format ("<hash>  <filename>") and a bare hash on its own line.
# Returns the lowercased digest, or $null if none found.
function Read-ExpectedSha256 {
    param([string]$Path)
    $text = Get-Content -LiteralPath $Path -Raw
    $m = [regex]::Match($text, '(?im)^\s*([0-9a-fA-F]{64})\b')
    if ($m.Success) { return $m.Groups[1].Value.ToLowerInvariant() }
    return $null
}

function Get-Sha256Hex {
    param([string]$Path)
    return (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

# Verify $ArchiveFile against the digest in $SidecarFile. Fails loud (installs
# nothing) on a missing sidecar, an unparseable digest, or a mismatch. The
# comparison is case-insensitive: both sides are lowercased.
function Confirm-ArchiveChecksum {
    param([string]$ArchiveFile, [string]$SidecarFile)
    if (-not (Test-Path -LiteralPath $SidecarFile)) {
        Fail "Checksum sidecar not found: $SidecarFile. Refusing to install unverified bytes."
    }
    $expected = Read-ExpectedSha256 -Path $SidecarFile
    if (-not $expected) {
        Fail "Could not parse a SHA-256 digest from $SidecarFile."
    }
    $actual = Get-Sha256Hex -Path $ArchiveFile
    if ($actual -ne $expected) {
        Fail "Checksum mismatch for $ArchiveFile. Expected $expected but got $actual. The archive may be corrupt or tampered with; not installing."
    }
    Write-Ok "Verified SHA-256 checksum for $(Split-Path -Leaf $ArchiveFile)"
}

# --- Staging (installer-owned, unique, never deleted) ------------------------

# Create a unique staging directory under TEMP for extraction. It is owned by
# this installer run and is intentionally left on disk for evidence/recovery --
# this installer never runs a cleanup/removal step.
function New-StagingDir {
    $tempBase = [System.IO.Path]::GetTempPath()
    $stage = Join-Path $tempBase ("lingtai-install-{0}" -f ([System.Guid]::NewGuid().ToString('N')))
    if (Test-Path -LiteralPath $stage) {
        # A GUID collision here signals leaked state; fail loud rather than reuse.
        Fail "Staging directory unexpectedly already exists: $stage"
    }
    New-Item -ItemType Directory -Force -Path $stage | Out-Null
    return $stage
}

# --- Version verification of the STAGED tui (before any BinDir write) ---------

# Run the staged lingtai-tui.exe and confirm its reported version contains the
# requested version. Fails loud on a run error or a mismatch. Called on the
# STAGED binary so a wrong-version archive never reaches BinDir.
function Confirm-StagedVersion {
    param([string]$StagedTui, [string]$Requested)

    # The staged fixture/binary accepts `version`, `--version`, or `-version`;
    # `version` matches install.sh's `lingtai-tui version` verification form.
    $probe = 'version'

    $out = $null
    $code = 0
    try {
        $out = & $StagedTui $probe 2>&1 | Out-String
        $code = $LASTEXITCODE
    } catch {
        Fail "Staged lingtai-tui.exe failed to run: $($_.Exception.Message)"
    }
    if ($code -ne 0) {
        Fail "Staged lingtai-tui.exe '$probe' exited $code. Output: $out"
    }

    if (-not [string]::IsNullOrWhiteSpace($Requested)) {
        # The real Go CLI and the Windows fixture both print exactly
        # "lingtai-tui <version>". Compare the complete trimmed line using ordinal
        # equality so v1.2.30 cannot satisfy a v1.2.3 request.
        $reported = $out.Trim()
        $expected = "lingtai-tui $Requested"
        if (-not [string]::Equals($reported, $expected, [System.StringComparison]::Ordinal)) {
            Fail "Version mismatch: expected '$expected' but the staged lingtai-tui reported: $reported"
        }
        Write-Ok "Staged lingtai-tui reports the requested version ($Requested)"
    } else {
        Write-Ok "Staged lingtai-tui runs ($($out.Trim()))"
    }
}

# --- PATH management ---------------------------------------------------------

# Add $Dir to the current process PATH and, unless -NoModifyPath, idempotently to
# the persistent user PATH (HKCU\Environment via [Environment], User scope).
# Never touches machine PATH; never requires admin.
function Add-ToPath {
    param([string]$Dir)

    # Process PATH first so the rest of this session sees the binaries.
    if (($env:PATH -split ';') -notcontains $Dir) {
        $env:PATH = "$Dir;$env:PATH"
    }

    if ($NoModifyPath) {
        Write-Step "Skipping persistent PATH update (-NoModifyPath). Add '$Dir' to PATH manually if needed."
        return
    }

    $userPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
    if ([string]::IsNullOrEmpty($userPath)) { $userPath = '' }
    $entries = $userPath -split ';' | Where-Object { $_ -ne '' }
    if ($entries -notcontains $Dir) {
        if ($userPath -eq '') { $newPath = $Dir } else { $newPath = "$userPath;$Dir" }
        [Environment]::SetEnvironmentVariable('PATH', $newPath, 'User')
        Write-Ok "Added '$Dir' to your user PATH (open a new terminal to pick it up everywhere)."
    } else {
        Write-Step "'$Dir' is already on the user PATH."
    }
}

# --- install.json metadata ---------------------------------------------------

# Mirror install.sh's install.json shape. install_method is deliberately
# "powershell" (not "source"): the TUI's source updater treats
# install_method="source" as permission to run install.sh through bash, a
# POSIX-only path that does not exist natively on Windows. There is no verified
# kernel bundle on this path, so no kernel_source block is written -- matching
# install.sh, which omits it entirely rather than writing empty strings.
function Write-InstallMetadata {
    param(
        [string]$GlobalDir,
        [string]$Prefix,
        [string]$BinDir,
        [string]$RequestedRef,
        [string]$ResolvedRef,
        [string[]]$ManagedBinaries
    )
    $stamped = $ResolvedRef -replace '^v', ''
    $meta = [ordered]@{
        schema           = 'lingtai.tui.install/v1'
        schema_version   = 1
        install_method   = 'powershell'
        install_kind     = 'powershell-local-artifact'
        self_update      = $false
        upgrade_command  = 're-run install.ps1 with a newer -ArchivePath/-Version'
        prefix           = $Prefix
        bin_dir          = $BinDir
        repo_url         = $RepoUrl
        requested_ref    = $RequestedRef
        resolved_ref     = $ResolvedRef
        resolved_commit  = ''
        stamped_version  = $stamped
        installed_at     = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
        managed_binaries = @($ManagedBinaries)
    }
    $metaPath = Join-Path $GlobalDir 'install.json'
    New-Item -ItemType Directory -Force -Path $GlobalDir | Out-Null
    ($meta | ConvertTo-Json -Depth 5) | Set-Content -LiteralPath $metaPath -Encoding UTF8
    Write-Ok "Wrote install metadata -> $metaPath"
}

# --- Runtime venv (fail-loud; no PyPI-by-name; mirrors install.sh) -----------

# The default (non -SkipVenv) runtime path installs LingTai's Python runtime ONLY
# from a verified pinned kernel release bundle, never from a package index by
# name (see RELEASING.md / ANATOMY.md install.sh entry). No such Windows release
# bundle is published today, so this fails loud with an actionable message rather
# than silently running `pip install lingtai`.
function Install-Venv {
    Fail @"
Cannot provision the Python runtime venv on native Windows yet.

LingTai's Python runtime is installed only from a verified pinned kernel release
bundle (an exact kernel wheel/sdist matched to the venv interpreter, verified by
SHA-256 and installed by explicit local file path) -- never from PyPI or any
package index by the name 'lingtai'. The current source-only release workflow
publishes no Windows release bundle, so there is nothing to pin against here.

This is a hard stop, not a silent fallback. Options:
  - Pass -SkipVenv to install the TUI/portal binaries only, then provision the
    Python runtime yourself (for example an editable install against a local
    lingtai-kernel checkout; see RELEASING.md / CLAUDE.md "Agent venv").
  - Use WSL2 + install.sh for a full-parity install with the pinned runtime.
"@
}

# --- Local-artifact install --------------------------------------------------

# Install FROM a local archive + checksum sidecar. Order is deliberate: verify
# the checksum, expand into installer-owned staging, require lingtai-tui.exe,
# confirm the staged tui reports the requested version -- and only THEN copy into
# BinDir. Any failure before the copy leaves BinDir untouched (nothing installed).
function Install-FromLocalArtifact {
    param(
        [string]$Archive,
        [string]$Sidecar,
        [string]$BinDir,
        [string]$Requested
    )

    if (-not (Test-Path -LiteralPath $Archive)) {
        Fail "Archive not found: $Archive"
    }

    Write-Info "Installing from local artifact: $(Split-Path -Leaf $Archive)"

    # 1. Verify checksum (case-insensitive). Readable in DryRun too.
    Confirm-ArchiveChecksum -ArchiveFile $Archive -SidecarFile $Sidecar

    if ($DryRun) {
        # DryRun performs ZERO filesystem writes: no staging, no extraction, no
        # BinDir/GlobalDir creation. Validate what is readable and print the plan.
        Write-Warn "DRY RUN: checksum verified; no staging, extraction, or install will occur."
        Write-Step "[dry-run] would expand the archive into an installer-owned staging dir under TEMP"
        Write-Step "[dry-run] would require lingtai-tui.exe and verify it reports version '$Requested'"
        Write-Step "[dry-run] would install lingtai-tui.exe (and optional lingtai-portal.exe) into $BinDir"
        return @()
    }

    # 2. Expand into a unique, installer-owned staging directory under TEMP.
    $stage = New-StagingDir
    Write-Step "Staging under $stage"
    $extract = Join-Path $stage 'extract'
    New-Item -ItemType Directory -Force -Path $extract | Out-Null
    try {
        Expand-Archive -LiteralPath $Archive -DestinationPath $extract -Force
    } catch {
        Fail "Failed to expand $Archive : $($_.Exception.Message). Staging kept for inspection: $stage"
    }

    # 3. Require lingtai-tui.exe (fail loud if absent, even with a valid checksum).
    $tui = Get-ChildItem -LiteralPath $extract -Recurse -Filter 'lingtai-tui.exe' -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if (-not $tui) {
        Fail "Archive does not contain lingtai-tui.exe. Staging kept for inspection: $stage"
    }

    # 4. Verify the STAGED tui reports the requested version BEFORE any BinDir write.
    Confirm-StagedVersion -StagedTui $tui.FullName -Requested $Requested

    # Optional portal.
    $portal = Get-ChildItem -LiteralPath $extract -Recurse -Filter 'lingtai-portal.exe' -ErrorAction SilentlyContinue |
        Select-Object -First 1

    # 5. Install idempotently into BinDir (only reached once staging validated).
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    $tuiDest = Join-Path $BinDir 'lingtai-tui.exe'
    Copy-Item -LiteralPath $tui.FullName -Destination $tuiDest -Force
    Write-Ok "Installed lingtai-tui.exe -> $BinDir"

    $managed = New-Object System.Collections.Generic.List[string]
    $managed.Add($tuiDest)
    if ($portal) {
        $portalDest = Join-Path $BinDir 'lingtai-portal.exe'
        Copy-Item -LiteralPath $portal.FullName -Destination $portalDest -Force
        Write-Ok "Installed lingtai-portal.exe -> $BinDir"
        $managed.Add($portalDest)
    } else {
        Write-Step "Archive has no lingtai-portal.exe; installing the TUI only."
    }

    return $managed.ToArray()
}

# --- Public (no -ArchivePath) install ----------------------------------------

# Resolve a tag and the Windows amd64 release asset. The source-only release
# workflow publishes no such asset today, so this fails loud rather than
# pretending one exists. Architecture is explicit: the release artifact is
# amd64-only; ARM64 is not claimed as supported.
function Install-FromPublicRelease {
    param([string]$BinDir, [string]$Requested)

    $arch = Get-Arch
    if ($arch -ne 'amd64') {
        Fail "The LingTai Windows release artifact is amd64-only; '$arch' is not supported natively yet. Use WSL2 + install.sh."
    }

    $tag = $Requested
    if ([string]::IsNullOrWhiteSpace($tag)) { $tag = '<latest>' }
    $assetName = "lingtai-$tag-windows-amd64.zip"

    Fail @"
No Windows release asset is available to download.

Public no-ArchivePath install would fetch '$assetName' (and its .sha256) from
$RepoUrl, but the current source-only release workflow publishes no Windows
amd64 release asset (see RELEASING.md). There is nothing to download, so this is
a hard stop rather than a silent degrade.

Options:
  - Install from a local artifact you already have:
      .\install.ps1 -ArchivePath <path-to>.zip -ChecksumPath <path-to>.zip.sha256 -Version $tag -SkipVenv
  - Use WSL2 + install.sh for a full-parity install:
      wsl --install
      curl -fsSL https://lingtai.ai/install.sh | bash
"@
}

# --- Main --------------------------------------------------------------------

function Invoke-Main {
    Write-Host ""
    Write-Host "LingTai -- native Windows installer (EXPERIMENTAL)" -ForegroundColor Magenta
    Write-Host "--------------------------------------------------" -ForegroundColor Magenta
    if ($DryRun) { Write-Warn "DRY RUN: no filesystem, PATH, or config writes will be made." }

    # Resolve per-user, non-admin defaults.
    if ([string]::IsNullOrWhiteSpace($BinDir))    { $BinDir    = Get-DefaultBinDir }
    if ([string]::IsNullOrWhiteSpace($GlobalDir)) { $GlobalDir = Get-DefaultGlobalDir }

    # prefix is the parent of BinDir, matching install.sh's <prefix>/bin layout.
    $prefix = Split-Path $BinDir -Parent
    if ([string]::IsNullOrWhiteSpace($prefix)) { $prefix = $BinDir }

    # Mode selection: local artifact requires BOTH ArchivePath and ChecksumPath.
    $haveArchive  = -not [string]::IsNullOrWhiteSpace($ArchivePath)
    $haveChecksum = -not [string]::IsNullOrWhiteSpace($ChecksumPath)
    if ($haveArchive -ne $haveChecksum) {
        Fail "-ArchivePath and -ChecksumPath must be provided together (local-artifact mode requires the sha256 sidecar)."
    }
    if ($haveArchive -and [string]::IsNullOrWhiteSpace($Version)) {
        Fail "-Version is required with -ArchivePath so staged bytes can be verified against an exact release."
    }

    Write-Info "Target BinDir: $BinDir"
    Write-Info "Target GlobalDir: $GlobalDir"

    # 1. Runtime capability gate. The current native-Windows runtime path is a
    # known hard stop, so reject it BEFORE binaries, PATH, or metadata can change.
    # -SkipVenv is the explicit TUI-only opt-out; DryRun remains write-free.
    if (-not $SkipVenv -and -not $DryRun) {
        Install-Venv   # never returns until a verified Windows bundle exists
    }

    # 2. Install binaries.
    if ($haveArchive) {
        $managed = Install-FromLocalArtifact -Archive $ArchivePath -Sidecar $ChecksumPath -BinDir $BinDir -Requested $Version
    } else {
        # Fails loud (no public Windows asset today). Never returns.
        $managed = Install-FromPublicRelease -BinDir $BinDir -Requested $Version
    }

    # 3. PATH. Skipped entirely in DryRun (no persistent writes).
    if ($DryRun) {
        Write-Step "[dry-run] would add '$BinDir' to the process and (unless -NoModifyPath) persistent user PATH"
    } else {
        Add-ToPath -Dir $BinDir
    }

    # 4. Runtime disposition. Non-skip/non-DryRun was rejected before writes.
    if ($SkipVenv) {
        Write-Warn "Skipping runtime venv (-SkipVenv). Provision the Python runtime yourself; the TUI/portal binaries are installed."
    } elseif ($DryRun) {
        Write-Step "[dry-run] would attempt runtime venv provisioning (which currently fails loud: no Windows kernel bundle)"
    }

    # 5. Metadata. Skipped in DryRun (no writes).
    if ($DryRun) {
        Write-Step "[dry-run] would write install metadata under $GlobalDir"
    } else {
        Write-InstallMetadata -GlobalDir $GlobalDir -Prefix $prefix -BinDir $BinDir `
            -RequestedRef $Version -ResolvedRef $Version -ManagedBinaries $managed
    }

    # 6. Summary.
    Write-Host ""
    if ($DryRun) {
        Write-Host "Dry run complete. Nothing was installed." -ForegroundColor Green
    } else {
        Write-Host "LingTai binaries installed." -ForegroundColor Green
        Write-Host ""
        Write-Host "EXPERIMENTAL: native Windows support runs some agent capabilities at" -ForegroundColor Yellow
        Write-Host "reduced fidelity. For full parity, use WSL2 + install.sh." -ForegroundColor Yellow
        if ($NoModifyPath) {
            Write-Host ""
            Write-Warn "BinDir was not added to persistent PATH (-NoModifyPath). Add '$BinDir' to PATH or run by full path."
        } else {
            Write-Host ""
            Write-Step "If 'lingtai-tui' is not found, open a new terminal so the updated PATH is picked up."
        }
    }
}

try {
    Invoke-Main
    exit 0
} catch {
    # The specific fail-loud message was already emitted to the error stream by
    # Fail (or by an unexpected exception). Ensure a non-zero exit; do NOT run any
    # cleanup/removal -- staging is left on disk for evidence/recovery.
    if ($_.Exception -and $_.Exception.Message) {
        Write-Host "error: $($_.Exception.Message)" -ForegroundColor Red
    }
    exit 1
}
