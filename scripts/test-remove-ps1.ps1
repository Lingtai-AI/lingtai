#requires -Version 5.1
<#
.SYNOPSIS
    Windows-hosted contract tests for the native PowerShell removal script
    (remove.ps1).

.DESCRIPTION
    The PowerShell analogue of scripts/test-lifecycle-assets.sh's remove.sh
    coverage, designed to run identically under Windows PowerShell 5.1
    (Desktop) and PowerShell 7+ (Core) on windows-latest. remove.ps1 is
    exercised only through its public parameter seams (-BinDir, -Yes) as a
    real child process against a real fixture: a real venv-shaped directory
    tree, a real lingtai-tui.exe stand-in, and a real install.json receipt
    built by this test. Every run isolates USERPROFILE under a throwaway test
    root, so a run can never touch the developer's real profile.

.NOTES
    Exit code 0 => all contract assertions passed. Non-zero => at least one
    assertion failed.
#>

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot  = Split-Path -Parent $ScriptDir
$RemoveScript = Join-Path $RepoRoot 'remove.ps1'

$script:Failures = 0
$script:Passed   = 0

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

function Assert-Contains {
    param([string]$Haystack, [string]$Needle, [string]$Label)
    Assert-True ($Haystack.Contains($Needle)) $Label
}

$TestRoot = Join-Path ([System.IO.Path]::GetTempPath()) ("lingtai-remove-ps1-test-{0}" -f ([Guid]::NewGuid().ToString('N')))
New-Item -ItemType Directory -Force -Path $TestRoot | Out-Null

$OriginalUserProfile = $env:USERPROFILE

function Invoke-Remove {
    param([string]$BinDir, [switch]$Yes, [string]$UserProfile)

    $argList = New-Object System.Collections.Generic.List[string]
    $argList.Add('-NoProfile')
    $argList.Add('-NonInteractive')
    $argList.Add('-ExecutionPolicy'); $argList.Add('Bypass')
    $argList.Add('-File'); $argList.Add($RemoveScript)
    if ($BinDir) { $argList.Add('-BinDir'); $argList.Add($BinDir) }
    if ($Yes) { $argList.Add('-Yes') }

    $psHost = (Get-Process -Id $PID).Path
    [string[]]$invokeArgs = $argList.ToArray()
    $outFile = Join-Path $TestRoot ("out-{0}.txt" -f ([Guid]::NewGuid().ToString('N')))
    $errFile = Join-Path $TestRoot ("err-{0}.txt" -f ([Guid]::NewGuid().ToString('N')))

    $savedUserProfile = $env:USERPROFILE
    $savedErrorActionPreference = $ErrorActionPreference
    try {
        if ($UserProfile) { $env:USERPROFILE = $UserProfile }
        $ErrorActionPreference = 'Continue'
        & $psHost @invokeArgs 1> $outFile 2> $errFile
        $exitCode = $LASTEXITCODE
    } finally {
        $env:USERPROFILE = $savedUserProfile
        $ErrorActionPreference = $savedErrorActionPreference
    }
    return @{
        ExitCode = $exitCode
        Output   = "$(Get-Content -LiteralPath $outFile -Raw -ErrorAction SilentlyContinue)`n$(Get-Content -LiteralPath $errFile -Raw -ErrorAction SilentlyContinue)"
    }
}

# New-Fixture builds one self-contained fake USERPROFILE/BinDir/venv/receipt
# under $TestRoot, mirroring scripts/test-lifecycle-assets.sh's remove_fixture.
function New-Fixture {
    param([string]$Suffix)
    $userProfile = Join-Path $TestRoot "profile-$Suffix"
    $binDir = Join-Path $TestRoot "bin-$Suffix"
    $globalDir = Join-Path $userProfile '.lingtai-tui'
    $runtimeRoot = Join-Path $globalDir 'runtime'
    $venvDir = Join-Path $runtimeRoot 'venv'
    New-Item -ItemType Directory -Force -Path $binDir | Out-Null
    New-Item -ItemType Directory -Force -Path (Join-Path $venvDir 'Scripts') | Out-Null
    Set-Content -LiteralPath (Join-Path $venvDir 'Scripts\python.exe') -Value 'stand-in' -NoNewline

    $tuiTarget = Join-Path $binDir 'lingtai-tui.exe'
    Set-Content -LiteralPath $tuiTarget -Value 'stand-in tui binary' -NoNewline

    $metadataPath = Join-Path $globalDir 'install.json'
    $receipt = [ordered]@{
        schema           = 'lingtai.tui.install/v1'
        schema_version   = 1
        install_method   = 'powershell'
        install_kind     = 'powershell-release-asset'
        bin_dir          = $binDir
        stamped_version  = '0.12.0'
        managed_binaries = @($tuiTarget)
        runtime_venv     = $venvDir
    }
    $json = $receipt | ConvertTo-Json -Depth 5
    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($metadataPath, $json, $utf8NoBom)

    # A NOT-owned sentinel that must survive removal.
    Set-Content -LiteralPath (Join-Path $globalDir 'tui_config.json') -Value '{"unrelated":true}' -NoNewline

    return @{
        UserProfile = $userProfile
        BinDir      = $binDir
        GlobalDir   = $globalDir
        RuntimeRoot = $runtimeRoot
        VenvDir     = $venvDir
        Metadata    = $metadataPath
        TuiTarget   = $tuiTarget
    }
}

try {
    if (-not (Test-Path -LiteralPath $RemoveScript)) {
        Assert-True $false "remove.ps1 exists at $RemoveScript"
    } else {

        Write-Section 'happy path: real removal deletes owned artifacts, preserves NOT-owned state'
        $f1 = New-Fixture -Suffix 'happy'
        $r1 = Invoke-Remove -BinDir $f1.BinDir -Yes -UserProfile $f1.UserProfile
        Assert-Equal 0 $r1.ExitCode "happy-path removal exits 0 ($($r1.Output))"
        Assert-Contains $r1.Output 'PASS' 'happy-path reports PASS'
        Assert-True (-not (Test-Path -LiteralPath $f1.TuiTarget)) 'lingtai-tui.exe was deleted'
        Assert-True (-not (Test-Path -LiteralPath $f1.VenvDir)) 'runtime venv was deleted'
        Assert-True (-not (Test-Path -LiteralPath $f1.Metadata)) 'receipt was deleted'
        Assert-True (Test-Path -LiteralPath (Join-Path $f1.GlobalDir 'tui_config.json')) 'NOT-owned tui_config.json survived'

        Write-Section 'idempotent second run'
        $r1b = Invoke-Remove -BinDir $f1.BinDir -Yes -UserProfile $f1.UserProfile
        Assert-Equal 0 $r1b.ExitCode "second run exits 0 ($($r1b.Output))"
        Assert-Contains $r1b.Output 'nothing to remove' 'second run reports nothing to remove'

        Write-Section 'missing -Yes refuses to mutate'
        $f2 = New-Fixture -Suffix 'noyes'
        $r2 = Invoke-Remove -BinDir $f2.BinDir -UserProfile $f2.UserProfile
        Assert-Equal 1 $r2.ExitCode "missing -Yes exits 1 ($($r2.Output))"
        Assert-Contains $r2.Output '-Yes' 'error names the missing -Yes flag'
        Assert-True (Test-Path -LiteralPath $f2.TuiTarget) 'binary preserved when -Yes is missing'

        Write-Section 'missing -BinDir is a usage error'
        $f3 = New-Fixture -Suffix 'nobindir'
        $r3 = Invoke-Remove -Yes -UserProfile $f3.UserProfile
        Assert-Equal 1 $r3.ExitCode "missing -BinDir exits nonzero ($($r3.Output))"

        Write-Section 'bin_dir mismatch is refused and surfaces the receipt owner'
        $f4 = New-Fixture -Suffix 'mismatch'
        $otherBinDir = Join-Path $TestRoot 'bin-mismatch-other'
        New-Item -ItemType Directory -Force -Path $otherBinDir | Out-Null
        $r4 = Invoke-Remove -BinDir $otherBinDir -Yes -UserProfile $f4.UserProfile
        Assert-Equal 1 $r4.ExitCode "bin_dir mismatch exits 1 ($($r4.Output))"
        Assert-True (Test-Path -LiteralPath $f4.TuiTarget) 'binary preserved on bin_dir mismatch'
        Assert-Contains $r4.Output $f4.BinDir 'bin_dir mismatch error surfaces the receipt''s actual bin_dir'
        Assert-Contains $r4.Output '-Yes' 'bin_dir mismatch error names an actionable re-run'

        Write-Section 'partial/tampered receipt (missing required field) fails cleanly under Set-StrictMode'
        $f6 = New-Fixture -Suffix 'tampered'
        # install_kind is deliberately omitted -- a truncated/tampered
        # receipt, not merely an empty-string value. Prior to guarding every
        # property read with Get-JsonProperty, Set-StrictMode -Version Latest
        # would throw a raw PropertyNotFoundException here instead of the
        # intended Fail message; the script must still exit nonzero, still
        # leave the receipt in place, and must not silently delete anything.
        $tamperedReceipt = [ordered]@{
            schema           = 'lingtai.tui.install/v1'
            schema_version   = 1
            bin_dir          = $f6.BinDir
            managed_binaries = @($f6.TuiTarget)
        }
        $tamperedJson = $tamperedReceipt | ConvertTo-Json -Depth 5
        $utf8NoBomTampered = [System.Text.UTF8Encoding]::new($false)
        [System.IO.File]::WriteAllText($f6.Metadata, $tamperedJson, $utf8NoBomTampered)
        $r6 = Invoke-Remove -BinDir $f6.BinDir -Yes -UserProfile $f6.UserProfile
        Assert-Equal 1 $r6.ExitCode "tampered receipt (missing install_kind) exits 1, not a raw StrictMode crash ($($r6.Output))"
        Assert-True (Test-Path -LiteralPath $f6.Metadata) 'tampered receipt survives (not deleted on refusal)'
        Assert-True (Test-Path -LiteralPath $f6.TuiTarget) 'binary preserved when the receipt is tampered/partial'

        Write-Section 'partial/tampered receipt missing bin_dir entirely'
        $f7 = New-Fixture -Suffix 'tampered-nobindir'
        $tamperedReceipt2 = [ordered]@{
            schema           = 'lingtai.tui.install/v1'
            schema_version   = 1
            install_kind     = 'powershell-release-asset'
            managed_binaries = @($f7.TuiTarget)
        }
        $tamperedJson2 = $tamperedReceipt2 | ConvertTo-Json -Depth 5
        $utf8NoBomTampered2 = [System.Text.UTF8Encoding]::new($false)
        [System.IO.File]::WriteAllText($f7.Metadata, $tamperedJson2, $utf8NoBomTampered2)
        $r7 = Invoke-Remove -BinDir $f7.BinDir -Yes -UserProfile $f7.UserProfile
        Assert-Equal 1 $r7.ExitCode "tampered receipt (missing bin_dir) exits 1, not a raw StrictMode crash ($($r7.Output))"
        Assert-True (Test-Path -LiteralPath $f7.Metadata) 'tampered receipt (missing bin_dir) survives'
        Assert-True (Test-Path -LiteralPath $f7.TuiTarget) 'binary preserved when bin_dir is entirely missing from the receipt'

        Write-Section 'partial failure leaves the receipt intact and reports honestly'
        $f5 = New-Fixture -Suffix 'partial'
        # Simulate an undeletable runtime venv by holding a file handle open
        # inside it for the duration of the removal attempt -- Windows refuses
        # to delete a directory containing an open file handle, giving a real
        # (not simulated) partial-failure fault.
        $lockedFile = Join-Path $f5.VenvDir 'Scripts\python.exe'
        $stream = [System.IO.File]::Open($lockedFile, [System.IO.FileMode]::Open, [System.IO.FileAccess]::Read, [System.IO.FileShare]::None)
        try {
            $r5 = Invoke-Remove -BinDir $f5.BinDir -Yes -UserProfile $f5.UserProfile
        } finally {
            $stream.Close()
        }
        Assert-Equal 1 $r5.ExitCode "partial failure exits 1 ($($r5.Output))"
        Assert-Contains $r5.Output 'PARTIAL' 'partial failure reports PARTIAL'
        Assert-True (Test-Path -LiteralPath $f5.Metadata) 'receipt survives a partial failure'
        Assert-True (-not (Test-Path -LiteralPath $f5.TuiTarget)) 'binary was removed before the venv fault was hit'
        # Retry after the lock is released must complete the removal.
        $r5b = Invoke-Remove -BinDir $f5.BinDir -Yes -UserProfile $f5.UserProfile
        Assert-Equal 0 $r5b.ExitCode "retry after the fault clears exits 0 ($($r5b.Output))"
        Assert-True (-not (Test-Path -LiteralPath $f5.Metadata)) 'retry deletes the receipt'
    }
} finally {
    $env:USERPROFILE = $OriginalUserProfile
}

Write-Host ''
Write-Host ("summary: {0} passed, {1} failed" -f $script:Passed, $script:Failures)
Write-Host ("test root (kept for inspection): {0}" -f $TestRoot)

if ($script:Failures -gt 0) {
    Write-Host ''
    Write-Host 'RESULT: FAIL'
    exit 1
}
Write-Host ''
Write-Host 'RESULT: PASS'
exit 0
