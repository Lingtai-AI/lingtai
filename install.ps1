<#
.SYNOPSIS
    LingTai native Windows (PowerShell) installer — EXPERIMENTAL.

.DESCRIPTION
    One-click installer for the LingTai TUI and portal on native Windows. This is
    the PowerShell counterpart to install.sh. It downloads the prebuilt Windows
    release assets, installs lingtai-tui.exe (and lingtai-portal.exe), provisions
    the Python runtime venv with uv, and writes diagnostic install metadata.

    Native Windows is EXPERIMENTAL. The TUI and portal render fine, but two agent
    capabilities run at reduced fidelity natively:
      - daemon / subagents (分身) are currently unavailable on native Windows
      - the `bash` tool runs cmd.exe, not bash
    For full parity, use WSL2 and the install.sh path. See README for details.

    There is no native PowerShell self-update yet. To upgrade, re-run:
        iwr -useb https://lingtai.ai/install.ps1 | iex
    The metadata written here is deliberately marked install_method=powershell so
    the TUI updater does NOT mistake it for a bash-updatable source install.

.PARAMETER Version
    Release tag to install, e.g. v0.10.5. Defaults to the latest GitHub release.

.PARAMETER BinDir
    Directory to install binaries into. Defaults to a per-user location:
    %LOCALAPPDATA%\Programs\lingtai\bin. Never requires administrator.

.PARAMETER SkipPortal
    Install only lingtai-tui.exe, not lingtai-portal.exe.

.PARAMETER SkipVenv
    Skip Python runtime venv provisioning. The TUI will provision it on first run.

.PARAMETER NoModifyPath
    Do not add BinDir to the user PATH. By default the installer adds BinDir to
    both the persistent user PATH and the current process PATH.

.PARAMETER DryRun
    Print the actions that would be taken without downloading, installing, or
    modifying anything.

.EXAMPLE
    iwr -useb https://lingtai.ai/install.ps1 | iex

.EXAMPLE
    .\install.ps1 -Version v0.10.5 -SkipPortal

.NOTES
    Requires PowerShell 5.1 or later. Does not require administrator.
#>
[CmdletBinding()]
param(
    [string]$Version = $env:LINGTAI_VERSION,
    [string]$BinDir  = $env:LINGTAI_BIN_DIR,
    [switch]$SkipPortal,
    [switch]$SkipVenv,
    [switch]$NoModifyPath,
    [switch]$DryRun
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

# Silence the per-request progress bar: on Windows PowerShell 5.1 it makes
# Invoke-WebRequest downloads dramatically slower and clutters piped output.
$ProgressPreference = 'SilentlyContinue'

# Best-effort UTF-8 console output so the 分身/中文 banner lines render correctly.
# 5.1-compatible (no $PSStyle); swallow failures on redirected/no-console hosts.
try {
    [Console]::OutputEncoding = [System.Text.Encoding]::UTF8
    $OutputEncoding = [System.Text.Encoding]::UTF8
} catch {
    # No console (piped/CI) or restricted host — encoding is cosmetic, ignore.
}

# --- Constants ---------------------------------------------------------------

$Repo         = 'Lingtai-AI/lingtai'
$RepoUrl      = "https://github.com/$Repo"
$ApiBase      = "https://api.github.com/repos/$Repo"
$UvInstall    = 'https://astral.sh/uv/install.ps1'
$PyVersion    = '3.13'
$MaxRetries   = 3
$RetryDelaySec = 2

# --- Output helpers ----------------------------------------------------------

$script:HasFailed = $false

function Write-Info { param([string]$Message) Write-Host "==> $Message" -ForegroundColor Cyan }
function Write-Warn { param([string]$Message) Write-Host "warn: $Message" -ForegroundColor Yellow }
function Write-Ok   { param([string]$Message) Write-Host "  ok: $Message" -ForegroundColor Green }
function Write-Step { param([string]$Message) Write-Host "  -> $Message" -ForegroundColor DarkGray }

function Fail {
    param([string]$Message)
    Write-Host ""
    Write-Host "error: $Message" -ForegroundColor Red
    throw $Message
}

function Show-WslFallback {
    Write-Host ""
    Write-Host "For a full-parity install (all agent capabilities), use WSL2:" -ForegroundColor Yellow
    Write-Host "    wsl --install" -ForegroundColor White
    Write-Host "    # then, inside your WSL shell:" -ForegroundColor DarkGray
    Write-Host "    curl -fsSL https://lingtai.ai/install.sh | bash" -ForegroundColor White
}

# --- Preconditions -----------------------------------------------------------

if ($PSVersionTable.PSVersion.Major -lt 5) {
    Fail "PowerShell 5.1 or later is required (found $($PSVersionTable.PSVersion)). Update Windows PowerShell or install PowerShell 7."
}

# OS guard: this installer is for native Windows only. Fail clearly here rather
# than limping into arch/env detection on macOS/Linux PowerShell. $IsWindows only
# exists on PowerShell 6+, so on Windows PowerShell 5.1 (where it is undefined) we
# fall back to the platform enum and the OS env var, both of which are reliably
# "Windows" on 5.1.
$onWindows = $false
if (Get-Variable -Name IsWindows -Scope Global -ErrorAction SilentlyContinue) {
    # PowerShell 6+ (cross-platform): trust the built-in.
    $onWindows = [bool]$IsWindows
} else {
    # Windows PowerShell 5.1: no $IsWindows, but it only runs on Windows.
    $onWindows = ($env:OS -eq 'Windows_NT') -or `
                 ([System.Environment]::OSVersion.Platform -eq [System.PlatformID]::Win32NT)
}
if (-not $onWindows) {
    Fail @"
install.ps1 supports native Windows only.

On macOS or Linux, use the shell installer instead:
    curl -fsSL https://lingtai.ai/install.sh | bash

On Windows, use Windows PowerShell 5.1+ or PowerShell 7 for Windows.
"@
}

# --- Arch / platform detection -----------------------------------------------

function Get-Arch {
    # PROCESSOR_ARCHITECTURE reflects the host on 64-bit shells. Fall back to the
    # WOW64 variable in case the installer is somehow run from a 32-bit host.
    $raw = $env:PROCESSOR_ARCHITECTURE
    if ($env:PROCESSOR_ARCHITEW6432) { $raw = $env:PROCESSOR_ARCHITEW6432 }
    switch ($raw) {
        'AMD64' { return 'amd64' }
        'ARM64' { return 'arm64' }
        'x86'   { Fail "32-bit Windows (x86) is not supported. LingTai requires 64-bit Windows (amd64 or arm64)." }
        default { Fail "Unsupported processor architecture '$raw'. LingTai supports amd64 and arm64 on Windows." }
    }
}

# --- Path helpers ------------------------------------------------------------

function Get-DefaultBinDir {
    $base = $env:LOCALAPPDATA
    if ([string]::IsNullOrWhiteSpace($base)) {
        $base = Join-Path $env:USERPROFILE 'AppData\Local'
    }
    return Join-Path $base 'Programs\lingtai\bin'
}

function Get-GlobalDir {
    return Join-Path $env:USERPROFILE '.lingtai-tui'
}

# --- Network helpers (retry with short backoff) ------------------------------

# Invoke a scriptblock up to $MaxRetries times, sleeping $RetryDelaySec *
# attempt between tries. Returns the scriptblock's result on success; re-throws
# the last exception on final failure so callers keep their own fail-loud
# messages. 5.1-compatible: no ternary, no ??, no classes.
function Invoke-WithRetry {
    param(
        [ScriptBlock]$Action,
        [string]$What
    )
    $attempt = 0
    $lastErr = $null
    while ($attempt -lt $MaxRetries) {
        $attempt++
        try {
            return & $Action
        } catch {
            $lastErr = $_
            if ($attempt -lt $MaxRetries) {
                Write-Step "$What failed (attempt $attempt/$MaxRetries): $($_.Exception.Message); retrying..."
                Start-Sleep -Seconds ($RetryDelaySec * $attempt)
            }
        }
    }
    throw $lastErr
}

# Retrying GET returning parsed JSON.
function Invoke-RestWithRetry {
    param([string]$Url, [hashtable]$Headers)
    return Invoke-WithRetry -What "request to $Url" -Action {
        Invoke-RestMethod -Uri $Url -Headers $Headers -Method Get
    }.GetNewClosure()
}

# Retrying download to a file.
function Invoke-DownloadWithRetry {
    param([string]$Url, [string]$OutFile)
    Invoke-WithRetry -What "download of $Url" -Action {
        Invoke-WebRequest -UseBasicParsing -Uri $Url -OutFile $OutFile
    }.GetNewClosure() | Out-Null
}

# Retrying GET returning the raw response content (string).
function Invoke-DownloadStringWithRetry {
    param([string]$Url)
    return Invoke-WithRetry -What "download of $Url" -Action {
        (Invoke-WebRequest -UseBasicParsing -Uri $Url).Content
    }.GetNewClosure()
}

# Parse the first 64-hex SHA-256 digest from a `.sha256` sidecar file. Handles
# the common shasum/sha256sum format ("<hash>  <filename>") as well as a bare
# hash on its own line. Returns the uppercased digest, or $null if none found.
function Read-ExpectedSha256 {
    param([string]$Path)
    $text = Get-Content -LiteralPath $Path -Raw
    $m = [regex]::Match($text, '(?im)^\s*([0-9a-f]{64})\b')
    if ($m.Success) { return $m.Groups[1].Value.ToUpperInvariant() }
    return $null
}

# --- Release resolution ------------------------------------------------------

function Invoke-GitHubApi {
    param([string]$Url)
    $headers = @{ 'Accept' = 'application/vnd.github+json'; 'User-Agent' = 'lingtai-install-ps1' }
    # GITHUB_TOKEN lifts the unauthenticated rate limit in CI; optional otherwise.
    if ($env:GITHUB_TOKEN) { $headers['Authorization'] = "Bearer $($env:GITHUB_TOKEN)" }
    try {
        return Invoke-RestWithRetry -Url $Url -Headers $headers
    } catch {
        Fail "GitHub API request failed for $Url : $($_.Exception.Message)"
    }
}

function Resolve-Version {
    param([string]$Requested)
    if (-not [string]::IsNullOrWhiteSpace($Requested)) {
        return $Requested
    }
    if ($DryRun) {
        Write-Step "[dry-run] would resolve the latest release tag from GitHub"
        return 'vLATEST'
    }
    Write-Step "Resolving latest release tag from GitHub..."
    $rel = Invoke-GitHubApi "$ApiBase/releases/latest"
    if (-not $rel.tag_name) {
        Fail "Could not determine the latest release tag. Pass -Version explicitly (e.g. -Version v0.10.5)."
    }
    return $rel.tag_name
}

function Get-AssetUrl {
    param([string]$Tag, [string]$AssetName)
    $rel = Invoke-GitHubApi "$ApiBase/releases/tags/$Tag"
    if (-not $rel.assets) { return $null }
    $asset = $rel.assets | Where-Object { $_.name -eq $AssetName } | Select-Object -First 1
    if ($asset) { return $asset.browser_download_url }
    return $null
}

# --- uv discovery / bootstrap ------------------------------------------------

function Find-Uv {
    $candidates = @()
    $onPath = Get-Command uv -CommandType Application -ErrorAction SilentlyContinue
    if ($onPath) { $candidates += $onPath.Source }
    if ($env:UV_INSTALL_DIR)  { $candidates += (Join-Path $env:UV_INSTALL_DIR 'uv.exe') }
    $candidates += (Join-Path $env:USERPROFILE '.local\bin\uv.exe')
    $localApp = $env:LOCALAPPDATA
    if ([string]::IsNullOrWhiteSpace($localApp)) { $localApp = Join-Path $env:USERPROFILE 'AppData\Local' }
    $candidates += (Join-Path $localApp 'Programs\lingtai\uv\uv.exe')
    foreach ($c in $candidates) {
        if ($c -and (Test-Path -LiteralPath $c)) { return $c }
    }
    return $null
}

function Install-Uv {
    $localApp = $env:LOCALAPPDATA
    if ([string]::IsNullOrWhiteSpace($localApp)) { $localApp = Join-Path $env:USERPROFILE 'AppData\Local' }
    $uvDir = Join-Path $localApp 'Programs\lingtai\uv'
    Write-Step "Installing uv into $uvDir (official installer)..."
    if ($DryRun) {
        Write-Step "[dry-run] would run the official uv installer from $UvInstall"
        return (Join-Path $uvDir 'uv.exe')
    }
    New-Item -ItemType Directory -Force -Path $uvDir | Out-Null
    # Run the official uv installer in a child scope with our env overrides so we
    # do not modify the user's PATH and land uv in a predictable location. We only
    # ever execute this one well-known official script.
    $prevInstallDir = $env:UV_INSTALL_DIR
    $prevNoModify   = $env:UV_NO_MODIFY_PATH
    try {
        $env:UV_INSTALL_DIR   = $uvDir
        $env:UV_NO_MODIFY_PATH = '1'
        $uvScript = Invoke-DownloadStringWithRetry -Url $UvInstall
        $block    = [ScriptBlock]::Create($uvScript)
        & $block
    } catch {
        Fail "uv installation failed: $($_.Exception.Message)"
    } finally {
        $env:UV_INSTALL_DIR    = $prevInstallDir
        $env:UV_NO_MODIFY_PATH = $prevNoModify
    }
    $uvExe = Find-Uv
    if (-not $uvExe) {
        Fail "uv installer completed but uv.exe was not found. Install uv manually from https://docs.astral.sh/uv/ and re-run with uv on PATH."
    }
    return $uvExe
}

# --- PATH management ---------------------------------------------------------

function Add-ToUserPath {
    param([string]$Dir)
    if ($NoModifyPath) {
        Write-Step "Skipping PATH update (-NoModifyPath). Add '$Dir' to PATH manually."
        return
    }
    if ($DryRun) {
        Write-Step "[dry-run] would add '$Dir' to the current process PATH and persistent user PATH"
        return
    }
    # Current process PATH (so the rest of this session sees the binaries).
    if (($env:PATH -split ';') -notcontains $Dir) {
        $env:PATH = "$Dir;$env:PATH"
    }
    $userPath = [Environment]::GetEnvironmentVariable('PATH', 'User')
    if ([string]::IsNullOrEmpty($userPath)) { $userPath = '' }
    $entries = $userPath -split ';' | Where-Object { $_ -ne '' }
    if ($entries -notcontains $Dir) {
        $newPath = if ($userPath -eq '') { $Dir } else { "$userPath;$Dir" }
        [Environment]::SetEnvironmentVariable('PATH', $newPath, 'User')
        Write-Ok "Added '$Dir' to your user PATH (open a new terminal to pick it up everywhere)."
    } else {
        Write-Step "'$Dir' is already on the user PATH."
    }
}

# --- Binary install ----------------------------------------------------------

function Install-Binaries {
    param([string]$Tag, [string]$Arch, [string]$Dir)

    $assetName = "lingtai-$Tag-windows-$Arch.zip"
    $shaName   = "$assetName.sha256"
    Write-Info "Locating Windows release asset '$assetName'"

    if ($DryRun) {
        Write-Step "[dry-run] would look up asset '$assetName' (and '$shaName') in release $Tag"
        Write-Step "[dry-run] would download the zip and its .sha256, then verify the SHA-256 checksum"
        Write-Step "[dry-run] would install lingtai-tui.exe$(if (-not $SkipPortal) { ' and lingtai-portal.exe' }) into $Dir"
        return
    }

    $url = Get-AssetUrl -Tag $Tag -AssetName $assetName
    if (-not $url) {
        Fail @"
No Windows asset '$assetName' found in release $Tag.

Windows release assets are produced starting from releases that include the
Windows installer work (this PR). Pick a release that ships Windows assets, or
use WSL2 for now:
    wsl --install
    curl -fsSL https://lingtai.ai/install.sh | bash
"@
    }

    $shaUrl = Get-AssetUrl -Tag $Tag -AssetName $shaName
    if (-not $shaUrl) {
        Fail "Release $Tag has '$assetName' but no '$shaName' checksum asset. Refusing to install an unverifiable download."
    }

    $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("lingtai-install-" + [System.Guid]::NewGuid().ToString('N'))
    New-Item -ItemType Directory -Force -Path $tmp | Out-Null
    try {
        $zip = Join-Path $tmp $assetName
        Write-Step "Downloading $assetName ..."
        Invoke-DownloadWithRetry -Url $url -OutFile $zip

        # Verify the SHA-256 checksum against the published .sha256 sidecar.
        $shaFile = Join-Path $tmp $shaName
        Write-Step "Downloading $shaName ..."
        Invoke-DownloadWithRetry -Url $shaUrl -OutFile $shaFile
        $expected = Read-ExpectedSha256 -Path $shaFile
        if (-not $expected) {
            Fail "Could not parse a SHA-256 digest from $shaName."
        }
        $actual = (Get-FileHash -Algorithm SHA256 -LiteralPath $zip).Hash
        if ($actual -ne $expected) {
            Fail "Checksum mismatch for $assetName. Expected $expected but got $actual. The download may be corrupt or tampered with; not installing."
        }
        Write-Ok "Verified SHA-256 checksum for $assetName"

        $extract = Join-Path $tmp 'extract'
        New-Item -ItemType Directory -Force -Path $extract | Out-Null
        Expand-Archive -LiteralPath $zip -DestinationPath $extract -Force

        New-Item -ItemType Directory -Force -Path $Dir | Out-Null

        $tui = Get-ChildItem -Path $extract -Recurse -Filter 'lingtai-tui.exe' -ErrorAction SilentlyContinue | Select-Object -First 1
        if (-not $tui) { Fail "Archive $assetName did not contain lingtai-tui.exe." }
        Copy-Item -LiteralPath $tui.FullName -Destination (Join-Path $Dir 'lingtai-tui.exe') -Force
        Write-Ok "Installed lingtai-tui.exe -> $Dir"

        if (-not $SkipPortal) {
            $portal = Get-ChildItem -Path $extract -Recurse -Filter 'lingtai-portal.exe' -ErrorAction SilentlyContinue | Select-Object -First 1
            if (-not $portal) {
                Write-Warn "Archive did not contain lingtai-portal.exe; skipping portal (pass -SkipPortal to silence)."
            } else {
                Copy-Item -LiteralPath $portal.FullName -Destination (Join-Path $Dir 'lingtai-portal.exe') -Force
                Write-Ok "Installed lingtai-portal.exe -> $Dir"
            }
        }
    } finally {
        Remove-Item -LiteralPath $tmp -Recurse -Force -ErrorAction SilentlyContinue
    }
}

function Test-BinaryVersion {
    param([string]$Dir)
    if ($DryRun) { Write-Step "[dry-run] would verify lingtai-tui.exe --version"; return }

    $tuiExe = Join-Path $Dir 'lingtai-tui.exe'
    if (-not (Test-Path -LiteralPath $tuiExe)) { Fail "lingtai-tui.exe missing after install at $tuiExe." }
    try {
        $out = & $tuiExe --version 2>&1
        $code = $LASTEXITCODE
        if ($code -ne 0) { Fail "lingtai-tui.exe --version failed with exit code ${code}: $out" }
        Write-Ok "lingtai-tui.exe --version -> $out"
    } catch {
        Fail "lingtai-tui.exe did not run: $($_.Exception.Message)"
    }

    if (-not $SkipPortal) {
        $portalExe = Join-Path $Dir 'lingtai-portal.exe'
        if (Test-Path -LiteralPath $portalExe) {
            try {
                $pout = & $portalExe --version 2>&1
                $pcode = $LASTEXITCODE
                if ($pcode -ne 0) {
                    Write-Warn "lingtai-portal.exe --version failed with exit code ${pcode}: $pout"
                } else {
                    Write-Ok "lingtai-portal.exe --version -> $pout"
                }
            } catch {
                Write-Warn "lingtai-portal.exe did not run cleanly: $($_.Exception.Message)"
            }
        }
    }
}

# --- Runtime venv ------------------------------------------------------------

function Install-Venv {
    param([string]$Dir)

    $globalDir = Get-GlobalDir
    $venvDir   = Join-Path $globalDir 'runtime\venv'
    $venvPython = Join-Path $venvDir 'Scripts\python.exe'

    Write-Info "Provisioning Python runtime venv at $venvDir"

    if ($DryRun) {
        Write-Step "[dry-run] would ensure uv, then: uv venv --python $PyVersion `"$venvDir`""
        Write-Step "[dry-run] would run: uv pip -p `"$venvDir`" install --upgrade lingtai"
        Write-Step "[dry-run] would verify 'import lingtai' and stamp the env marker"
        return $true
    }

    $uv = Find-Uv
    if (-not $uv) {
        Write-Step "uv not found; installing it..."
        $uv = Install-Uv
    }
    Write-Ok "Using uv at $uv"

    New-Item -ItemType Directory -Force -Path (Split-Path $venvDir -Parent) | Out-Null

    Write-Step "Creating venv (Python $PyVersion)..."
    & $uv venv --python $PyVersion $venvDir
    if ($LASTEXITCODE -ne 0) { Fail "uv venv failed (exit $LASTEXITCODE)." }

    Write-Step "Installing the lingtai runtime package..."
    & $uv pip -p $venvDir install --upgrade lingtai
    if ($LASTEXITCODE -ne 0) { Fail "uv pip install lingtai failed (exit $LASTEXITCODE)." }

    if (-not (Test-Path -LiteralPath $venvPython)) {
        Fail "Runtime venv created but $venvPython is missing."
    }

    Write-Step "Verifying 'import lingtai'..."
    & $venvPython -c "import lingtai; print(lingtai.__version__)" | Out-Null
    if ($LASTEXITCODE -ne 0) { Fail "The runtime venv exists but cannot import lingtai." }
    Write-Ok "Runtime venv ready ($venvDir)."

    # Best-effort env marker stamp (records OS/arch so the TUI rebuilds a
    # cross-platform venv correctly). Non-fatal.
    try {
        & $venvPython -m lingtai.venv_resolve env-marker stamp --venv $venvDir *> $null
    } catch {
        Write-Step "env-marker stamp skipped (non-fatal)."
    }

    # lingtai-agent shim: a .cmd forwarder (never a symlink, which needs
    # privilege on Windows). Points at the venv entry point.
    $agentExe = Join-Path $venvDir 'Scripts\lingtai-agent.exe'
    if (Test-Path -LiteralPath $agentExe) {
        $shim = Join-Path $Dir 'lingtai-agent.cmd'
        $shimBody = "@echo off`r`n`"$agentExe`" %*`r`n"
        Set-Content -LiteralPath $shim -Value $shimBody -Encoding ASCII -NoNewline
        Write-Ok "Created lingtai-agent shim -> $shim"
    } else {
        Write-Step "lingtai-agent.exe not present in venv; skipping shim."
    }

    return $true
}

# --- install.json metadata ---------------------------------------------------

function Write-InstallMetadata {
    param(
        [string]$GlobalDir,
        [string]$Prefix,
        [string]$BinDir,
        [string]$RequestedRef,
        [string]$ResolvedRef
    )

    $tuiPath = Join-Path $BinDir 'lingtai-tui.exe'
    $managed = @($tuiPath)
    if (-not $SkipPortal) {
        $portalPath = Join-Path $BinDir 'lingtai-portal.exe'
        if ($DryRun -or (Test-Path -LiteralPath $portalPath)) { $managed += $portalPath }
    }

    # stamped_version mirrors install.sh: the tag without a leading 'v'.
    $stamped = $ResolvedRef -replace '^v', ''

    # This metadata is diagnostic/tracking ONLY. install_method is deliberately
    # NOT "source": the TUI's source updater treats install_method="source" as
    # permission to run `install.sh --update` through bash, which is a POSIX-only
    # update path that does not exist on native Windows. Using "powershell" makes
    # isSourceInstallMetadata() (tui/internal/config/venv.go) return false, so the
    # updater correctly falls back to "unknown" and prints manual-upgrade guidance
    # instead of trying to shell out to bash. Native PowerShell self-update is not
    # supported yet; upgrade by re-running the installer.
    $meta = [ordered]@{
        schema           = 'lingtai.tui.install/v1'
        schema_version   = 1
        install_method   = 'powershell'
        install_kind     = 'powershell-prebuilt'
        self_update      = $false
        upgrade_command  = 'iwr -useb https://lingtai.ai/install.ps1 | iex'
        prefix           = $Prefix
        bin_dir          = $BinDir
        repo_url         = $RepoUrl
        requested_ref    = $RequestedRef
        resolved_ref     = $ResolvedRef
        resolved_commit  = ''
        stamped_version  = $stamped
        installed_at     = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
        managed_binaries = $managed
    }

    $metaPath = Join-Path $GlobalDir 'install.json'
    if ($DryRun) {
        Write-Step "[dry-run] would write install metadata -> $metaPath"
        return
    }
    New-Item -ItemType Directory -Force -Path $GlobalDir | Out-Null
    ($meta | ConvertTo-Json -Depth 5) | Set-Content -LiteralPath $metaPath -Encoding UTF8
    Write-Ok "Wrote install metadata -> $metaPath"
}

# --- Main --------------------------------------------------------------------

function Main {
    Write-Host ""
    Write-Host "LingTai — native Windows installer (EXPERIMENTAL)" -ForegroundColor Magenta
    Write-Host "------------------------------------------------" -ForegroundColor Magenta
    if ($DryRun) { Write-Warn "DRY RUN: no downloads, installs, or PATH/file changes will be made." }

    $arch = Get-Arch
    if ([string]::IsNullOrWhiteSpace($BinDir)) { $BinDir = Get-DefaultBinDir }
    # prefix is the parent of BinDir, matching install.sh's <prefix>/bin convention
    # (default BinDir ...\Programs\lingtai\bin -> prefix ...\Programs\lingtai).
    $prefix = Split-Path $BinDir -Parent
    if ([string]::IsNullOrWhiteSpace($prefix)) { $prefix = $BinDir }
    $globalDir = Get-GlobalDir

    $tag = Resolve-Version -Requested $Version
    Write-Info "Installing LingTai $tag (windows/$arch) into $BinDir"

    # 1. Binaries
    Install-Binaries -Tag $tag -Arch $arch -Dir $BinDir
    Test-BinaryVersion -Dir $BinDir

    # 2. PATH
    Add-ToUserPath -Dir $BinDir

    # 3. Runtime venv. Install-Venv calls Fail (which throws) on any error, so a
    # runtime failure propagates to the outer catch and prints the WSL fallback —
    # no clean success is printed without a working runtime.
    if ($SkipVenv) {
        Write-Warn "Skipping runtime venv (-SkipVenv). The TUI will provision it on first run."
    } else {
        Install-Venv -Dir $BinDir | Out-Null
    }

    # 4. Metadata
    Write-InstallMetadata -GlobalDir $globalDir -Prefix $prefix -BinDir $BinDir -RequestedRef ($Version) -ResolvedRef $tag

    # 5. Summary + experimental banner
    Write-Host ""
    Write-Host "LingTai installed." -ForegroundColor Green
    Write-Host ""
    Write-Host "EXPERIMENTAL: native Windows support" -ForegroundColor Yellow
    Write-Host "  The TUI and portal work, but two agent capabilities run at reduced" -ForegroundColor Yellow
    Write-Host "  fidelity natively: daemon/subagents (分身) are unavailable, and the" -ForegroundColor Yellow
    Write-Host '  bash tool runs cmd.exe rather than bash.' -ForegroundColor Yellow
    Write-Host "  For full parity, use WSL2 + install.sh." -ForegroundColor Yellow
    Write-Host ""
    Write-Host "Next steps:" -ForegroundColor Cyan
    Write-Host "    mkdir my-project; cd my-project" -ForegroundColor White
    Write-Host "    lingtai-tui" -ForegroundColor White
    Write-Host ""
    Write-Host "To upgrade later (no native PowerShell self-update yet), re-run:" -ForegroundColor Cyan
    Write-Host "    iwr -useb https://lingtai.ai/install.ps1 | iex" -ForegroundColor White
    if ($NoModifyPath) {
        Write-Host ""
        Write-Warn "BinDir was not added to PATH (-NoModifyPath). Run binaries by full path or add '$BinDir' to PATH."
    } else {
        Write-Host ""
        Write-Step "If 'lingtai-tui' is not found, open a new terminal so the updated PATH is picked up."
    }
}

try {
    Main
    exit 0
} catch {
    if (-not $DryRun) { Show-WslFallback }
    exit 1
}
