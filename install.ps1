#requires -Version 5.1
<#
.SYNOPSIS
    LingTai native Windows (PowerShell) installer.

.DESCRIPTION
    One-click installer for the LingTai TUI (and portal) on native Windows. It is
    the PowerShell counterpart to install.sh and parses/runs identically under
    Windows PowerShell 5.1 (Desktop) and PowerShell 7+ (Core).

    Three install sources are supported:

      * PUBLIC MODE (no -ArchivePath, the default): resolve one exact vX.Y.Z TUI
        release tag from GitHub (an explicit -Version, or the latest release
        resolved once), download and strictly validate that release's
        lingtai-bundle-manifest.json (schema lingtai.tui.bundle/v1), download the
        lingtai-<tag>-windows-amd64.zip archive plus its .sha256 sidecar, verify
        the archive's SHA-256 against the manifest before extraction, and confirm
        the staged lingtai-tui.exe and lingtai-portal.exe are present, with the
        TUI reporting exactly that tag, before touching BinDir.

      * LOCAL ARTIFACT MODE (-ArchivePath + -ChecksumPath): install the TUI/portal
        binaries FROM an already-downloaded release archive plus its sha256
        sidecar, with no network use for the binary install itself. The archive is
        verified, expanded into an installer-owned staging directory, the staged
        lingtai-tui.exe is run to confirm it reports the requested -Version, and
        only THEN are the binaries copied into -BinDir. This is the seam the
        Windows contract suite exercises. The default (non -SkipVenv) runtime step
        still resolves the pinned bundle for -Version over the network exactly as
        in public mode, since the kernel pin is not shipped inside the archive.

      * CURRENT-MAIN DEV MODE (-Latest): resolve and pin refs/heads/main to full
        commits in both Lingtai-AI/lingtai and Lingtai-AI/lingtai-kernel before
        checkout, build both native Windows binaries from the pinned TUI tree,
        and install the pinned kernel checkout by local path into the runtime venv.

    Both release modes provision the Python runtime venv (default, non -SkipVenv) ONLY
    from the resolved release's pinned kernel bundle: the bundle manifest's
    kernel_tag/kernel_manifest_filename select the lingtai-kernel release
    manifest, a wheel matching the venv's actual CPython 3.11/3.12/3.13 win_amd64
    interpreter is selected and SHA-256 verified, and only that local wheel path
    is installed -- LingTai is never installed from a package index by name and
    the kernel tag is never resolved as "latest" or changed from the pin.
    -SkipVenv is the explicit binary-only mode that skips all of this and creates
    no venv; it still requires and installs both the TUI and portal. -DryRun
    performs the same resolution/validation reads but writes nothing.

.PARAMETER Version
    Release tag/version to install, e.g. v0.11.4. In local-artifact mode the
    staged lingtai-tui.exe MUST report exactly this version or the install aborts
    before touching BinDir. Defaults to $env:LINGTAI_VERSION.

.PARAMETER BinDir
    Directory the binaries install into. Defaults to a per-user, non-admin
    location: %LOCALAPPDATA%\Programs\lingtai\bin. Never requires administrator.

.PARAMETER Latest
    Explicit current-main development mode. Pins and checks out main in both
    repositories, builds lingtai-tui.exe and the required lingtai-portal.exe,
    and installs the checked-out kernel source as a non-editable local build
    into the runtime venv.

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
    # Public mode: resolve the latest release and install the TUI/portal plus
    # the pinned kernel runtime.
    irm https://lingtai.ai/install.ps1 | iex

.EXAMPLE
    # Public mode, exact version, TUI/portal binaries only.
    &([scriptblock]::Create((irm https://lingtai.ai/install.ps1))) -Version v0.11.4 -SkipVenv

.EXAMPLE
    .\install.ps1 -ArchivePath .\lingtai-v0.11.4-windows-amd64.zip `
                  -ChecksumPath .\lingtai-v0.11.4-windows-amd64.zip.sha256 `
                  -Version v0.11.4 -SkipVenv

.EXAMPLE
    .\install.ps1 -Latest -BinDir "$env:LOCALAPPDATA\Programs\lingtai\bin"

.NOTES
    Requires PowerShell 5.1 or later. Does not require administrator.
    Exit 0 => success. Non-zero => a fail-loud error. Validation and the
    runtime-provisioning gate fail before BinDir writes; an unexpected
    OS-level copy/metadata failure may leave partial files and is reported
    honestly.
#>
[CmdletBinding()]
param(
    [string]$Version      = $env:LINGTAI_VERSION,
    [string]$BinDir       = $env:LINGTAI_BIN_DIR,
    [string]$GlobalDir    = $env:LINGTAI_GLOBAL_DIR,
    [string]$ArchivePath,
    [string]$ChecksumPath,
    [switch]$Latest,
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
# Overridable only for the offline contract suite (env vars, same pattern as
# install.sh's LINGTAI_GITEE_OWNER/LINGTAI_GITEE_REPO); production installs
# always use the real GitHub API.
$ApiBase = if ($env:LINGTAI_GITHUB_API_BASE) { $env:LINGTAI_GITHUB_API_BASE } else { "https://api.github.com/repos/$Repo" }
$KernelApiBase = if ($env:LINGTAI_KERNEL_GITHUB_API_BASE) { $env:LINGTAI_KERNEL_GITHUB_API_BASE } else { "https://api.github.com/repos/Lingtai-AI/lingtai-kernel" }

# --- Output helpers ----------------------------------------------------------

function Write-Info { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Warn { param([string]$Message) Write-Host "warn: $Message" -ForegroundColor Yellow }
function Write-Ok   { param([string]$Message) Write-Host "  ok: $Message" -ForegroundColor Green }
function Write-Step { param([string]$Message) Write-Host "  -> $Message" -ForegroundColor DarkGray }

# Write-Phase prints a numbered progress banner ("[2/6] ...") so a user
# watching a transient console always knows which stage the installer is in
# and roughly how much remains -- installs that stall on a network step are
# otherwise indistinguishable from a hung or crashed window.
$script:PhaseCount = 0
function Write-Phase {
    param([string]$Name)
    $script:PhaseCount++
    Write-Host ""
    Write-Host "[$($script:PhaseCount)/$TotalPhases] $Name" -ForegroundColor Cyan
}
$TotalPhases = 7

# --- Progress reporting -------------------------------------------------------

# Long phases (npm ci, two Go builds, a pip install) previously ran with their
# output sent to Out-Null and no heading, so a -Latest install showed nothing at
# all for minutes and looked hung. These helpers give every long phase a heading
# printed BEFORE the work starts, plus an elapsed time when it finishes, so the
# terminal always says what is currently running and how long it took.

$script:InstallClock = [System.Diagnostics.Stopwatch]::StartNew()
$script:PhaseNumber  = 0
$script:PhaseTotal   = 0

function Set-PhaseTotal {
    param([int]$Total)
    $script:PhaseTotal  = $Total
    $script:PhaseNumber = 0
}

# Human-readable elapsed time: "48s" / "3m 07s". Seconds-resolution on purpose --
# these phases run for minutes and millisecond noise would only add width.
function Format-Duration {
    param([TimeSpan]$Span)
    if ($Span.TotalMinutes -ge 1) {
        return ('{0}m {1:00}s' -f [int]$Span.TotalMinutes, $Span.Seconds)
    }
    return ('{0}s' -f [int][Math]::Max(1, [Math]::Round($Span.TotalSeconds)))
}

# Announce a phase BEFORE it runs (that ordering is the whole point) and return
# the stopwatch the caller closes with Complete-Phase.
function Start-Phase {
    param([string]$Message)
    $script:PhaseNumber++
    $label = if ($script:PhaseTotal -gt 0) { "[$($script:PhaseNumber)/$($script:PhaseTotal)]" } else { '==>' }
    Write-Host "$label $Message" -ForegroundColor Cyan
    return [System.Diagnostics.Stopwatch]::StartNew()
}

function Complete-Phase {
    param([System.Diagnostics.Stopwatch]$Clock, [string]$Message)
    $Clock.Stop()
    Write-Host ("  ok: {0} ({1})" -f $Message, (Format-Duration $Clock.Elapsed)) -ForegroundColor Green
}

# Write-Completion closes a successful install with what was installed, where it
# went, and what to run next. The previous ending was a single green line naming
# two 40-character SHAs, which said nothing about how to actually start LingTai
# or where its state lives.
function Write-Completion {
    param(
        [string]$BinDir,
        [string]$GlobalDir,
        [string]$Headline,
        [System.Collections.Specialized.OrderedDictionary]$Facts
    )
    $rule = '-' * 60
    Write-Host ''
    Write-Host $rule -ForegroundColor Green
    Write-Host "  LingTai: $Headline" -ForegroundColor Green
    Write-Host $rule -ForegroundColor Green
    Write-Host ''
    # Each line is emitted as ONE Write-Host. A `-NoNewline` label followed by a
    # second Write-Host renders correctly on a live console but splits across two
    # lines as soon as the stream is redirected -- piping the install to a log, or
    # a CI job capturing it, turned every label/value pair into two lines.
    if ($Facts -and $Facts.Count -gt 0) {
        $width = 0
        foreach ($key in $Facts.Keys) { if ($key.Length -gt $width) { $width = $key.Length } }
        foreach ($key in $Facts.Keys) {
            Write-Host ("  {0}  {1}" -f $key.PadRight($width), $Facts[$key])
        }
        Write-Host ''
    }
    Write-Host '  Locations' -ForegroundColor Cyan
    Write-Host "    binaries   $BinDir"
    Write-Host "    state      $GlobalDir"
    Write-Host "    runtime    $(Join-Path $GlobalDir 'runtime\venv')"
    Write-Host ''
    Write-Host '  Commands' -ForegroundColor Cyan
    Write-Host '    lingtai-tui       start the terminal UI' -ForegroundColor Green
    Write-Host '    lingtai-portal    start the web portal' -ForegroundColor Green
    Write-Host ''
    Write-Host ('  Total time: {0}' -f (Format-Duration $script:InstallClock.Elapsed)) -ForegroundColor DarkGray
    if (-not $NoModifyPath) {
        Write-Host '  Open a new terminal so the updated PATH is picked up everywhere.' -ForegroundColor Yellow
    }
    Write-Host ''
}

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
# POSIX-only path that does not exist natively on Windows. kernel_* fields are
# written only on a verified bundle-provisioned venv install (mirrors
# install.sh's install_kernel_from_bundle contract) and omitted (not written
# as empty strings) on -SkipVenv installs.
function Write-InstallMetadata {
    param(
        [string]$GlobalDir,
        [string]$Prefix,
        [string]$BinDir,
        [string]$RequestedRef,
        [string]$ResolvedRef,
        [string]$ResolvedCommit = '',
        [string]$InstallKind = 'powershell-local-artifact',
        [string[]]$ManagedBinaries,
        [string]$KernelSource = '',
        [string]$KernelBundleId = '',
        [string]$KernelVersion = '',
        [string]$KernelProvider = '',
        [string]$SourceMode = '',
        [string]$TuiCommit = '',
        [string]$KernelCommit = ''
    )
    $stamped = $ResolvedRef -replace '^v', ''
    $upgradeCommand = 're-run install.ps1 with a newer -ArchivePath/-Version'
    if ($SourceMode -eq 'latest-main') {
        if ($TuiCommit -notmatch '^[0-9a-fA-F]{40}$') { Fail "Current-main install metadata requires a full TUI commit SHA." }
        $stamped = "main-$($TuiCommit.ToLowerInvariant())"
        $upgradeCommand = 're-run install.ps1 -Latest'
    }
    $meta = [ordered]@{
        schema           = 'lingtai.tui.install/v1'
        schema_version   = 1
        install_method   = 'powershell'
        install_kind     = $InstallKind
        self_update      = $false
        upgrade_command  = $upgradeCommand
        prefix           = $Prefix
        bin_dir          = $BinDir
        repo_url         = $RepoUrl
        requested_ref    = $RequestedRef
        resolved_ref     = $ResolvedRef
        resolved_commit  = $ResolvedCommit
        stamped_version  = $stamped
        installed_at     = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
        managed_binaries = @($ManagedBinaries)
    }
    if ($KernelSource) {
        $meta['kernel_source']       = $KernelSource
        $meta['kernel_version']      = $KernelVersion
        $meta['kernel_provider']     = $KernelProvider
        if ($KernelBundleId) { $meta['kernel_bundle_id'] = $KernelBundleId }
    }
    if ($SourceMode) {
        $meta['source_mode']  = $SourceMode
        $meta['tui_commit']   = $TuiCommit
        $meta['kernel_commit'] = $KernelCommit
    }
    $metaPath = Join-Path $GlobalDir 'install.json'
    New-Item -ItemType Directory -Force -Path $GlobalDir | Out-Null
    $json = $meta | ConvertTo-Json -Depth 5
    # Windows PowerShell 5.1's Set-Content -Encoding UTF8 emits a BOM, while
    # Go's encoding/json rejects BOM-prefixed input. Use one explicit BOM-less
    # encoding on both Desktop 5.1 and PowerShell Core.
    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($metaPath, $json, $utf8NoBom)
    Write-Ok "Wrote install metadata -> $metaPath"
}

# --- GitHub API helpers -------------------------------------------------------

# Invoke-GitHubApi performs a plain unauthenticated GET against the GitHub API
# and returns the parsed JSON body. Fails loud (never returns $null) so every
# caller can assume a valid object on success.
function Invoke-GitHubApi {
    param([string]$Url)
    try {
        return Invoke-RestMethod -Uri $Url -Headers @{ 'User-Agent' = 'lingtai-install-ps1' } -UseBasicParsing
    } catch {
        Fail "GitHub API request failed: $Url ($($_.Exception.Message))"
    }
}

# Get-TextAssetContent downloads $Url and returns its body as decoded UTF-8
# text, for callers that need the raw text of a downloaded release asset
# (the bundle/kernel manifests) rather than an already-parsed object.
#
# Invoke-WebRequest's .Content property is NOT safe to read directly across
# PowerShell hosts here: on Windows PowerShell 5.1 (Desktop), -UseBasicParsing
# only decodes .Content to a [string] when the response's Content-Type is
# recognized as text (text/*, application/json, ...); for anything else
# (including the text/html HttpListener falls back to, or the
# application/octet-stream a real CDN/release-asset host commonly serves) it
# instead surfaces as a [byte[]] -- which PowerShell's default array-to-string
# coercion turns into a space-separated list of decimal byte values, not
# decoded text, and ConvertFrom-Json then fails on that decimal-number
# garbage. PowerShell 7 Core's .Content is always a string, but can still
# carry a leading UTF-8 BOM (U+FEFF) that ConvertFrom-Json chokes on as
# trailing "additional text" after the first token.
#
# RawContentStream is present on both editions' response object and is
# ALWAYS the untouched raw byte stream regardless of Content-Type
# classification, so decoding it explicitly as UTF-8 (and stripping an
# optional BOM) is the one code path that behaves identically on PS 5.1 and
# PS7 no matter what Content-Type the server did or didn't declare.
function Get-TextAssetContent {
    param([string]$Url)
    $response = $null
    try {
        $response = Invoke-WebRequest -Uri $Url -UseBasicParsing
    } catch {
        Fail "Download failed: $Url ($($_.Exception.Message))"
    }
    $bytes = $response.RawContentStream.ToArray()
    if (-not $bytes -or $bytes.Length -eq 0) {
        Fail "Empty response body from $Url."
    }
    $text = [System.Text.Encoding]::UTF8.GetString($bytes).TrimStart([char]0xFEFF)
    if ([string]::IsNullOrWhiteSpace($text)) {
        Fail "Response from $Url decoded to empty/unsupported text content."
    }
    return $text
}

# Resolve-PublicTag resolves $Requested to an exact vX.Y.Z tag: validated as-is
# if given, or the repo's latest release tag if empty. "latest" is resolved
# through the release API exactly once -- never re-queried on a fallback.
function Resolve-PublicTag {
    param([string]$Requested)
    if (-not [string]::IsNullOrWhiteSpace($Requested)) {
        if ($Requested -notmatch '^v\d+\.\d+\.\d+$') {
            Fail "-Version '$Requested' is not an exact vX.Y.Z release tag."
        }
        return $Requested
    }
    $release = Invoke-GitHubApi -Url "$ApiBase/releases/latest"
    $tag = $release.tag_name
    if ([string]::IsNullOrWhiteSpace($tag) -or $tag -notmatch '^v\d+\.\d+\.\d+$') {
        Fail "Could not resolve an exact vX.Y.Z tag from the latest GitHub release (got '$tag')."
    }
    return $tag
}

# Get-ReleaseAssetUrl returns the browser_download_url for a named asset on an
# exact tag's release, or $null if that release has no such asset. Uses the
# release-by-tag listing so a missing asset is detected before any download.
function Get-ReleaseAssetUrl {
    param([string]$Tag, [string]$Name)
    $release = Invoke-GitHubApi -Url "$ApiBase/releases/tags/$Tag"
    $asset = $release.assets | Where-Object { $_.name -eq $Name } | Select-Object -First 1
    if (-not $asset) { return $null }
    return $asset.browser_download_url
}

# --- Bundle manifest (schema lingtai.tui.bundle/v1) --------------------------

# Confirm-BundleManifest performs the same strict validation as install.sh's
# parse_bundle_manifest: exact object shape, no duplicate JSON keys (detected
# via .psobject.Properties on the raw parse, since ConvertFrom-Json silently
# keeps the LAST value for a duplicate key rather than erroring), a matching
# lingtai-<tag>-windows-amd64.zip archive entry, and well-formed provider
# blocks. Returns a hashtable with the fields callers need (ArchiveSha256,
# KernelTag, KernelVersion, KernelManifestFilename, BundleId) on success;
# fails loud on any shape/content violation.
function Confirm-BundleManifest {
    param([string]$RawJson, [string]$ExpectedTag)

    # Duplicate key detection, scoped per JSON object: PowerShell's JSON
    # parser keeps the LAST value for a duplicate key silently, so scan the
    # raw text before trusting the parsed object. Scoping matters -- this
    # manifest's own schema has "repo" under BOTH providers.github and
    # providers.gitee, which is legitimate; only a duplicate key WITHIN the
    # same object (e.g. two top-level "schema" fields, or "repo" appearing
    # twice inside one provider block) is a real violation. A depth-tracking
    # scan (each "{" pushes a fresh key set, each "}" pops it) mirrors
    # install.sh's per-object object_pairs_hook check without a full parser.
    #
    # This same scan also captures generated_at's ORIGINAL source token, but
    # ONLY the occurrence at the manifest's own top-level object depth. The
    # stack is pre-loaded with one HashSet before scanning starts, so the
    # top-level object's own opening brace pushes a second frame -- its keys
    # are seen at $seenStack.Count -eq 2, not 1 (Count is 1 only before that
    # opening brace is reached, and again after its matching closing brace
    # pops back, never while its own keys are being scanned; traced against
    # the actual push/pop sequence, not assumed). An unanchored whole-document
    # regex would accept the FIRST textual "generated_at" match anywhere,
    # including one nested inside another object earlier in the text;
    # anchoring the value match to \G at the exact offset right after the
    # top-level key's own match (via the INSTANCE Regex.Match(input, startAt)
    # overload on a compiled [regex] object, with a \G-leading pattern, which
    # only matches starting AT that index, never later in the string)
    # guarantees the captured token is the top-level key's own value, not any
    # other occurrence, regardless of JSON field order. This must be the
    # instance overload, not the static [regex]::Match($s, $pattern, $arg)
    # overload -- that 3-arg STATIC overload's third parameter is
    # RegexOptions, not a start offset, and silently rejects an integer index
    # as an invalid RegexOptions value on both PS 5.1 and PS7 (reproduced
    # from a live CI failure on both hosts).
    $generatedAtValueRegex = [regex]'\G\s*"([^"\\]*)"'
    $keyOrBraceMatches = [regex]::Matches($RawJson, '[{}]|"([A-Za-z_]+)"\s*:')
    $seenStack = New-Object 'System.Collections.Generic.Stack[System.Collections.Generic.HashSet[string]]'
    $seenStack.Push((New-Object 'System.Collections.Generic.HashSet[string]'))
    $generatedAtToken = $null
    foreach ($m in $keyOrBraceMatches) {
        if ($m.Value -eq '{') {
            $seenStack.Push((New-Object 'System.Collections.Generic.HashSet[string]'))
        } elseif ($m.Value -eq '}') {
            if ($seenStack.Count -gt 1) { $seenStack.Pop() | Out-Null }
        } elseif ($m.Groups[1].Success) {
            if (-not $seenStack.Peek().Add($m.Groups[1].Value)) {
                Fail "invalid strict bundle manifest: duplicate JSON key: $($m.Groups[1].Value)"
            }
            if ($seenStack.Count -eq 2 -and $m.Groups[1].Value -eq 'generated_at') {
                $valueMatch = $generatedAtValueRegex.Match($RawJson, $m.Index + $m.Length)
                $generatedAtToken = if ($valueMatch.Success) { $valueMatch.Groups[1].Value } else { $null }
            }
        }
    }

    try {
        $data = $RawJson | ConvertFrom-Json
    } catch {
        Fail "invalid strict bundle manifest: could not parse JSON ($($_.Exception.Message))"
    }

    $requiredKeys = @('schema','bundle_id','tui_tag','tui_commit','generated_at','kernel_tag','kernel_version','kernel_manifest_filename','archives','providers')
    $actualKeys = @($data.psobject.Properties.Name)
    $missing = $requiredKeys | Where-Object { $actualKeys -notcontains $_ }
    $extra = $actualKeys | Where-Object { $requiredKeys -notcontains $_ }
    if ($missing -or $extra) {
        Fail "invalid strict bundle manifest: manifest has the wrong object shape"
    }

    if ($data.schema -ne 'lingtai.tui.bundle/v1') { Fail "invalid strict bundle manifest: unexpected schema" }
    foreach ($key in @('bundle_id','tui_tag','tui_commit','kernel_tag','kernel_version','kernel_manifest_filename')) {
        if ([string]::IsNullOrEmpty($data.$key)) { Fail "invalid strict bundle manifest: $key must be a nonempty string" }
    }
    if ($data.bundle_id -ne $data.tui_tag -or $data.tui_tag -ne $ExpectedTag) {
        Fail "invalid strict bundle manifest: bundle_id/tui_tag does not equal resolved tag"
    }
    if ($data.tui_commit -notmatch '^[0-9a-f]{40}$') {
        Fail "invalid strict bundle manifest: tui_commit must be a 40-character lowercase commit SHA"
    }
    # Validated against $generatedAtToken (the top-level source token
    # captured above), never against $data.generated_at: PowerShell 7 Core's
    # ConvertFrom-Json silently auto-converts ISO-8601-looking strings to
    # [datetime], with no cross-edition opt-out (-DateKind is PS 7.5+ only),
    # and DateTime.ToString()'s current-culture rendering then fails this
    # strict check even for a well-formed source value -- reproduced from a
    # live PS7 CI failure.
    if (-not $generatedAtToken -or $generatedAtToken -notmatch '^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$') {
        Fail "invalid strict bundle manifest: generated_at must be YYYY-MM-DDTHH:MM:SSZ"
    }

    $archives = @($data.archives)
    if ($archives.Count -eq 0) { Fail "invalid strict bundle manifest: archives must be a nonempty array" }
    $names = New-Object 'System.Collections.Generic.HashSet[string]'
    foreach ($archive in $archives) {
        $archiveKeys = @($archive.psobject.Properties.Name)
        if ((($archiveKeys | Sort-Object) -join ',') -ne 'filename,sha256') {
            Fail "invalid strict bundle manifest: archive entry has the wrong object shape"
        }
        $name = $archive.filename
        if ([string]::IsNullOrEmpty($name)) { Fail "invalid strict bundle manifest: archive filename must be a nonempty string" }
        if (-not $names.Add($name)) { Fail "invalid strict bundle manifest: archives contains duplicate filenames" }
        $isPosix = $name -match '^lingtai-[^/]+-(darwin|linux)-(amd64|arm64)\.tar\.gz$'
        # Named $isWindowsArchive, NOT $isWindows: PowerShell variable names are
        # case-insensitive, and $IsWindows is PS7+'s automatic read-only OS
        # variable -- assigning a local $isWindows collides with it and throws
        # "Cannot overwrite variable IsWindows because it is read-only or
        # constant." on PS7 (PS5.1 has no automatic $IsWindows, so it never hit
        # this). Reproduced from a live CI failure isolated to the PS7 job.
        $isWindowsArchive = $name -match '^lingtai-[^/]+-windows-amd64\.zip$'
        if (-not ($isPosix -or $isWindowsArchive)) { Fail "invalid strict bundle manifest: archive filename is invalid" }
        if ($archive.sha256 -notmatch '^[0-9a-f]{64}$') { Fail "invalid strict bundle manifest: archive sha256 must be lowercase 64-hex" }
    }

    $target = "lingtai-$ExpectedTag-windows-amd64.zip"
    $hits = @($archives | Where-Object { $_.filename -eq $target })
    if ($hits.Count -ne 1) { Fail "invalid strict bundle manifest: expected exactly one archive for $target, found $($hits.Count)" }

    $providerKeys = @($data.providers.psobject.Properties.Name | Sort-Object) -join ','
    if ($providerKeys -ne 'gitee,github') { Fail "invalid strict bundle manifest: providers has the wrong object shape" }
    if ([string]::IsNullOrEmpty($data.providers.github.repo) -or $data.providers.github.repo -notmatch '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$') {
        Fail "invalid strict bundle manifest: github repo is invalid"
    }
    if ([string]::IsNullOrEmpty($data.providers.gitee.owner) -or $data.providers.gitee.owner -notmatch '^[A-Za-z0-9_.-]+$') {
        Fail "invalid strict bundle manifest: gitee owner is invalid"
    }
    if ([string]::IsNullOrEmpty($data.providers.gitee.repo) -or $data.providers.gitee.repo -notmatch '^[A-Za-z0-9_.-]+$') {
        Fail "invalid strict bundle manifest: gitee repo is invalid"
    }

    return @{
        ArchiveFilename         = $target
        ArchiveSha256           = $hits[0].sha256
        TuiCommit               = $data.tui_commit
        KernelTag               = $data.kernel_tag
        KernelVersion            = $data.kernel_version
        KernelManifestFilename  = $data.kernel_manifest_filename
        BundleId                = $data.bundle_id
    }
}

# Get-BundleManifest resolves the tag's lingtai-bundle-manifest.json asset and
# returns the Confirm-BundleManifest result. Fails loud if the release has no
# such asset or it fails strict validation -- there is no fallback source.
function Get-BundleManifest {
    param([string]$Tag)
    $url = Get-ReleaseAssetUrl -Tag $Tag -Name 'lingtai-bundle-manifest.json'
    if (-not $url) {
        Fail "Release $Tag has no lingtai-bundle-manifest.json asset. LingTai's Windows install requires a pinned bundle; there is no unpinned fallback."
    }
    $raw = Get-TextAssetContent -Url $url
    return Confirm-BundleManifest -RawJson $raw -ExpectedTag $Tag
}

# --- Kernel release manifest (schema lingtai.kernel.release/v1) --------------

# Confirm-KernelManifest strictly validates the kernel release manifest: exact
# schema, well-formed artifact entries (wheel filename/sha256/python_tag/
# abi_tag/platform_tag), and a declared kernel_version. Returns the parsed
# object on success.
function Confirm-KernelManifest {
    param([string]$RawJson, [string]$ExpectedKernelTag)
    try {
        $data = $RawJson | ConvertFrom-Json
    } catch {
        Fail "invalid kernel release manifest: could not parse JSON ($($_.Exception.Message))"
    }
    if ($data.schema -ne 'lingtai.kernel.release/v1') {
        Fail "invalid kernel release manifest: unexpected schema '$($data.schema)'"
    }
    if ([string]::IsNullOrEmpty($data.kernel_version)) {
        Fail "invalid kernel release manifest: kernel_version must be a nonempty string"
    }
    $expectedVersion = $ExpectedKernelTag -replace '^v', ''
    if ($data.kernel_version -ne $expectedVersion) {
        Fail "invalid kernel release manifest: kernel_version '$($data.kernel_version)' does not match the pinned kernel tag $ExpectedKernelTag"
    }
    foreach ($art in @($data.artifacts)) {
        if ($art.kind -eq 'wheel') {
            # Validate the filename shape BEFORE it is ever used in a
            # download URL or Join-Path -- a malformed/adversarial filename
            # (path separators, traversal) is rejected here rather than
            # relying on downstream code to handle it safely. This is a SAFETY
            # check, not a platform gate: the kernel release manifest carries
            # win_amd64, macosx_*, and manylinux_* wheels together, and every
            # one must pass. Windows-only selection happens later in
            # Select-KernelWheel, which matches the venv's exact
            # cp311/312/313-win_amd64 tag.
            if ([string]::IsNullOrEmpty($art.filename) -or $art.filename -notmatch '^lingtai-[0-9A-Za-z_.+!-]+-(cp3(1[1-3]))-\1-[0-9A-Za-z_.+-]+\.whl$') {
                Fail "invalid kernel release manifest: wheel artifact has an invalid filename '$($art.filename)'"
            }
            if ($art.sha256 -notmatch '^[0-9a-f]{64}$') {
                Fail "invalid kernel release manifest: wheel artifact '$($art.filename)' has a malformed sha256"
            }
        }
    }
    return $data
}

# Get-KernelAssetUrl returns the browser_download_url for a named asset on the
# pinned kernel release, or $null if that release has no such asset -- the
# kernel-repo analogue of Get-ReleaseAssetUrl.
function Get-KernelAssetUrl {
    param([string]$KernelTag, [string]$Name)
    $release = Invoke-GitHubApi -Url "$KernelApiBase/releases/tags/$KernelTag"
    $asset = $release.assets | Where-Object { $_.name -eq $Name } | Select-Object -First 1
    if (-not $asset) { return $null }
    return $asset.browser_download_url
}

# Get-KernelManifest fetches and validates the kernel release manifest for the
# bundle's pinned kernel_tag from Lingtai-AI/lingtai-kernel. Fails loud if the
# kernel release or its manifest asset is missing.
function Get-KernelManifest {
    param([string]$KernelTag, [string]$ManifestFilename)
    $url = Get-KernelAssetUrl -KernelTag $KernelTag -Name $ManifestFilename
    if (-not $url) {
        Fail "Pinned kernel release $KernelTag has no $ManifestFilename asset."
    }
    $raw = Get-TextAssetContent -Url $url
    return Confirm-KernelManifest -RawJson $raw -ExpectedKernelTag $KernelTag
}

# --- Runtime venv (fail-loud; no PyPI-by-name; mirrors install.sh) -----------

# Find-VenvPython locates an already-available CPython 3.11/3.12/3.13 (amd64)
# via the Windows `py` launcher (preferred, most reliable version selection)
# or a bare `python`/`python3` on PATH. This probe never downloads or
# bootstraps Python; -Latest owns any separate prerequisite repair step.
function Get-SupportedVenvPythonDiscovery {
    $invalidDirectories = New-Object System.Collections.Generic.List[string]
    $invalidDetails = New-Object System.Collections.Generic.List[string]

    # PowerShell 5.1 turns native stderr into ErrorRecord objects. With this
    # script's fail-loud ErrorActionPreference=Stop, the Windows `py` launcher
    # exits the whole installer when it exists but has no installed runtimes
    # ("No suitable Python runtime found") unless the probe is isolated here.
    function Invoke-PythonDiscoveryProbe {
        param([string]$Launcher, [string[]]$Arguments)
        $savedErrorActionPreference = $ErrorActionPreference
        $records = @()
        $exitCode = 1
        try {
            $ErrorActionPreference = 'Continue'
            $records = @(& $Launcher @Arguments 2>&1)
            $exitCode = $LASTEXITCODE
        } catch {
            $records = @($_)
        } finally {
            $ErrorActionPreference = $savedErrorActionPreference
        }
        $output = (@($records | ForEach-Object {
            if ($_ -is [System.Management.Automation.ErrorRecord]) { $_.Exception.Message }
            else { [string]$_ }
        }) -join "`n").Trim()
        return @{ ExitCode = $exitCode; Output = $output }
    }

    # Include implementation identity: PyPy and other Python-compatible runtimes
    # cannot satisfy the managed CPython wheel/venv contract merely by matching
    # the requested language version and pointer width.
    $probeCode = 'import struct, sys; print(sys.implementation.name + '':'' + str(sys.version_info.major) + ''.'' + str(sys.version_info.minor) + '':'' + str(struct.calcsize(''P'') * 8))'

    $py = Get-Command -Name 'py' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($py) {
        $pyProbeDetails = @()
        foreach ($minor in @('3.13', '3.12', '3.11')) {
            $probeResult = Invoke-PythonDiscoveryProbe -Launcher $py.Source -Arguments @("-$minor", '-c', $probeCode)
            $probeExit = $probeResult.ExitCode
            $probe = $probeResult.Output
            if ($probeExit -eq 0 -and $probe -match '^cpython:3\.(11|12|13):64$') {
                return @{ Python = @{ Launcher = $py.Source; Args = @("-$minor") }; InvalidDirectories = @(); Detail = "launcher $($py.Source)" }
            }
            $pyProbeDetails += if ($probe) { "$minor=$probe" } else { "$minor=exit $probeExit" }
        }
        $pyDir = Split-Path -Parent $py.Source
        if ($pyDir) { $invalidDirectories.Add($pyDir) | Out-Null }
        $invalidDetails.Add("py ($($py.Source)): $($pyProbeDetails -join ', ')") | Out-Null
    }

    foreach ($name in @('python', 'python3')) {
        $cmd = Get-Command -Name $name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($cmd) {
            # Single-quoted Python literal only (no embedded ") -- Windows PowerShell
            # 5.1's native argument-array-to-command-line reconstruction mishandles
            # embedded double quotes, corrupting the string the interpreter receives.
            $probeResult = Invoke-PythonDiscoveryProbe -Launcher $cmd.Source -Arguments @('-c', $probeCode)
            $probeExit = $probeResult.ExitCode
            $probe = $probeResult.Output
            if ($probeExit -eq 0 -and $probe -match '^cpython:3\.(11|12|13):64$') {
                return @{ Python = @{ Launcher = $cmd.Source; Args = @() }; InvalidDirectories = @(); Detail = "launcher $($cmd.Source)" }
            }
            $cmdDir = Split-Path -Parent $cmd.Source
            if ($cmdDir) { $invalidDirectories.Add($cmdDir) | Out-Null }
            $observed = if ($probe) { $probe } else { "exit $probeExit" }
            $invalidDetails.Add("$name ($($cmd.Source)): $observed") | Out-Null
        }
    }

    $detail = if ($invalidDetails.Count -gt 0) {
        "requires 64-bit CPython 3.11-3.13; rejected $(@($invalidDetails) -join '; ')"
    } else {
        'missing; requires 64-bit CPython 3.11-3.13'
    }
    return @{
        Python = $null
        InvalidDirectories = @($invalidDirectories | Select-Object -Unique)
        Detail = $detail
    }
}

function Find-SupportedVenvPython {
    $discovery = Get-SupportedVenvPythonDiscovery
    return $discovery.Python
}

function Find-VenvPython {
    $found = Find-SupportedVenvPython
    if ($found) { return $found }
    Fail @"
No supported Python interpreter (CPython 3.11, 3.12, or 3.13, 64-bit) was found
via the 'py' launcher or 'python'/'python3' on PATH.

LingTai's Windows runtime venv is created from an already-available supported
Python installation at this stage; the release/local-artifact path does not
bootstrap an unpinned Python/uv toolchain. Install Python 3.11+ (for example from
python.org or the Microsoft Store) and re-run, or pass -SkipVenv to install
the TUI/portal binaries only (both binaries are still required).
"@
}

# Get-VenvWheelTag returns the venv interpreter's "cpXY-cpXY-win_amd64" tag by
# querying the venv's own Python -- never assumed from the bootstrap
# interpreter, since venv creation could in principle target a different
# minor version than the one that created it.
function Get-VenvWheelTag {
    param([string]$VenvPython)
    # Single-quoted Python literal only (no embedded ") -- Windows PowerShell
    # 5.1's native argument-array-to-command-line reconstruction mishandles
    # embedded double quotes, corrupting the string the interpreter receives.
    $probe = & $VenvPython '-c' 'import struct, sys; print(''cp'' + str(sys.version_info.major) + str(sys.version_info.minor) + '':'' + str(struct.calcsize(''P'') * 8))' 2>$null
    if ($LASTEXITCODE -ne 0 -or $probe -notmatch '^cp3(11|12|13):64$') {
        Fail "The managed venv must use 64-bit CPython 3.11, 3.12, or 3.13 before a win_amd64 wheel can be selected (got '$probe')."
    }
    $tag = ($probe -split ':')[0]
    return "$tag-$tag-win_amd64"
}

# Select-KernelWheel picks the manifest artifact whose
# "<python_tag>-<abi_tag>-<platform_tag>" combination equals $WheelTag exactly
# -- mirrors install.sh's select_kernel_wheel matching rule, restricted to the
# cp311/cp312/cp313 win_amd64 wheels this Windows slice supports. Fails loud
# if no match exists; there is no sdist fallback on native Windows (a build
# toolchain is not assumed present).
function Select-KernelWheel {
    param($KernelManifest, [string]$WheelTag)
    foreach ($art in @($KernelManifest.artifacts)) {
        if ($art.kind -ne 'wheel') { continue }
        $combo = "$($art.python_tag)-$($art.abi_tag)-$($art.platform_tag)"
        if ($combo -eq $WheelTag) { return $art }
    }
    Fail "Pinned kernel release $($KernelManifest.kernel_version) publishes no wheel matching '$WheelTag'. This Windows install requires an exact cp311/cp312/cp313 win_amd64 wheel; there is no sdist fallback natively."
}

# Install-KernelWheel downloads the selected wheel, verifies its manifest
# digest, and installs it into the venv by explicit local file path.
# LingTai's own bytes are NEVER requested from a package index by name.
function Install-KernelWheel {
    param([string]$VenvPython, $Wheel, [string]$KernelTag, [string]$StageDir)

    $downloadUrl = Get-KernelAssetUrl -KernelTag $KernelTag -Name $Wheel.filename
    if (-not $downloadUrl) { Fail "Pinned kernel release $KernelTag has no $($Wheel.filename) asset even though its manifest references it." }
    $dest = Join-Path $StageDir $Wheel.filename
    Write-Info "Downloading kernel wheel: $($Wheel.filename) (kernel $KernelTag) ..."
    try {
        Invoke-WebRequest -Uri $downloadUrl -OutFile $dest -UseBasicParsing
    } catch {
        Fail "Download failed for $downloadUrl ($($_.Exception.Message))"
    }
    $actual = Get-Sha256Hex -Path $dest
    if ($actual -ne $Wheel.sha256) {
        Fail "Checksum mismatch for $($Wheel.filename). Expected $($Wheel.sha256) but got $actual. Refusing to install an unverified kernel artifact. Retained at $dest for diagnosis."
    }
    Write-Ok "Verified SHA-256 for $($Wheel.filename)"

    # Explicit local path: pip never requests the package name "lingtai" from
    # any index here -- only third-party dependency resolution goes through
    # the index, exactly like install.sh's install_kernel_from_bundle.
    Write-Info "Installing lingtai from the verified local wheel (dependencies resolve via the configured package index) ..."
    # pip's stdout is voided (Out-Null), not just left to print: PowerShell
    # has no per-statement return-value isolation, so an unsuppressed native
    # command's stdout becomes part of this function's own output and, from
    # there, leaks into any caller that bare-calls it -- this previously
    # corrupted Install-Venv's return value ($kernelMeta) at its call site.
    # $LASTEXITCODE is unaffected by Out-Null.
    & $VenvPython '-m' 'pip' 'install' $dest | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "pip install of the local wheel failed (exit $LASTEXITCODE)." }
}

# Confirm-KernelImport preserves the release-wheel import/version/non-editable
# verification contract. When VenvDir + KernelSource are supplied by -Latest,
# it additionally requires the import to stay inside that managed venv and PEP
# 610 provenance to identify the exact non-editable pinned source checkout.
function Confirm-KernelImport {
    param(
        [string]$VenvPython,
        [string]$ExpectedVersion,
        [string]$VenvDir,
        [string]$KernelSource
    )
    $strictSource = -not ([string]::IsNullOrWhiteSpace($VenvDir) -and [string]::IsNullOrWhiteSpace($KernelSource))
    if ($strictSource -and ([string]::IsNullOrWhiteSpace($VenvDir) -or [string]::IsNullOrWhiteSpace($KernelSource))) {
        Fail "Strict kernel source verification requires both VenvDir and KernelSource."
    }
    $mode = if ($strictSource) { 'source' } else { 'wheel' }
    $expectedArg = if ([string]::IsNullOrWhiteSpace($ExpectedVersion)) { '-' } else { $ExpectedVersion }
    $venvArg = if ($strictSource) { $VenvDir } else { '-' }
    $sourceArg = if ($strictSource) { $KernelSource } else { '-' }

    # Single-quoted Python throughout (no embedded ") -- Windows PowerShell 5.1's
    # native argument-array-to-command-line reconstruction mishandles embedded
    # double quotes, corrupting the script text the interpreter actually receives.
    $probeScript = @'
import importlib.metadata as m
import json
from pathlib import Path
import sys
from urllib.parse import unquote, urlparse
from urllib.request import url2pathname

def fail(code, detail):
    print(code + ':' + detail)
    sys.exit(1)

def normalize_dist_name(raw):
    return (raw or '').strip().lower().replace('-', '_')

# Does this distribution's own RECORD claim the file the interpreter actually
# imported? Metadata alone is not enough: a .dist-info directory is just a
# directory, so only its declared file list proves it describes THIS code.
def dist_owns(candidate, target):
    if target is None:
        return False
    try:
        entries = candidate.files
    except Exception:
        return False
    if not entries:
        return False
    for entry in entries:
        try:
            if Path(candidate.locate_file(entry)).resolve() == target:
                return True
        except Exception:
            continue
    return False

# Resolve the distribution that PROVIDES the imported lingtai module, rather
# than trusting importlib.metadata.distribution('lingtai').
#
# distribution()/from_name() matches on the .dist-info DIRECTORY NAME and
# returns the first filesystem hit. A managed venv can accumulate a leftover
# lingtai-<old>.dist-info that pip cannot remove -- pip skips a .dist-info
# whose METADATA is missing or nameless (WARNING: ... due to invalid metadata
# entry 'name') instead of uninstalling it -- and that orphan sorts ahead of
# the real one. The probe then read the ORPHAN's stale direct_url.json and
# reported DIRECT_URL_WRONG_SOURCE naming a path this installer never used,
# failing a correct install with a misleading provenance error.
#
# Requiring BOTH a matching metadata Name and RECORD ownership of the imported
# file is strictly stronger than the old name lookup: the verified provenance
# now demonstrably belongs to the code that was imported. Genuine ambiguity
# (two distributions claiming the same module) still fails loud.
def resolve_lingtai_dist(target):
    named = []
    owners = []
    for candidate in m.distributions():
        try:
            metadata = candidate.metadata
            dist_name = normalize_dist_name(metadata['Name']) if metadata else ''
        except Exception:
            continue
        if dist_name != 'lingtai':
            continue
        named.append(candidate)
        if dist_owns(candidate, target):
            owners.append(candidate)
    if target is not None:
        if len(owners) == 1:
            return owners[0]
        if len(owners) > 1:
            fail('DIST_AMBIGUOUS', str(len(owners)) + ' installed distributions claim ' + str(target))
        fail('DIST_NOT_FOUND_FOR_MODULE', str(target))
    if len(named) == 1:
        return named[0]
    if len(named) > 1:
        fail('DIST_AMBIGUOUS', str(len(named)) + ' lingtai distributions are installed')
    fail('DIST_NOT_FOUND', 'no installed distribution provides lingtai')

try:
    import lingtai
except ImportError as exc:
    fail('IMPORT_FAILED', str(exc))

mode = sys.argv[1]
expected_version = '' if sys.argv[2] == '-' else sys.argv[2]
if mode not in ('wheel', 'source'):
    fail('INVALID_VERIFICATION_MODE', mode)

module_value = getattr(lingtai, '__file__', None)
module_path = Path(module_value).resolve() if module_value else None
dist = resolve_lingtai_dist(module_path)
version = dist.version
if expected_version and version != expected_version:
    fail('VERSION_MISMATCH', str(version))

direct_source = '<wheel>'
if mode == 'source':
    venv_dir = Path(sys.argv[3]).resolve()
    kernel_source = Path(sys.argv[4]).resolve()
    if module_path is None:
        fail('MODULE_PATH_MISSING', 'lingtai.__file__ is empty')
    try:
        module_path.relative_to(venv_dir)
    except ValueError:
        fail('MODULE_OUTSIDE_VENV', str(module_path))

    try:
        direct_url_text = dist.read_text('direct_url.json')
    except Exception as exc:
        fail('DIRECT_URL_UNREADABLE', str(exc))
    if not direct_url_text:
        fail('DIRECT_URL_MISSING', 'direct_url.json is absent or empty')
    try:
        direct_url = json.loads(direct_url_text)
    except Exception as exc:
        fail('DIRECT_URL_INVALID', str(exc))
    if not isinstance(direct_url, dict):
        fail('DIRECT_URL_INVALID', 'top-level value is not an object')
    dir_info = direct_url.get('dir_info')
    if not isinstance(dir_info, dict):
        fail('DIRECT_URL_INVALID', 'dir_info is missing or not an object')
    if dir_info.get('editable', False) is not False:
        fail('DIRECT_URL_EDITABLE', 'editable local install is not allowed')
    url = direct_url.get('url')
    if not isinstance(url, str) or not url:
        fail('DIRECT_URL_INVALID', 'url is missing')
    parsed = urlparse(url)
    if parsed.scheme.lower() != 'file':
        fail('DIRECT_URL_NOT_LOCAL', url)
    path_text = url2pathname(unquote(parsed.path))
    if parsed.netloc and parsed.netloc.lower() not in ('', 'localhost'):
        path_text = '//' + parsed.netloc + path_text
    direct_source = Path(path_text).resolve()
    if direct_source != kernel_source:
        fail('DIRECT_URL_WRONG_SOURCE', str(direct_source))
else:
    editable = False
    try:
        direct_url_text = dist.read_text('direct_url.json')
        if direct_url_text:
            direct_url = json.loads(direct_url_text)
            editable = bool(direct_url.get('dir_info', {}).get('editable', False))
    except Exception:
        pass
    if editable:
        fail('DIRECT_URL_EDITABLE', 'release wheel install is editable')

print('OK')
print(str(version))
print(str(module_path) if module_path is not None else '<unknown>')
print(str(direct_source))
'@
    $probe = @(& $VenvPython '-c' $probeScript $mode $expectedArg $venvArg $sourceArg 2>$null)
    if ($LASTEXITCODE -ne 0 -or $probe.Count -lt 4 -or $probe[0].Trim() -ne 'OK') {
        $kind = if ($strictSource) { 'pinned main kernel source' } else { 'release kernel wheel' }
        Fail "Post-install verification failed for the $kind ($($probe -join '; '))"
    }
    $installedVersion = $probe[1].Trim()
    if ($ExpectedVersion -and $installedVersion -ne $ExpectedVersion) {
        Fail "Installed lingtai version '$installedVersion' does not match the pinned kernel manifest version '$ExpectedVersion'."
    }
    if ($strictSource) {
        Write-Ok "Verified lingtai $installedVersion imports from the managed venv and matches the non-editable pinned kernel source."
    } else {
        Write-Ok "Verified lingtai $installedVersion imports and is a non-editable wheel install."
    }
    return $installedVersion
}

# Write-KernelProvenance writes an additive provenance stamp alongside the
# venv (not a new runtime protocol) recording exactly what was installed.
function Write-KernelProvenance {
    param(
        [string]$VenvDir,
        [string]$TuiTag,
        [string]$TuiCommit,
        [string]$BundleId,
        [string]$KernelTag,
        [string]$KernelVersion,
        [string]$WheelFilename,
        [string]$WheelSha256,
        [string]$Provider
    )
    $provenance = [ordered]@{
        schema          = 'lingtai.tui.kernel-provenance/v1'
        tui_tag         = $TuiTag
        tui_commit      = $TuiCommit
        bundle_id       = $BundleId
        kernel_tag      = $KernelTag
        kernel_version  = $KernelVersion
        wheel_filename  = $WheelFilename
        wheel_sha256    = $WheelSha256
        provider        = $Provider
        installed_at    = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    }
    $path = Join-Path $VenvDir 'kernel-provenance.json'
    $json = $provenance | ConvertTo-Json -Depth 5
    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($path, $json, $utf8NoBom)
    Write-Ok "Wrote kernel provenance -> $path"
}

# Copy-ManagedBinary installs one built/staged binary over its destination, even
# when that destination is currently RUNNING.
#
# Windows refuses to overwrite or delete a mapped executable image, so a plain
# `Copy-Item -Force` onto a running lingtai-tui.exe fails with "The process
# cannot access the file ... because it is being used by another process."
# Because the destination copy is the very last step, an ordinary `-Latest`
# re-install with the TUI or portal open discarded a completed build -- several
# minutes of checkout and compilation -- at the final instruction.
#
# Windows does, however, allow a running image to be RENAMED: the live process
# keeps executing from the renamed file while the original name is freed
# immediately. So park the old binary beside itself and retry the copy. The
# running instance keeps the old code until it is restarted, which is inherent
# to replacing a running program and is stated rather than papered over.
#
# This is the rename-then-replace half of what a from-scratch installer does
# here; deliberately NOT the other half -- no process is killed. Terminating a
# user's running agent supervisor to update a binary is a far worse outcome than
# telling them to restart it.
function Copy-ManagedBinary {
    param([string]$Source, [string]$Destination)

    try {
        Copy-Item -LiteralPath $Source -Destination $Destination -Force
        return
    } catch {
        if (-not (Test-Path -LiteralPath $Destination)) {
            Fail "Could not install $(Split-Path -Leaf $Destination) into $(Split-Path -Parent $Destination) ($($_.Exception.Message))."
        }
    }

    $parked = "$Destination.old-$(Get-Date -Format 'yyyyMMddHHmmss')"
    try {
        Rename-Item -LiteralPath $Destination -NewName (Split-Path -Leaf $parked) -ErrorAction Stop
    } catch {
        Fail @"
Could not replace $Destination because it is in use, and it could not be moved aside either ($($_.Exception.Message)).
Close LingTai (lingtai-tui / lingtai-portal) and re-run; the completed build is kept and the next run reuses it.
"@
    }
    Write-Warn "$(Split-Path -Leaf $Destination) was running; moved the old binary to $(Split-Path -Leaf $parked) and installed the new one. Restart it to pick up this build."
    try {
        Copy-Item -LiteralPath $Source -Destination $Destination -Force
    } catch {
        # Put the original back so a failed replace never leaves BinDir without
        # a working binary at the expected name.
        try { Rename-Item -LiteralPath $parked -NewName (Split-Path -Leaf $Destination) -ErrorAction Stop } catch { }
        Fail "Could not install $(Split-Path -Leaf $Destination) after moving the running binary aside ($($_.Exception.Message))."
    }
}

# Remove-ParkedManagedBinaries deletes `*.old-<stamp>` binaries parked by an
# earlier run whose process has since exited. Best-effort: one still held open
# simply stays for the next install to collect.
function Remove-ParkedManagedBinaries {
    param([string]$BinDir)
    if (-not (Test-Path -LiteralPath $BinDir)) { return }
    foreach ($stale in @(Get-ChildItem -LiteralPath $BinDir -Filter 'lingtai-*.exe.old-*' -File -ErrorAction SilentlyContinue)) {
        try { Remove-Item -LiteralPath $stale.FullName -Force -ErrorAction Stop } catch { }
    }
}

# Remove-OrphanedKernelDistInfo deletes LingTai's OWN unusable .dist-info
# directories from the installer-managed venv before an install writes a new
# one.
#
# pip refuses to uninstall a .dist-info whose METADATA is absent or carries no
# Name (it logs "Skipping <dir> due to invalid metadata entry 'name'" and moves
# on), so such a directory survives every subsequent install. It still shadows
# the real distribution for importlib.metadata's directory-name lookup, which
# is how a stale provenance record broke otherwise-correct -Latest installs.
# Confirm-KernelImport no longer trusts that lookup, but leaving the orphan in
# place keeps re-emitting pip warnings and re-arms the same trap for any other
# reader, so the installer removes what it can prove is unusable.
#
# Deliberately narrow -- NOT a venv rebuild. Only LingTai's own metadata
# directory is touched: the sibling <name>-<version>.dist-info convention
# normalizes the name part, so lingtai-kernel appears as lingtai_kernel-*
# and never matches, and the venv's resolved dependency tree is preserved. A
# directory with parseable METADATA is left alone even when stale, because pip
# can and does replace that one itself.
function Remove-OrphanedKernelDistInfo {
    param([string]$VenvDir)
    $sitePackages = Join-Path $VenvDir 'Lib\site-packages'
    if (-not (Test-Path -LiteralPath $sitePackages)) { return }
    $candidates = @(Get-ChildItem -LiteralPath $sitePackages -Directory -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -match '^lingtai-[^-]+\.dist-info$' })
    foreach ($candidate in $candidates) {
        $metadataPath = Join-Path $candidate.FullName 'METADATA'
        if (Test-Path -LiteralPath $metadataPath) {
            $nameLine = @(Select-String -LiteralPath $metadataPath -Pattern '^Name:\s*\S' -ErrorAction SilentlyContinue)
            if ($nameLine.Count -gt 0) { continue }
        }
        Write-Warn "Removing unusable LingTai metadata pip cannot uninstall: $($candidate.FullName)"
        try {
            Remove-Item -LiteralPath $candidate.FullName -Recurse -Force
        } catch {
            Fail "Could not remove the unusable metadata directory $($candidate.FullName) ($($_.Exception.Message)). Close anything using the runtime venv, delete that directory manually, then re-run."
        }
    }
}

# Install-Venv provisions %USERPROFILE%\.lingtai-tui\runtime\venv from the
# bundle's pinned kernel release, exactly like install.sh's
# ensure_runtime_venv/install_kernel_from_bundle: create the venv from an
# already-available supported Python, select the wheel matching the venv's
# actual interpreter tag, verify its digest, install by explicit local path,
# verify import/version/provenance, and only then write the provenance stamp.
# LingTai is NEVER installed from a package index by name and the kernel tag
# is NEVER changed from the one the bundle manifest pins. Returns a hashtable
# of kernel_source/kernel_bundle_id/kernel_version/kernel_provider for
# Write-InstallMetadata.
function Install-Venv {
    param([hashtable]$Bundle, [string]$TuiTag, [string]$GlobalDir)

    $venvDir = Join-Path $GlobalDir 'runtime\venv'
    Write-Info "Provisioning Python runtime venv at $venvDir ..."

    # Every native-command/void-intent call below is piped to Out-Null (see
    # Install-KernelWheel for why: leaked stdout here previously corrupted
    # this function's `return @{...}` into a mixed array, which failed with
    # "The property 'KernelSource' cannot be found on this object" at the
    # Invoke-Main call site -- confirmed from a live CI failure). Out-Null
    # does not affect $LASTEXITCODE, still read after each native call.
    $bootstrap = Find-VenvPython
    if (-not (Test-Path -LiteralPath $venvDir)) {
        New-Item -ItemType Directory -Force -Path (Split-Path $venvDir -Parent) | Out-Null
        $venvArgs = @($bootstrap.Args) + @('-m', 'venv', $venvDir)
        & $bootstrap.Launcher @venvArgs | Out-Null
        if ($LASTEXITCODE -ne 0) { Fail "Failed to create the venv at $venvDir (exit $LASTEXITCODE)." }
    }
    $venvPython = Join-Path $venvDir 'Scripts\python.exe'
    if (-not (Test-Path -LiteralPath $venvPython)) {
        Fail "Venv created at $venvDir but Scripts\python.exe is missing."
    }

    Remove-OrphanedKernelDistInfo -VenvDir $venvDir

    $wheelTag = Get-VenvWheelTag -VenvPython $venvPython
    $kernelManifest = Get-KernelManifest -KernelTag $Bundle.KernelTag -ManifestFilename $Bundle.KernelManifestFilename
    $wheel = Select-KernelWheel -KernelManifest $kernelManifest -WheelTag $wheelTag

    $stage = New-StagingDir
    Install-KernelWheel -VenvPython $venvPython -Wheel $wheel -KernelTag $Bundle.KernelTag -StageDir $stage | Out-Null
    $installedVersion = Confirm-KernelImport -VenvPython $venvPython -ExpectedVersion $kernelManifest.kernel_version

    Write-KernelProvenance -VenvDir $venvDir -TuiTag $TuiTag -TuiCommit $Bundle.TuiCommit -BundleId $Bundle.BundleId `
        -KernelTag $Bundle.KernelTag -KernelVersion $installedVersion -WheelFilename $wheel.filename `
        -WheelSha256 $wheel.sha256 -Provider 'github' | Out-Null

    return @{
        KernelSource   = 'bundle'
        KernelBundleId = $Bundle.BundleId
        KernelVersion  = $installedVersion
        KernelProvider = 'github'
    }
}

# --- Mainland-China build mirrors --------------------------------------------

# Initialize-BuildMirrors is the native-Windows counterpart to install.sh's
# CN-restricted-network fallback, which install.ps1 previously had no equivalent
# of at all -- so a mainland-China -Latest install fetched Go modules from
# proxy.golang.org and npm packages from registry.npmjs.org and simply hung
# until they timed out. The POSIX script has covered this since it gained
# --latest; this closes the gap for the Windows path.
#
# Same policy as install.sh, deliberately, so the two installers behave alike:
#
#   * Probe once, bounded (LINGTAI_MIRROR_TIMEOUT, default 3s). Only if
#     proxy.golang.org is unreachable do we switch anything.
#   * FAIL OPEN. A probe that errors, times out, or is ambiguous means "not
#     CN-restricted" -- never switch mirrors on a bad guess.
#   * NEVER override an explicit pre-set value. A user (or CI) who exported
#     GOPROXY/GOSUMDB/NPM_CONFIG_REGISTRY/LINGTAI_PYPI_INDEX_URL already stated
#     their intent; install.sh keys that decision off GOPROXY and so does this.
#   * LINGTAI_ASSUME_CN=1 forces the mirror set with no probe, for hosts with no
#     outbound access to the probe URL at all.
#
# Returns the PyPI index URL the pip steps should use, or $null for pip's
# default. This is the only mirror decision the caller has to thread through;
# Go and npm read theirs from the process environment the builds inherit.
function Initialize-BuildMirrors {
    $explicitIndex = $env:LINGTAI_PYPI_INDEX_URL
    if (-not [string]::IsNullOrWhiteSpace($explicitIndex)) {
        Write-Step "Using LINGTAI_PYPI_INDEX_URL for Python dependencies: $explicitIndex"
        return $explicitIndex
    }

    $timeout = 3
    if ($env:LINGTAI_MIRROR_TIMEOUT -match '^\d+$') { $timeout = [int]$env:LINGTAI_MIRROR_TIMEOUT }

    $assumeCn = ($env:LINGTAI_ASSUME_CN -eq '1')
    $goproxyPreset = -not [string]::IsNullOrWhiteSpace($env:GOPROXY)

    if (-not $assumeCn) {
        if ($goproxyPreset) {
            Write-Step "GOPROXY is already set; leaving build mirrors as configured."
            return $null
        }
        $reachable = $false
        try {
            $probe = Invoke-WebRequest -Uri 'https://proxy.golang.org/github.com/golang/go/@latest' `
                -UseBasicParsing -TimeoutSec $timeout -Method Head -ErrorAction Stop
            $reachable = ($null -ne $probe)
        } catch {
            $reachable = $false
        }
        if ($reachable) { return $null }
        Write-Info "proxy.golang.org unreachable within ${timeout}s; using China-friendly build mirrors."
    } else {
        Write-Info 'LINGTAI_ASSUME_CN=1; using China-friendly build mirrors without probing.'
    }

    if (-not $goproxyPreset) {
        $env:GOPROXY = 'https://goproxy.cn,direct'
        Write-Step "GOPROXY=$env:GOPROXY"
    }
    if ([string]::IsNullOrWhiteSpace($env:GOSUMDB)) {
        $env:GOSUMDB = 'sum.golang.google.cn'
        Write-Step "GOSUMDB=$env:GOSUMDB"
    }
    if ([string]::IsNullOrWhiteSpace($env:NPM_CONFIG_REGISTRY)) {
        $env:NPM_CONFIG_REGISTRY = 'https://registry.npmmirror.com'
        Write-Step "NPM_CONFIG_REGISTRY=$env:NPM_CONFIG_REGISTRY"
    }
    # Same mirror install.sh's PYPI_INDEX_URL_GITEE_DEFAULT uses, so a CN host
    # resolves Python dependencies from a reachable index too. LingTai's own
    # bytes are still never fetched from an index by name -- only the local
    # wheel/checkout path is installed, and this affects its dependencies only.
    $index = 'https://mirrors.tuna.tsinghua.edu.cn/pypi/web/simple'
    Write-Step "Python dependency index: $index"
    return $index
}

# --- Current-main development install ---------------------------------------

function Resolve-MainSha {
    param([string]$RemoteUrl, [string]$Label)
    # PS 5.1 promotes native stderr to a terminating ErrorRecord under the
    # script's global Stop policy, so a `git ls-remote` that writes to stderr
    # (a controlled test shim, or git emitting a network diagnostic) would
    # throw AT the native call and bypass the precise $LASTEXITCODE failure
    # below. Relax to Continue around the call (same pattern as the winget
    # bootstrap path) so stderr is discarded by 2>$null and the real exit
    # code drives the message.
    $savedErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        $lines = & git ls-remote $RemoteUrl 'refs/heads/main' 2>$null
        $gitExit = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $savedErrorActionPreference
    }
    if ($gitExit -ne 0) { Fail "Could not resolve $Label refs/heads/main. Install Git and verify network access to $RemoteUrl." }
    $sha = ($lines | Select-Object -First 1) -split '\s+' | Select-Object -First 1
    if ($sha -notmatch '^[0-9a-fA-F]{40}$') { Fail "Could not resolve a full $Label main commit from $RemoteUrl." }
    return $sha.ToLowerInvariant()
}

# Invoke-NativeBuild runs one build/checkout command, captures its combined
# output to a log, and reports elapsed time.
#
# Previously this was `& $Tool @Arguments | Out-Null`, which had two costs. The
# terminal went silent for minutes across `npm ci` and two Go builds with no
# indication of what was running, and a failure surfaced ONLY as "(exit N)" --
# the compiler/npm diagnostic that actually said why had been discarded, so the
# error named the step but never the cause. Output now lands in $LogPath, the
# tail is printed on failure, and the full log is kept for inspection.
#
# stderr is merged with `2>&1` so a failure's real diagnostic is captured, and
# that REQUIRES relaxing $ErrorActionPreference around the call: on Windows
# PowerShell 5.1 any text a native command writes to stderr becomes a
# NativeCommandError under the script's fail-loud 'Stop' policy, which would
# abort on npm/go progress chatter that is not an error at all. The real exit
# code is captured immediately and remains the only success signal.
function Invoke-NativeBuild {
    param(
        [string]$Tool,
        [string[]]$Arguments,
        [string]$Failure,
        [string]$LogPath
    )
    $rendered = "$Tool $($Arguments -join ' ')"
    Write-Step $rendered
    $clock = [System.Diagnostics.Stopwatch]::StartNew()

    $saved = $ErrorActionPreference
    $exitCode = 1
    try {
        $ErrorActionPreference = 'Continue'
        $lines = @(& $Tool @Arguments 2>&1 | ForEach-Object {
            if ($_ -is [System.Management.Automation.ErrorRecord]) { $_.Exception.Message } else { [string]$_ }
        })
        $exitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $saved
    }
    $clock.Stop()

    if ($LogPath) {
        try {
            $header = @("", "### $rendered (exit $exitCode, $(Format-Duration $clock.Elapsed))")
            [System.IO.File]::AppendAllLines($LogPath, [string[]](@($header) + $lines))
        } catch {
            # A log-write failure must never mask the build result itself.
            Write-Warn "Could not append to the build log $LogPath ($($_.Exception.Message))."
        }
    }

    if ($exitCode -ne 0) {
        $tail = @($lines | Select-Object -Last 30)
        if ($tail.Count -gt 0) {
            Write-Host "  --- last $($tail.Count) line(s) of output ---" -ForegroundColor DarkGray
            foreach ($line in $tail) { Write-Host "  | $line" -ForegroundColor DarkGray }
        }
        $where = if ($LogPath) { " Full output: $LogPath." } else { '' }
        Fail "$Failure (exit $exitCode).$where"
    }
    Write-Host ("      done in {0}" -f (Format-Duration $clock.Elapsed)) -ForegroundColor DarkGray
}

function Confirm-DevPrerequisites {
    param([switch]$SkipBootstrap)
    $packages = [ordered]@{
        git    = 'Git.Git'
        go     = 'GoLang.Go'
        node   = 'OpenJS.NodeJS.LTS'
        npm    = 'OpenJS.NodeJS.LTS'
        python = 'Python.Python.3.13'
    }
    # Existing invalid command directories are preserved but moved behind the
    # refreshed Machine/User PATH so freshly installed valid tools can win.
    $deprioritizedPathDirs = @()

    function Get-UniquePrerequisitePackages {
        param([array]$Items)
        $seen = @{}
        $unique = @()
        foreach ($item in $Items) {
            $package = $item.Value.Package
            if (-not $seen.ContainsKey($package)) {
                $seen[$package] = $true
                $unique += $package
            }
        }
        return $unique
    }

    function Get-WingetInstallArgs {
        param([string]$Package)
        return @('install','--id',$Package,'--exact','--source','winget','--accept-source-agreements','--accept-package-agreements','--disable-interactivity','--silent')
    }

    function Format-WingetInstallCommand {
        param([string]$Package)
        return "winget $((Get-WingetInstallArgs -Package $Package) -join ' ')"
    }

    $status = [ordered]@{}
    $git = Get-Command -Name 'git' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    $gitVersion = ''
    if ($git) { $gitVersion = (& $git.Source '--version' 2>$null | Out-String).Trim() }
    $gitValid = [bool]($git -and $LASTEXITCODE -eq 0 -and $gitVersion)
    $status.git = [ordered]@{ Command = 'git'; Package = $packages.git; Valid = $gitValid; Detail = if ($gitVersion) { $gitVersion } else { 'missing or git --version failed' } }
    if ($git -and -not $gitValid) { $deprioritizedPathDirs += (Split-Path -Parent $git.Source) }

    $go = Get-Command -Name 'go' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    $goVersion = ''
    if ($go) { $goVersion = (& $go.Source version 2>$null | Out-String).Trim() }
    $goValid = [bool]($go -and $LASTEXITCODE -eq 0 -and $goVersion)
    $status.go = [ordered]@{ Command = 'go'; Package = $packages.go; Valid = $goValid; Detail = if ($goVersion) { $goVersion } else { 'missing or go version failed' } }
    if ($go -and -not $goValid) { $deprioritizedPathDirs += (Split-Path -Parent $go.Source) }

    $node = Get-Command -Name 'node' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    $nodeVersion = ''
    $nodeSupported = $false
    if ($node) {
        $nodeVersion = (& $node.Source '--version' 2>$null | Out-String).Trim()
        if ($LASTEXITCODE -eq 0 -and $nodeVersion -match '^v(\d+)\.(\d+)\.(\d+)$') {
            $nodeMajor = [int]$Matches[1]; $nodeMinor = [int]$Matches[2]
            $nodeSupported = (($nodeMajor -eq 20 -and $nodeMinor -ge 19) -or ($nodeMajor -eq 22 -and $nodeMinor -ge 12) -or $nodeMajor -gt 22)
        }
    }
    # Supported Node policy: 20.19+, 22.12+, or a newer major; Node 21 and
    # Node 22 below 22.12 are unsupported.
    $nodeDetail = if ($nodeSupported) { $nodeVersion } elseif ($nodeVersion) { "unsupported version $nodeVersion (Node 21 and Node 22 below 22.12 are unsupported)" } else { 'missing or node --version failed' }
    $status.node = [ordered]@{ Command = 'node'; Package = $packages.node; Valid = $nodeSupported; Detail = $nodeDetail }
    if ($node -and -not $nodeSupported) { $deprioritizedPathDirs += (Split-Path -Parent $node.Source) }

    $npm = Get-Command -Name 'npm' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    $npmVersion = ''
    if ($npm) { $npmVersion = (& $npm.Source '--version' 2>$null | Out-String).Trim() }
    $npmValid = [bool]($npm -and $LASTEXITCODE -eq 0 -and $npmVersion)
    $status.npm = [ordered]@{ Command = 'npm'; Package = $packages.npm; Valid = $npmValid; Detail = if ($npmVersion) { $npmVersion } else { 'missing or npm --version failed' } }
    if ($npm -and -not $npmValid) { $deprioritizedPathDirs += (Split-Path -Parent $npm.Source) }

    $pythonDiscovery = Get-SupportedVenvPythonDiscovery
    $python = $pythonDiscovery.Python
    $status.python = [ordered]@{ Command = 'py/python'; Package = $packages.python; Valid = [bool]$python; Detail = if ($python) { "launcher $($python.Launcher)" } else { $pythonDiscovery.Detail } }
    if (-not $python) { $deprioritizedPathDirs += @($pythonDiscovery.InvalidDirectories) }

    $missing = @($status.GetEnumerator() | Where-Object { -not $_.Value.Valid })
    if ($missing.Count -eq 0) {
        Write-Ok "Using build prerequisites: $goVersion; Node.js $nodeVersion; Python launcher $($python.Launcher)"
        return @{ Ready = $true; Deferred = $false; Status = $status }
    }

    Write-Warn "-Latest found unsatisfied prerequisites: $($missing.Name -join ', ')"
    foreach ($item in $missing) { Write-Step "$($item.Name): $($item.Value.Detail); normal -Latest would install $($item.Value.Package)" }
    $uniquePackages = @(Get-UniquePrerequisitePackages -Items $missing)
    Write-Step "winget repair commands:"
    foreach ($package in $uniquePackages) { Write-Step "  $(Format-WingetInstallCommand -Package $package)" }
    if ($DryRun) {
        Write-Step '[dry-run] no winget invocation or prerequisite installation; stopping before main checkout/build'
        return @{ Ready = $false; Deferred = $true; Status = $status }
    }

    if ($SkipBootstrap) { return @{ Ready = $false; Deferred = $false; Status = $status } }

    $winget = Get-Command -Name 'winget' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if (-not $winget) {
        Fail "-Latest cannot install missing prerequisites because winget was not found. Install Microsoft App Installer/winget, ensure 'winget' is on PATH, then re-run. Required repair commands:`n$((@($uniquePackages | ForEach-Object { Format-WingetInstallCommand -Package $_ }) -join "`n"))"
    }
    foreach ($package in $uniquePackages) {
        $wingetArgs = Get-WingetInstallArgs -Package $package
        $wingetCommand = Format-WingetInstallCommand -Package $package
        Write-Info "Installing prerequisite package $package ..."
        $savedErrorActionPreference = $ErrorActionPreference
        $wingetExit = $null
        $wingetOutput = ''
        $wingetInvokeError = $null
        try {
            # Native stderr becomes ErrorRecord objects under PS5.1 even with the
            # local Continue policy. Preserve the real exit code while rendering
            # only each record's message, not PowerShell's NativeCommandError
            # wrapper/type metadata.
            $ErrorActionPreference = 'Continue'
            $wingetRecords = @(& $winget.Source @wingetArgs 2>&1)
            $wingetExit = $LASTEXITCODE
            $wingetOutput = (@($wingetRecords | ForEach-Object {
                if ($_ -is [System.Management.Automation.ErrorRecord]) { $_.Exception.Message }
                else { [string]$_ }
            }) -join "`n")
        } catch {
            $wingetInvokeError = $_.Exception.Message
        } finally {
            $ErrorActionPreference = $savedErrorActionPreference
        }
        if ($wingetInvokeError) {
            Fail "winget could not start prerequisite package ${package}: $wingetCommand. Check package policy, elevation, and App Installer, then re-run. Error: $wingetInvokeError"
        }
        if ($null -eq $wingetExit) { $wingetExit = 1 }
        if ($wingetExit -ne 0) {
            Fail "winget command failed for prerequisite package $package (exit $wingetExit): $wingetCommand. Check package policy, elevation, and App Installer, then re-run. Output: $wingetOutput"
        }
    }
    Refresh-ProcessPath -DeprioritizeDirectories $deprioritizedPathDirs
    $rechecked = Confirm-DevPrerequisitesAfterBootstrap
    if (-not $rechecked.Ready) {
        $failed = @($rechecked.Status.GetEnumerator() | Where-Object { -not $_.Value.Valid })
        Fail "-Latest prerequisite bootstrap completed but validation still fails for $($failed.Name -join ', '). Packages attempted: $($uniquePackages -join ', ')."
    }
    return $rechecked
}

function Refresh-ProcessPath {
    param([string[]]$DeprioritizeDirectories = @())
    function Get-PathKey {
        param([string]$PathEntry)
        if ([string]::IsNullOrWhiteSpace($PathEntry)) { return '' }
        return $PathEntry.Trim().TrimEnd([char[]]'\/')
    }

    $original = @($env:PATH -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    $deprioritizedKeys = @($DeprioritizeDirectories | ForEach-Object { Get-PathKey -PathEntry $_ } | Where-Object { $_ })
    $preferredOriginal = @()
    $deprioritizedOriginal = @()
    foreach ($entry in $original) {
        if ($deprioritizedKeys -contains (Get-PathKey -PathEntry $entry)) {
            $deprioritizedOriginal += $entry
        } else {
            $preferredOriginal += $entry
        }
    }

    # The process-scoped overrides are a hermetic contract-test seam only;
    # normal installs still read the real Machine/User values.
    $machineOverride = [Environment]::GetEnvironmentVariable('LINGTAI_TEST_MACHINE_PATH', 'Process')
    $userOverride = [Environment]::GetEnvironmentVariable('LINGTAI_TEST_USER_PATH', 'Process')
    $machine = if ($null -ne $machineOverride) { $machineOverride } else { [Environment]::GetEnvironmentVariable('PATH', 'Machine') }
    $user = if ($null -ne $userOverride) { $userOverride } else { [Environment]::GetEnvironmentVariable('PATH', 'User') }
    $refreshed = @($machine, $user) | Where-Object { $_ } |
        ForEach-Object { $_ -split ';' } |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    $preferredRefreshed = @()
    $deprioritizedRefreshed = @()
    foreach ($entry in $refreshed) {
        if ($deprioritizedKeys -contains (Get-PathKey -PathEntry $entry)) {
            $deprioritizedRefreshed += $entry
        } else {
            $preferredRefreshed += $entry
        }
    }
    $merged = @($preferredOriginal, $preferredRefreshed, $deprioritizedOriginal, $deprioritizedRefreshed) |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    $unique = @()
    $seen = @{}
    foreach ($entry in $merged) {
        $cleanEntry = $entry.Trim()
        $key = Get-PathKey -PathEntry $cleanEntry
        if ($key -and -not $seen.ContainsKey($key)) {
            $seen[$key] = $true
            $unique += $cleanEntry
        }
    }
    $env:PATH = ($unique -join ';')
    Write-Step 'Refreshed process PATH from Machine + User environment, preserved process-only entries, and moved previously invalid command directories behind refreshed package paths.'
}

function Confirm-DevPrerequisitesAfterBootstrap {
    return (Confirm-DevPrerequisites -SkipBootstrap)
}

# Resolve both pins before either checkout. Build output remains under an
# installer-owned staging directory until both binaries and their versions are
# validated, so destination writes happen only after the complete pair exists.
function Build-LatestMain {
    $phase = Start-Phase 'Checking build prerequisites (git, Go, Node.js/npm, CPython 3.11-3.13) ...'
    $prerequisites = Confirm-DevPrerequisites
    if ($prerequisites.Deferred) { return @{ DryRun = $true; PrerequisitesDeferred = $true } }
    Complete-Phase -Clock $phase -Message 'prerequisites satisfied'
    $tuiSha = Resolve-MainSha -RemoteUrl $RepoUrl -Label 'TUI'
    $kernelSha = Resolve-MainSha -RemoteUrl 'https://github.com/Lingtai-AI/lingtai-kernel.git' -Label 'kernel'
    Write-Info "Resolved TUI main commit: $tuiSha"
    Write-Info "Resolved kernel main commit: $kernelSha"
    if ($DryRun) {
        Write-Step "[dry-run] would shallow-checkout both pinned main commits and build lingtai-tui.exe plus required lingtai-portal.exe"
        return @{ TuiSha = $tuiSha; KernelSha = $kernelSha; DryRun = $true }
    }

    $stage = New-StagingDir
    $tuiSource = Join-Path $stage 'lingtai'
    $kernelSource = Join-Path $stage 'lingtai-kernel'
    # One log for the whole build, beside the staging tree the installer already
    # keeps for evidence, so a failure tail always has a full transcript behind it.
    $buildLog = Join-Path $stage 'build.log'
    Write-Step "Build log: $buildLog"

    $phase = Start-Phase 'Checking out the pinned TUI commit ...'
    Invoke-NativeBuild -Tool 'git' -Arguments @('clone','--depth','1','--branch','main',$RepoUrl,$tuiSource) -Failure 'TUI main checkout failed' -LogPath $buildLog
    Invoke-NativeBuild -Tool 'git' -Arguments @('-C',$tuiSource,'fetch','--depth','1','origin',$tuiSha) -Failure 'TUI pinned commit fetch failed' -LogPath $buildLog
    Invoke-NativeBuild -Tool 'git' -Arguments @('-C',$tuiSource,'checkout','--detach',$tuiSha) -Failure 'TUI pinned checkout failed' -LogPath $buildLog
    $actualTui = (& git -C $tuiSource rev-parse HEAD).Trim().ToLowerInvariant()
    if ($actualTui -ne $tuiSha) { Fail "TUI checkout mismatch: resolved $tuiSha but checked out $actualTui. Staging kept at $stage." }
    Complete-Phase -Clock $phase -Message "TUI at $($tuiSha.Substring(0,12))"

    $phase = Start-Phase 'Checking out the pinned kernel commit ...'
    Invoke-NativeBuild -Tool 'git' -Arguments @('clone','--depth','1','--branch','main','https://github.com/Lingtai-AI/lingtai-kernel.git',$kernelSource) -Failure 'kernel main checkout failed' -LogPath $buildLog
    Invoke-NativeBuild -Tool 'git' -Arguments @('-C',$kernelSource,'fetch','--depth','1','origin',$kernelSha) -Failure 'kernel pinned commit fetch failed' -LogPath $buildLog
    Invoke-NativeBuild -Tool 'git' -Arguments @('-C',$kernelSource,'checkout','--detach',$kernelSha) -Failure 'kernel pinned checkout failed' -LogPath $buildLog
    $actualKernel = (& git -C $kernelSource rev-parse HEAD).Trim().ToLowerInvariant()
    if ($actualKernel -ne $kernelSha) { Fail "kernel checkout mismatch: resolved $kernelSha but checked out $actualKernel. Staging kept at $stage." }
    Complete-Phase -Clock $phase -Message "kernel at $($kernelSha.Substring(0,12))"

    $version = "main-$tuiSha"
    $tuiOut = Join-Path $stage 'lingtai-tui.exe'
    $portalOut = Join-Path $stage 'lingtai-portal.exe'

    $phase = Start-Phase 'Building the portal web frontend (npm ci + npm run build; the longest step) ...'
    Push-Location (Join-Path $tuiSource 'portal/web')
    try {
        Invoke-NativeBuild -Tool 'npm' -Arguments @('ci') -Failure 'portal frontend dependency install failed' -LogPath $buildLog
        Invoke-NativeBuild -Tool 'npm' -Arguments @('run','build') -Failure 'portal frontend build failed' -LogPath $buildLog
    } finally { Pop-Location }
    Complete-Phase -Clock $phase -Message 'portal web assets built'

    $phase = Start-Phase 'Compiling lingtai-tui.exe ...'
    Push-Location (Join-Path $tuiSource 'tui')
    try { Invoke-NativeBuild -Tool 'go' -Arguments @('build','-trimpath','-ldflags',"-X main.version=$version",'-o',$tuiOut,'.') -Failure 'lingtai-tui.exe build failed' -LogPath $buildLog }
    finally { Pop-Location }
    Complete-Phase -Clock $phase -Message 'lingtai-tui.exe compiled'

    $phase = Start-Phase 'Compiling lingtai-portal.exe ...'
    Push-Location (Join-Path $tuiSource 'portal')
    try { Invoke-NativeBuild -Tool 'go' -Arguments @('build','-trimpath','-ldflags',"-X main.version=$version",'-o',$portalOut,'.') -Failure 'lingtai-portal.exe build failed' -LogPath $buildLog }
    finally { Pop-Location }
    Complete-Phase -Clock $phase -Message 'lingtai-portal.exe compiled'

    Confirm-StagedVersion -StagedTui $tuiOut -Requested $version
    $portalProbe = & $portalOut 'version' 2>&1 | Out-String
    if ($LASTEXITCODE -ne 0 -or $portalProbe.Trim() -ne "lingtai-portal $version") {
        Fail "Built lingtai-portal.exe failed provenance verification (expected 'lingtai-portal $version', got '$($portalProbe.Trim())'). Staging kept at $stage."
    }
    return @{ Stage = $stage; TuiSource = $tuiSource; KernelSource = $kernelSource; TuiSha = $tuiSha; KernelSha = $kernelSha; Version = $version; Tui = $tuiOut; Portal = $portalOut; DryRun = $false }
}

function Install-MainVenv {
    param([string]$KernelSource, [string]$KernelSha, [string]$GlobalDir, [string]$PythonIndexUrl)
    $phase = Start-Phase 'Provisioning the Python runtime venv and installing the pinned kernel ...'
    $bootstrap = Find-VenvPython
    $venvDir = Join-Path $GlobalDir 'runtime\venv'
    if (-not (Test-Path -LiteralPath $venvDir)) {
        Write-Step "Creating the venv at $venvDir"
        New-Item -ItemType Directory -Force -Path (Split-Path $venvDir -Parent) | Out-Null
        $venvArgs = @($bootstrap.Args) + @('-m','venv',$venvDir)
        & $bootstrap.Launcher @venvArgs | Out-Null
        if ($LASTEXITCODE -ne 0) { Fail "-Latest could not create the runtime venv at $venvDir." }
    } else {
        Write-Step "Reusing the existing venv at $venvDir"
    }
    $python = Join-Path $venvDir 'Scripts\python.exe'
    if (-not (Test-Path -LiteralPath $python)) { Fail "-Latest runtime venv has no Scripts\python.exe at $venvDir." }
    Get-VenvWheelTag -VenvPython $python | Out-Null
    Remove-OrphanedKernelDistInfo -VenvDir $venvDir
    $head = (& git -C $KernelSource rev-parse HEAD).Trim().ToLowerInvariant()
    if ($head -ne $KernelSha) { Fail "Kernel source changed before install: expected $KernelSha, got $head." }
    # Only third-party DEPENDENCY resolution goes through an index; LingTai's own
    # bytes still come exclusively from the verified local checkout path below.
    $indexArgs = @()
    if (-not [string]::IsNullOrWhiteSpace($PythonIndexUrl)) {
        $indexArgs = @('--index-url', $PythonIndexUrl)
        Write-Step "Resolving dependencies from $PythonIndexUrl"
    }
    Write-Info "Installing lingtai from the verified checked-out kernel source path (non-editable local build) ..."
    Write-Step 'pip install (this resolves the dependency tree and can take a few minutes)'
    $pipArgs = @('-m','pip','install') + $indexArgs + @($KernelSource)
    & $python @pipArgs | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "-Latest kernel source install failed (exit $LASTEXITCODE)." }
    # A reused managed venv can already contain the same LingTai version with a
    # stale editable/local direct_url.json. The dependency-resolving install
    # above may then report the requirement satisfied without replacing that
    # root package. Reinstall only LingTai itself from this exact checked-out
    # source so provenance is deterministic while preserving resolved deps.
    Write-Step 'pip install --force-reinstall --no-deps (pins provenance to this exact checkout)'
    & $python '-m' 'pip' 'install' '--force-reinstall' '--no-deps' $KernelSource | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "-Latest pinned kernel reinstall failed (exit $LASTEXITCODE)." }
    $version = Confirm-KernelImport -VenvPython $python -ExpectedVersion '' -VenvDir $venvDir -KernelSource $KernelSource
    Complete-Phase -Clock $phase -Message "lingtai $version installed into the managed venv"
    return @{ KernelSource='main'; KernelVersion=$version; KernelProvider='github'; KernelCommit=$KernelSha }
}

function Install-FromBuiltMain {
    param([hashtable]$Build, [string]$BinDir)
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    $tuiDest = Join-Path $BinDir 'lingtai-tui.exe'; $portalDest = Join-Path $BinDir 'lingtai-portal.exe'
    Remove-ParkedManagedBinaries -BinDir $BinDir
    Copy-ManagedBinary -Source $Build.Tui -Destination $tuiDest
    Copy-ManagedBinary -Source $Build.Portal -Destination $portalDest
    Write-Ok "Installed pinned main binaries into $BinDir"
    return @($tuiDest,$portalDest)
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
        Write-Step "[dry-run] would install lingtai-tui.exe and lingtai-portal.exe into $BinDir"
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

    # Require the portal before any destination write. A verified archive that
    # omits it is not a complete Windows bundle, including under -SkipVenv.
    $portal = Get-ChildItem -LiteralPath $extract -Recurse -Filter 'lingtai-portal.exe' -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if (-not $portal) {
        Fail "Archive does not contain required lingtai-portal.exe. Staging kept for inspection: $stage"
    }

    # 5. Install idempotently into BinDir (only reached once both binaries and
    # the staged TUI version have been validated).
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    Remove-ParkedManagedBinaries -BinDir $BinDir
    $tuiDest = Join-Path $BinDir 'lingtai-tui.exe'
    Copy-ManagedBinary -Source $tui.FullName -Destination $tuiDest
    Write-Ok "Installed lingtai-tui.exe -> $BinDir"

    $managed = New-Object System.Collections.Generic.List[string]
    $managed.Add($tuiDest)
    $portalDest = Join-Path $BinDir 'lingtai-portal.exe'
    Copy-ManagedBinary -Source $portal.FullName -Destination $portalDest
    Write-Ok "Installed lingtai-portal.exe -> $BinDir"
    $managed.Add($portalDest)

    return $managed.ToArray()
}

# --- Public (no -ArchivePath) install ----------------------------------------

# Install-FromPublicRelease resolves $Requested (or latest) to an exact tag,
# validates that release's bundle manifest, downloads the Windows archive and
# its sha256 sidecar, verifies the archive against the manifest digest (and
# cross-checks the sidecar agrees), stages/extracts it, and confirms the
# staged lingtai-tui.exe reports exactly the resolved tag before any BinDir
# write. Returns @{ Managed = <copied binary paths>; Bundle = <bundle hashtable>; Tag = <resolved tag> }.
# Architecture is explicit: the release artifact is amd64-only; ARM64 is not
# claimed as supported.
#
# -ResolvedTag/-ResolvedBundle let a caller that already resolved "latest"
# once (Invoke-Main's runtime-gate step, when the venv step ran first) pass
# that SAME resolution through instead of this function re-resolving "latest"
# a second, independent time -- which would risk installing a newer release
# than the one the venv/kernel step just validated if a new tag published in
# between the two calls.
function Install-FromPublicRelease {
    param([string]$BinDir, [string]$Requested, [string]$ResolvedTag, [hashtable]$ResolvedBundle)

    $arch = Get-Arch
    if ($arch -ne 'amd64') {
        Fail "The LingTai Windows release artifact is amd64-only; '$arch' is not supported natively yet. Use WSL2 + install.sh."
    }

    if ($ResolvedTag -and $ResolvedBundle) {
        $tag = $ResolvedTag
        $bundle = $ResolvedBundle
    } else {
        $tag = Resolve-PublicTag -Requested $Requested
        Write-Info "Resolved release tag: $tag"
        $bundle = Get-BundleManifest -Tag $tag
        Write-Ok "Validated bundle manifest (kernel $($bundle.KernelTag))"
    }

    if ($DryRun) {
        Write-Step "[dry-run] would download $($bundle.ArchiveFilename) and its .sha256 sidecar from $RepoUrl release $tag"
        Write-Step "[dry-run] would verify the archive against the bundle manifest digest, stage, and verify the staged version"
        Write-Step "[dry-run] would install lingtai-tui.exe and lingtai-portal.exe into $BinDir"
        return @{ Managed = @(); Bundle = $bundle; Tag = $tag }
    }

    $zipUrl = Get-ReleaseAssetUrl -Tag $tag -Name $bundle.ArchiveFilename
    if (-not $zipUrl) { Fail "Release $tag has no $($bundle.ArchiveFilename) asset even though the bundle manifest references it." }
    $shaUrl = Get-ReleaseAssetUrl -Tag $tag -Name "$($bundle.ArchiveFilename).sha256"
    if (-not $shaUrl) { Fail "Release $tag has no $($bundle.ArchiveFilename).sha256 sidecar even though the bundle manifest references the archive." }

    $stage = New-StagingDir
    $archivePath = Join-Path $stage $bundle.ArchiveFilename
    $sidecarPath = "$archivePath.sha256"
    Write-Info "Downloading $($bundle.ArchiveFilename) (release $tag) ..."
    try {
        Invoke-WebRequest -Uri $zipUrl -OutFile $archivePath -UseBasicParsing
        Invoke-WebRequest -Uri $shaUrl -OutFile $sidecarPath -UseBasicParsing
    } catch {
        Fail "Download failed ($($_.Exception.Message)). Staging kept for inspection: $stage"
    }

    # The sidecar is fetched from the SAME release as the archive; verify it
    # agrees with the bundle manifest digest before trusting either, then
    # verify the downloaded bytes against that digest -- mirrors install.sh's
    # mixed-provenance guard in try_release_asset.
    $sidecarDigest = Read-ExpectedSha256 -Path $sidecarPath
    if (-not $sidecarDigest) { Fail "Could not parse a SHA-256 digest from $sidecarPath." }
    if ($sidecarDigest -ne $bundle.ArchiveSha256) {
        Fail "Release checksum sidecar disagrees with the bundle manifest for $($bundle.ArchiveFilename); refusing mixed provenance."
    }
    Confirm-ArchiveChecksum -ArchiveFile $archivePath -SidecarFile $sidecarPath

    $managed = Install-FromLocalArtifact -Archive $archivePath -Sidecar $sidecarPath -BinDir $BinDir -Requested $tag
    return @{ Managed = $managed; Bundle = $bundle; Tag = $tag }
}

# --- Main --------------------------------------------------------------------

function Invoke-Main {
    Write-Host ""
    Write-Host "LingTai -- native Windows installer" -ForegroundColor Magenta
    Write-Host "------------------------------------" -ForegroundColor Magenta
    if ($DryRun) { Write-Warn "DRY RUN: no filesystem, PATH, or config writes will be made." }

    # Resolve per-user, non-admin defaults.
    if ([string]::IsNullOrWhiteSpace($BinDir))    { $BinDir    = Get-DefaultBinDir }
    if ([string]::IsNullOrWhiteSpace($GlobalDir)) { $GlobalDir = Get-DefaultGlobalDir }

    $rawArch = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $rawArch = $env:PROCESSOR_ARCHITEW6432 }
    if ($Latest -and $rawArch -ne 'AMD64') {
        Fail "-Latest supports native Windows amd64 only; this host is not amd64. Use WSL2 with install.sh --latest."
    }

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
    if ($Latest) {
        $conflicts = New-Object System.Collections.Generic.List[string]
        if ($haveArchive) { $conflicts.Add('-ArchivePath/-ChecksumPath') }
        if (-not [string]::IsNullOrWhiteSpace($Version)) { $conflicts.Add('-Version/LINGTAI_VERSION') }
        if ($SkipVenv) { $conflicts.Add('-SkipVenv') }
        if ($conflicts.Count -gt 0) {
            Fail "-Latest cannot be combined with $($conflicts -join ', '). Current-main mode always builds both binaries and provisions the checked-out kernel runtime."
        }
        Write-Info 'Mode: current main development install (-Latest)'
        Write-Step "Binaries -> $BinDir"
        Write-Step "State    -> $GlobalDir"
        # 7 phases: prerequisites, 2 checkouts, portal web, 2 Go builds, runtime venv.
        Set-PhaseTotal 7
        $pythonIndexUrl = if ($DryRun) { $null } else { Initialize-BuildMirrors }
        $mainBuild = Build-LatestMain
        if ($DryRun) {
            Write-Step "[dry-run] would install the kernel checkout into $GlobalDir\runtime\venv"
            Write-Step "[dry-run] would copy both pinned binaries into $BinDir and write additive install.json main provenance"
            return
        }
        $mainKernel = Install-MainVenv -KernelSource $mainBuild.KernelSource -KernelSha $mainBuild.KernelSha -GlobalDir $GlobalDir -PythonIndexUrl $pythonIndexUrl
        $mainManaged = Install-FromBuiltMain -Build $mainBuild -BinDir $BinDir
        Add-ToPath -Dir $BinDir
        Write-InstallMetadata -GlobalDir $GlobalDir -Prefix $prefix -BinDir $BinDir -RequestedRef 'main' -ResolvedRef 'main' -ResolvedCommit $mainBuild.TuiSha -InstallKind 'powershell-latest-main' -ManagedBinaries $mainManaged -KernelSource $mainKernel.KernelSource -KernelVersion $mainKernel.KernelVersion -KernelProvider $mainKernel.KernelProvider -SourceMode 'latest-main' -TuiCommit $mainBuild.TuiSha -KernelCommit $mainBuild.KernelSha
        Write-Completion -BinDir $BinDir -GlobalDir $GlobalDir -Headline 'Current-main development install complete.' -Facts ([ordered]@{
            'TUI commit'     = $mainBuild.TuiSha
            'kernel commit'  = $mainBuild.KernelSha
            'kernel version' = $mainKernel.KernelVersion
            'stamped as'     = $mainBuild.Version
            'build log'      = (Join-Path $mainBuild.Stage 'build.log')
        })
        return
    }

    Write-Info "Target BinDir: $BinDir"
    Write-Info "Target GlobalDir: $GlobalDir"

    # 1. Resolve the bundle up front (metadata reads only, no writes) so the
    # runtime capability gate below can run BEFORE any binary/PATH/metadata
    # write, exactly like the previous hard-stop did. Local-artifact mode has
    # no bundle shipped inside the archive, so it resolves the SAME bundle a
    # public install of -Version would (this is the only network use in that
    # mode, and only when the venv step is not skipped).
    Write-Phase "Resolve release bundle"
    $bundle = $null
    $resolvedTag = $Version
    if (-not $SkipVenv) {
        if ($haveArchive) {
            $resolvedTag = $Version
            if (-not $DryRun) { $bundle = Get-BundleManifest -Tag $resolvedTag }
        } else {
            $resolvedTag = Resolve-PublicTag -Requested $Version
            $bundle = Get-BundleManifest -Tag $resolvedTag
            Write-Ok "Validated bundle manifest (kernel $($bundle.KernelTag))"
        }
    }

    # 2. Runtime capability gate. Provisioned BEFORE binaries/PATH/metadata can
    # change, so a runtime failure never leaves a half-installed TUI. -SkipVenv
    # is the explicit TUI-only opt-out; DryRun performs no writes at all.
    Write-Phase "Runtime capability gate"
    $kernelMeta = $null
    if (-not $SkipVenv -and -not $DryRun) {
        $kernelMeta = Install-Venv -Bundle $bundle -TuiTag $resolvedTag -GlobalDir $GlobalDir
        # Static shape check on Install-Venv's return contract. This function
        # returns a plain hashtable with exactly these four keys; if that ever
        # regresses (e.g. an unsuppressed statement inside Install-Venv or a
        # function it calls leaks native-command/pipeline output, turning the
        # return value into a mixed array instead of a bare hashtable), fail
        # here with a precise diagnostic instead of letting a malformed
        # $kernelMeta reach Write-InstallMetadata and fail three frames away
        # with a confusing "property cannot be found" error.
        $expectedKernelMetaKeys = @('KernelSource', 'KernelBundleId', 'KernelVersion', 'KernelProvider')
        if ($kernelMeta -isnot [hashtable]) {
            # $kernelMeta.GetType() throws on $null -- compute the type
            # description first so this diagnostic never masks itself with a
            # secondary "cannot call a method on a null-valued expression".
            $actualKernelMetaType = if ($null -eq $kernelMeta) { 'null' } else { $kernelMeta.GetType().FullName }
            Fail "Internal error: Install-Venv returned a $actualKernelMetaType, not a hashtable. Its return value was likely polluted by an unsuppressed statement's output."
        }
        $missingKernelMetaKeys = $expectedKernelMetaKeys | Where-Object { -not $kernelMeta.ContainsKey($_) }
        if ($missingKernelMetaKeys) {
            Fail "Internal error: Install-Venv's return value is missing expected key(s): $($missingKernelMetaKeys -join ', ')."
        }
    } elseif (-not $SkipVenv -and $DryRun) {
        if ($bundle) {
            Write-Step "[dry-run] would provision the runtime venv from kernel $($bundle.KernelTag) at $GlobalDir\runtime\venv"
        } else {
            Write-Step "[dry-run] would resolve the release bundle for $resolvedTag and provision the runtime venv at $GlobalDir\runtime\venv"
        }
    }

    # 3. Install binaries. When step 1 already resolved a tag/bundle (public
    # mode, venv not skipped), pass that SAME resolution through instead of
    # letting Install-FromPublicRelease re-resolve "latest" a second time.
    Write-Phase "Install binaries"
    if ($haveArchive) {
        $managed = Install-FromLocalArtifact -Archive $ArchivePath -Sidecar $ChecksumPath -BinDir $BinDir -Requested $Version
    } elseif ($bundle) {
        $result = Install-FromPublicRelease -BinDir $BinDir -Requested $Version -ResolvedTag $resolvedTag -ResolvedBundle $bundle
        $managed = $result.Managed
    } else {
        $result = Install-FromPublicRelease -BinDir $BinDir -Requested $Version
        $managed = $result.Managed
        $resolvedTag = $result.Tag
        $bundle = $result.Bundle
    }

    # 4. PATH. Skipped entirely in DryRun (no persistent writes).
    Write-Phase "Update PATH"
    if ($DryRun) {
        Write-Step "[dry-run] would add '$BinDir' to the process and (unless -NoModifyPath) persistent user PATH"
    } else {
        Add-ToPath -Dir $BinDir
    }

    # 5. Runtime disposition.
    Write-Phase "Runtime disposition"
    if ($SkipVenv) {
        Write-Warn "Skipping runtime venv (-SkipVenv). Provision the Python runtime yourself; the TUI/portal binaries are installed."
    }

    # 6. Metadata. Skipped in DryRun (no writes).
    Write-Phase "Install metadata"
    if ($DryRun) {
        Write-Step "[dry-run] would write install metadata under $GlobalDir"
    } else {
        $metaArgs = @{
            GlobalDir       = $GlobalDir
            Prefix          = $prefix
            BinDir          = $BinDir
            RequestedRef    = $Version
            ResolvedRef     = $resolvedTag
            ResolvedCommit  = $(if ($bundle) { $bundle.TuiCommit } else { '' })
            InstallKind     = $(if ($haveArchive) { 'powershell-local-artifact' } else { 'powershell-release-asset' })
            ManagedBinaries = $managed
        }
        if ($kernelMeta) {
            $metaArgs['KernelSource']   = $kernelMeta.KernelSource
            $metaArgs['KernelBundleId'] = $kernelMeta.KernelBundleId
            $metaArgs['KernelVersion']  = $kernelMeta.KernelVersion
            $metaArgs['KernelProvider'] = $kernelMeta.KernelProvider
        }
        Write-InstallMetadata @metaArgs
    }

    # 7. Summary.
    Write-Phase "Summary"
    Write-Host ""
    if ($DryRun) {
        Write-Host "Dry run complete. Nothing was installed." -ForegroundColor Green
    } else {
        Write-Host "LingTai installed." -ForegroundColor Green
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
    # Same transient-console problem as the failure path: when launched by
    # double-click / shortcut / Start-Process / `curl | iex`, `exit 0` closes the
    # window the instant the install finishes. On a fast machine steps 3-7
    # complete in under a second after the big download, so the window vanishing
    # right after the download reads as a crash ("闪退") even though the
    # install succeeded. Only pause for a real interactive console; piped/
    # automated invocations stay non-blocking.
    if ($Host.Name -eq 'ConsoleHost' -and -not [Console]::IsOutputRedirected) {
        Write-Host ""
        Write-Host "Installation complete. Press Enter to close this window..." -ForegroundColor DarkGray
        [void][Console]::ReadLine()
    }
    exit 0
} catch {
    # The specific fail-loud message was already emitted to the error stream by
    # Fail (or by an unexpected exception). Ensure a non-zero exit; do NOT run any
    # cleanup/removal -- staging is left on disk for evidence/recovery.
    if ($_.Exception -and $_.Exception.Message) {
        Write-Host "error: $($_.Exception.Message)" -ForegroundColor Red
    }
    # Interactive pause: when this script is launched from a transient console
    # (double-click, shortcut, Start-Process, `curl | iex`), the window closes
    # the instant `exit` runs -- the error above vanishes before it can be read
    # and the install looks like a silent crash ("闪退"). Only pause for a real
    # interactive console; piped/automated invocations stay non-blocking.
    if ($Host.Name -eq 'ConsoleHost' -and -not [Console]::IsOutputRedirected) {
        Write-Host ""
        Write-Host "Installation failed. Press Enter to close this window..." -ForegroundColor DarkGray
        [void][Console]::ReadLine()
    }
    exit 1
}
