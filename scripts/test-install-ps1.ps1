#requires -Version 5.1
<#
.SYNOPSIS
    Windows-hosted contract tests for the native PowerShell installer (install.ps1).

.DESCRIPTION
    This is the test-first, implementation-independent contract for the
    Windows-native `install.ps1`. It is the PowerShell analogue of
    scripts/test-install-sh.sh and is designed to run *identically* under both
    Windows PowerShell 5.1 (Desktop) and PowerShell 7+ (Core) on windows-latest.

    The suite begins with an explicit "installer script is present"
    precondition. If install.ps1 is missing, the contract fails loudly instead
    of silently exercising a stub; when present, every behavior below is driven
    only through the documented public parameter seams.

    The installer is exercised ONLY through its public parameter seams so the
    contract stays independent of implementation details:

        -Version <string>        exact release tag/version to install
        -BinDir <path>           directory the binaries install into
        -GlobalDir <path>        per-user global state dir (~/.lingtai-tui analogue)
        -ArchivePath <path>      local fixture archive to install FROM
                                 (download/expand equivalent; no network)
        -ChecksumPath <path>     SHA-256 sidecar for -ArchivePath
        -SkipVenv                skip the Python runtime venv step
        -NoModifyPath            do not persist PATH changes
        -DryRun                  plan only; make no filesystem writes

    Every run isolates HOME / USERPROFILE / LOCALAPPDATA / TEMP / TMP and all
    install outputs under a single throwaway test root, so a run can never touch
    the developer's real profile or PATH.

    Public-mode (no -ArchivePath) contracts -- tag/latest resolution, bundle
    manifest validation, Windows archive+sidecar download/verify, and the
    pinned-kernel venv/wheel install -- are driven against a local
    System.Net.HttpListener fake GitHub API (see "fake GitHub API server"
    below), reached through install.ps1's LINGTAI_GITHUB_API_BASE /
    LINGTAI_KERNEL_GITHUB_API_BASE override seam. No test in this suite depends
    on a live GitHub release.

.NOTES
    Exit code 0 => all contract tests passed (only possible once install.ps1
    exists AND satisfies the contract). Non-zero => at least one contract
    assertion failed, including the expected "installer absent" RED state.
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

# ---------------------------------------------------------------------------
# Locate the repository root and the (future) installer under test.
# ---------------------------------------------------------------------------
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot  = Split-Path -Parent $ScriptDir
$InstallScript = Join-Path $RepoRoot 'install.ps1'

# ---------------------------------------------------------------------------
# Tiny test harness (no external module dependency -- Pester is not guaranteed
# to be present on windows-latest for both PS 5.1 and PS 7).
# ---------------------------------------------------------------------------
$script:Failures = 0
$script:Passed   = 0
$script:NotYet   = 0

function Write-Section {
    param([string]$Name)
    Write-Host ''
    Write-Host "== $Name =="
}

function Assert-True {
    param([bool]$Condition, [string]$Label)
    if ($Condition) {
        $script:Passed++
        Write-Host "  ok   - $Label"
    } else {
        $script:Failures++
        Write-Host "  FAIL - $Label"
    }
}

function Assert-Equal {
    param($Expected, $Actual, [string]$Label)
    if ($Expected -eq $Actual) {
        $script:Passed++
        Write-Host "  ok   - $Label"
    } else {
        $script:Failures++
        Write-Host "  FAIL - $Label : expected [$Expected], got [$Actual]"
    }
}

# A test whose behavior is deliberately deferred. It records the gap loudly and
# honestly instead of pretending the contract point is covered.
function Skip-NotYet {
    param([string]$Label, [string]$Reason)
    $script:NotYet++
    Write-Host "  NOT-YET - $Label : $Reason"
}

# ---------------------------------------------------------------------------
# Environment isolation. Every install output, and every ambient location the
# installer might read/write, is redirected under one test root. Original
# values are captured and restored in the finally block.
# ---------------------------------------------------------------------------
$IsolatedVars = @('HOME', 'USERPROFILE', 'LOCALAPPDATA', 'TEMP', 'TMP')
$SavedEnv = @{}
foreach ($name in $IsolatedVars) {
    $SavedEnv[$name] = [Environment]::GetEnvironmentVariable($name, 'Process')
}

# The test root deliberately contains a SPACE so that every child path derived
# from it forces the installer invocation and Windows path handling through the
# argument-quoting path (a common source of Windows installer bugs).
$TestRoot = Join-Path ([IO.Path]::GetTempPath()) ("lingtai ps inst {0}" -f ([Guid]::NewGuid().ToString('N')))
New-Item -ItemType Directory -Force -Path $TestRoot | Out-Null

function New-IsolatedHome {
    <#
      Returns a fresh, empty isolated HOME/profile and points every ambient env
      var at it, so an install invocation cannot escape $TestRoot. Each contract
      test that installs gets its own home so tests never cross-contaminate.

      The home directory name contains a SPACE, and so does the 'bin' leaf used
      by callers (via Join-Path on this home), so child-process argument quoting
      and Windows spaced-path handling are exercised end to end.
    #>
    # PowerShell variable names are case-insensitive; `$home` would collide with
    # the automatic read-only `$HOME` variable on PowerShell 7.
    $isolatedHome = Join-Path $TestRoot ("home dir {0}" -f ([Guid]::NewGuid().ToString('N')))
    $localAppData = Join-Path $isolatedHome 'AppData\Local'
    $tempDir = Join-Path $isolatedHome 'Temp'
    New-Item -ItemType Directory -Force -Path $isolatedHome, $localAppData, $tempDir | Out-Null
    [Environment]::SetEnvironmentVariable('HOME', $isolatedHome, 'Process')
    [Environment]::SetEnvironmentVariable('USERPROFILE', $isolatedHome, 'Process')
    [Environment]::SetEnvironmentVariable('LOCALAPPDATA', $localAppData, 'Process')
    [Environment]::SetEnvironmentVariable('TEMP', $tempDir, 'Process')
    [Environment]::SetEnvironmentVariable('TMP', $tempDir, 'Process')
    return $isolatedHome
}

# ---------------------------------------------------------------------------
# Deterministic local Windows fixture archive.
#
# Builds a .zip containing REAL native Windows `lingtai-tui.exe` and
# `lingtai-portal.exe` executables plus a matching SHA-256 sidecar, entirely
# offline. On Windows the installer invokes the explicit installed
# `lingtai-tui.exe` path and runs `<binary> version` (or `--version`) to verify
# it reports the requested version; Windows will reject a text file at a .exe
# path as an invalid PE image before any `.cmd` fallback is considered, so the
# fixture MUST be a genuine PE executable.
#
# The stubs are compiled offline from a tiny stdlib-only Go program using the Go
# toolchain that is already present on windows-latest (and in this Go repo). No
# setup-go and no network. If `go` is unavailable, fixture setup fails LOUDLY
# rather than silently degrading to a non-executable shim.
#
# Two flavors are produced per contract need:
#   * a "good" stub that prints an exact version line, and
#   * (per-test) a "wrong version" or "missing tui" variant.
#
# The contract asserts on the installer's *observable* behavior (version match /
# required-binary presence); the builders below are the single source of
# fixtures.
# ---------------------------------------------------------------------------

# Resolve the Go toolchain once. Fail loudly (not silently) if absent -- a fixture
# that cannot produce a real .exe would make the whole suite dishonest.
$script:GoExe = $null
function Get-GoToolchain {
    if ($null -ne $script:GoExe) { return $script:GoExe }
    $go = Get-Command -Name 'go' -CommandType Application -ErrorAction SilentlyContinue |
        Select-Object -First 1
    if (-not $go) {
        throw "fixture setup: 'go' toolchain not found on PATH. A real native lingtai-tui.exe / lingtai-portal.exe fixture cannot be built offline without it. Install Go on the runner (already present on windows-latest) -- do NOT fall back to a non-PE shim."
    }
    $script:GoExe = $go.Source
    return $script:GoExe
}

function New-StubExe {
    <#
      Compiles a REAL native Windows executable at $Path that, when invoked as
      `<path> version` or `<path> --version`, prints $VersionLine to stdout and
      exits 0. Built offline from a stdlib-only Go program via `go build`.
      The installer resolves and runs the explicit .exe path, so this must be a
      genuine PE image -- no text/.cmd shim.
    #>
    param(
        [string]$Path,
        [string]$VersionLine
    )
    $go = Get-GoToolchain
    $dir = Split-Path -Parent $Path
    if (-not (Test-Path $dir)) { New-Item -ItemType Directory -Force -Path $dir | Out-Null }

    # Per-build throwaway Go module dir under $TestRoot (isolated; GOFLAGS=-mod=mod
    # not needed since there are no imports beyond the standard library).
    $buildDir = Join-Path $TestRoot ("gostub-{0}" -f ([Guid]::NewGuid().ToString('N')))
    New-Item -ItemType Directory -Force -Path $buildDir | Out-Null

    # The version line is embedded as a Go string literal; %q-style quoting via
    # a here-string is unsafe for arbitrary input, but our version lines are
    # controlled ASCII. Escape backslashes and double-quotes defensively.
    $escaped = $VersionLine.Replace('\', '\\').Replace('"', '\"')
    $goSrc = @"
package main

import (
	"fmt"
	"os"
)

func main() {
	// Print the version for `version`, `--version`, or `-version`; the
	// installer's verification calls one of these forms.
	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "version", "--version", "-version":
			fmt.Println("$escaped")
			os.Exit(0)
		}
	}
	fmt.Println("$escaped")
	os.Exit(0)
}
"@
    Set-Content -LiteralPath (Join-Path $buildDir 'main.go') -Value $goSrc -Encoding ASCII
    Set-Content -LiteralPath (Join-Path $buildDir 'go.mod') -Value "module lingtaistub`n`ngo 1.21`n" -Encoding ASCII

    # Build a native windows/amd64 PE from the throwaway module directory.
    # GOOS/GOARCH are pinned so the fixture is a real Windows executable
    # regardless of any cross-build env on the runner. Fully offline: stdlib-only,
    # GOFLAGS neutralized, GOPROXY=off. Build env is saved and restored so it
    # never leaks into the installer child processes launched later.
    $savedGo = @{}
    foreach ($gv in 'GOOS','GOARCH','CGO_ENABLED','GOPROXY','GOFLAGS') {
        $savedGo[$gv] = [Environment]::GetEnvironmentVariable($gv, 'Process')
    }
    try {
        $env:GOOS = 'windows'
        $env:GOARCH = 'amd64'
        $env:CGO_ENABLED = '0'
        $env:GOPROXY = 'off'
        $env:GOFLAGS = ''
        # `-o <abs path>` with the module directory as the build target ('.'),
        # run from $buildDir so the module is resolved cleanly.
        Push-Location -LiteralPath $buildDir
        try {
            $buildOut = & $go build -o $Path '.' 2>&1
        } finally {
            Pop-Location
        }
        if ($LASTEXITCODE -ne 0) {
            throw "fixture setup: 'go build' failed for $Path (exit $LASTEXITCODE): $buildOut"
        }
    } finally {
        foreach ($gv in $savedGo.Keys) {
            [Environment]::SetEnvironmentVariable($gv, $savedGo[$gv], 'Process')
        }
    }
    if (-not (Test-Path -LiteralPath $Path)) {
        throw "fixture setup: 'go build' reported success but produced no file at $Path"
    }
}

function Get-Sha256Hex {
    param([string]$Path)
    (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash.ToLowerInvariant()
}

function New-FixtureArchive {
    <#
      Builds a deterministic .zip fixture under $TestRoot and returns a hashtable
      with ArchivePath, ChecksumPath, and the version the stub reports.

      Options:
        -Version         version string the tui/portal stubs report
        -IncludeTui      include lingtai-tui.exe stub (default $true)
        -IncludePortal   include lingtai-portal.exe stub (default $true)
        -TuiVersion      override the version the TUI stub reports (for the
                         wrong-version case); defaults to -Version
    #>
    param(
        [string]$Version = 'v9.9.9',
        [bool]$IncludeTui = $true,
        [bool]$IncludePortal = $true,
        [string]$TuiVersion
    )
    if (-not $TuiVersion) { $TuiVersion = $Version }

    $stage = Join-Path $TestRoot ("fixture-{0}" -f ([Guid]::NewGuid().ToString('N')))
    New-Item -ItemType Directory -Force -Path $stage | Out-Null

    if ($IncludeTui) {
        New-StubExe -Path (Join-Path $stage 'lingtai-tui.exe') -VersionLine "lingtai-tui $TuiVersion"
    }
    if ($IncludePortal) {
        New-StubExe -Path (Join-Path $stage 'lingtai-portal.exe') -VersionLine "lingtai-portal $Version"
    }

    # Unique GUID destination; a pre-existing file here would signal a serious
    # problem (GUID collision or leaked state), so fail loudly rather than
    # deleting anything.
    $archivePath = Join-Path $TestRoot ("lingtai-{0}.zip" -f ([Guid]::NewGuid().ToString('N')))
    if (Test-Path -LiteralPath $archivePath) {
        throw "fixture setup: archive destination unexpectedly already exists: $archivePath"
    }
    # Compress-Archive is available on both PS 5.1 and PS 7.
    Compress-Archive -Path (Join-Path $stage '*') -DestinationPath $archivePath

    $sha = Get-Sha256Hex -Path $archivePath
    $checksumPath = "$archivePath.sha256"
    # Sidecar format mirrors `sha256sum`: "<hex>  <filename>".
    Set-Content -LiteralPath $checksumPath -Value ("{0}  {1}" -f $sha, (Split-Path -Leaf $archivePath)) -Encoding ASCII

    return @{
        ArchivePath  = $archivePath
        ChecksumPath = $checksumPath
        Sha256       = $sha
        Version      = $Version
        Stage        = $stage
    }
}

# ---------------------------------------------------------------------------
# Fake GitHub API + release-asset server (System.Net.HttpListener).
#
# install.ps1's public-mode resolution (Resolve-PublicTag, Get-ReleaseAssetUrl,
# Get-BundleManifest, Get-KernelManifest, Install-KernelWheel) reads its base
# URLs from $env:LINGTAI_GITHUB_API_BASE / $env:LINGTAI_KERNEL_GITHUB_API_BASE
# -- the exact override seam install.ps1 exposes for this offline suite (mirrors
# install.sh's LINGTAI_GITEE_OWNER/LINGTAI_GITEE_REPO pattern). Pointing those
# at a local HttpListener lets every public-mode contract below run fully
# offline and deterministically, with no dependency on a live GitHub release.
#
# Routes are registered as exact path -> {Status, Body} entries; a request for
# an unregistered path returns 404, matching "no such asset" from the real API.
#
# NOTE ON RELEASE ASSET DOWNLOADS: the real GitHub API's release JSON embeds a
# `browser_download_url` per asset, and install.ps1 downloads FROM that URL --
# it never constructs a download URL itself. This fake server exploits that:
# every asset's browser_download_url simply points back at this same listener
# under /assets/<name>, so registering the bytes at that path is sufficient;
# install.ps1 needs no additional knowledge of a separate download host.
# ---------------------------------------------------------------------------
$script:FakeApiListener = $null
$script:FakeApiPrefix = $null
$script:FakeApiRoutes = @{}
$script:FakeApiJob = $null

function Start-FakeGitHubApi {
    $port = Get-Random -Minimum 30000 -Maximum 40000
    $prefix = "http://127.0.0.1:$port/"
    $listener = New-Object System.Net.HttpListener
    $listener.Prefixes.Add($prefix)
    $listener.Start()
    $script:FakeApiListener = $listener
    $script:FakeApiPrefix = $prefix
    $script:FakeApiRoutes = @{}

    # A background runspace pumps requests so the main test thread is never
    # blocked; each response is looked up by exact path in $script:FakeApiRoutes
    # via a synchronized hashtable (thread-safe cross-runspace sharing).
    $sync = [hashtable]::Synchronized($script:FakeApiRoutes)
    $script:FakeApiRoutes = $sync

    $ps = [powershell]::Create()
    $ps.AddScript({
        param($listener, $routes)
        while ($listener.IsListening) {
            try {
                $context = $listener.GetContext()
            } catch {
                break
            }
            $path = $context.Request.Url.AbsolutePath
            $route = $routes[$path]
            if ($route) {
                $context.Response.StatusCode = $route.Status
                # The real GitHub API always answers with Content-Type: application/json;
                # install.ps1's Invoke-GitHubApi relies on Invoke-RestMethod's
                # Content-Type-driven auto-parsing to turn the body into an object
                # (HttpListener otherwise defaults to text/html, which Invoke-RestMethod
                # returns as an unparsed string -- silently breaking every .tag_name /
                # .assets access downstream). Set it whenever the route declares one.
                if ($route.ContentType) { $context.Response.ContentType = $route.ContentType }
                # Routes always carry raw bytes (BodyBytes) so binary assets
                # (zips, wheels) round-trip exactly -- never re-encoded through
                # a string, which would corrupt non-ASCII byte values.
                $bytes = $route.BodyBytes
                $context.Response.ContentLength64 = $bytes.Length
                $context.Response.OutputStream.Write($bytes, 0, $bytes.Length)
            } else {
                $context.Response.StatusCode = 404
                $bytes = [System.Text.Encoding]::UTF8.GetBytes('not found')
                $context.Response.ContentLength64 = $bytes.Length
                $context.Response.OutputStream.Write($bytes, 0, $bytes.Length)
            }
            $context.Response.OutputStream.Close()
        }
    }) | Out-Null
    $ps.AddArgument($listener) | Out-Null
    $ps.AddArgument($sync) | Out-Null
    $script:FakeApiJob = $ps.BeginInvoke()
    $script:FakeApiPs = $ps

    return $prefix
}

function Stop-FakeGitHubApi {
    if ($script:FakeApiListener) {
        try { $script:FakeApiListener.Stop() } catch {}
        try { $script:FakeApiListener.Close() } catch {}
    }
    if ($script:FakeApiPs) {
        try { $script:FakeApiPs.Stop() } catch {}
        try { $script:FakeApiPs.Dispose() } catch {}
    }
}

function Register-FakeApiRoute {
    <#
      Registers a raw-bytes response. Prefer this (or the Text/Bytes wrappers
      below) over hand-building the route hashtable so every path stores
      BodyBytes, never a re-encoded string.
    #>
    param([string]$Path, [byte[]]$BodyBytes, [int]$Status = 200, [string]$ContentType = '')
    $script:FakeApiRoutes[$Path] = @{ Status = $Status; BodyBytes = $BodyBytes; ContentType = $ContentType }
}

function Register-FakeApiRouteText {
    param([string]$Path, [string]$Text, [int]$Status = 200, [string]$ContentType = '')
    Register-FakeApiRoute -Path $Path -BodyBytes ([System.Text.Encoding]::UTF8.GetBytes($Text)) -Status $Status -ContentType $ContentType
}

function Register-FakeApiAsset {
    <#
      Registers a binary asset at /assets/<name> and returns the object shape
      the GitHub release-assets API embeds ({name, browser_download_url}).
    #>
    param([string]$Name, [byte[]]$Bytes)
    Register-FakeApiRoute -Path "/assets/$Name" -BodyBytes $Bytes
    return @{ name = $Name; browser_download_url = "$($script:FakeApiPrefix)assets/$Name" }
}

function Register-FakeApiAssetText {
    param([string]$Name, [string]$Text)
    Register-FakeApiRouteText -Path "/assets/$Name" -Text $Text
    return @{ name = $Name; browser_download_url = "$($script:FakeApiPrefix)assets/$Name" }
}

function Register-FakeRelease {
    <#
      Registers GET /repos/<repo>/releases/tags/<tag> (and, if -Latest, also
      .../releases/latest) with the given asset list, mirroring the real
      GitHub release API's {tag_name, assets: [{name, browser_download_url}]}.
    #>
    param([string]$ApiPathPrefix, [string]$Tag, [array]$Assets, [switch]$Latest)
    $body = @{ tag_name = $Tag; assets = $Assets } | ConvertTo-Json -Depth 6
    # install.ps1's Invoke-GitHubApi reads this route through Invoke-RestMethod,
    # which only auto-parses the body into an object when Content-Type says JSON
    # (HttpListener's unset default is text/html); the real GitHub API always
    # answers these endpoints with application/json, so mirror that here.
    Register-FakeApiRouteText -Path "$ApiPathPrefix/releases/tags/$Tag" -Text $body -ContentType 'application/json; charset=utf-8'
    if ($Latest) {
        Register-FakeApiRouteText -Path "$ApiPathPrefix/releases/latest" -Text $body -ContentType 'application/json; charset=utf-8'
    }
}

# ---------------------------------------------------------------------------
# Bundle / kernel manifest fixture builders (schema lingtai.tui.bundle/v1 and
# lingtai.kernel.release/v1) -- same shapes install.sh's strict parser and
# scripts/test-install-sh-gitee-bundle.sh's fixtures use.
# ---------------------------------------------------------------------------
function New-BundleManifestJson {
    param(
        [string]$Tag,
        [string]$TuiCommit = ('a' * 40),
        [string]$KernelTag = 'v0.18.0',
        [string]$KernelVersion = '0.18.0',
        [string]$ArchiveFilename,
        [string]$ArchiveSha256
    )
    if (-not $ArchiveFilename) { $ArchiveFilename = "lingtai-$Tag-windows-amd64.zip" }
    $manifest = [ordered]@{
        schema                   = 'lingtai.tui.bundle/v1'
        bundle_id                = $Tag
        tui_tag                  = $Tag
        tui_commit                = $TuiCommit
        generated_at              = '2026-07-21T00:00:00Z'
        kernel_tag                = $KernelTag
        kernel_version             = $KernelVersion
        kernel_manifest_filename   = 'lingtai-kernel-release-manifest.json'
        archives                  = @(@{ filename = $ArchiveFilename; sha256 = $ArchiveSha256 })
        providers                 = @{
            github = @{ repo = 'Lingtai-AI/lingtai' }
            gitee  = @{ owner = 'huangzesen1997'; repo = 'lingtai' }
        }
    }
    return ($manifest | ConvertTo-Json -Depth 6)
}

function New-KernelManifestJson {
    param(
        [string]$KernelVersion = '0.18.0',
        [array]$Wheels
    )
    $artifacts = @()
    foreach ($w in $Wheels) {
        $artifacts += @{
            filename    = $w.filename
            sha256      = $w.sha256
            kind        = 'wheel'
            python_tag  = $w.python_tag
            abi_tag     = $w.abi_tag
            platform_tag = $w.platform_tag
        }
    }
    $manifest = [ordered]@{
        schema         = 'lingtai.kernel.release/v1'
        kernel_version = $KernelVersion
        artifacts      = $artifacts
    }
    return ($manifest | ConvertTo-Json -Depth 6)
}

# ---------------------------------------------------------------------------
# Installer invocation seam. Runs install.ps1 in a child PowerShell using the
# SAME host executable that is running this suite, so PS 5.1 vs PS 7 coverage is
# driven entirely by which host launches this file. Captures stdout, stderr, and
# exit code without ever throwing on non-zero exit (the contract inspects the
# exit code explicitly).
# ---------------------------------------------------------------------------
function Invoke-Installer {
    param([hashtable]$Params)

    if (-not (Test-Path -LiteralPath $InstallScript)) {
        # Honest RED signal: there is no script to invoke yet.
        return @{
            ExitCode = 127
            Stdout   = ''
            Stderr   = "install.ps1 not found at $InstallScript"
            Ran      = $false
        }
    }

    # Build a parameter splat string for the child invocation.
    $argList = New-Object System.Collections.Generic.List[string]
    $argList.Add('-NoProfile')
    $argList.Add('-NonInteractive')
    $argList.Add('-ExecutionPolicy'); $argList.Add('Bypass')
    $argList.Add('-File'); $argList.Add($InstallScript)
    foreach ($k in $Params.Keys) {
        $v = $Params[$k]
        if ($v -is [bool]) {
            if ($v) { $argList.Add("-$k") }   # switch parameter
        } else {
            $argList.Add("-$k"); $argList.Add([string]$v)
        }
    }

    # Use the same host process for parity between PS 5.1 and PS 7. Invoke it
    # directly with PowerShell array splatting so every argument remains a
    # distinct argv entry even when InstallScript/BinDir/GlobalDir contain
    # spaces. Start-Process joins an ArgumentList array into one string and can
    # silently lose those boundaries unless callers add platform-specific quote
    # escaping; this contract harness must not create that false failure mode.
    $psHost = (Get-Process -Id $PID).Path
    [string[]]$invokeArgs = $argList.ToArray()
    $outFile = Join-Path $TestRoot ("out-{0}.txt" -f ([Guid]::NewGuid().ToString('N')))
    $errFile = Join-Path $TestRoot ("err-{0}.txt" -f ([Guid]::NewGuid().ToString('N')))
    & $psHost @invokeArgs 1> $outFile 2> $errFile
    $exitCode = $LASTEXITCODE
    return @{
        ExitCode = $exitCode
        Stdout   = (Get-Content -LiteralPath $outFile -Raw -ErrorAction SilentlyContinue)
        Stderr   = (Get-Content -LiteralPath $errFile -Raw -ErrorAction SilentlyContinue)
        Ran      = $true
    }
}

# Snapshot of an ENTIRE tree -- every descendant directory AND file -- for DryRun
# no-write assertions. Enumerating files only would let the creation of empty
# directories evade the no-write claim, so directories are captured too. Each
# entry is recorded as "<D|F>\t<relative-path>" using a path relative to $Path so
# the snapshot is stable and comparable across before/after and across the two
# PowerShell hosts. Sorted for deterministic comparison.
function Get-TreeSnapshot {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return @() }
    $root = (Resolve-Path -LiteralPath $Path).ProviderPath.TrimEnd('\')
    Get-ChildItem -LiteralPath $Path -Recurse -Force -ErrorAction SilentlyContinue |
        ForEach-Object {
            $type = if ($_.PSIsContainer) { 'D' } else { 'F' }
            $full = $_.FullName
            $rel = $full
            if ($full.StartsWith($root)) { $rel = $full.Substring($root.Length).TrimStart('\') }
            "{0}`t{1}" -f $type, $rel
        } | Sort-Object
}

try {
    # -----------------------------------------------------------------------
    # PRECONDITION: installer script must exist. Until it does, this fails and
    # the whole suite is honestly RED.
    # -----------------------------------------------------------------------
    Write-Section 'precondition: installer script present'
    $installerPresent = Test-Path -LiteralPath $InstallScript
    Assert-True $installerPresent "install.ps1 exists at repo root ($InstallScript)"
    if (-not $installerPresent) {
        Write-Host ''
        Write-Host "install.ps1 is absent -- the contract suite is RED by design (test-first)."
        Write-Host "Every contract test below will now fail for the same reason. Implement"
        Write-Host "install.ps1 against the public seams to turn this suite green."
    }

    # Report the host so the workflow logs prove PS 5.1 and PS 7 both parsed and
    # executed the identical file.
    Write-Host ("  host: {0} {1}" -f $PSVersionTable.PSEdition, $PSVersionTable.PSVersion)

    # -----------------------------------------------------------------------
    # Fixture self-check (does not need the installer): the fixture builder must
    # produce an archive whose sidecar matches, proving the RED failures below
    # are about the missing installer, not a broken fixture.
    # -----------------------------------------------------------------------
    Write-Section 'fixture: deterministic archive + matching sidecar'
    $fx = New-FixtureArchive -Version 'v9.9.9'
    Assert-True (Test-Path -LiteralPath $fx.ArchivePath) 'fixture archive was created'
    Assert-True (Test-Path -LiteralPath $fx.ChecksumPath) 'fixture checksum sidecar was created'
    Assert-Equal $fx.Sha256 (Get-Sha256Hex -Path $fx.ArchivePath) 'sidecar sha256 matches archive bytes'

    # -----------------------------------------------------------------------
    # Fake GitHub API + asset server. Every test in this suite -- including the
    # pre-existing local-artifact contracts below, which now also resolve a
    # bundle manifest for their default (non -SkipVenv) runtime step -- points
    # install.ps1 at this listener via LINGTAI_GITHUB_API_BASE /
    # LINGTAI_KERNEL_GITHUB_API_BASE so NOTHING in this suite depends on a live
    # GitHub release. An unregistered path 404s exactly like a real missing
    # release/asset, which is what gives CONTRACT 9b (below) its deterministic
    # "no bundle available" failure without a live network dependency.
    # -----------------------------------------------------------------------
    Write-Section 'fixture: fake GitHub API server'
    $apiPrefix = Start-FakeGitHubApi
    Assert-True (-not [string]::IsNullOrWhiteSpace($apiPrefix)) 'fake GitHub API server started'
    [Environment]::SetEnvironmentVariable('LINGTAI_GITHUB_API_BASE', "$($apiPrefix)repos/Lingtai-AI/lingtai", 'Process')
    [Environment]::SetEnvironmentVariable('LINGTAI_KERNEL_GITHUB_API_BASE', "$($apiPrefix)repos/Lingtai-AI/lingtai-kernel", 'Process')

    # -----------------------------------------------------------------------
    # CONTRACT 1: local fixture install (download/expand equivalent).
    # A clean install from -ArchivePath must succeed and place lingtai-tui(.exe)
    # (and lingtai-portal(.exe)) under -BinDir.
    # -----------------------------------------------------------------------
    Write-Section 'contract: install from local fixture archive'
    $home1 = New-IsolatedHome
    $binDir1 = Join-Path $home1 'bin dir'
    $globalDir1 = Join-Path $home1 '.lingtai-tui'
    $r1 = Invoke-Installer @{
        Version      = 'v9.9.9'
        BinDir       = $binDir1
        GlobalDir    = $globalDir1
        ArchivePath  = $fx.ArchivePath
        ChecksumPath = $fx.ChecksumPath
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-Equal 0 $r1.ExitCode 'clean fixture install exits 0'
    $tui1 = Join-Path $binDir1 'lingtai-tui.exe'
    Assert-True (Test-Path -LiteralPath $tui1) 'installed lingtai-tui.exe under BinDir'
    Assert-True (Test-Path -LiteralPath (Join-Path $binDir1 'lingtai-portal.exe')) 'installed lingtai-portal.exe under BinDir'
    $metaPath1 = Join-Path $globalDir1 'install.json'
    Assert-True (Test-Path -LiteralPath $metaPath1) 'successful install wrote install.json'
    $metaBytes1 = [System.IO.File]::ReadAllBytes($metaPath1)
    $hasUtf8Bom1 = ($metaBytes1.Length -ge 3) -and `
        ($metaBytes1[0] -eq 0xEF) -and ($metaBytes1[1] -eq 0xBB) -and ($metaBytes1[2] -eq 0xBF)
    Assert-True (-not $hasUtf8Bom1) 'install.json is UTF-8 without BOM on this host'
    $meta1 = $null
    try { $meta1 = Get-Content -LiteralPath $metaPath1 -Raw | ConvertFrom-Json } catch { $meta1 = $null }
    Assert-True ($null -ne $meta1) 'install.json parses as JSON'
    if ($null -ne $meta1) {
        Assert-Equal 'powershell' $meta1.install_method 'install.json records powershell install method'
    }

    # -----------------------------------------------------------------------
    # CONTRACT 2: checksum enforcement -- wrong checksum fails loudly, no install.
    # -----------------------------------------------------------------------
    Write-Section 'contract: fail-loud on wrong checksum'
    $home2 = New-IsolatedHome
    $binDir2 = Join-Path $home2 'bin dir'
    $badChecksum = Join-Path $TestRoot ("bad-{0}.sha256" -f ([Guid]::NewGuid().ToString('N')))
    Set-Content -LiteralPath $badChecksum -Value ("{0}  {1}" -f ('0' * 64), (Split-Path -Leaf $fx.ArchivePath)) -Encoding ASCII
    $r2 = Invoke-Installer @{
        Version      = 'v9.9.9'
        BinDir       = $binDir2
        GlobalDir    = (Join-Path $home2 '.lingtai-tui')
        ArchivePath  = $fx.ArchivePath
        ChecksumPath = $badChecksum
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-True ($r2.ExitCode -ne 0) 'wrong checksum exits non-zero'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir2 'lingtai-tui.exe'))) 'wrong checksum installs nothing'

    # -----------------------------------------------------------------------
    # CONTRACT 3: missing checksum sidecar fails loudly (never install
    # unverified bytes).
    # -----------------------------------------------------------------------
    Write-Section 'contract: fail-loud on missing checksum sidecar'
    $home3 = New-IsolatedHome
    $binDir3 = Join-Path $home3 'bin dir'
    $missingChecksum = Join-Path $TestRoot ("does-not-exist-{0}.sha256" -f ([Guid]::NewGuid().ToString('N')))
    $r3 = Invoke-Installer @{
        Version      = 'v9.9.9'
        BinDir       = $binDir3
        GlobalDir    = (Join-Path $home3 '.lingtai-tui')
        ArchivePath  = $fx.ArchivePath
        ChecksumPath = $missingChecksum
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-True ($r3.ExitCode -ne 0) 'missing checksum sidecar exits non-zero'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir3 'lingtai-tui.exe'))) 'missing checksum installs nothing'

    # -----------------------------------------------------------------------
    # CONTRACT 4: required TUI binary enforcement. An archive missing
    # lingtai-tui must fail loudly even with a valid checksum.
    # -----------------------------------------------------------------------
    Write-Section 'contract: fail-loud on missing required TUI binary'
    $home4 = New-IsolatedHome
    $binDir4 = Join-Path $home4 'bin dir'
    $fxNoTui = New-FixtureArchive -Version 'v9.9.9' -IncludeTui $false -IncludePortal $true
    $r4 = Invoke-Installer @{
        Version      = 'v9.9.9'
        BinDir       = $binDir4
        GlobalDir    = (Join-Path $home4 '.lingtai-tui')
        ArchivePath  = $fxNoTui.ArchivePath
        ChecksumPath = $fxNoTui.ChecksumPath
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-True ($r4.ExitCode -ne 0) 'archive without lingtai-tui exits non-zero'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir4 'lingtai-tui.exe'))) 'missing-tui archive installs nothing'

    # -----------------------------------------------------------------------
    # CONTRACT 5: exact version verification. The installed TUI must report the
    # requested version; a mismatch fails loudly.
    # -----------------------------------------------------------------------
    Write-Section 'contract: exact version verification'
    $home5 = New-IsolatedHome
    $binDir5 = Join-Path $home5 'bin dir'
    # Fixture whose TUI stub reports a DIFFERENT version than requested.
    $fxWrongVer = New-FixtureArchive -Version 'v9.9.9' -TuiVersion 'v0.0.1'
    $r5 = Invoke-Installer @{
        Version      = 'v9.9.9'
        BinDir       = $binDir5
        GlobalDir    = (Join-Path $home5 '.lingtai-tui')
        ArchivePath  = $fxWrongVer.ArchivePath
        ChecksumPath = $fxWrongVer.ChecksumPath
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-True ($r5.ExitCode -ne 0) 'version mismatch between requested and reported exits non-zero'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir5 'lingtai-tui.exe'))) 'version mismatch installs nothing'

    # A prefix/superset is not an exact match: v9.9.90 must not satisfy v9.9.9.
    $home5b = New-IsolatedHome
    $binDir5b = Join-Path $home5b 'bin dir'
    $fxNearVer = New-FixtureArchive -Version 'v9.9.9' -TuiVersion 'v9.9.90'
    $r5b = Invoke-Installer @{
        Version      = 'v9.9.9'
        BinDir       = $binDir5b
        GlobalDir    = (Join-Path $home5b '.lingtai-tui')
        ArchivePath  = $fxNearVer.ArchivePath
        ChecksumPath = $fxNearVer.ChecksumPath
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-True ($r5b.ExitCode -ne 0) 'near-match version v9.9.90 does not satisfy exact request v9.9.9'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir5b 'lingtai-tui.exe'))) 'near-match version installs nothing'

    # -----------------------------------------------------------------------
    # CONTRACT 6: idempotent second install. Re-running the same install over an
    # existing one must succeed (exit 0) and leave the binary present.
    # -----------------------------------------------------------------------
    Write-Section 'contract: idempotent second install'
    $home6 = New-IsolatedHome
    $binDir6 = Join-Path $home6 'bin dir'
    $globalDir6 = Join-Path $home6 '.lingtai-tui'
    $common6 = @{
        Version      = 'v9.9.9'
        BinDir       = $binDir6
        GlobalDir    = $globalDir6
        ArchivePath  = $fx.ArchivePath
        ChecksumPath = $fx.ChecksumPath
        SkipVenv     = $true
        NoModifyPath = $true
    }
    $r6a = Invoke-Installer $common6
    $r6b = Invoke-Installer $common6
    Assert-Equal 0 $r6a.ExitCode 'first install exits 0'
    Assert-Equal 0 $r6b.ExitCode 'idempotent second install exits 0'
    Assert-True (Test-Path -LiteralPath (Join-Path $binDir6 'lingtai-tui.exe')) 'binary still present after second install'

    # -----------------------------------------------------------------------
    # CONTRACT 7: DryRun makes no filesystem writes.
    # -----------------------------------------------------------------------
    Write-Section 'contract: DryRun performs no writes'
    $home7 = New-IsolatedHome
    $binDir7 = Join-Path $home7 'bin dir'
    $globalDir7 = Join-Path $home7 '.lingtai-tui'
    New-Item -ItemType Directory -Force -Path $binDir7, $globalDir7 | Out-Null
    $before7 = @(Get-TreeSnapshot $binDir7) + @(Get-TreeSnapshot $globalDir7)
    $r7 = Invoke-Installer @{
        Version      = 'v9.9.9'
        BinDir       = $binDir7
        GlobalDir    = $globalDir7
        ArchivePath  = $fx.ArchivePath
        ChecksumPath = $fx.ChecksumPath
        SkipVenv     = $true
        NoModifyPath = $true
        DryRun       = $true
    }
    $after7 = @(Get-TreeSnapshot $binDir7) + @(Get-TreeSnapshot $globalDir7)
    Assert-Equal 0 $r7.ExitCode 'DryRun exits 0'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir7 'lingtai-tui.exe'))) 'DryRun did not install the TUI binary'
    # Full-tree comparison (files AND directories) -- a DryRun that created an
    # empty directory would be caught here, not just one that wrote a file.
    Assert-Equal ($before7 -join "`n") ($after7 -join "`n") 'DryRun left the complete tree (files and directories) unchanged'

    # -----------------------------------------------------------------------
    # CONTRACT 8: nonzero exit propagation. A structurally invalid invocation
    # (e.g. a nonexistent archive path) must surface as a non-zero exit rather
    # than a silent success.
    # -----------------------------------------------------------------------
    Write-Section 'contract: nonzero exit propagation on bad archive path'
    $home8 = New-IsolatedHome
    $binDir8 = Join-Path $home8 'bin dir'
    $r8 = Invoke-Installer @{
        Version      = 'v9.9.9'
        BinDir       = $binDir8
        GlobalDir    = (Join-Path $home8 '.lingtai-tui')
        ArchivePath  = (Join-Path $TestRoot 'no-such-archive.zip')
        ChecksumPath = $fx.ChecksumPath
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-True ($r8.ExitCode -ne 0) 'nonexistent archive path exits non-zero'

    # -----------------------------------------------------------------------
    # CONTRACT 9: SkipVenv is honored -- no runtime venv is created under
    # GlobalDir when -SkipVenv is passed. (The full venv path needs Python and
    # network and is out of scope for the offline smoke; here we assert the
    # negative: the skip seam prevents venv creation.)
    # -----------------------------------------------------------------------
    Write-Section 'contract: SkipVenv creates no runtime venv'
    $home9 = New-IsolatedHome
    $binDir9 = Join-Path $home9 'bin dir'
    $globalDir9 = Join-Path $home9 '.lingtai-tui'
    $r9 = Invoke-Installer @{
        Version      = 'v9.9.9'
        BinDir       = $binDir9
        GlobalDir    = $globalDir9
        ArchivePath  = $fx.ArchivePath
        ChecksumPath = $fx.ChecksumPath
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-Equal 0 $r9.ExitCode 'SkipVenv install exits 0'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $globalDir9 'runtime\venv'))) 'SkipVenv left no runtime venv'

    # A default (non -SkipVenv) install for a tag with no resolvable bundle
    # (v9.9.9 has no registered route on the fake API, so the bundle-manifest
    # fetch 404s exactly like a real missing release) must fail loud BEFORE any
    # binary or metadata write, so a caller never gets a failed command that
    # already changed BinDir.
    Write-Section 'contract: unresolvable runtime bundle fails before install writes'
    $home9b = New-IsolatedHome
    $binDir9b = Join-Path $home9b 'bin dir'
    $globalDir9b = Join-Path $home9b '.lingtai-tui'
    $r9b = Invoke-Installer @{
        Version      = 'v9.9.9'
        BinDir       = $binDir9b
        GlobalDir    = $globalDir9b
        ArchivePath  = $fx.ArchivePath
        ChecksumPath = $fx.ChecksumPath
        NoModifyPath = $true
    }
    Assert-True ($r9b.ExitCode -ne 0) 'unresolvable runtime bundle exits non-zero'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir9b 'lingtai-tui.exe'))) 'runtime preflight failure installs no TUI binary'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir9b 'lingtai-portal.exe'))) 'runtime preflight failure installs no portal binary'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $globalDir9b 'install.json'))) 'runtime preflight failure writes no install metadata'

    # -----------------------------------------------------------------------
    # CONTRACT 10: NoModifyPath does not persist PATH.
    #
    # A real PATH-persistence test would read/write the user PATH via the
    # registry (HKCU\Environment) or [Environment]::SetEnvironmentVariable(...,
    # 'User'), which is process-global machine state that cannot be isolated
    # under $TestRoot and cannot be safely restored across an abrupt failure on
    # a shared runner. We therefore only assert the SAFE negative here: with
    # -NoModifyPath, the install must not touch persistent PATH. The affirmative
    # "PATH persistence actually happens" case is left as an explicit NOT-YET
    # rather than faked, per the contract's honesty requirement.
    # -----------------------------------------------------------------------
    Write-Section 'contract: NoModifyPath leaves persistent PATH untouched'
    $home10 = New-IsolatedHome
    $binDir10 = Join-Path $home10 'bin dir'
    $userPathBefore = [Environment]::GetEnvironmentVariable('PATH', 'User')
    $r10 = Invoke-Installer @{
        Version      = 'v9.9.9'
        BinDir       = $binDir10
        GlobalDir    = (Join-Path $home10 '.lingtai-tui')
        ArchivePath  = $fx.ArchivePath
        ChecksumPath = $fx.ChecksumPath
        SkipVenv     = $true
        NoModifyPath = $true
    }
    $userPathAfter = [Environment]::GetEnvironmentVariable('PATH', 'User')
    Assert-Equal 0 $r10.ExitCode 'NoModifyPath install exits 0'
    Assert-Equal $userPathBefore $userPathAfter 'NoModifyPath did not change persistent user PATH'

    Skip-NotYet 'PATH persistence (affirmative case)' `
        'Persisting PATH writes process-global HKCU\Environment state that cannot be isolated under the test root nor safely restored on a shared runner after an abrupt failure; implement with a safe save/restore harness before enabling.'

    # =========================================================================
    # PUBLIC MODE (no -ArchivePath): resolution, bundle-manifest validation, and
    # the pinned-kernel runtime path, all served by the fake GitHub API above.
    # =========================================================================

    # -----------------------------------------------------------------------
    # CONTRACT 11: successful public-mode resolve + install with -SkipVenv.
    # An explicit -Version resolves the bundle manifest, downloads the Windows
    # archive + sidecar, verifies both against the manifest digest, confirms
    # the staged version, and installs into BinDir -- no live GitHub release
    # involved anywhere in this test.
    # -----------------------------------------------------------------------
    Write-Section 'contract: public mode resolves tag, validates bundle, installs from release asset'
    $pubTag = 'v11.0.0'
    $pubFx = New-FixtureArchive -Version $pubTag
    $pubZipBytes = [System.IO.File]::ReadAllBytes($pubFx.ArchivePath)
    $pubZipAsset = Register-FakeApiAsset -Name "lingtai-$pubTag-windows-amd64.zip" -Bytes $pubZipBytes
    $pubShaAsset = Register-FakeApiAssetText -Name "lingtai-$pubTag-windows-amd64.zip.sha256" -Text ("{0}  lingtai-$pubTag-windows-amd64.zip" -f $pubFx.Sha256)
    $pubBundleJson = New-BundleManifestJson -Tag $pubTag -ArchiveSha256 $pubFx.Sha256
    $pubManifestAsset = Register-FakeApiAssetText -Name 'lingtai-bundle-manifest.json' -Text $pubBundleJson
    Register-FakeRelease -ApiPathPrefix '/repos/Lingtai-AI/lingtai' -Tag $pubTag -Assets @($pubZipAsset, $pubShaAsset, $pubManifestAsset)

    $home11 = New-IsolatedHome
    $binDir11 = Join-Path $home11 'bin dir'
    $globalDir11 = Join-Path $home11 '.lingtai-tui'
    $r11 = Invoke-Installer @{
        Version      = $pubTag
        BinDir       = $binDir11
        GlobalDir    = $globalDir11
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-Equal 0 $r11.ExitCode 'public-mode install exits 0'
    Assert-True (Test-Path -LiteralPath (Join-Path $binDir11 'lingtai-tui.exe')) 'public-mode install placed lingtai-tui.exe under BinDir'
    $meta11 = $null
    try { $meta11 = Get-Content -LiteralPath (Join-Path $globalDir11 'install.json') -Raw | ConvertFrom-Json } catch {}
    Assert-True ($null -ne $meta11) 'public-mode install wrote a parseable install.json'
    if ($null -ne $meta11) {
        Assert-Equal 'powershell-release-asset' $meta11.install_kind 'public-mode install.json records install_kind powershell-release-asset'
        Assert-Equal $pubTag $meta11.resolved_ref 'public-mode install.json records the resolved tag'
    }

    # -----------------------------------------------------------------------
    # CONTRACT 12: release checksum sidecar disagreeing with the bundle
    # manifest digest is refused as mixed provenance -- installs nothing.
    # -----------------------------------------------------------------------
    Write-Section 'contract: public mode refuses a sidecar that disagrees with the bundle manifest digest'
    $mixTag = 'v11.0.1'
    $mixFx = New-FixtureArchive -Version $mixTag
    $mixZipAsset = Register-FakeApiAsset -Name "lingtai-$mixTag-windows-amd64.zip" -Bytes ([System.IO.File]::ReadAllBytes($mixFx.ArchivePath))
    # Sidecar reports a DIFFERENT (well-formed but wrong) digest than the bundle manifest.
    $wrongDigest = '0' * 64
    $mixShaAsset = Register-FakeApiAssetText -Name "lingtai-$mixTag-windows-amd64.zip.sha256" -Text ("{0}  lingtai-$mixTag-windows-amd64.zip" -f $wrongDigest)
    $mixBundleJson = New-BundleManifestJson -Tag $mixTag -ArchiveSha256 $mixFx.Sha256
    $mixManifestAsset = Register-FakeApiAssetText -Name 'lingtai-bundle-manifest.json' -Text $mixBundleJson
    Register-FakeRelease -ApiPathPrefix '/repos/Lingtai-AI/lingtai' -Tag $mixTag -Assets @($mixZipAsset, $mixShaAsset, $mixManifestAsset)

    $home12 = New-IsolatedHome
    $binDir12 = Join-Path $home12 'bin dir'
    $r12 = Invoke-Installer @{
        Version      = $mixTag
        BinDir       = $binDir12
        GlobalDir    = (Join-Path $home12 '.lingtai-tui')
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-True ($r12.ExitCode -ne 0) 'mixed-provenance sidecar/manifest digest exits non-zero'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir12 'lingtai-tui.exe'))) 'mixed-provenance digest installs nothing'

    # -----------------------------------------------------------------------
    # CONTRACT 13: malformed bundle manifest (wrong schema) fails loud.
    # -----------------------------------------------------------------------
    Write-Section 'contract: public mode rejects a malformed bundle manifest'
    $badTag = 'v11.0.2'
    $badManifestAsset = Register-FakeApiAssetText -Name 'lingtai-bundle-manifest.json' -Text '{"schema":"unexpected/v1"}'
    Register-FakeRelease -ApiPathPrefix '/repos/Lingtai-AI/lingtai' -Tag $badTag -Assets @($badManifestAsset)

    $home13 = New-IsolatedHome
    $binDir13 = Join-Path $home13 'bin dir'
    $r13 = Invoke-Installer @{
        Version      = $badTag
        BinDir       = $binDir13
        GlobalDir    = (Join-Path $home13 '.lingtai-tui')
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-True ($r13.ExitCode -ne 0) 'malformed bundle manifest schema exits non-zero'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir13 'lingtai-tui.exe'))) 'malformed bundle manifest installs nothing'

    # -----------------------------------------------------------------------
    # CONTRACT 13b: a bundle manifest with a genuine duplicate key WITHIN the
    # same JSON object fails loud. This is the regression case for a bug where
    # duplicate-key detection was a flat scan over the whole document: the
    # manifest schema legitimately has "repo" once under providers.github and
    # once under providers.gitee (different objects, same leaf key name), which
    # a flat scan would misreport as a duplicate and reject every valid
    # manifest. The scoped (depth-tracked) check must accept that shape while
    # still catching a REAL duplicate within one object.
    # -----------------------------------------------------------------------
    Write-Section 'contract: public mode rejects a manifest with a same-object duplicate key'
    $dupTag = 'v11.0.2b'
    # Two "schema" keys inside the SAME top-level object -- a genuine violation.
    $dupManifestText = '{"schema":"lingtai.tui.bundle/v1","schema":"lingtai.tui.bundle/v1","bundle_id":"' + $dupTag + '","tui_tag":"' + $dupTag + '","tui_commit":"' + ('a' * 40) + '","generated_at":"2026-07-21T00:00:00Z","kernel_tag":"v0.18.0","kernel_version":"0.18.0","kernel_manifest_filename":"lingtai-kernel-release-manifest.json","archives":[{"filename":"lingtai-' + $dupTag + '-windows-amd64.zip","sha256":"' + ('a' * 64) + '"}],"providers":{"github":{"repo":"Lingtai-AI/lingtai"},"gitee":{"owner":"huangzesen1997","repo":"lingtai"}}}'
    $dupManifestAsset = Register-FakeApiAssetText -Name 'lingtai-bundle-manifest.json' -Text $dupManifestText
    Register-FakeRelease -ApiPathPrefix '/repos/Lingtai-AI/lingtai' -Tag $dupTag -Assets @($dupManifestAsset)

    $home13b = New-IsolatedHome
    $binDir13b = Join-Path $home13b 'bin dir'
    $r13b = Invoke-Installer @{
        Version      = $dupTag
        BinDir       = $binDir13b
        GlobalDir    = (Join-Path $home13b '.lingtai-tui')
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-True ($r13b.ExitCode -ne 0) 'same-object duplicate key exits non-zero'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir13b 'lingtai-tui.exe'))) 'same-object duplicate key installs nothing'
    # Note: CONTRACT 11 (and every other public-mode contract in this suite)
    # already proves the positive case -- a well-formed manifest with "repo"
    # legitimately duplicated across providers.github/providers.gitee installs
    # successfully -- so no separate positive assertion is needed here.

    # -----------------------------------------------------------------------
    # CONTRACT 14: a release whose bundle manifest references a Windows archive
    # that was never actually uploaded fails loud (asset listing has no such
    # name) rather than attempting an unresolvable download.
    # -----------------------------------------------------------------------
    Write-Section 'contract: public mode fails when the manifest-referenced archive asset is missing'
    $missTag = 'v11.0.3'
    $missBundleJson = New-BundleManifestJson -Tag $missTag -ArchiveSha256 ('a' * 64)
    $missManifestAsset = Register-FakeApiAssetText -Name 'lingtai-bundle-manifest.json' -Text $missBundleJson
    # Deliberately do NOT register the zip/sha256 assets for this tag.
    Register-FakeRelease -ApiPathPrefix '/repos/Lingtai-AI/lingtai' -Tag $missTag -Assets @($missManifestAsset)

    $home14 = New-IsolatedHome
    $binDir14 = Join-Path $home14 'bin dir'
    $r14 = Invoke-Installer @{
        Version      = $missTag
        BinDir       = $binDir14
        GlobalDir    = (Join-Path $home14 '.lingtai-tui')
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-True ($r14.ExitCode -ne 0) 'missing manifest-referenced archive asset exits non-zero'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir14 'lingtai-tui.exe'))) 'missing archive asset installs nothing'

    # -----------------------------------------------------------------------
    # CONTRACT 15: a correctly-checksummed archive whose staged lingtai-tui.exe
    # reports the WRONG version still fails loud (the staged-version gate from
    # local-artifact mode applies identically to public-mode downloads).
    # -----------------------------------------------------------------------
    Write-Section 'contract: public mode enforces staged version even with a valid checksum'
    $wrongVerTag = 'v11.0.4'
    $wrongVerFx = New-FixtureArchive -Version $wrongVerTag -TuiVersion 'v0.0.1'
    $wvZipAsset = Register-FakeApiAsset -Name "lingtai-$wrongVerTag-windows-amd64.zip" -Bytes ([System.IO.File]::ReadAllBytes($wrongVerFx.ArchivePath))
    $wvShaAsset = Register-FakeApiAssetText -Name "lingtai-$wrongVerTag-windows-amd64.zip.sha256" -Text ("{0}  lingtai-$wrongVerTag-windows-amd64.zip" -f $wrongVerFx.Sha256)
    $wvBundleJson = New-BundleManifestJson -Tag $wrongVerTag -ArchiveSha256 $wrongVerFx.Sha256
    $wvManifestAsset = Register-FakeApiAssetText -Name 'lingtai-bundle-manifest.json' -Text $wvBundleJson
    Register-FakeRelease -ApiPathPrefix '/repos/Lingtai-AI/lingtai' -Tag $wrongVerTag -Assets @($wvZipAsset, $wvShaAsset, $wvManifestAsset)

    $home15 = New-IsolatedHome
    $binDir15 = Join-Path $home15 'bin dir'
    $r15 = Invoke-Installer @{
        Version      = $wrongVerTag
        BinDir       = $binDir15
        GlobalDir    = (Join-Path $home15 '.lingtai-tui')
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-True ($r15.ExitCode -ne 0) 'staged version mismatch in public mode exits non-zero'
    Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir15 'lingtai-tui.exe'))) 'staged version mismatch in public mode installs nothing'

    # -----------------------------------------------------------------------
    # CONTRACT 16: "latest" resolves through the release API exactly once. No
    # -Version means GET .../releases/latest, and the resolved tag (not the
    # literal string "latest") is what gets validated/installed/recorded.
    # -----------------------------------------------------------------------
    Write-Section 'contract: public mode resolves latest through the release API once'
    $latestTag = 'v11.1.0'
    $latestFx = New-FixtureArchive -Version $latestTag
    $latZipAsset = Register-FakeApiAsset -Name "lingtai-$latestTag-windows-amd64.zip" -Bytes ([System.IO.File]::ReadAllBytes($latestFx.ArchivePath))
    $latShaAsset = Register-FakeApiAssetText -Name "lingtai-$latestTag-windows-amd64.zip.sha256" -Text ("{0}  lingtai-$latestTag-windows-amd64.zip" -f $latestFx.Sha256)
    $latBundleJson = New-BundleManifestJson -Tag $latestTag -ArchiveSha256 $latestFx.Sha256
    $latManifestAsset = Register-FakeApiAssetText -Name 'lingtai-bundle-manifest.json' -Text $latBundleJson
    Register-FakeRelease -ApiPathPrefix '/repos/Lingtai-AI/lingtai' -Tag $latestTag -Assets @($latZipAsset, $latShaAsset, $latManifestAsset) -Latest

    $home16 = New-IsolatedHome
    $binDir16 = Join-Path $home16 'bin dir'
    $globalDir16 = Join-Path $home16 '.lingtai-tui'
    $r16 = Invoke-Installer @{
        BinDir       = $binDir16
        GlobalDir    = $globalDir16
        SkipVenv     = $true
        NoModifyPath = $true
    }
    Assert-Equal 0 $r16.ExitCode 'no -Version (latest resolution) install exits 0'
    Assert-True (Test-Path -LiteralPath (Join-Path $binDir16 'lingtai-tui.exe')) 'latest-resolved install placed lingtai-tui.exe under BinDir'
    $meta16 = $null
    try { $meta16 = Get-Content -LiteralPath (Join-Path $globalDir16 'install.json') -Raw | ConvertFrom-Json } catch {}
    if ($null -ne $meta16) {
        Assert-Equal $latestTag $meta16.resolved_ref 'latest resolution records the concrete resolved tag, not the literal "latest"'
    } else {
        Assert-True $false 'latest-resolved install wrote a parseable install.json'
    }

    # -----------------------------------------------------------------------
    # CONTRACT 17: public mode -DryRun performs no writes (resolution/validation
    # reads are allowed; no staging/bin/global directories are created).
    # -----------------------------------------------------------------------
    Write-Section 'contract: public mode DryRun performs no writes'
    $home17 = New-IsolatedHome
    $binDir17 = Join-Path $home17 'bin dir'
    $globalDir17 = Join-Path $home17 '.lingtai-tui'
    New-Item -ItemType Directory -Force -Path $binDir17, $globalDir17 | Out-Null
    $before17 = @(Get-TreeSnapshot $binDir17) + @(Get-TreeSnapshot $globalDir17)
    $r17 = Invoke-Installer @{
        Version      = $pubTag
        BinDir       = $binDir17
        GlobalDir    = $globalDir17
        SkipVenv     = $true
        NoModifyPath = $true
        DryRun       = $true
    }
    $after17 = @(Get-TreeSnapshot $binDir17) + @(Get-TreeSnapshot $globalDir17)
    Assert-Equal 0 $r17.ExitCode 'public-mode DryRun exits 0'
    Assert-Equal ($before17 -join "`n") ($after17 -join "`n") 'public-mode DryRun left the complete tree unchanged'

    # -----------------------------------------------------------------------
    # CONTRACT 18: pinned-kernel runtime install end to end, using the REAL
    # Python already present on windows-latest plus a genuine (trivial,
    # dependency-free) wheel built offline -- proves the full
    # resolve -> bundle -> venv -> kernel-manifest -> wheel-select -> verify ->
    # pip-install -> import/version/provenance chain, not just its gates.
    # -----------------------------------------------------------------------
    Write-Section 'contract: full runtime install from a real venv + genuine wheel'
    # Discover a genuinely SUPPORTED interpreter (cp311/cp312/cp313) the same way
    # install.ps1's own Find-VenvPython does: try the py launcher pinned to each
    # exact supported minor in turn, then fall back to a bare python/python3 on
    # PATH. Bare `py -3` (no minor) is deliberately NOT used here -- the launcher
    # resolves that to the HIGHEST registered version, which can silently be a
    # newer, unsupported CPython (e.g. cp314) even when a supported one is also
    # installed/pinned in this job, permanently skipping this contract instead of
    # exercising it.
    $probeArgs = @('-c', 'import sys; print(''cp'' + str(sys.version_info.major) + str(sys.version_info.minor))')
    $pyCmd = $null
    $cpTag = $null
    $py = Get-Command -Name 'py' -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($py) {
        foreach ($minor in @('3.13', '3.12', '3.11')) {
            $probe = (& $py.Source "-$minor" @probeArgs) 2>$null
            if ($LASTEXITCODE -eq 0 -and $probe -match '^cp3(11|12|13)$') {
                $pyCmd = @{ Source = $py.Source; ExtraArgs = @("-$minor") }
                $cpTag = $probe
                break
            }
        }
    }
    if (-not $pyCmd) {
        foreach ($name in @('python', 'python3')) {
            $cmd = Get-Command -Name $name -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1
            if ($cmd) {
                $probe = (& $cmd.Source @probeArgs) 2>$null
                if ($LASTEXITCODE -eq 0 -and $probe -match '^cp3(11|12|13)$') {
                    $pyCmd = @{ Source = $cmd.Source; ExtraArgs = @() }
                    $cpTag = $probe
                    break
                }
            }
        }
    }
    if (-not $pyCmd) {
        Skip-NotYet 'full runtime install (real Python + wheel)' 'no supported CPython 3.11/3.12/3.13 found via the py launcher or python/python3 on PATH'
    } else {
        $tag18 = 'v11.2.0'
        $fx18 = New-FixtureArchive -Version $tag18
        $zipAsset18 = Register-FakeApiAsset -Name "lingtai-$tag18-windows-amd64.zip" -Bytes ([System.IO.File]::ReadAllBytes($fx18.ArchivePath))
        $shaAsset18 = Register-FakeApiAssetText -Name "lingtai-$tag18-windows-amd64.zip.sha256" -Text ("{0}  lingtai-$tag18-windows-amd64.zip" -f $fx18.Sha256)

        # Build a minimal, dependency-free "lingtai" wheel offline via pip's
        # own wheel machinery (no network: --no-build-isolation, no index).
        $wheelSrc = Join-Path $TestRoot ("lingtai-wheel-src-{0}" -f ([Guid]::NewGuid().ToString('N')))
        $pkgDir = Join-Path $wheelSrc 'lingtai'
        New-Item -ItemType Directory -Force -Path $pkgDir | Out-Null
        Set-Content -LiteralPath (Join-Path $pkgDir '__init__.py') -Value '__version__ = "0.18.0"' -Encoding ASCII
        $pyprojectToml = @'
[build-system]
requires = ["setuptools"]
build-backend = "setuptools.build_meta"

[project]
name = "lingtai"
version = "0.18.0"
'@
        Set-Content -LiteralPath (Join-Path $wheelSrc 'pyproject.toml') -Value $pyprojectToml -Encoding ASCII
        $wheelOutDir = Join-Path $TestRoot ("lingtai-wheel-out-{0}" -f ([Guid]::NewGuid().ToString('N')))
        New-Item -ItemType Directory -Force -Path $wheelOutDir | Out-Null
        $wheelBuildArgs = @('-m', 'pip', 'wheel', '--no-deps', '--no-build-isolation', '-w', $wheelOutDir, $wheelSrc)
        $wheelBuildArgs = @($pyCmd.ExtraArgs) + $wheelBuildArgs
        & $pyCmd.Source @wheelBuildArgs *> (Join-Path $TestRoot 'wheel-build.log')
        $builtWheel = Get-ChildItem -LiteralPath $wheelOutDir -Filter 'lingtai-*.whl' -ErrorAction SilentlyContinue | Select-Object -First 1

        if (-not $builtWheel) {
            Skip-NotYet 'full runtime install (real Python + wheel)' 'could not build an offline test wheel (setuptools/pip wheel unavailable) -- see wheel-build.log'
        } else {
            # Rename to the exact cpXY-cpXY-win_amd64 platform tag Select-KernelWheel expects.
            $renamedWheel = Join-Path $wheelOutDir "lingtai-0.18.0-$cpTag-$cpTag-win_amd64.whl"
            Move-Item -LiteralPath $builtWheel.FullName -Destination $renamedWheel -Force
            $wheelSha = Get-Sha256Hex -Path $renamedWheel
            $wheelBytes = [System.IO.File]::ReadAllBytes($renamedWheel)
            $wheelAsset = Register-FakeApiAsset -Name (Split-Path -Leaf $renamedWheel) -Bytes $wheelBytes

            $kernelManifestJson = New-KernelManifestJson -KernelVersion '0.18.0' -Wheels @(
                @{ filename = (Split-Path -Leaf $renamedWheel); sha256 = $wheelSha; python_tag = $cpTag; abi_tag = $cpTag; platform_tag = 'win_amd64' }
            )
            $kernelManifestAsset = Register-FakeApiAssetText -Name 'lingtai-kernel-release-manifest.json' -Text $kernelManifestJson
            Register-FakeRelease -ApiPathPrefix '/repos/Lingtai-AI/lingtai-kernel' -Tag 'v0.18.0' -Assets @($wheelAsset, $kernelManifestAsset)

            $bundleJson18 = New-BundleManifestJson -Tag $tag18 -ArchiveSha256 $fx18.Sha256 -KernelTag 'v0.18.0' -KernelVersion '0.18.0'
            $manifestAsset18 = Register-FakeApiAssetText -Name 'lingtai-bundle-manifest.json' -Text $bundleJson18
            Register-FakeRelease -ApiPathPrefix '/repos/Lingtai-AI/lingtai' -Tag $tag18 -Assets @($zipAsset18, $shaAsset18, $manifestAsset18)

            $home18 = New-IsolatedHome
            $binDir18 = Join-Path $home18 'bin dir'
            $globalDir18 = Join-Path $home18 '.lingtai-tui'
            $r18 = Invoke-Installer @{
                Version      = $tag18
                BinDir       = $binDir18
                GlobalDir    = $globalDir18
                NoModifyPath = $true
            }
            Assert-Equal 0 $r18.ExitCode "full runtime install exits 0 (stderr: $($r18.Stderr))"
            Assert-True (Test-Path -LiteralPath (Join-Path $binDir18 'lingtai-tui.exe')) 'full runtime install placed lingtai-tui.exe under BinDir'
            Assert-True (Test-Path -LiteralPath (Join-Path $globalDir18 'runtime\venv\Scripts\python.exe')) 'full runtime install created the venv'
            Assert-True (Test-Path -LiteralPath (Join-Path $globalDir18 'runtime\venv\kernel-provenance.json')) 'full runtime install wrote kernel provenance'
            $meta18 = $null
            try { $meta18 = Get-Content -LiteralPath (Join-Path $globalDir18 'install.json') -Raw | ConvertFrom-Json } catch {}
            if ($null -ne $meta18) {
                Assert-Equal 'bundle' $meta18.kernel_source 'full runtime install.json records kernel_source=bundle'
                Assert-Equal '0.18.0' $meta18.kernel_version 'full runtime install.json records the installed kernel version'
            } else {
                Assert-True $false 'full runtime install wrote a parseable install.json'
            }

            # Wheel digest mismatch must fail loud and touch no BinDir/venv.
            Write-Section 'contract: kernel wheel checksum mismatch fails loud, installs nothing'
            $badKernelManifestJson = New-KernelManifestJson -KernelVersion '0.18.0' -Wheels @(
                @{ filename = (Split-Path -Leaf $renamedWheel); sha256 = ('f' * 64); python_tag = $cpTag; abi_tag = $cpTag; platform_tag = 'win_amd64' }
            )
            Register-FakeApiRouteText -Path '/assets/lingtai-kernel-release-manifest.json' -Text $badKernelManifestJson
            $home19 = New-IsolatedHome
            $binDir19 = Join-Path $home19 'bin dir'
            $globalDir19 = Join-Path $home19 '.lingtai-tui'
            $r19 = Invoke-Installer @{
                Version      = $tag18
                BinDir       = $binDir19
                GlobalDir    = $globalDir19
                NoModifyPath = $true
            }
            Assert-True ($r19.ExitCode -ne 0) 'kernel wheel digest mismatch exits non-zero'
            Assert-True (-not (Test-Path -LiteralPath (Join-Path $binDir19 'lingtai-tui.exe'))) 'kernel wheel digest mismatch installs no TUI binary'
            Assert-True (-not (Test-Path -LiteralPath (Join-Path $globalDir19 'install.json'))) 'kernel wheel digest mismatch writes no install metadata'
            # Restore the good kernel manifest for any later reuse of this route.
            Register-FakeApiRouteText -Path '/assets/lingtai-kernel-release-manifest.json' -Text $kernelManifestJson
        }
    }

} finally {
    # -----------------------------------------------------------------------
    # Restore ambient environment. NOTE: this only restores the isolated env
    # vars for THIS process; it never runs a destructive cleanup trap over
    # anything outside $TestRoot. The throwaway $TestRoot is intentionally left
    # on disk for post-mortem inspection by the CI job.
    # -----------------------------------------------------------------------
    foreach ($name in $IsolatedVars) {
        [Environment]::SetEnvironmentVariable($name, $SavedEnv[$name], 'Process')
    }
    [Environment]::SetEnvironmentVariable('LINGTAI_GITHUB_API_BASE', $null, 'Process')
    [Environment]::SetEnvironmentVariable('LINGTAI_KERNEL_GITHUB_API_BASE', $null, 'Process')
    Stop-FakeGitHubApi
}

# ---------------------------------------------------------------------------
# Summary + exit code.
# ---------------------------------------------------------------------------
Write-Host ''
Write-Host ("summary: {0} passed, {1} failed, {2} not-yet" -f $script:Passed, $script:Failures, $script:NotYet)
Write-Host ("test root (kept for inspection): {0}" -f $TestRoot)

if ($script:Failures -gt 0) {
    Write-Host ''
    Write-Host 'RESULT: FAIL'
    exit 1
}
Write-Host ''
Write-Host 'RESULT: PASS'
exit 0
