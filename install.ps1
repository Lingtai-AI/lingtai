#requires -Version 5.1
<#
.SYNOPSIS
    LingTai native Windows (PowerShell) installer.

.DESCRIPTION
    One-click installer for the LingTai TUI (and portal) on native Windows. It is
    the PowerShell counterpart to install.sh and parses/runs identically under
    Windows PowerShell 5.1 (Desktop) and PowerShell 7+ (Core).

    Two install sources are supported:

      * PUBLIC MODE (no -ArchivePath, the default): resolve one exact vX.Y.Z TUI
        release tag from GitHub (an explicit -Version, or the latest release
        resolved once), download and strictly validate that release's
        lingtai-bundle-manifest.json (schema lingtai.tui.bundle/v1), download the
        lingtai-<tag>-windows-amd64.zip archive plus its .sha256 sidecar, verify
        the archive's SHA-256 against the manifest before extraction, and confirm
        the staged lingtai-tui.exe reports exactly that tag before touching BinDir.

      * LOCAL ARTIFACT MODE (-ArchivePath + -ChecksumPath): install the TUI/portal
        binaries FROM an already-downloaded release archive plus its sha256
        sidecar, with no network use for the binary install itself. The archive is
        verified, expanded into an installer-owned staging directory, the staged
        lingtai-tui.exe is run to confirm it reports the requested -Version, and
        only THEN are the binaries copied into -BinDir. This is the seam the
        Windows contract suite exercises. The default (non -SkipVenv) runtime step
        still resolves the pinned bundle for -Version over the network exactly as
        in public mode, since the kernel pin is not shipped inside the archive.

    Both modes provision the Python runtime venv (default, non -SkipVenv) ONLY
    from the resolved release's pinned kernel bundle: the bundle manifest's
    kernel_tag/kernel_manifest_filename select the lingtai-kernel release
    manifest, a wheel matching the venv's actual CPython 3.11/3.12/3.13 win_amd64
    interpreter is selected and SHA-256 verified, and only that local wheel path
    is installed -- LingTai is never installed from a package index by name and
    the kernel tag is never resolved as "latest" or changed from the pin.
    -SkipVenv is the explicit TUI-only mode that skips all of this and creates no
    venv; -DryRun performs the same resolution/validation reads but writes
    nothing.

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
        [string]$KernelProvider = ''
    )
    $stamped = $ResolvedRef -replace '^v', ''
    $meta = [ordered]@{
        schema           = 'lingtai.tui.install/v1'
        schema_version   = 1
        install_method   = 'powershell'
        install_kind     = $InstallKind
        self_update      = $false
        upgrade_command  = 're-run install.ps1 with a newer -ArchivePath/-Version'
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
        $meta['kernel_bundle_id']    = $KernelBundleId
        $meta['kernel_version']      = $KernelVersion
        $meta['kernel_provider']     = $KernelProvider
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
    $keyOrBraceMatches = [regex]::Matches($RawJson, '[{}]|"([A-Za-z_]+)"\s*:')
    $seenStack = New-Object 'System.Collections.Generic.Stack[System.Collections.Generic.HashSet[string]]'
    $seenStack.Push((New-Object 'System.Collections.Generic.HashSet[string]'))
    foreach ($m in $keyOrBraceMatches) {
        if ($m.Value -eq '{') {
            $seenStack.Push((New-Object 'System.Collections.Generic.HashSet[string]'))
        } elseif ($m.Value -eq '}') {
            if ($seenStack.Count -gt 1) { $seenStack.Pop() | Out-Null }
        } elseif ($m.Groups[1].Success) {
            if (-not $seenStack.Peek().Add($m.Groups[1].Value)) {
                Fail "invalid strict bundle manifest: duplicate JSON key: $($m.Groups[1].Value)"
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
    if ($data.generated_at -notmatch '^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$') {
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
        $isWindows = $name -match '^lingtai-[^/]+-windows-amd64\.zip$'
        if (-not ($isPosix -or $isWindows)) { Fail "invalid strict bundle manifest: archive filename is invalid" }
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
            # relying on downstream code to handle it safely.
            if ([string]::IsNullOrEmpty($art.filename) -or $art.filename -notmatch '^lingtai-[0-9A-Za-z_.+!-]+-(cp3(1[1-3]))-\1-win_amd64\.whl$') {
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
# or a bare `python`/`python3` on PATH. Never downloads or bootstraps a
# Python runtime -- fails loud with an actionable message if none is found.
function Find-VenvPython {
    $py = Get-Command -Name 'py' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    foreach ($minor in @('3.13', '3.12', '3.11')) {
        if ($py) {
            $probe = & $py.Source "-$minor" '-c' 'print(1)' 2>$null
            if ($LASTEXITCODE -eq 0 -and $probe -eq '1') {
                return @{ Launcher = $py.Source; Args = @("-$minor") }
            }
        }
    }
    foreach ($name in @('python', 'python3')) {
        $cmd = Get-Command -Name $name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($cmd) {
            # Single-quoted Python literal only (no embedded ") -- Windows PowerShell
            # 5.1's native argument-array-to-command-line reconstruction mishandles
            # embedded double quotes, corrupting the string the interpreter receives.
            $verOut = & $cmd.Source '-c' 'import sys; print(str(sys.version_info.major) + ''.'' + str(sys.version_info.minor))' 2>$null
            if ($LASTEXITCODE -eq 0 -and $verOut -match '^3\.(1[1-3])$') {
                return @{ Launcher = $cmd.Source; Args = @() }
            }
        }
    }
    Fail @"
No supported Python interpreter (CPython 3.11, 3.12, or 3.13, 64-bit) was found
via the 'py' launcher or 'python'/'python3' on PATH.

LingTai's Windows runtime venv is created from an already-available supported
Python installation -- this installer never downloads or bootstraps an
unpinned Python/uv toolchain. Install Python 3.11+ (for example from
python.org or the Microsoft Store) and re-run, or pass -SkipVenv to install
the TUI/portal binaries only.
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
    $tag = & $VenvPython '-c' 'import sys; print(''cp'' + str(sys.version_info.major) + str(sys.version_info.minor))' 2>$null
    if ($LASTEXITCODE -ne 0 -or $tag -notmatch '^cp3(11|12|13)$') {
        Fail "Could not determine a supported CPython tag from the venv interpreter (got '$tag')."
    }
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
    & $VenvPython '-m' 'pip' 'install' $dest
    if ($LASTEXITCODE -ne 0) { Fail "pip install of the local wheel failed (exit $LASTEXITCODE)." }
}

# Confirm-KernelImport verifies the freshly installed distribution imports,
# reports its version, and proves it is not an editable/source install (no
# direct_url.json, or one that is not editable) -- the same provenance bar
# install.sh's bundle path holds itself to.
function Confirm-KernelImport {
    param([string]$VenvPython, [string]$ExpectedVersion)
    # Single-quoted Python throughout (no embedded ") -- Windows PowerShell 5.1's
    # native argument-array-to-command-line reconstruction mishandles embedded
    # double quotes, corrupting the script text the interpreter actually receives.
    $probe = & $VenvPython '-c' @'
import importlib.metadata as m
import json
import sys
try:
    import lingtai
except ImportError as exc:
    print('IMPORT_FAILED:' + str(exc))
    sys.exit(1)
dist = m.distribution('lingtai')
version = dist.version
editable = False
try:
    direct_url_text = dist.read_text('direct_url.json')
    if direct_url_text:
        direct_url = json.loads(direct_url_text)
        editable = bool(direct_url.get('dir_info', {}).get('editable', False))
except Exception:
    pass
print('OK:' + str(version) + ':' + str(editable))
'@ 2>$null
    if ($LASTEXITCODE -ne 0 -or -not $probe -or $probe -notmatch '^OK:') {
        Fail "Post-install verification failed: could not import lingtai in the venv ($probe)"
    }
    $parts = $probe -split ':'
    $installedVersion = $parts[1]
    $isEditable = $parts[2] -eq 'True'
    if ($isEditable) {
        Fail "Installed lingtai distribution is editable/source-provenance; the Windows bundle install must be a real wheel install."
    }
    if ($ExpectedVersion -and $installedVersion -ne $ExpectedVersion) {
        Fail "Installed lingtai version '$installedVersion' does not match the pinned kernel manifest version '$ExpectedVersion'."
    }
    Write-Ok "Verified lingtai $installedVersion imports and is a non-editable wheel install."
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

    $bootstrap = Find-VenvPython
    if (-not (Test-Path -LiteralPath $venvDir)) {
        New-Item -ItemType Directory -Force -Path (Split-Path $venvDir -Parent) | Out-Null
        $venvArgs = @($bootstrap.Args) + @('-m', 'venv', $venvDir)
        & $bootstrap.Launcher @venvArgs
        if ($LASTEXITCODE -ne 0) { Fail "Failed to create the venv at $venvDir (exit $LASTEXITCODE)." }
    }
    $venvPython = Join-Path $venvDir 'Scripts\python.exe'
    if (-not (Test-Path -LiteralPath $venvPython)) {
        Fail "Venv created at $venvDir but Scripts\python.exe is missing."
    }

    $wheelTag = Get-VenvWheelTag -VenvPython $venvPython
    $kernelManifest = Get-KernelManifest -KernelTag $Bundle.KernelTag -ManifestFilename $Bundle.KernelManifestFilename
    $wheel = Select-KernelWheel -KernelManifest $kernelManifest -WheelTag $wheelTag

    $stage = New-StagingDir
    Install-KernelWheel -VenvPython $venvPython -Wheel $wheel -KernelTag $Bundle.KernelTag -StageDir $stage
    $installedVersion = Confirm-KernelImport -VenvPython $venvPython -ExpectedVersion $kernelManifest.kernel_version

    Write-KernelProvenance -VenvDir $venvDir -TuiTag $TuiTag -TuiCommit $Bundle.TuiCommit -BundleId $Bundle.BundleId `
        -KernelTag $Bundle.KernelTag -KernelVersion $installedVersion -WheelFilename $wheel.filename `
        -WheelSha256 $wheel.sha256 -Provider 'github'

    return @{
        KernelSource   = 'bundle'
        KernelBundleId = $Bundle.BundleId
        KernelVersion  = $installedVersion
        KernelProvider = 'github'
    }
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
        Write-Step "[dry-run] would install lingtai-tui.exe (and optional lingtai-portal.exe) into $BinDir"
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

    # 1. Resolve the bundle up front (metadata reads only, no writes) so the
    # runtime capability gate below can run BEFORE any binary/PATH/metadata
    # write, exactly like the previous hard-stop did. Local-artifact mode has
    # no bundle shipped inside the archive, so it resolves the SAME bundle a
    # public install of -Version would (this is the only network use in that
    # mode, and only when the venv step is not skipped).
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
    $kernelMeta = $null
    if (-not $SkipVenv -and -not $DryRun) {
        $kernelMeta = Install-Venv -Bundle $bundle -TuiTag $resolvedTag -GlobalDir $GlobalDir
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
    if ($DryRun) {
        Write-Step "[dry-run] would add '$BinDir' to the process and (unless -NoModifyPath) persistent user PATH"
    } else {
        Add-ToPath -Dir $BinDir
    }

    # 5. Runtime disposition.
    if ($SkipVenv) {
        Write-Warn "Skipping runtime venv (-SkipVenv). Provision the Python runtime yourself; the TUI/portal binaries are installed."
    }

    # 6. Metadata. Skipped in DryRun (no writes).
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
