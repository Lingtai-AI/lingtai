#requires -Version 5.1
<#
.SYNOPSIS
    LingTai native Windows installer for lingtai-tui and its Python runtime.

.DESCRIPTION
    With no explicit version/ref/provider, the TUI and kernel independently
    resolve their latest releases through lingtai.ai. The TUI source archive is
    verified, extracted, and always built locally. The kernel is installed from
    a verified local source archive selected from its release manifest, even
    when compatible wheels are published. A component whose mirror path is
    unavailable falls back to its own latest GitHub release without changing
    the other component's provider.

    Explicit -Version, -Ref, -Latest, -Update, and local -ArchivePath modes are
    preserved. -FromSource selects the GitHub source provider for the TUI; it
    does not select a prebuilt TUI archive. -SkipVenv is the binary-only opt-out.

.PARAMETER Version
    Exact stable release tag to install, such as v0.19.1.

.PARAMETER BinDir
    Directory for the installed lingtai-tui.exe. Defaults to a per-user path.

.PARAMETER GlobalDir
    Directory for installer state and the managed runtime. Defaults to
    %USERPROFILE%\.lingtai-tui.

.PARAMETER ArchivePath
    Explicit local TUI archive (.zip), used with -ChecksumPath and -Version.

.PARAMETER ChecksumPath
    SHA-256 sidecar for -ArchivePath.

.PARAMETER SkipVenv
    Install only the TUI binary; do not provision or advertise a Python runtime.

.PARAMETER NoModifyPath
    Leave the persistent user PATH unchanged.

.PARAMETER NonInteractive
    Do not pause for console input after success or failure.

.PARAMETER FromSource
    Prefer the GitHub TUI source path. This is retained for parity with
    install.sh; stable PowerShell builds are source-only already.

.PARAMETER Source
    TUI source provider: auto|mirror|github. The ordinary no-version path uses
    mirror with a GitHub source fallback.

.PARAMETER Latest
    Build TUI main and install the independently pinned kernel main checkout.

.PARAMETER Ref
    Build an exact TUI branch, tag, or commit. Requires -SkipVenv.

.PARAMETER Update
    Update an existing installation in place from an exact release tag.

.PARAMETER DryRun
    Resolve and validate without binary, PATH, runtime, or receipt writes.
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
    [switch]$NonInteractive,
    [string]$Ref,
    [switch]$FromSource,
    [string]$Source = $env:LINGTAI_SOURCE,
    [switch]$Update,
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
# install.sh's test URL overrides); production explicit-GitHub installs use
# the real GitHub API.
$ApiBase = if ($env:LINGTAI_GITHUB_API_BASE) { $env:LINGTAI_GITHUB_API_BASE } else { "https://api.github.com/repos/$Repo" }
$KernelApiBase = if ($env:LINGTAI_KERNEL_GITHUB_API_BASE) { $env:LINGTAI_KERNEL_GITHUB_API_BASE } else { "https://api.github.com/repos/Lingtai-AI/lingtai-kernel" }

# --- Source provider (default mirror-resolved source; explicit modes use GitHub) ---
$KernelRepo = 'Lingtai-AI/lingtai-kernel'
$MirrorBase = if ($env:LINGTAI_WEB_BASE) { $env:LINGTAI_WEB_BASE.TrimEnd('/') } else { 'https://lingtai.ai' }
$script:TuiProvider = 'mirror'
$script:KernelProvider = 'mirror'
$script:MirrorTuiLatest = $null
$script:MirrorKernelLatest = $null

# --- Output helpers ----------------------------------------------------------

function Write-Info { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Warn { param([string]$Message) Write-Host "warn: $Message" -ForegroundColor Yellow }
function Write-Ok   { param([string]$Message) Write-Host "  ok: $Message" -ForegroundColor Green }
function Write-Step { param([string]$Message) Write-Host "  -> $Message" -ForegroundColor DarkGray }

# --- Progress reporting -------------------------------------------------------

# Long build and runtime phases previously ran with their
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
        [System.Collections.Specialized.OrderedDictionary]$Facts,
        [string]$RuntimeDir = ''
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
    if (-not [string]::IsNullOrWhiteSpace($RuntimeDir)) {
        Write-Host "    runtime    $RuntimeDir"
    }
    Write-Host ''
    Write-Host '  Commands' -ForegroundColor Cyan
    Write-Host '    lingtai-tui       start the terminal UI' -ForegroundColor Green
    Write-Host ''
    Write-Host ('  Total time: {0}' -f (Format-Duration $script:InstallClock.Elapsed)) -ForegroundColor DarkGray
    Write-Host '  This installer PowerShell process is ready to invoke lingtai-tui now.' -ForegroundColor Green
    if ($NoModifyPath) {
        Write-Host "  Persistence was intentionally skipped (-NoModifyPath). If a separate/calling PowerShell process needs PATH, run: `$env:Path = `"$BinDir;`$env:Path`"" -ForegroundColor Yellow
    } else {
        Write-Host '  New PowerShell windows inherit the updated user PATH.' -ForegroundColor Yellow
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

function Resolve-PhysicalDirectoryIdentity {
    param([string]$Directory)

    try {
        $fullDirectory = [IO.Path]::GetFullPath($Directory)
        if ($null -eq ('LingTai.NativePath' -as [type])) {
            Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
using System.Text;
using Microsoft.Win32.SafeHandles;

namespace LingTai {
    public static class NativePath {
        private const uint FileShareRead = 0x00000001;
        private const uint FileShareWrite = 0x00000002;
        private const uint FileShareDelete = 0x00000004;
        private const uint OpenExisting = 3;
        private const uint FileFlagBackupSemantics = 0x02000000;

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern SafeFileHandle CreateFile(
            string fileName,
            uint desiredAccess,
            uint shareMode,
            IntPtr securityAttributes,
            uint creationDisposition,
            uint flagsAndAttributes,
            IntPtr templateFile);

        [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
        private static extern uint GetFinalPathNameByHandle(
            SafeFileHandle file,
            StringBuilder path,
            uint pathLength,
            uint flags);

        public static string GetFinalPathName(string path) {
            using (SafeFileHandle handle = CreateFile(
                path,
                0,
                FileShareRead | FileShareWrite | FileShareDelete,
                IntPtr.Zero,
                OpenExisting,
                FileFlagBackupSemantics,
                IntPtr.Zero)) {
                if (handle.IsInvalid) {
                    throw new Win32Exception(Marshal.GetLastWin32Error());
                }

                StringBuilder buffer = new StringBuilder(32768);
                uint length = GetFinalPathNameByHandle(handle, buffer, (uint)buffer.Capacity, 0);
                if (length == 0) {
                    throw new Win32Exception(Marshal.GetLastWin32Error());
                }
                if (length < buffer.Capacity) {
                    return buffer.ToString();
                }

                buffer = new StringBuilder((int)length + 1);
                length = GetFinalPathNameByHandle(handle, buffer, (uint)buffer.Capacity, 0);
                if (length == 0 || length >= buffer.Capacity) {
                    throw new Win32Exception(Marshal.GetLastWin32Error());
                }
                return buffer.ToString();
            }
        }
    }
}
'@ -ErrorAction Stop | Out-Null
        }
        return [LingTai.NativePath]::GetFinalPathName($fullDirectory)
    } catch {
        Fail "Could not resolve physical PATH directory at $Directory."
    }
}

function Remove-OtherTuiOnPath {
    param([string]$CanonicalPath)

    $canonicalFull = [IO.Path]::GetFullPath($CanonicalPath)
    $canonicalDirectory = [IO.Path]::GetDirectoryName($canonicalFull)
    $canonicalIdentity = Resolve-PhysicalDirectoryIdentity -Directory $canonicalDirectory
    $seen = @{}
    foreach ($rawEntry in @($env:PATH -split ';')) {
        if ([string]::IsNullOrWhiteSpace($rawEntry)) { continue }
        try { $directory = [IO.Path]::GetFullPath($rawEntry.Trim()) } catch { continue }
        if (-not (Test-Path -LiteralPath $directory -PathType Container)) { continue }
        $candidate = Join-Path $directory 'lingtai-tui.exe'
        $candidateFull = [IO.Path]::GetFullPath($candidate)
        $item = Get-Item -LiteralPath $candidate -Force -ErrorAction SilentlyContinue
        if (-not $item -or $item.PSIsContainer) { continue }

        $directoryIdentity = Resolve-PhysicalDirectoryIdentity -Directory $directory
        $directoryKey = $directoryIdentity.ToLowerInvariant()
        if ($seen.ContainsKey($directoryKey)) { continue }
        $seen[$directoryKey] = $true
        if ([StringComparer]::OrdinalIgnoreCase.Equals($directoryIdentity, $canonicalIdentity)) { continue }
        try {
            Remove-Item -LiteralPath $candidate -Force -ErrorAction Stop
        } catch {
            Fail "Could not remove conflicting lingtai-tui.exe at $candidateFull."
        }
        if (Get-Item -LiteralPath $candidate -Force -ErrorAction SilentlyContinue) {
            Fail "Could not remove conflicting lingtai-tui.exe at $candidateFull."
        }
    }
}

# --- install.json metadata ---------------------------------------------------

# Mirror install.sh's install.json shape. install_method is deliberately
# "powershell" (not "source"): the TUI's source updater treats
# install_method="source" as permission to run install.sh through bash, a
# POSIX-only path that does not exist natively on Windows. kernel_* fields are
# written only after a verified runtime install and omitted on -SkipVenv.
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
        [string]$KernelReleaseTag = '',
        [string]$KernelVersion = '',
        [string]$KernelProvider = '',
        [string]$TuiProvider = '',
        [string]$SourceMode = '',
        [string]$TuiCommit = '',
        [string]$KernelCommit = ''
    )
    # The receipt's stamped_version mirrors install.sh exactly: the resolved tag
    # WITH its v prefix (e.g. v0.19.1), because that is what `lingtai-tui version`
    # reports (release.yml builds with -X main.version=$TAG) and what the TUI
    # updater compares against (tui_updater.go compares StampedVersion to the
    # vX.Y.Z GitHub release tag). Stripping the v here previously made both
    # verify.ps1 and the TUI's own updater fail on healthy Windows installs.
    $stamped = $ResolvedRef
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
    if ($TuiProvider) { $meta['tui_provider'] = $TuiProvider }
    if ($KernelSource) {
        $meta['kernel_source']       = $KernelSource
        if ($KernelSource -eq 'release') {
            $meta['kernel_release_tag'] = $KernelReleaseTag
        }
        $meta['kernel_version']      = $KernelVersion
        $meta['kernel_provider']     = $KernelProvider
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
# text, for callers that need a manifest rather than an already-parsed object.
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

# Parse the already-live latest/v1 projection with PowerShell's native JSON
# parser. The returned hashtable binds one tag to its named size/SHA records.
function ConvertFrom-MirrorLatest {
    param([string]$RawJson, [string]$ExpectedRepo)
    try { $data = $RawJson | ConvertFrom-Json } catch { Fail "Invalid lingtai.ai latest metadata: $($_.Exception.Message)" }
    $keys = @($data.psobject.Properties.Name | Sort-Object)
    if (($keys -join ',') -ne 'assets,release_id,schema,source_repo,tag') { Fail 'Invalid lingtai.ai latest metadata shape.' }
    if ($data.schema -ne 'lingtai.release_mirror.latest/v1' -or $data.source_repo -ne $ExpectedRepo) {
        Fail "Invalid lingtai.ai latest metadata schema/source_repo for $ExpectedRepo."
    }
    if ($data.tag -isnot [string] -or $data.tag -notmatch '^v\d+\.\d+\.\d+$') { Fail 'Invalid lingtai.ai latest tag.' }
    try { $releaseId = [int64]$data.release_id } catch { Fail 'Invalid lingtai.ai release_id.' }
    if ($releaseId -le 0) { Fail 'Invalid lingtai.ai release_id.' }
    $assets = @{}
    foreach ($asset in @($data.assets)) {
        $assetKeys = @($asset.psobject.Properties.Name | Sort-Object)
        if (($assetKeys -join ',') -ne 'name,sha256,size') { Fail 'Invalid lingtai.ai asset metadata shape.' }
        if ($asset.name -isnot [string] -or $asset.name -notmatch '^[A-Za-z0-9._+-]+$' -or $assets.ContainsKey($asset.name)) {
            Fail 'Invalid or duplicate lingtai.ai asset name.'
        }
        if ($asset.sha256 -isnot [string] -or $asset.sha256 -notmatch '^[0-9a-f]{64}$') { Fail "Invalid lingtai.ai SHA256 for $($asset.name)." }
        try { $size = [int64]$asset.size } catch { Fail "Invalid lingtai.ai size for $($asset.name)." }
        if ($size -le 0) { Fail "Invalid lingtai.ai size for $($asset.name)." }
        $assets[$asset.name] = @{ Sha256 = $asset.sha256; Size = $size }
    }
    if ($assets.Count -eq 0) { Fail 'Invalid lingtai.ai latest metadata: assets is empty.' }
    return @{ Tag = $data.tag; Assets = $assets }
}

function Get-MirrorLatest {
    param([string]$RequestedRepo)
    if ($RequestedRepo -eq $Repo -and $script:MirrorTuiLatest) { return $script:MirrorTuiLatest }
    if ($RequestedRepo -eq $KernelRepo -and $script:MirrorKernelLatest) { return $script:MirrorKernelLatest }
    $url = "$MirrorBase/dl/$RequestedRepo/latest.json"
    try {
        $raw = Get-TextAssetContent -Url $url
        $latest = ConvertFrom-MirrorLatest -RawJson $raw -ExpectedRepo $RequestedRepo
    } catch {
        Fail "lingtai.ai could not provide valid latest metadata at $url."
    }
    if ($RequestedRepo -eq $Repo) { $script:MirrorTuiLatest = $latest }
    if ($RequestedRepo -eq $KernelRepo) { $script:MirrorKernelLatest = $latest }
    return $latest
}

function Get-MirrorAssetRecord {
    param([string]$RequestedRepo, [string]$Tag, [string]$Name)
    $latest = Get-MirrorLatest -RequestedRepo $RequestedRepo
    if ($latest.Tag -ne $Tag -or -not $latest.Assets.ContainsKey($Name)) {
        Fail "lingtai.ai latest metadata does not select $RequestedRepo/$Tag/$Name."
    }
    return $latest.Assets[$Name]
}

function Get-MirrorAssetBytes {
    param([string]$RequestedRepo, [string]$Tag, [string]$Name)
    $record = Get-MirrorAssetRecord -RequestedRepo $RequestedRepo -Tag $Tag -Name $Name
    $url = "$MirrorBase/dl/$RequestedRepo/$Tag/$Name"
    try { $response = Invoke-WebRequest -Uri $url -UseBasicParsing } catch {
        Fail "Selected lingtai.ai asset failed: $url."
    }
    $bytes = $response.RawContentStream.ToArray()
    if ($bytes.Length -ne $record.Size) { Fail "Size mismatch for selected lingtai.ai asset $url." }
    $hasher = [System.Security.Cryptography.SHA256]::Create()
    try { $actual = ([BitConverter]::ToString($hasher.ComputeHash($bytes))).Replace('-', '').ToLowerInvariant() } finally { $hasher.Dispose() }
    if ($actual -ne $record.Sha256) { Fail "SHA256 mismatch for selected lingtai.ai asset $url." }
    return ,$bytes
}

function Get-MirrorAssetText {
    param([string]$RequestedRepo, [string]$Tag, [string]$Name)
    $bytes = Get-MirrorAssetBytes -RequestedRepo $RequestedRepo -Tag $Tag -Name $Name
    return [System.Text.Encoding]::UTF8.GetString($bytes).TrimStart([char]0xFEFF)
}

function Save-MirrorAsset {
    param([string]$RequestedRepo, [string]$Tag, [string]$Name, [string]$Destination)
    $bytes = Get-MirrorAssetBytes -RequestedRepo $RequestedRepo -Tag $Tag -Name $Name
    [System.IO.File]::WriteAllBytes($Destination, $bytes)
}

function Resolve-PublicTag {
    param([string]$Requested, [string]$Provider = $script:TuiProvider)
    if (-not [string]::IsNullOrWhiteSpace($Requested)) {
        if ($Requested -notmatch '^v\d+\.\d+\.\d+$') { Fail "-Version '$Requested' is not an exact vX.Y.Z release tag." }
        return $Requested
    }
    if ($Provider -eq 'mirror') { return (Get-MirrorLatest -RequestedRepo $Repo).Tag }
    $release = Invoke-GitHubApi -Url "$ApiBase/releases/latest"
    $tag = $release.tag_name
    if ([string]::IsNullOrWhiteSpace($tag) -or $tag -notmatch '^v\d+\.\d+\.\d+$') {
        Fail "Could not resolve an exact latest TUI tag from GitHub (got '$tag')."
    }
    return $tag
}

# Source/provider selection is TUI-only. Kernel resolution has its own provider
# state and fallback loop.
function Resolve-SourceProvider {
    $arg = if ([string]::IsNullOrWhiteSpace($Source)) { 'auto' } else { $Source.ToLowerInvariant() }
    switch ($arg) {
        'mirror' { $script:TuiProvider = 'mirror' }
        'auto'   { $script:TuiProvider = 'mirror' }
        'github' { $script:TuiProvider = 'github' }
        'gitee'  { Fail '-Source gitee is retired. Use -Source mirror or -Source github.' }
        default  { Fail "-Source must be one of mirror|github|auto, got: $Source" }
    }
    if (-not [string]::IsNullOrWhiteSpace($Version) -or $Ref -or $FromSource -or $Update -or $Latest -or $ArchivePath) {
        $script:TuiProvider = 'github'
    }
    $script:KernelProvider = 'mirror'
}

function Get-TuiApiBase { return $ApiBase }
function Get-KernelApiBase { return $KernelApiBase }

function Get-ReleaseAssetUrl {
    param([string]$Tag, [string]$Name)
    $release = Invoke-GitHubApi -Url "$ApiBase/releases/tags/$Tag"
    $asset = $release.assets | Where-Object { $_.name -eq $Name } | Select-Object -First 1
    if (-not $asset) { return $null }
    return $asset.browser_download_url
}

function Get-TuiSourceAssetName {
    param([string]$Tag)
    return "lingtai-$Tag-source.tar.gz"
}

# Prefer refs/tags/<tag>^{} for annotated tags. The direct ref is accepted only
# when no peeled ref exists (a lightweight tag).
function Get-PeeledTagCommit {
    param([string]$Repository, [string]$Tag)
    $line = @(& git ls-remote --tags $Repository "refs/tags/$Tag^{}" 2>$null | Select-Object -First 1)
    $sha = if ($line.Count -gt 0) { ([string]$line[0] -split '\s+')[0] } else { '' }
    if ($sha -notmatch '^[0-9a-f]{40}$') {
        $line = @(& git ls-remote --tags $Repository "refs/tags/$Tag" 2>$null | Select-Object -First 1)
        $sha = if ($line.Count -gt 0) { ([string]$line[0] -split '\s+')[0] } else { '' }
    }
    if ($sha -notmatch '^[0-9a-f]{40}$') { Fail "Could not resolve the peeled commit for TUI tag $Tag." }
    return $sha
}

function Save-TuiSourceArchive {
    param([string]$Tag, [string]$Provider, [string]$Destination)
    $name = Get-TuiSourceAssetName -Tag $Tag
    if ($Provider -eq 'mirror') {
        Save-MirrorAsset -RequestedRepo $Repo -Tag $Tag -Name $name -Destination $Destination
        return
    }
    $url = Get-ReleaseAssetUrl -Tag $Tag -Name $name
    if (-not $url) { Fail "GitHub release $Tag has no producer-owned source archive $name." }
    $sidecarUrl = Get-ReleaseAssetUrl -Tag $Tag -Name "$name.sha256"
    if (-not $sidecarUrl) { Fail "GitHub release $Tag has no checksum for $name." }
    Invoke-WebRequest -UseBasicParsing -Uri $url -OutFile $Destination
    $sidecar = "$Destination.sha256"
    Invoke-WebRequest -UseBasicParsing -Uri $sidecarUrl -OutFile $sidecar
    Confirm-ArchiveChecksum -ArchiveFile $Destination -SidecarFile $sidecar
}

# --- Kernel release manifest (schema lingtai.kernel.release/v1) --------------

# Confirm-KernelManifest strictly validates the kernel release manifest: exact
# top-level and artifact shapes, matching tag/version, nonempty commit/time
# provenance, and self-consistent wheel/sdist records. Returns the parsed
# object on success.
function Confirm-KernelManifest {
    param([string]$RawJson, [string]$ExpectedKernelTag)
    try {
        $data = $RawJson | ConvertFrom-Json
    } catch {
        Fail "invalid kernel release manifest: could not parse JSON ($($_.Exception.Message))"
    }
    if ($null -eq $data) {
        Fail 'invalid kernel release manifest: top-level value must be an object'
    }
    $topKeys = @($data.psobject.Properties.Name | Sort-Object)
    if (($topKeys -join ',') -ne 'artifacts,commit,generated_at,kernel_tag,kernel_version,schema,sdist_fallback') {
        Fail 'invalid kernel release manifest: unexpected top-level keys'
    }
    if ($data.schema -ne 'lingtai.kernel.release/v1') {
        Fail "invalid kernel release manifest: unexpected schema '$($data.schema)'"
    }
    foreach ($key in @('kernel_version', 'kernel_tag', 'commit', 'generated_at', 'sdist_fallback')) {
        $value = $data.$key
        if ($value -isnot [string] -or [string]::IsNullOrEmpty($value)) {
            Fail "invalid kernel release manifest: $key must be a nonempty string"
        }
    }
    $expectedVersion = $ExpectedKernelTag -replace '^v', ''
    if ($data.kernel_tag -ne $ExpectedKernelTag -or $data.kernel_version -ne $expectedVersion) {
        Fail "invalid kernel release manifest: manifest is for $($data.kernel_tag)/$($data.kernel_version), expected $ExpectedKernelTag/$expectedVersion"
    }
    if ($data.artifacts -isnot [array] -or @($data.artifacts).Count -eq 0) {
        Fail 'invalid kernel release manifest: artifacts must be a nonempty list'
    }

    $seen = @{}
    $hasSdist = $false
    $hasDeclaredSdist = $false
    foreach ($art in @($data.artifacts)) {
        if ($null -eq $art) {
            Fail 'invalid kernel release manifest: artifact must be an object'
        }
        $artifactKeys = @($art.psobject.Properties.Name | Sort-Object)
        if (($artifactKeys -join ',') -ne 'abi_tag,filename,kind,platform_tag,python_tag,sha256') {
            Fail 'invalid kernel release manifest: artifact has the wrong shape'
        }
        $filename = $art.filename
        if ($filename -isnot [string] -or [string]::IsNullOrEmpty($filename) -or $seen.ContainsKey($filename)) {
            Fail "invalid kernel release manifest: artifact has an invalid or duplicate filename '$filename'"
        }
        $seen[$filename] = $true
        if ($art.sha256 -isnot [string] -or $art.sha256 -notmatch '^[0-9a-f]{64}$') {
            Fail "invalid kernel release manifest: artifact '$filename' has a malformed sha256"
        }
        if ($art.kind -eq 'wheel') {
            if ($art.python_tag -isnot [string] -or [string]::IsNullOrEmpty($art.python_tag) -or
                $art.abi_tag -isnot [string] -or [string]::IsNullOrEmpty($art.abi_tag) -or
                $art.platform_tag -isnot [string] -or [string]::IsNullOrEmpty($art.platform_tag)) {
                Fail "invalid kernel release manifest: wheel artifact '$filename' has empty tags"
            }
            $parts = if ($filename -match '\.whl$') { $filename.Substring(0, $filename.Length - 4).Split('-') } else { @() }
            if ($parts.Count -ne 5 -or $parts[0] -ne 'lingtai' -or $parts[1] -ne $expectedVersion -or
                $parts[2] -ne $art.python_tag -or $parts[3] -ne $art.abi_tag -or $parts[4] -ne $art.platform_tag) {
                Fail "invalid kernel release manifest: wheel artifact '$filename' disagrees with its tags"
            }
        } elseif ($art.kind -eq 'sdist') {
            $hasSdist = $true
            if ($filename -eq $data.sdist_fallback) { $hasDeclaredSdist = $true }
            if ($filename -ne "lingtai-$expectedVersion.tar.gz") {
                Fail "invalid kernel release manifest: sdist artifact '$filename' is not the selected version"
            }
            if ($null -ne $art.python_tag -or $null -ne $art.abi_tag -or $null -ne $art.platform_tag) {
                Fail "invalid kernel release manifest: sdist artifact '$filename' has wheel tags"
            }
        } else {
            Fail "invalid kernel release manifest: artifact '$filename' has unsupported kind '$($art.kind)'"
        }
    }
    if (-not $hasSdist -or -not $hasDeclaredSdist -or $data.sdist_fallback -notmatch '\.tar\.gz$') {
        Fail 'invalid kernel release manifest: sdist_fallback is not a listed sdist'
    }
    return $data
}

# Get-KernelAssetUrl returns the browser_download_url for a named asset on the
# pinned kernel release, or $null if that release has no such asset -- the
# kernel-repo analogue of Get-ReleaseAssetUrl for mirror or GitHub.
function Get-KernelAssetUrl {
    param([string]$KernelTag, [string]$Name, [string]$Provider = $script:KernelProvider)
    if ($Provider -eq 'mirror') {
        Get-MirrorAssetRecord -RequestedRepo $KernelRepo -Tag $KernelTag -Name $Name | Out-Null
        return "$MirrorBase/dl/$KernelRepo/$KernelTag/$Name"
    }
    $release = Invoke-GitHubApi -Url "$KernelApiBase/releases/tags/$KernelTag"
    $asset = $release.assets | Where-Object { $_.name -eq $Name } | Select-Object -First 1
    if (-not $asset) { return $null }
    return $asset.browser_download_url
}


# Get-KernelManifest fetches and validates one exact kernel release manifest.
function Get-KernelManifest {
    param([string]$KernelTag, [string]$ManifestFilename, [string]$Provider = $script:KernelProvider)
    if ($Provider -eq 'mirror') {
        $raw = Get-MirrorAssetText -RequestedRepo $KernelRepo -Tag $KernelTag -Name $ManifestFilename
        return Confirm-KernelManifest -RawJson $raw -ExpectedKernelTag $KernelTag
    }
    $url = Get-KernelAssetUrl -KernelTag $KernelTag -Name $ManifestFilename -Provider $Provider
    if (-not $url) { Fail "Kernel release $KernelTag has no $ManifestFilename asset." }
    $raw = Get-TextAssetContent -Url $url
    return Confirm-KernelManifest -RawJson $raw -ExpectedKernelTag $KernelTag
}

function Resolve-KernelLatestTag {
    param([string]$Provider)
    if ($Provider -eq 'mirror') { return (Get-MirrorLatest -RequestedRepo $KernelRepo).Tag }
    $release = Invoke-GitHubApi -Url "$KernelApiBase/releases/latest"
    $tag = $release.tag_name
    if ([string]::IsNullOrWhiteSpace($tag) -or $tag -notmatch '^v\d+\.\d+\.\d+$') {
        Fail "Could not resolve an exact latest kernel tag from GitHub (got '$tag')."
    }
    return $tag
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
the TUI binary only.
"@
}

# Get-KernelSourceArtifact returns the exact sdist named by the existing
# release-manifest `sdist_fallback` field. Wheel records remain valid manifest
# data, but are deliberately not candidates for the stable/default installer.
function Get-KernelSourceArtifact {
    param($KernelManifest)
    foreach ($art in @($KernelManifest.artifacts)) {
        if ($art.kind -eq 'sdist' -and $art.filename -eq $KernelManifest.sdist_fallback) {
            return $art
        }
    }
    Fail "Kernel release $($KernelManifest.kernel_tag) has no declared sdist source artifact."
}

# Install-KernelSource downloads the declared source archive, verifies its
# manifest digest, and gives its local path to pip so pip builds/installs it.
# LingTai's own bytes are NEVER requested from a package index by name.
function Install-KernelSource {
    param([string]$VenvPython, $SourceArtifact, [string]$KernelTag, [string]$StageDir, [string]$Provider)

    $downloadUrl = Get-KernelAssetUrl -KernelTag $KernelTag -Name $SourceArtifact.filename -Provider $Provider
    if (-not $downloadUrl) { Fail "Kernel release $KernelTag has no $($SourceArtifact.filename) asset even though its manifest references it." }
    $dest = Join-Path $StageDir $SourceArtifact.filename
    Write-Info "Downloading kernel source archive: $($SourceArtifact.filename) (kernel $KernelTag) ..."
    if ($Provider -eq 'mirror') {
        Save-MirrorAsset -RequestedRepo $KernelRepo -Tag $KernelTag -Name $SourceArtifact.filename -Destination $dest
    } else {
        try {
            Invoke-WebRequest -Uri $downloadUrl -OutFile $dest -UseBasicParsing
        } catch {
            Fail "Download failed for $downloadUrl ($($_.Exception.Message))"
        }
    }
    $actual = Get-Sha256Hex -Path $dest
    if ($actual -ne $SourceArtifact.sha256) {
        Fail "Checksum mismatch for $($SourceArtifact.filename). Expected $($SourceArtifact.sha256) but got $actual. Refusing to install an unverified kernel source archive. Retained at $dest for diagnosis."
    }
    Write-Ok "Verified SHA-256 for $($SourceArtifact.filename)"

    # Explicit local path: pip never requests the package name "lingtai" from
    # any index here -- only third-party dependency resolution uses an index.
    Write-Info "Building and installing lingtai from the verified local source archive (dependencies resolve via the configured package index) ..."
    # pip's stdout is voided (Out-Null), not just left to print: PowerShell
    # has no per-statement return-value isolation, so an unsuppressed native
    # command's stdout becomes part of this function's own output and, from
    # there, leaks into any caller that bare-calls it. $LASTEXITCODE remains
    # available after Out-Null.
    & $VenvPython '-m' 'pip' 'install' $dest | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "pip install of the local source archive failed (exit $LASTEXITCODE)." }
}

# Confirm-KernelImport preserves the release-source import/version/non-editable
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
    $mode = if ($strictSource) { 'source' } else { 'release' }
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
if mode not in ('release', 'source'):
    fail('INVALID_VERIFICATION_MODE', mode)

module_value = getattr(lingtai, '__file__', None)
module_path = Path(module_value).resolve() if module_value else None
dist = resolve_lingtai_dist(module_path)
version = dist.version
if expected_version and version != expected_version:
    fail('VERSION_MISMATCH', str(version))

direct_source = '<release-source>'
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
        fail('DIRECT_URL_EDITABLE', 'release source install is editable')

print('OK')
print(str(version))
print(str(module_path) if module_path is not None else '<unknown>')
print(str(direct_source))
'@
    $probe = @(& $VenvPython '-c' $probeScript $mode $expectedArg $venvArg $sourceArg 2>$null)
    if ($LASTEXITCODE -ne 0 -or $probe.Count -lt 4 -or $probe[0].Trim() -ne 'OK') {
        $kind = if ($strictSource) { 'pinned main kernel source' } else { 'release kernel source archive' }
        Fail "Post-install verification failed for the $kind ($($probe -join '; '))"
    }
    $installedVersion = $probe[1].Trim()
    if ($ExpectedVersion -and $installedVersion -ne $ExpectedVersion) {
        Fail "Installed lingtai version '$installedVersion' does not match the pinned kernel manifest version '$ExpectedVersion'."
    }
    if ($strictSource) {
        Write-Ok "Verified lingtai $installedVersion imports from the managed venv and matches the non-editable pinned kernel source."
    } else {
        Write-Ok "Verified lingtai $installedVersion imports and is a non-editable release source install."
    }
    return $installedVersion
}

# Write-KernelProvenance writes an additive provenance stamp alongside the
# venv (not a new runtime protocol) recording exactly what was installed.
function Write-KernelProvenance {
    param(
        [string]$VenvDir,
        [string]$KernelTag,
        [string]$KernelVersion,
        [string]$SourceFilename,
        [string]$SourceSha256,
        [string]$Provider
    )
    $provenance = [ordered]@{
        schema          = 'lingtai.tui.kernel-provenance/v1'
        kernel_tag      = $KernelTag
        kernel_version  = $KernelVersion
        source_filename = $SourceFilename
        source_sha256   = $SourceSha256
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
# re-install with the TUI open discarded a completed build -- several
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
Close lingtai-tui and re-run; the completed build is kept and the next run reuses it.
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

# Install-Venv resolves the kernel independently of the TUI. The ordinary path
# tries lingtai.ai latest first and falls back only this component to GitHub.
# Every artifact is verified before pip receives its local path.
function Install-Venv {
    param([string]$GlobalDir)

    $venvDir = Join-Path $GlobalDir 'runtime\venv'
    # Every native-command/void-intent call below is piped to Out-Null (see
    # Install-KernelSource for why: leaked stdout here previously corrupted
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

    $failures = New-Object System.Collections.Generic.List[string]
    foreach ($provider in @('mirror', 'github')) {
        try {
            $script:KernelProvider = $provider
            $kernelTag = Resolve-KernelLatestTag -Provider $provider
            $manifestName = 'lingtai-kernel-release-manifest.json'
            $manifest = Get-KernelManifest -KernelTag $kernelTag -ManifestFilename $manifestName -Provider $provider
            $sourceArtifact = Get-KernelSourceArtifact -KernelManifest $manifest
            $stage = New-StagingDir
            Install-KernelSource -VenvPython $venvPython -SourceArtifact $sourceArtifact -KernelTag $kernelTag -StageDir $stage -Provider $provider | Out-Null
            $installedVersion = Confirm-KernelImport -VenvPython $venvPython -ExpectedVersion $manifest.kernel_version
            Write-KernelProvenance -VenvDir $venvDir -KernelTag $kernelTag -KernelVersion $installedVersion `
                -SourceFilename $sourceArtifact.filename -SourceSha256 $sourceArtifact.sha256 -Provider $provider | Out-Null
            return @{
                KernelSource     = 'release'
                KernelReleaseTag = $kernelTag
                KernelVersion    = $installedVersion
                KernelProvider   = $provider
            }
        } catch {
            $failures.Add("${provider}: $($_.Exception.Message)") | Out-Null
            if ($provider -eq 'mirror') {
                Write-Warn 'lingtai.ai kernel release is unavailable; falling back to the latest GitHub kernel release.'
            }
        }
    }
    Fail "Could not install a verified latest kernel release ($($failures -join '; '))."
}

# --- Mainland-China build mirrors --------------------------------------------

# Initialize-BuildMirrors is the native-Windows counterpart to install.sh's
# CN-restricted-network fallback, which install.ps1 previously had no equivalent
# of at all -- so a mainland-China -Latest install fetched Go modules from
# proxy.golang.org and simply hung
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
#     GOPROXY/GOSUMDB/LINGTAI_PYPI_INDEX_URL already stated
#     their intent; install.sh keys that decision off GOPROXY and so does this.
#   * LINGTAI_ASSUME_CN=1 forces the mirror set with no probe, for hosts with no
#     outbound access to the probe URL at all.
#
# Returns the PyPI index URL the pip steps should use, or $null for pip's
# default. Go reads its mirror settings from the process environment.
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
    # Same mirror install.sh's PYPI_INDEX_URL_GITEE_DEFAULT uses, so a CN host
    # resolves Python dependencies from a reachable index too. LingTai's own
    # bytes are still never fetched from an index by name -- only the local
    # source-archive/checkout path is installed, and this affects its dependencies only.
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
# terminal went silent for minutes across native builds with no
# indication of what was running, and a failure surfaced ONLY as "(exit N)" --
# the compiler diagnostic that actually said why had been discarded, so the
# error named the step but never the cause. Output now lands in $LogPath, the
# tail is printed on failure, and the full log is kept for inspection.
#
# stderr is merged with `2>&1` so a failure's real diagnostic is captured, and
# that REQUIRES relaxing $ErrorActionPreference around the call: on Windows
# PowerShell 5.1 any text a native command writes to stderr becomes a
# NativeCommandError under the script's fail-loud 'Stop' policy, which would
# abort on native-command progress chatter that is not an error. The real exit
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
    param([switch]$SkipBootstrap, [switch]$SkipPython)
    $packages = [ordered]@{
        git    = 'Git.Git'
        go     = 'GoLang.Go'
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

    $python = $null
    if (-not $SkipPython) {
        $pythonDiscovery = Get-SupportedVenvPythonDiscovery
        $python = $pythonDiscovery.Python
        $status.python = [ordered]@{ Command = 'py/python'; Package = $packages.python; Valid = [bool]$python; Detail = if ($python) { "launcher $($python.Launcher)" } else { $pythonDiscovery.Detail } }
        if (-not $python) { $deprioritizedPathDirs += @($pythonDiscovery.InvalidDirectories) }
    }

    $missing = @($status.GetEnumerator() | Where-Object { -not $_.Value.Valid })
    if ($missing.Count -eq 0) {
        $pythonDetail = if ($SkipPython) { 'Python not required for this TUI-only build' } else { "Python launcher $($python.Launcher)" }
        Write-Ok "Using build prerequisites: $goVersion; $pythonDetail"
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
    $rechecked = Confirm-DevPrerequisitesAfterBootstrap -SkipPython:$SkipPython
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
    param([switch]$SkipPython)
    return (Confirm-DevPrerequisites -SkipBootstrap -SkipPython:$SkipPython)
}

# Current-main mode independently pins the TUI and kernel main commits.
function Build-LatestMain {
    $phase = Start-Phase 'Checking build prerequisites (git, Go, CPython 3.11-3.13) ...'
    $prerequisites = Confirm-DevPrerequisites
    if ($prerequisites.Deferred) { return @{ DryRun = $true; PrerequisitesDeferred = $true } }
    Complete-Phase -Clock $phase -Message 'prerequisites satisfied'
    $tuiSha = Resolve-MainSha -RemoteUrl $RepoUrl -Label 'TUI'
    $kernelSha = Resolve-MainSha -RemoteUrl 'https://github.com/Lingtai-AI/lingtai-kernel.git' -Label 'kernel'
    Write-Info "Resolved TUI main commit: $tuiSha"
    Write-Info "Resolved kernel main commit: $kernelSha"
    if ($DryRun) {
        Write-Step '[dry-run] would shallow-checkout both pinned main commits and build lingtai-tui.exe'
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

    $phase = Start-Phase 'Compiling lingtai-tui.exe ...'
    Push-Location (Join-Path $tuiSource 'tui')
    try { Invoke-NativeBuild -Tool 'go' -Arguments @('build','-trimpath','-ldflags',"-X main.version=$version",'-o',$tuiOut,'.') -Failure 'lingtai-tui.exe build failed' -LogPath $buildLog }
    finally { Pop-Location }
    Complete-Phase -Clock $phase -Message 'lingtai-tui.exe compiled'

    Confirm-StagedVersion -StagedTui $tuiOut -Requested $version
    return @{ Stage = $stage; TuiSource = $tuiSource; KernelSource = $kernelSource; TuiSha = $tuiSha; KernelSha = $kernelSha; Version = $version; Tui = $tuiOut; DryRun = $false }
}

# Resolve-RefSha resolves an arbitrary git ref (branch/tag/commit) to a full
# 40-hex commit SHA over the remote, mirroring install.sh --ref source builds.
# A bare 40-hex commit is accepted as-is; anything else is tried as a branch
# and then a tag. Fails loud on any unresolved/invalid ref.
function Resolve-RefSha {
    param([string]$RemoteUrl, [string]$Label, [string]$Ref)
    if ($Ref -match '^[0-9a-fA-F]{40}$') { return $Ref.ToLowerInvariant() }
    $savedErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        foreach ($candidate in @("refs/heads/$Ref", "refs/tags/$Ref^{}", "refs/tags/$Ref")) {
            $lines = & git ls-remote $RemoteUrl $candidate 2>$null
            $gitExit = $LASTEXITCODE
            if ($gitExit -eq 0 -and $lines) {
                $sha = (($lines | Select-Object -First 1) -split '\s+' | Select-Object -First 1)
                if ($sha -match '^[0-9a-fA-F]{40}$') { return $sha.ToLowerInvariant() }
            }
        }
    } finally {
        $ErrorActionPreference = $savedErrorActionPreference
    }
    Fail "Could not resolve $Label ref '$Ref' from $RemoteUrl (tried branch and peeled tag). Install Git and verify network access."
}

# Build-SourceRef is the GitHub checkout path for explicit refs and the fallback
# for a GitHub release that predates the producer-owned source archive.
function Build-SourceRef {
    param([string]$Ref, [switch]$SkipPython)
    $phase = Start-Phase 'Checking TUI build prerequisites (git, Go; Python when runtime is enabled) ...'
    $prerequisites = Confirm-DevPrerequisites -SkipPython:$SkipPython
    if ($prerequisites.Deferred) { return @{ DryRun = $true; PrerequisitesDeferred = $true } }
    Complete-Phase -Clock $phase -Message 'prerequisites satisfied'
    $tuiSha = Resolve-RefSha -RemoteUrl $RepoUrl -Label 'TUI' -Ref $Ref
    Write-Info "Resolved TUI ref '$Ref' commit: $tuiSha"
    if ($DryRun) {
        Write-Step "[dry-run] would shallow-checkout TUI ref '$Ref' and build lingtai-tui.exe"
        return @{ TuiSha = $tuiSha; KernelSha = ''; DryRun = $true }
    }

    $stage = New-StagingDir
    $tuiSource = Join-Path $stage 'lingtai'
    $buildLog = Join-Path $stage 'build.log'
    Write-Step "Build log: $buildLog"

    $phase = Start-Phase "Checking out TUI ref '$Ref' ..."
    Invoke-NativeBuild -Tool 'git' -Arguments @('clone','--filter=blob:none','--no-checkout',$RepoUrl,$tuiSource) -Failure "TUI repository checkout failed" -LogPath $buildLog
    Invoke-NativeBuild -Tool 'git' -Arguments @('-C',$tuiSource,'fetch','--depth','1','origin',$tuiSha) -Failure "TUI ref '$Ref' pinned commit fetch failed" -LogPath $buildLog
    Invoke-NativeBuild -Tool 'git' -Arguments @('-C',$tuiSource,'checkout','--detach',$tuiSha) -Failure "TUI ref '$Ref' pinned checkout failed" -LogPath $buildLog
    $actualTui = (& git -C $tuiSource rev-parse HEAD).Trim().ToLowerInvariant()
    if ($actualTui -ne $tuiSha) { Fail "TUI checkout mismatch: resolved $tuiSha but checked out $actualTui. Staging kept at $stage." }
    Complete-Phase -Clock $phase -Message "TUI at $($tuiSha.Substring(0,12))"

    $version = if ($Ref -match '^v\d+\.\d+\.\d+$') { $Ref } else { "$Ref-$tuiSha" }
    $tuiOut = Join-Path $stage 'lingtai-tui.exe'

    $phase = Start-Phase 'Compiling lingtai-tui.exe ...'
    Push-Location (Join-Path $tuiSource 'tui')
    try { Invoke-NativeBuild -Tool 'go' -Arguments @('build','-trimpath','-ldflags',"-X main.version=$version",'-o',$tuiOut,'.') -Failure 'lingtai-tui.exe build failed' -LogPath $buildLog }
    finally { Pop-Location }
    Complete-Phase -Clock $phase -Message 'lingtai-tui.exe compiled'

    Confirm-StagedVersion -StagedTui $tuiOut -Requested $version
    return @{ Stage = $stage; TuiSource = $tuiSource; KernelSource = ''; TuiSha = $tuiSha; KernelSha = ''; Version = $version; Tui = $tuiOut; DryRun = $false }
}

function Build-ReleaseSourceArchive {
    param([string]$Tag, [string]$Provider, [switch]$SkipPython)
    $phase = Start-Phase 'Checking TUI build prerequisites (git, Go; Python when runtime is enabled) ...'
    $prerequisites = Confirm-DevPrerequisites -SkipPython:$SkipPython
    if ($prerequisites.Deferred) { return @{ DryRun = $true; PrerequisitesDeferred = $true } }
    Complete-Phase -Clock $phase -Message 'prerequisites satisfied'
    if ($DryRun) {
        Write-Step "[dry-run] would verify, extract, and build the producer-owned TUI source archive for $Tag"
        return @{ TuiSha = ''; KernelSha = ''; Version = $Tag; DryRun = $true }
    }

    $stage = New-StagingDir
    $source = Join-Path $stage 'lingtai'
    $archive = Join-Path $stage (Get-TuiSourceAssetName -Tag $Tag)
    $buildLog = Join-Path $stage 'build.log'
    New-Item -ItemType Directory -Force -Path $source | Out-Null
    Write-Step "Build log: $buildLog"

    $phase = Start-Phase "Downloading and verifying TUI source $Tag ..."
    Save-TuiSourceArchive -Tag $Tag -Provider $Provider -Destination $archive
    $tuiSha = if ($Provider -eq 'github') { Get-PeeledTagCommit -Repository $RepoUrl -Tag $Tag } else { '' }
    Complete-Phase -Clock $phase -Message 'source archive verified'

    $phase = Start-Phase 'Extracting TUI source archive ...'
    Invoke-NativeBuild -Tool 'tar' -Arguments @('-xzf',$archive,'-C',$source,'--strip-components','1') -Failure 'TUI source archive extraction failed' -LogPath $buildLog
    Complete-Phase -Clock $phase -Message 'source archive extracted'

    $tuiOut = Join-Path $stage 'lingtai-tui.exe'
    $phase = Start-Phase 'Compiling lingtai-tui.exe ...'
    Push-Location (Join-Path $source 'tui')
    try { Invoke-NativeBuild -Tool 'go' -Arguments @('build','-trimpath','-ldflags',"-X main.version=$Tag",'-o',$tuiOut,'.') -Failure 'lingtai-tui.exe build failed' -LogPath $buildLog }
    finally { Pop-Location }
    Complete-Phase -Clock $phase -Message 'lingtai-tui.exe compiled'
    Confirm-StagedVersion -StagedTui $tuiOut -Requested $Tag
    return @{ Stage = $stage; TuiSource = $source; KernelSource = ''; TuiSha = $tuiSha; KernelSha = ''; Version = $Tag; Tui = $tuiOut; DryRun = $false }
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
    param([hashtable]$Build, [string]$BinDir, [string]$Label = 'pinned main')
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    $tuiDest = Join-Path $BinDir 'lingtai-tui.exe'
    Remove-ParkedManagedBinaries -BinDir $BinDir
    Copy-ManagedBinary -Source $Build.Tui -Destination $tuiDest
    Confirm-StagedVersion -StagedTui $tuiDest -Requested $Build.Version
    Write-Ok "Installed $Label TUI binary into $BinDir"
    return @($tuiDest)
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
        Write-Step "[dry-run] would install lingtai-tui.exe into $BinDir"
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

    # 5. Install idempotently into BinDir.
    New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
    Remove-ParkedManagedBinaries -BinDir $BinDir
    $tuiDest = Join-Path $BinDir 'lingtai-tui.exe'
    Copy-ManagedBinary -Source $tui.FullName -Destination $tuiDest
    Confirm-StagedVersion -StagedTui $tuiDest -Requested $Requested
    Write-Ok "Installed lingtai-tui.exe -> $BinDir"

    return @($tuiDest)
}

# --- Main --------------------------------------------------------------------

function Invoke-Main {
    Write-Host ''
    Write-Host 'LingTai -- native Windows installer' -ForegroundColor Magenta
    Write-Host '------------------------------------' -ForegroundColor Magenta
    if ($DryRun) { Write-Warn 'DRY RUN: no filesystem, PATH, runtime, or receipt writes will be made.' }

    # Validate public option values before selecting a provider or resolving any
    # remote metadata. Keep this direct so an invalid -Source cannot influence
    # provider behavior through a later helper.
    $sourceArg = if ([string]::IsNullOrWhiteSpace($Source)) { 'auto' } else { $Source.ToLowerInvariant() }
    if ($sourceArg -eq 'gitee') { Fail '-Source gitee is retired. Use -Source mirror or -Source github.' }
    if ($sourceArg -notin @('auto', 'mirror', 'github')) {
        Fail "-Source must be one of mirror|github|auto, got: $Source"
    }

    if (-not $Update -and [string]::IsNullOrWhiteSpace($BinDir)) { $BinDir = Get-DefaultBinDir }
    if ([string]::IsNullOrWhiteSpace($GlobalDir)) { $GlobalDir = Get-DefaultGlobalDir }

    $haveArchive = -not [string]::IsNullOrWhiteSpace($ArchivePath)
    $haveChecksum = -not [string]::IsNullOrWhiteSpace($ChecksumPath)
    if ($haveArchive -ne $haveChecksum) { Fail '-ArchivePath and -ChecksumPath must be provided together.' }
    if ($haveArchive -and [string]::IsNullOrWhiteSpace($Version)) {
        Fail '-Version is required with -ArchivePath so staged bytes can be checked against an exact release.'
    }

    if ($Update) {
        if ($Latest -or $Ref -or $haveArchive) { Fail '-Update cannot be combined with -Latest, -Ref, or -ArchivePath/-ChecksumPath.' }
        if ([string]::IsNullOrWhiteSpace($Version)) { Fail '-Update requires -Version <release-tag>.' }
        if ([string]::IsNullOrWhiteSpace($BinDir)) {
            $receiptPath = Join-Path $GlobalDir 'install.json'
            if (-not (Test-Path -LiteralPath $receiptPath)) { Fail "-Update requires an existing install receipt at $receiptPath." }
            $existing = Get-Content -LiteralPath $receiptPath -Raw | ConvertFrom-Json
            if ([string]::IsNullOrWhiteSpace($existing.bin_dir)) {
                Fail "Existing install receipt at $receiptPath has no bin_dir; pass -BinDir explicitly."
            }
            $BinDir = $existing.bin_dir
            Write-Step "Update target BinDir (from receipt): $BinDir"
        }
        if (-not (Test-Path -LiteralPath $BinDir)) { Fail "Update target bin dir does not exist: $BinDir" }
    }

    $prefix = Split-Path $BinDir -Parent
    if ([string]::IsNullOrWhiteSpace($prefix)) { $prefix = $BinDir }

    if ($Ref) {
        $conflicts = New-Object System.Collections.Generic.List[string]
        if ($Latest) { $conflicts.Add('-Latest') }
        if ($Update) { $conflicts.Add('-Update') }
        if ($Version) { $conflicts.Add('-Version/LINGTAI_VERSION') }
        if ($haveArchive) { $conflicts.Add('-ArchivePath/-ChecksumPath') }
        if ($conflicts.Count -gt 0) { Fail "-Ref cannot be combined with $($conflicts -join ', ')." }
        if (-not $SkipVenv) { Fail '-Ref has no kernel selection. Pass -SkipVenv to install only the exact TUI ref.' }
        Set-PhaseTotal $(if ($DryRun) { 1 } else { 3 })
        $refBuild = Build-SourceRef -Ref $Ref -SkipPython:$SkipVenv
        if ($DryRun) {
            Write-Step "[dry-run] would install the built TUI into $BinDir and write source-ref provenance."
            return
        }
        $managed = Install-FromBuiltMain -Build $refBuild -BinDir $BinDir -Label "ref $Ref"
        Add-ToPath -Dir $BinDir
        $metaArgs = @{
            GlobalDir=$GlobalDir; Prefix=$prefix; BinDir=$BinDir; RequestedRef=$Ref; ResolvedRef=$Ref
            ResolvedCommit=$refBuild.TuiSha; InstallKind='powershell-source-ref'; ManagedBinaries=$managed
            TuiProvider='github'; SourceMode='source-ref'; TuiCommit=$refBuild.TuiSha
        }
        Write-InstallMetadata @metaArgs
        Remove-OtherTuiOnPath -CanonicalPath (Join-Path $BinDir 'lingtai-tui.exe')
        Write-Completion -BinDir $BinDir -GlobalDir $GlobalDir -Headline "Source build of '$Ref' complete." -Facts ([ordered]@{
            'TUI commit' = $refBuild.TuiSha
            'stamped as' = $refBuild.Version
            'build log'  = (Join-Path $refBuild.Stage 'build.log')
        })
        return
    }

    if ($Latest) {
        $conflicts = New-Object System.Collections.Generic.List[string]
        if ($haveArchive) { $conflicts.Add('-ArchivePath/-ChecksumPath') }
        if ($Version) { $conflicts.Add('-Version/LINGTAI_VERSION') }
        if ($SkipVenv) { $conflicts.Add('-SkipVenv') }
        if ($FromSource) { $conflicts.Add('-FromSource') }
        if ($conflicts.Count -gt 0) { Fail "-Latest cannot be combined with $($conflicts -join ', ')." }
        Set-PhaseTotal $(if ($DryRun) { 1 } else { 5 })
        $pythonIndexUrl = if ($DryRun) { $null } else { Initialize-BuildMirrors }
        $mainBuild = Build-LatestMain
        if ($DryRun) {
            Write-Step "[dry-run] would install kernel main into $GlobalDir\runtime\venv and the TUI into $BinDir."
            return
        }
        $mainKernel = Install-MainVenv -KernelSource $mainBuild.KernelSource -KernelSha $mainBuild.KernelSha -GlobalDir $GlobalDir -PythonIndexUrl $pythonIndexUrl
        $managed = Install-FromBuiltMain -Build $mainBuild -BinDir $BinDir
        Add-ToPath -Dir $BinDir
        $metaArgs = @{
            GlobalDir=$GlobalDir; Prefix=$prefix; BinDir=$BinDir; RequestedRef='main'; ResolvedRef='main'
            ResolvedCommit=$mainBuild.TuiSha; InstallKind='powershell-latest-main'; ManagedBinaries=$managed
            KernelSource=$mainKernel.KernelSource; KernelVersion=$mainKernel.KernelVersion
            KernelProvider=$mainKernel.KernelProvider; TuiProvider='github'; SourceMode='latest-main'
            TuiCommit=$mainBuild.TuiSha; KernelCommit=$mainBuild.KernelSha
        }
        Write-InstallMetadata @metaArgs
        Remove-OtherTuiOnPath -CanonicalPath (Join-Path $BinDir 'lingtai-tui.exe')
        Write-Completion -BinDir $BinDir -GlobalDir $GlobalDir -Headline 'Current-main development install complete.' -Facts ([ordered]@{
            'TUI commit' = $mainBuild.TuiSha
            'kernel commit' = $mainBuild.KernelSha
            'kernel version' = $mainKernel.KernelVersion
            'stamped as' = $mainBuild.Version
            'build log' = (Join-Path $mainBuild.Stage 'build.log')
        }) -RuntimeDir (Join-Path $GlobalDir 'runtime\venv')
        return
    }

    # A runtime without the current install receipt is not a supported legacy
    # layout. Do not migrate or adopt it: ignore an old caller's pinned release
    # and run the ordinary latest-release installer path noninteractively.
    $receiptPath = Join-Path $GlobalDir 'install.json'
    $runtimeRoot = Join-Path $GlobalDir 'runtime'
    if (-not $Update -and -not $Latest -and -not $Ref -and -not $FromSource -and -not $haveArchive -and
        -not $SkipVenv -and (Test-Path -LiteralPath $runtimeRoot) -and
        -not (Test-Path -LiteralPath $receiptPath)) {
        $Version = ''
        Write-Warn 'Non-canonical runtime detected; reinstalling the latest release.'
    }

    if ($FromSource -and $haveArchive) { Fail '-FromSource cannot be combined with -ArchivePath/-ChecksumPath.' }
    if (-not $haveArchive -and (Get-Arch) -ne 'amd64') {
        Fail 'Native Windows release installs currently require amd64. Use WSL2 with install.sh on ARM64.'
    }

    if (-not $haveArchive) {
        Resolve-SourceProvider
        if ($script:TuiProvider -eq 'mirror') {
            Write-Info 'TUI source: latest producer-owned archive from lingtai.ai'
            Write-Step 'If that TUI source is unavailable, only the TUI falls back to the latest GitHub source release.'
        } else {
            Write-Info "TUI source provider: $($script:TuiProvider)"
        }
    }

    # A local artifact has one deterministic installer phase. Source builds
    # may retry through a provider-specific fallback, so their phase banners
    # intentionally remain unnumbered rather than showing a false denominator.
    if ($haveArchive) {
        Set-PhaseTotal $(if ($SkipVenv) { 4 } else { 5 })
    } else {
        Set-PhaseTotal 0
    }

    Write-Info "Target BinDir: $BinDir"
    Write-Info "Target GlobalDir: $GlobalDir"

    $resolvedTag = $Version
    $tuiBuild = $null
    $managed = @()
    $tuiProviderForReceipt = 'local'
    if ($haveArchive) {
        $phase = Start-Phase 'Validating and installing local TUI artifact ...'
        Write-Step "Local artifact selected for exact TUI version $Version."
        $managed = Install-FromLocalArtifact -Archive $ArchivePath -Sidecar $ChecksumPath -BinDir $BinDir -Requested $Version
        $phaseMessage = if ($DryRun) { 'local TUI artifact validated; nothing installed' } else { 'local TUI artifact installed' }
        Complete-Phase -Clock $phase -Message $phaseMessage
    } elseif ($script:TuiProvider -eq 'mirror') {
        try {
            $resolvedTag = Resolve-PublicTag -Requested '' -Provider 'mirror'
            $tuiBuild = Build-ReleaseSourceArchive -Tag $resolvedTag -Provider 'mirror' -SkipPython:$SkipVenv
            Write-Info "Latest TUI source release is $resolvedTag (lingtai.ai)"
            $tuiProviderForReceipt = 'mirror'
        } catch {
            Write-Warn 'lingtai.ai TUI source is unavailable; falling back to the latest GitHub TUI source release.'
            $script:TuiProvider = 'github'
            $resolvedTag = Resolve-PublicTag -Requested '' -Provider 'github'
            try {
                $tuiBuild = Build-ReleaseSourceArchive -Tag $resolvedTag -Provider 'github' -SkipPython:$SkipVenv
            } catch {
                Write-Warn "GitHub release $resolvedTag has no usable producer source archive; checking out its peeled tag commit."
                $tuiBuild = Build-SourceRef -Ref $resolvedTag -SkipPython:$SkipVenv
            }
            $tuiProviderForReceipt = 'github'
        }
    } else {
        $resolvedTag = Resolve-PublicTag -Requested $Version -Provider 'github'
        try {
            $tuiBuild = Build-ReleaseSourceArchive -Tag $resolvedTag -Provider 'github' -SkipPython:$SkipVenv
        } catch {
            Write-Warn "GitHub release $resolvedTag has no usable producer source archive; checking out its peeled tag commit."
            $tuiBuild = Build-SourceRef -Ref $resolvedTag -SkipPython:$SkipVenv
        }
        $tuiProviderForReceipt = 'github'
    }

    $kernelMeta = $null
    if ($SkipVenv) {
        Write-Warn 'Skipping runtime venv (-SkipVenv). Provision the Python runtime yourself.'
    } else {
        $phase = Start-Phase 'Provisioning the independent kernel runtime ...'
        if ($DryRun) {
            Write-Step "[dry-run] would independently resolve and install the latest verified kernel release into $GlobalDir\runtime\venv."
        } else {
            $kernelMeta = Install-Venv -GlobalDir $GlobalDir
            $expectedKeys = @('KernelSource','KernelReleaseTag','KernelVersion','KernelProvider')
            if ($kernelMeta -isnot [hashtable] -or @($expectedKeys | Where-Object { -not $kernelMeta.ContainsKey($_) }).Count -gt 0) {
                Fail 'Internal error: Install-Venv returned malformed kernel metadata.'
            }
            Write-Info "Resolved latest verified kernel release: $($kernelMeta.KernelReleaseTag) (provider $($kernelMeta.KernelProvider))"
        }
        $phaseMessage = if ($DryRun) { 'kernel runtime plan prepared; nothing installed' } else { "lingtai $($kernelMeta.KernelVersion) installed into the managed venv" }
        Complete-Phase -Clock $phase -Message $phaseMessage
    }

    if (-not $haveArchive) {
        $phase = Start-Phase 'Installing the built TUI ...'
        if ($DryRun) {
            Write-Step "[dry-run] would install the locally built lingtai-tui.exe into $BinDir."
        } else {
            $managed = Install-FromBuiltMain -Build $tuiBuild -BinDir $BinDir -Label "release $resolvedTag"
        }
        $phaseMessage = if ($DryRun) { 'TUI install plan prepared; nothing installed' } else { 'TUI binary installed' }
        Complete-Phase -Clock $phase -Message $phaseMessage
    }

    $phase = Start-Phase 'Updating PATH ...'
    if ($DryRun) {
        if ($NoModifyPath) {
            Write-Step "[dry-run] would leave process and user PATH unchanged (-NoModifyPath)."
        } else {
            Write-Step "[dry-run] would add '$BinDir' to process and user PATH."
        }
    } else {
        Add-ToPath -Dir $BinDir
    }
    $phaseMessage = if ($DryRun) { 'PATH update plan prepared; nothing changed' } elseif ($NoModifyPath) { 'process PATH updated; persistent user PATH unchanged' } else { 'PATH updated' }
    Complete-Phase -Clock $phase -Message $phaseMessage

    $phase = Start-Phase 'Writing install metadata ...'
    if ($DryRun) {
        Write-Step "[dry-run] would write install metadata under $GlobalDir."
    } else {
        $metaArgs = @{
            GlobalDir=$GlobalDir; Prefix=$prefix; BinDir=$BinDir; RequestedRef=$Version; ResolvedRef=$resolvedTag
            ResolvedCommit=$(if ($tuiBuild) { $tuiBuild.TuiSha } else { '' })
            InstallKind=$(if ($haveArchive) { 'powershell-local-artifact' } else { 'powershell-source-build' })
            ManagedBinaries=$managed; TuiProvider=$tuiProviderForReceipt
        }
        if ($kernelMeta) {
            $metaArgs['KernelSource'] = $kernelMeta.KernelSource
            $metaArgs['KernelReleaseTag'] = $kernelMeta.KernelReleaseTag
            $metaArgs['KernelVersion'] = $kernelMeta.KernelVersion
            $metaArgs['KernelProvider'] = $kernelMeta.KernelProvider
        }
        Write-InstallMetadata @metaArgs
    }
    $phaseMessage = if ($DryRun) { 'metadata plan prepared; nothing written' } else { 'install metadata written' }
    Complete-Phase -Clock $phase -Message $phaseMessage

    $phase = Start-Phase 'Summarizing installation ...'
    if ($DryRun) {
        Write-Host 'Dry run complete. Nothing was installed.' -ForegroundColor Green
        Complete-Phase -Clock $phase -Message 'dry-run complete; nothing installed'
        return
    }
    $facts = [ordered]@{
        'TUI release' = $resolvedTag
        'TUI provider' = $tuiProviderForReceipt
    }
    if ($kernelMeta) {
        $facts['kernel release'] = $kernelMeta.KernelReleaseTag
        $facts['kernel provider'] = $kernelMeta.KernelProvider
    }
    Complete-Phase -Clock $phase -Message 'installation summary ready'
    $runtimeDir = if ($kernelMeta) { Join-Path $GlobalDir 'runtime\venv' } else { '' }
    Remove-OtherTuiOnPath -CanonicalPath (Join-Path $BinDir 'lingtai-tui.exe')
    Write-Completion -BinDir $BinDir -GlobalDir $GlobalDir -Headline 'Install complete.' -Facts $facts -RuntimeDir $runtimeDir
}
try {
    Invoke-Main
    # Same transient-console problem as the failure path: when launched by
    # double-click / shortcut / Start-Process / `curl | iex`, `exit 0` closes the
    # window the instant the install finishes. On a fast machine the remaining
    # phases complete in under a second after the big download, so the window vanishing
    # right after the download reads as a crash ("闪退") even though the
    # install succeeded. Only pause for a real interactive console; piped/
    # automated invocations stay non-blocking, and -NonInteractive never pauses.
    if (-not $NonInteractive -and $Host.Name -eq 'ConsoleHost' -and -not [Console]::IsOutputRedirected) {
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
    # interactive console; piped/automated invocations stay non-blocking, and
    # -NonInteractive never pauses.
    if (-not $NonInteractive -and $Host.Name -eq 'ConsoleHost' -and -not [Console]::IsOutputRedirected) {
        Write-Host ""
        Write-Host "Installation failed. Press Enter to close this window..." -ForegroundColor DarkGray
        [void][Console]::ReadLine()
    }
    exit 1
}
