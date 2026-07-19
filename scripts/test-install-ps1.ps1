#requires -Version 5.1
<#
.SYNOPSIS
    Windows-hosted contract tests for the native PowerShell installer (install.ps1).

.DESCRIPTION
    This is the test-first, implementation-independent contract for the
    Windows-native `install.ps1`. It is the PowerShell analogue of
    scripts/test-install-sh.sh and is designed to run *identically* under both
    Windows PowerShell 5.1 (Desktop) and PowerShell 7+ (Core) on windows-latest.

    The installer does not exist yet. Until install.ps1 is added at the
    repository root, EVERY contract test below fails loudly at the "installer
    script is present" precondition. That RED state is intentional and correct:
    it proves the suite exercises a real script rather than a stub, and it must
    stay red until the installer is implemented against exactly these seams.

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

    # The default native runtime path is currently unsupported. Its known
    # fail-loud gate must run before any binary or metadata write so a caller
    # never gets a failed command that already changed BinDir.
    Write-Section 'contract: unavailable default runtime fails before install writes'
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
    Assert-True ($r9b.ExitCode -ne 0) 'unavailable default runtime exits non-zero'
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
