#requires -Version 5.1
<#
.SYNOPSIS
    Focused source/mechanical contract checks for the native PowerShell installer.

.DESCRIPTION
    The release installer is TUI-only: it builds the producer-owned TUI source
    archive locally and resolves the kernel independently. This suite keeps the
    existing tiny assertion harness, exercises the offline local-artifact
    DryRun seam and the production receipt writer in an isolated temporary
    directory, and checks source ordering for provider, phase, receipt, and
    -FromSource behavior. It does not contact a provider or install binaries or
    a runtime.
#>
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$RepoRoot = Split-Path -Parent $ScriptDir
$InstallScript = Join-Path $RepoRoot 'install.ps1'
$ShellInstaller = Join-Path $RepoRoot 'install.sh'
$script:Failures = 0
$script:Passed = 0

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

function Assert-NotContains {
    param([string]$Haystack, [string]$Needle, [string]$Label)
    Assert-True (-not $Haystack.Contains($Needle)) $Label
}

function Get-TreeSnapshot {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return '<absent>' }
    $root = [IO.Path]::GetFullPath($Path).TrimEnd([char[]]'\/')
    $items = @("D`t")
    foreach ($item in @(Get-ChildItem -LiteralPath $Path -Recurse -Force | Sort-Object FullName)) {
        $kind = if ($item.PSIsContainer) { 'D' } else { 'F' }
        $relative = $item.FullName.Substring($root.Length).TrimStart([char[]]'\/')
        $items += "$kind`t$relative"
    }
    return ($items -join "`n")
}

function Invoke-Installer {
    param([hashtable]$Arguments)
    $hostPath = (Get-Process -Id $PID).Path
    $argList = @('-NoProfile', '-NonInteractive', '-File', $InstallScript)
    foreach ($key in $Arguments.Keys) {
        $value = $Arguments[$key]
        if ($value -is [bool]) {
            if ($value) { $argList += "-$key" }
        } elseif ($value -is [System.Management.Automation.SwitchParameter]) {
            if ($value.IsPresent) { $argList += "-$key" }
        } else {
            $argList += "-$key"
            $argList += [string]$value
        }
    }
    $stdoutPath = Join-Path $env:TEMP ("installer-test-out-{0}.log" -f ([Guid]::NewGuid().ToString('N')))
    $stderrPath = Join-Path $env:TEMP ("installer-test-err-{0}.log" -f ([Guid]::NewGuid().ToString('N')))
    $saved = $ErrorActionPreference
    try {
        $ErrorActionPreference = 'Continue'
        & $hostPath @argList 1> $stdoutPath 2> $stderrPath
        $exitCode = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $saved
    }
    $stdout = if (Test-Path -LiteralPath $stdoutPath) { Get-Content -LiteralPath $stdoutPath -Raw } else { '' }
    $stderr = if (Test-Path -LiteralPath $stderrPath) { Get-Content -LiteralPath $stderrPath -Raw } else { '' }
    Remove-Item -LiteralPath $stdoutPath, $stderrPath -Force -ErrorAction SilentlyContinue
    return @{ ExitCode = $exitCode; Stdout = $stdout; Stderr = $stderr; Output = "$stdout`n$stderr" }
}

try {
    Write-Section 'precondition: installer sources present'
    Assert-True (Test-Path -LiteralPath $InstallScript) "install.ps1 exists at $InstallScript"
    Assert-True (Test-Path -LiteralPath $ShellInstaller) "install.sh exists at $ShellInstaller"
    if (-not (Test-Path -LiteralPath $InstallScript)) { throw 'install.ps1 is absent' }
    $psText = Get-Content -LiteralPath $InstallScript -Raw
    $shText = Get-Content -LiteralPath $ShellInstaller -Raw
    Write-Host ("  host: {0} {1}" -f $PSVersionTable.PSEdition, $PSVersionTable.PSVersion)

    Write-Section 'contract: both installer paths are TUI-only'
    foreach ($pair in @(@($psText, 'install.ps1'), @($shText, 'install.sh'))) {
        $text = $pair[0]
        $label = $pair[1]
        foreach ($retired in @('lingtai-portal', 'SKIP_PORTAL', '--skip-portal', 'lingtai.tui.bundle', 'kernel-release.json')) {
            Assert-NotContains $text $retired "$label has no retired installer surface '$retired'"
        }
        Assert-True (-not [regex]::IsMatch($text, '(?im)(^|[^a-z0-9_])(node|npm)([^a-z0-9_]|$)')) "$label has no Node/npm requirement"
        Assert-True (-not [regex]::IsMatch($text, '(?im)managed_binaries.*portal')) "$label has no Portal receipt path"
    }
    Assert-Contains $psText 'lingtai-tui.exe' 'PowerShell installer retains the TUI binary path'
    Assert-NotContains $psText 'lingtai-portal.exe' 'PowerShell installer has no Portal binary path'
    Assert-Contains $shText 'lingtai-tui' 'shell installer retains the TUI binary path'
    Assert-NotContains $shText 'lingtai-portal' 'shell installer has no Portal binary path'

    Write-Section 'contract: validation precedes provider selection'
    $sourceValidation = $psText.IndexOf('$sourceArg = if ([string]::IsNullOrWhiteSpace($Source))')
    $sourceProvider = $psText.LastIndexOf('Resolve-SourceProvider')
    Assert-True ($sourceValidation -ge 0 -and $sourceProvider -gt $sourceValidation) 'PowerShell validates -Source before resolving a provider'
    Assert-Contains $psText 'if ($sourceArg -eq ''gitee'') { Fail' 'retired provider is rejected in the pre-provider validation block'
    Assert-Contains $psText 'if ($haveArchive -ne $haveChecksum) { Fail' 'archive/checksum pairing is validated before provider selection'

    Write-Section 'contract: phase and completion truthfulness'
    Assert-Contains $psText 'function Start-Phase' 'phase start helper exists'
    Assert-Contains $psText 'function Complete-Phase' 'phase completion helper exists'
    Assert-Contains $psText 'Write-Host "$label $Message"' 'phase heading is emitted before phase work'
    Assert-Contains $psText '$Clock.Stop()' 'phase completion records elapsed time after work'
    Assert-Contains $psText 'Set-PhaseTotal $(if ($SkipVenv) { 4 } else { 5 })' 'local-artifact phase count matches runtime conditionality'
    Assert-Contains $psText 'Set-PhaseTotal 0' 'source-build phases avoid a false fixed denominator'
    Assert-Contains $psText '$kernelMeta = $null' 'kernel completion facts begin absent'
    Assert-Contains $psText 'if ($kernelMeta) {' 'kernel receipt/completion facts are conditional'
    $dryKernelAnnouncement = $psText.IndexOf('Write-Step "[dry-run] would independently resolve and install the latest verified kernel release')
    $kernelResultAnnouncement = $psText.IndexOf('Write-Info "Resolved latest verified kernel release:')
    Assert-True ($dryKernelAnnouncement -ge 0 -and $kernelResultAnnouncement -gt $dryKernelAnnouncement) 'dry-run has a plan message, not a false resolved-kernel announcement'
    Assert-Contains $psText 'if ($kernelMeta) { Join-Path $GlobalDir ''runtime\venv'' } else { '''' }' 'completion runtime location is conditional on a real runtime result'

    Write-Section 'contract: release kernel tag receipt field is conditional'
    Assert-Contains $psText "[string]`$KernelReleaseTag = ''" 'metadata writer accepts an optional kernel release tag'
    Assert-Contains $psText "if (`$KernelSource -eq 'release')" 'kernel release tag has an explicit release-source guard'
    Assert-Contains $psText "`$meta['kernel_release_tag'] = `$KernelReleaseTag" 'release receipts write kernel_release_tag'
    Assert-Contains $psText "`$metaArgs['KernelReleaseTag'] = `$kernelMeta.KernelReleaseTag" 'main metadata call passes the resolved kernel release tag'
    $releaseGuard = $psText.IndexOf("if (`$KernelSource -eq 'release')")
    $releaseField = $psText.IndexOf("`$meta['kernel_release_tag'] = `$KernelReleaseTag")
    $kernelBlock = $psText.IndexOf('if ($KernelSource) {')
    Assert-True ($releaseGuard -gt $kernelBlock -and $releaseField -gt $releaseGuard) 'kernel_release_tag is nested under non-skip kernel metadata and release-only guards'
    Assert-True ($psText.IndexOf("`$meta['kernel_release_tag'] = `$KernelReleaseTag") -lt $psText.IndexOf("if (`$SourceMode) {")) 'non-release source-mode metadata does not write kernel_release_tag'

    Write-Section 'contract: emitted receipt carries the kernel tag only for release provenance'
    $metadataTestRoot = Join-Path ([IO.Path]::GetTempPath()) ("lingtai ps receipt contract {0}" -f ([Guid]::NewGuid().ToString('N')))
    try {
        $tokens = $null
        $parseErrors = $null
        $installAst = [System.Management.Automation.Language.Parser]::ParseFile($InstallScript, [ref]$tokens, [ref]$parseErrors)
        Assert-Equal 0 (@($parseErrors).Count) 'install.ps1 parses before isolated receipt-writer execution'
        $metadataFunction = $installAst.Find({
            param($node)
            $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Write-InstallMetadata'
        }, $true)
        Assert-True ($null -ne $metadataFunction) 'production Write-InstallMetadata function is discoverable'
        if ($null -ne $metadataFunction) {
            $script:RepoUrl = 'https://github.com/Lingtai-AI/lingtai.git'
            function Write-Ok { param([string]$Message) }
            Invoke-Expression $metadataFunction.Extent.Text

            $releaseRoot = Join-Path $metadataTestRoot 'release'
            Write-InstallMetadata -GlobalDir $releaseRoot -Prefix 'C:\Lingtai' -BinDir 'C:\Lingtai\bin' `
                -RequestedRef 'v1.2.3' -ResolvedRef 'v1.2.3' -ManagedBinaries @('C:\Lingtai\bin\lingtai-tui.exe') `
                -KernelSource 'release' -KernelReleaseTag 'v2.3.4' -KernelVersion '2.3.4' -KernelProvider 'mirror' -TuiProvider 'mirror'
            $releaseReceipt = Get-Content -LiteralPath (Join-Path $releaseRoot 'install.json') -Raw | ConvertFrom-Json
            Assert-Equal 'v2.3.4' $releaseReceipt.kernel_release_tag 'emitted release receipt preserves the resolved kernel tag'

            $mainRoot = Join-Path $metadataTestRoot 'main'
            Write-InstallMetadata -GlobalDir $mainRoot -Prefix 'C:\Lingtai' -BinDir 'C:\Lingtai\bin' `
                -RequestedRef 'main' -ResolvedRef 'main' -ManagedBinaries @('C:\Lingtai\bin\lingtai-tui.exe') `
                -KernelSource 'main' -KernelVersion 'main-test' -KernelProvider 'github' -TuiProvider 'github'
            $mainReceipt = Get-Content -LiteralPath (Join-Path $mainRoot 'install.json') -Raw | ConvertFrom-Json
            Assert-True (-not ($mainReceipt.PSObject.Properties.Name -contains 'kernel_release_tag')) 'non-release receipt omits kernel_release_tag'
        }
    } finally {
        if (Test-Path -LiteralPath $metadataTestRoot) { Remove-Item -LiteralPath $metadataTestRoot -Recurse -Force -ErrorAction SilentlyContinue }
    }

    Write-Section 'contract: local artifact DryRun is read-only and honestly worded'
    $testRoot = $null
    $testRoot = Join-Path ([IO.Path]::GetTempPath()) ("lingtai ps installer contract {0}" -f ([Guid]::NewGuid().ToString('N')))
    $inputDir = Join-Path $testRoot 'input'
    $tempDir = Join-Path $testRoot 'temp'
    $binDir = Join-Path $testRoot 'bin'
    $globalDir = Join-Path $testRoot 'global'
    New-Item -ItemType Directory -Force -Path $inputDir, $tempDir | Out-Null
    $fixture = Join-Path $inputDir 'lingtai-v1.2.3.zip'
    $fixtureFile = Join-Path $inputDir 'lingtai-tui.exe'
    Set-Content -LiteralPath $fixtureFile -Value 'offline fixture' -Encoding ASCII
    Compress-Archive -LiteralPath $fixtureFile -DestinationPath $fixture
    $digest = (Get-FileHash -LiteralPath $fixture -Algorithm SHA256).Hash.ToLowerInvariant()
    $sidecar = "$fixture.sha256"
    Set-Content -LiteralPath $sidecar -Value ("{0}  {1}" -f $digest, (Split-Path -Leaf $fixture)) -Encoding ASCII
    $savedTemp = $env:TEMP
    $savedTmp = $env:TMP
    try {
        $env:TEMP = $tempDir
        $env:TMP = $tempDir
        $beforeTemp = Get-TreeSnapshot $tempDir
        $dry = Invoke-Installer @{ ArchivePath = $fixture; ChecksumPath = $sidecar; Version = 'v1.2.3'; BinDir = $binDir; GlobalDir = $globalDir; DryRun = $true; NoModifyPath = $true }
        $afterTemp = Get-TreeSnapshot $tempDir
    } finally {
        $env:TEMP = $savedTemp
        $env:TMP = $savedTmp
    }
    Assert-Equal 0 $dry.ExitCode 'local-artifact DryRun exits successfully'
    Assert-Equal $beforeTemp $afterTemp 'local-artifact DryRun creates no staging/temp state'
    Assert-True (-not (Test-Path -LiteralPath $binDir)) 'local-artifact DryRun creates no BinDir'
    Assert-True (-not (Test-Path -LiteralPath $globalDir)) 'local-artifact DryRun creates no GlobalDir/receipt'
    Assert-Contains $dry.Output 'DRY RUN' 'DryRun truthfully announces read-only mode'
    Assert-Contains $dry.Output 'local TUI artifact validated; nothing installed' 'DryRun uses local-artifact completion wording'
    Assert-Contains $dry.Output 'NoModifyPath' 'DryRun explains -NoModifyPath'
    Assert-NotContains $dry.Output 'Resolved latest verified kernel release' 'DryRun does not falsely announce a resolved kernel'
    Assert-NotContains $dry.Output 'Compiling lingtai-tui.exe' 'local-artifact DryRun performs no build'
    Assert-NotContains $dry.Output 'Downloading' 'local-artifact DryRun performs no network download'

    Write-Section 'contract: -FromSource retains live GitHub source semantics'
    $fromSourceClause = 'if (-not [string]::IsNullOrWhiteSpace($Version) -or $Ref -or $FromSource -or $Update -or $Latest -or $ArchivePath)'
    Assert-Contains $psText $fromSourceClause '-FromSource selects the GitHub TUI provider'
    Assert-Contains $psText 'if ($FromSource -and $haveArchive) { Fail' '-FromSource remains incompatible with local artifact input'
    Assert-Contains $psText 'Build-ReleaseSourceArchive -Tag $resolvedTag -Provider ''github''' '-FromSource uses the live GitHub source-archive build path'
    Assert-NotContains $psText 'Install-FromLocalArtifact -Archive $ArchivePath' '-FromSource does not silently switch to local artifact installation'
    Assert-Contains $psText 'only the TUI falls back to the latest GitHub source release' 'TUI provider fallback wording is component-scoped'
    Assert-Contains $psText 'The kernel is installed from a verified local release artifact' 'kernel wording stays independent of TUI source choice'
    Assert-NotContains $psText 'Choose GitHub explicitly with -Source github' 'mirror helper errors do not prescribe a provider'
    Assert-Contains $psText 'lingtai.ai TUI source is unavailable; falling back to the latest GitHub TUI source release.' 'TUI caller retains its automatic fallback warning'
    Assert-Contains $psText 'lingtai.ai kernel release is unavailable; falling back to the latest GitHub kernel release.' 'kernel caller retains its automatic fallback warning'
} finally {
    if (Test-Path -LiteralPath $testRoot) { Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue }
}

Write-Host ''
Write-Host ("summary: {0} passed, {1} failed" -f $script:Passed, $script:Failures)
if ($script:Failures -gt 0) {
    Write-Host 'RESULT: FAIL'
    exit 1
}
Write-Host 'RESULT: PASS'
exit 0
