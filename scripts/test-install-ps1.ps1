#requires -Version 5.1
<#
.SYNOPSIS
    Focused source/mechanical contract checks for the native PowerShell installer.

.DESCRIPTION
    The release installer is TUI-only: it builds the producer-owned TUI source
    archive locally and resolves the kernel independently. This suite keeps the
    existing tiny assertion harness, exercises the offline local-artifact
    DryRun seam, production source-install/provider-loop functions, and the
    production receipt writer in isolated temporary directories. Its source
    install regression uses a fake Windows command and mocks all network/package
    seams; it never contacts a provider or changes shared configuration/auth.
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

$testRoot = $null
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

    Write-Section 'contract: stable kernel installs use the declared source artifact'
    foreach ($retired in @('Get-VenvWheelTag', 'Select-KernelWheel', 'Install-KernelWheel', 'python_platform_tags', 'select_kernel_wheel')) {
        Assert-NotContains $psText $retired "PowerShell stable kernel path has no wheel selector '$retired'"
    }
    Assert-Contains $psText 'function Get-KernelSourceArtifact' 'PowerShell selects the manifest-declared kernel source artifact'
    Assert-Contains $psText '$sourceArtifact = Get-KernelSourceArtifact -KernelManifest $manifest' 'PowerShell stable provisioning selects the source artifact'
    Assert-Contains $psText 'Install-KernelSource -VenvPython $venvPython -SourceArtifact $sourceArtifact' 'PowerShell stable provisioning installs the selected source artifact'
    Assert-Contains $psText 'Write-Info "Building and installing lingtai from the verified local source archive' 'PowerShell labels the local source install honestly'
    Assert-Contains $psText "'pip' 'install' `$dest" 'PowerShell passes the downloaded source archive to pip by local path'

    $sourceTokens = $null
    $sourceParseErrors = $null
    $sourceAst = [System.Management.Automation.Language.Parser]::ParseFile($InstallScript, [ref]$sourceTokens, [ref]$sourceParseErrors)
    Assert-Equal 0 @($sourceParseErrors).Count 'install.ps1 parses before source-artifact execution'
    $sourceSelectorFunction = $sourceAst.Find({
        param($node)
        $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Get-KernelSourceArtifact'
    }, $true)
    Assert-True ($null -ne $sourceSelectorFunction) 'source-artifact selector is discoverable through the PowerShell AST'
    if ($null -ne $sourceSelectorFunction) {
        Invoke-Expression $sourceSelectorFunction.Extent.Text
        $sourceManifestFixture = [pscustomobject]@{
            sdist_fallback = 'lingtai-2.3.4.tar.gz'
            artifacts = @(
                [pscustomobject]@{ kind = 'wheel'; filename = 'lingtai-2.3.4-cp312-cp312-win_amd64.whl' }
                [pscustomobject]@{ kind = 'sdist'; filename = 'lingtai-2.3.4.tar.gz' }
            )
        }
        $selectedSource = Get-KernelSourceArtifact -KernelManifest $sourceManifestFixture
        Assert-Equal 'sdist' $selectedSource.kind 'source-artifact selector ignores a compatible wheel'
        Assert-Equal 'lingtai-2.3.4.tar.gz' $selectedSource.filename 'source-artifact selector returns sdist_fallback'
    }

    $venvFunction = $sourceAst.Find({
        param($node)
        $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Install-Venv'
    }, $true)
    Assert-True ($null -ne $venvFunction) 'stable venv provisioning is discoverable through the PowerShell AST'
    if ($null -ne $venvFunction) {
        $venvText = $venvFunction.Extent.Text
        Assert-Contains $venvText "foreach (`$provider in @('mirror', 'github'))" 'kernel provisioning owns its independent provider loop'
        Assert-Contains $venvText 'Resolve-KernelLatestTag -Provider $provider' 'kernel latest resolution receives only its own provider'
        Assert-Contains $venvText 'Get-KernelManifest -KernelTag $kernelTag -ManifestFilename $manifestName -Provider $provider' 'kernel manifest resolution receives only its own provider'
        Assert-Contains $venvText 'Install-KernelSource -VenvPython $venvPython -SourceArtifact $sourceArtifact -KernelTag $kernelTag -StageDir $stage -Provider $provider' 'kernel source download receives only its own provider'
        Assert-NotContains $venvText '$script:TuiProvider' 'kernel provider loop does not mutate TUI provider state'
    }

    $sourceInstallerFunction = $sourceAst.Find({
        param($node)
        $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Install-KernelSource'
    }, $true)
    $hashFunction = $sourceAst.Find({
        param($node)
        $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Get-Sha256Hex'
    }, $true)
    Assert-True ($null -ne $sourceInstallerFunction) 'kernel source installer is discoverable through the PowerShell AST'
    Assert-True ($null -ne $hashFunction) 'production SHA-256 helper is discoverable through the PowerShell AST'

    Write-Section 'execution: verified kernel sdist is the local pip input'
    $kernelSourceTestRoot = Join-Path ([IO.Path]::GetTempPath()) ("lingtai ps kernel source contract {0}" -f ([Guid]::NewGuid().ToString('N')))
    try {
        $sourceFixtureDir = Join-Path $kernelSourceTestRoot 'fixture'
        $sourceStageDir = Join-Path $kernelSourceTestRoot 'stage'
        New-Item -ItemType Directory -Force -Path $sourceFixtureDir, $sourceStageDir | Out-Null
        $kernelSdistFixture = Join-Path $sourceFixtureDir 'lingtai-2.3.4.tar.gz'
        Set-Content -LiteralPath $kernelSdistFixture -Value 'controlled kernel sdist bytes' -Encoding ASCII
        $kernelWheelFixture = Join-Path $sourceFixtureDir 'lingtai-2.3.4-cp312-cp312-win_amd64.whl'
        Set-Content -LiteralPath $kernelWheelFixture -Value 'controlled compatible wheel bytes' -Encoding ASCII
        $fakePython = Join-Path $sourceFixtureDir 'python.cmd'
        Set-Content -LiteralPath $fakePython -Value @('@echo off', '> "%~dp0pip-args.log" echo %*', 'exit /b 0') -Encoding ASCII
        $pipArgsLog = Join-Path $sourceFixtureDir 'pip-args.log'
        $kernelSdistSha = (Get-FileHash -LiteralPath $kernelSdistFixture -Algorithm SHA256).Hash.ToLowerInvariant()
        $kernelWheelSha = (Get-FileHash -LiteralPath $kernelWheelFixture -Algorithm SHA256).Hash.ToLowerInvariant()
        $kernelWheelRecord = [pscustomobject]@{
            kind = 'wheel'
            filename = 'lingtai-2.3.4-cp312-cp312-win_amd64.whl'
            sha256 = $kernelWheelSha
            python_tag = 'cp312'
            abi_tag = 'cp312'
            platform_tag = 'win_amd64'
        }
        $kernelSdistRecord = [pscustomobject]@{
            kind = 'sdist'
            filename = 'lingtai-2.3.4.tar.gz'
            sha256 = $kernelSdistSha
            python_tag = $null
            abi_tag = $null
            platform_tag = $null
        }
        $sourceManifestFixture = [pscustomobject]@{
            kernel_tag = 'v2.3.4'
            kernel_version = '2.3.4'
            sdist_fallback = $kernelSdistRecord.filename
            artifacts = @($kernelWheelRecord, $kernelSdistRecord)
        }
        $selectedSource = Get-KernelSourceArtifact -KernelManifest $sourceManifestFixture
        Assert-Equal 'sdist' $selectedSource.kind 'execution fixture exposes a declared sdist beside a compatible wheel'
        Assert-Equal $kernelSdistRecord.filename $selectedSource.filename 'execution fixture selects the manifest-declared sdist'

        $script:KernelAssetRequests = New-Object System.Collections.Generic.List[object]
        $script:KernelSdistFixture = $kernelSdistFixture
        function Write-Info { param([string]$Message) }
        function Write-Ok { param([string]$Message) }
        function Fail { param([string]$Message) throw $Message }
        function Get-KernelAssetUrl {
            param([string]$KernelTag, [string]$Name, [string]$Provider)
            $script:KernelAssetRequests.Add([pscustomobject]@{ KernelTag = $KernelTag; Name = $Name; Provider = $Provider }) | Out-Null
            return 'fixture://kernel-source'
        }
        function Invoke-WebRequest {
            param([string]$Uri, [string]$OutFile, [switch]$UseBasicParsing)
            [System.IO.File]::Copy($script:KernelSdistFixture, $OutFile, $true)
        }
        if ($null -ne $hashFunction) { Invoke-Expression $hashFunction.Extent.Text }
        if ($null -ne $sourceInstallerFunction) { Invoke-Expression $sourceInstallerFunction.Extent.Text }
        if ($null -ne $sourceInstallerFunction -and $null -ne $hashFunction) {
            Install-KernelSource -VenvPython $fakePython -SourceArtifact $selectedSource -KernelTag $sourceManifestFixture.kernel_tag -StageDir $sourceStageDir -Provider 'github'
            $downloadedSource = Join-Path $sourceStageDir $kernelSdistRecord.filename
            Assert-True (Test-Path -LiteralPath $downloadedSource) 'kernel source download writes the declared archive into staging'
            Assert-Equal $kernelSdistSha (Get-Sha256Hex -Path $downloadedSource) 'kernel source download is checksum-valid'
            Assert-Equal 'github' $script:KernelAssetRequests[0].Provider 'kernel source download uses the requested provider'
            Assert-Equal $kernelSdistRecord.filename $script:KernelAssetRequests[0].Name 'kernel source download requests the declared sdist filename'
            $pipArgs = Get-Content -LiteralPath $pipArgsLog -Raw
            Assert-Contains $pipArgs '-m pip install' 'fake Windows Python records the pip install invocation'
            Assert-Contains $pipArgs $downloadedSource 'pip receives the downloaded source archive by local path'
            Assert-NotContains $pipArgs $kernelWheelRecord.filename 'pip never receives the compatible wheel record'

            $mismatchedSource = [pscustomobject]@{
                kind = 'sdist'
                filename = $kernelSdistRecord.filename
                sha256 = ('f' * 64)
            }
            $mismatchStageDir = Join-Path $kernelSourceTestRoot 'mismatch-stage'
            New-Item -ItemType Directory -Force -Path $mismatchStageDir | Out-Null
            Remove-Item -LiteralPath $pipArgsLog -Force -ErrorAction SilentlyContinue
            $mismatchFailed = $false
            try {
                Install-KernelSource -VenvPython $fakePython -SourceArtifact $mismatchedSource -KernelTag $sourceManifestFixture.kernel_tag -StageDir $mismatchStageDir -Provider 'github'
            } catch {
                $mismatchFailed = $true
            }
            Assert-True $mismatchFailed 'kernel source checksum mismatch fails before pip'
            Assert-True (-not (Test-Path -LiteralPath $pipArgsLog)) 'checksum failure makes no package-install command'
        }
    } finally {
        if (Test-Path -LiteralPath $kernelSourceTestRoot) { Remove-Item -LiteralPath $kernelSourceTestRoot -Recurse -Force -ErrorAction SilentlyContinue }
    }

    Write-Section 'execution: stable kernel loop falls back without changing TUI provider'
    $kernelLoopTestRoot = Join-Path ([IO.Path]::GetTempPath()) ("lingtai ps kernel loop contract {0}" -f ([Guid]::NewGuid().ToString('N')))
    try {
        $loopVenvDir = Join-Path $kernelLoopTestRoot 'runtime\venv'
        $loopVenvPython = Join-Path $loopVenvDir 'Scripts\python.exe'
        New-Item -ItemType Directory -Force -Path (Split-Path $loopVenvPython -Parent) | Out-Null
        Set-Content -LiteralPath $loopVenvPython -Value 'controlled venv marker' -Encoding ASCII
        $script:KernelLoopProviders = New-Object System.Collections.Generic.List[string]
        $script:KernelLoopInstallCalls = New-Object System.Collections.Generic.List[object]
        $script:KernelLoopManifest = $sourceManifestFixture
        $script:KernelLoopTuiProvider = 'fixture-tui'
        function Find-VenvPython { return @{ Launcher = 'fixture-python'; Args = @() } }
        function Remove-OrphanedKernelDistInfo { param([string]$VenvDir) }
        function New-StagingDir {
            $stage = Join-Path $kernelLoopTestRoot ("stage-{0}" -f $script:KernelLoopProviders.Count)
            New-Item -ItemType Directory -Force -Path $stage | Out-Null
            return $stage
        }
        function Resolve-KernelLatestTag {
            param([string]$Provider)
            $script:KernelLoopProviders.Add($Provider) | Out-Null
            return 'v2.3.4'
        }
        function Get-KernelManifest {
            param([string]$KernelTag, [string]$ManifestFilename, [string]$Provider)
            return $script:KernelLoopManifest
        }
        function Install-KernelSource {
            param([string]$VenvPython, $SourceArtifact, [string]$KernelTag, [string]$StageDir, [string]$Provider)
            $script:KernelLoopInstallCalls.Add([pscustomobject]@{
                Provider = $Provider
                SourceFilename = $SourceArtifact.filename
                StageDir = $StageDir
            }) | Out-Null
            if ($Provider -eq 'mirror') { throw 'controlled mirror source failure' }
        }
        function Confirm-KernelImport {
            param([string]$VenvPython, [string]$ExpectedVersion)
            return $ExpectedVersion
        }
        function Write-KernelProvenance {
            param([string]$VenvDir, [string]$KernelTag, [string]$KernelVersion, [string]$SourceFilename, [string]$SourceSha256, [string]$Provider)
        }
        function Write-Warn { param([string]$Message) }
        if ($null -ne $venvFunction) { Invoke-Expression $venvFunction.Extent.Text }
        $script:KernelProvider = 'mirror'
        $script:TuiProvider = $script:KernelLoopTuiProvider
        if ($null -ne $venvFunction) {
            $loopResult = Install-Venv -GlobalDir $kernelLoopTestRoot
            Assert-Equal 'mirror,github' ($script:KernelLoopProviders -join ',') 'stable kernel loop tries mirror before GitHub'
            Assert-Equal 2 $script:KernelLoopInstallCalls.Count 'stable kernel loop executes source install for both providers'
            Assert-Equal $kernelSdistRecord.filename $script:KernelLoopInstallCalls[0].SourceFilename 'mirror attempt receives the declared sdist'
            Assert-Equal $kernelSdistRecord.filename $script:KernelLoopInstallCalls[1].SourceFilename 'GitHub fallback receives the declared sdist'
            Assert-Equal 'github' $loopResult.KernelProvider 'successful kernel fallback records GitHub as provider'
            Assert-Equal 'github' $script:KernelProvider 'kernel provider state ends at the successful GitHub provider'
            Assert-Equal $script:KernelLoopTuiProvider $script:TuiProvider 'kernel fallback leaves TUI provider state unchanged'
        }
    } finally {
        if (Test-Path -LiteralPath $kernelLoopTestRoot) { Remove-Item -LiteralPath $kernelLoopTestRoot -Recurse -Force -ErrorAction SilentlyContinue }
    }

    Write-Section 'contract: validation precedes provider selection'
    $sourceValidation = $psText.IndexOf('$sourceArg = if ([string]::IsNullOrWhiteSpace($Source))')
    $sourceProvider = $psText.LastIndexOf('Resolve-SourceProvider')
    Assert-True ($sourceValidation -ge 0 -and $sourceProvider -gt $sourceValidation) 'PowerShell validates -Source before resolving a provider'
    Assert-Contains $psText 'if ($sourceArg -eq ''gitee'') { Fail' 'retired provider is rejected in the pre-provider validation block'
    Assert-Contains $psText 'if ($haveArchive -ne $haveChecksum) { Fail' 'archive/checksum pairing is validated before provider selection'

    Write-Section 'contract: stale TUI auto-heal is PATH-scoped'
    Assert-Contains $psText 'function Remove-OtherTuiOnPath' 'PowerShell defines the PATH-scoped stale TUI cleanup'
    $addToPathStart = $psText.IndexOf('function Add-ToPath')
    $removeOtherStart = $psText.IndexOf('function Remove-OtherTuiOnPath')
    Assert-NotContains $psText.Substring($addToPathStart, $removeOtherStart - $addToPathStart) 'Remove-OtherTuiOnPath -CanonicalPath' 'PowerShell PATH mutation does not delete stale TUI copies before receipt success'
    $invokeMainText = $psText.Substring($psText.IndexOf('function Invoke-Main'))
    Assert-Equal 3 ([regex]::Matches($invokeMainText, '(?s)Write-InstallMetadata @metaArgs.*?Remove-OtherTuiOnPath.*?Write-Completion').Count) 'every real PowerShell branch cleans stale TUI copies after metadata and before completion'
    Assert-Contains $psText 'Could not remove conflicting lingtai-tui.exe at' 'PowerShell stale TUI cleanup fails loudly with the exact path'
    Assert-Contains $psText 'Confirm-StagedVersion -StagedTui $tuiDest' 'PowerShell verifies the installed canonical TUI target directly'
    Assert-Contains $psText 'This installer PowerShell process is ready to invoke lingtai-tui now.' 'PowerShell completion states the installer process is ready immediately'
    Assert-Contains $psText 'Persistence was intentionally skipped (-NoModifyPath). If a separate/calling PowerShell process needs PATH, run:' 'PowerShell -NoModifyPath completion scopes the activation command to a separate process'
    Assert-Contains $psText 'New PowerShell windows inherit the updated user PATH.' 'PowerShell completion describes persistent PATH inheritance'
    Assert-NotContains $psText 'Before invoking lingtai-tui' 'PowerShell completion has no false current-process activation warning'
    Assert-Contains $psText 'function Resolve-PhysicalDirectoryIdentity' 'PowerShell resolves physical directory identity before PATH cleanup'
    $pathHealFunction = $sourceAst.Find({
        param($node)
        $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Remove-OtherTuiOnPath'
    }, $true)
    $physicalResolverFunction = $sourceAst.Find({
        param($node)
        $node -is [System.Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'Resolve-PhysicalDirectoryIdentity'
    }, $true)
    Assert-True ($null -ne $pathHealFunction) 'stale TUI cleanup is discoverable through the PowerShell AST'
    Assert-True ($null -ne $physicalResolverFunction) 'physical directory identity resolver is discoverable through the PowerShell AST'
    if ($null -ne $pathHealFunction -and $null -ne $physicalResolverFunction) {
        $pathHealText = $pathHealFunction.Extent.Text
        $candidateStart = $pathHealText.IndexOf('$candidate = Join-Path $directory ''lingtai-tui.exe''')
        $candidateProbe = $pathHealText.IndexOf('$item = Get-Item -LiteralPath $candidate', $candidateStart)
        $directoryResolve = $pathHealText.IndexOf('$directoryIdentity = Resolve-PhysicalDirectoryIdentity -Directory $directory')
        Assert-True ($candidateStart -ge 0 -and $candidateProbe -gt $candidateStart -and $directoryResolve -gt $candidateProbe) 'PowerShell probes each PATH TUI candidate before resolving physical directory identity'

        $pathHealRoot = Join-Path ([IO.Path]::GetTempPath()) ("lingtai ps path heal contract {0}" -f ([Guid]::NewGuid().ToString('N')))
        $canonicalDir = Join-Path $pathHealRoot 'canonical'
        $aliasDir = Join-Path $pathHealRoot 'canonical-alias'
        $staleDir = Join-Path $pathHealRoot 'stale'
        $missingDir = Join-Path $pathHealRoot 'missing'
        $savedPath = $env:PATH
        try {
            New-Item -ItemType Directory -Force -Path $canonicalDir, $staleDir | Out-Null
            Set-Content -LiteralPath (Join-Path $canonicalDir 'lingtai-tui.exe') -Value 'canonical' -Encoding ASCII
            Set-Content -LiteralPath (Join-Path $staleDir 'lingtai-tui.exe') -Value 'stale' -Encoding ASCII
            # PowerShell 5.1 has no Junction item type; mklink is an inbox
            # Windows command and does not add a test/runtime dependency.
            & $env:ComSpec /d /c ('mklink /J "{0}" "{1}"' -f $aliasDir, $canonicalDir) 2>&1 | Out-Null
            $junctionExit = $LASTEXITCODE
            Assert-Equal 0 $junctionExit 'native Windows test creates a junction alias to the canonical directory'
            Assert-True (Test-Path -LiteralPath $aliasDir -PathType Container) 'native Windows test has a reparse-point PATH alias'
            $aliasAttributes = (Get-Item -LiteralPath $aliasDir -Force).Attributes
            Assert-True (($aliasAttributes -band [IO.FileAttributes]::ReparsePoint) -ne 0) 'native Windows test confirms the PATH alias is a reparse point'
            $env:PATH = "$staleDir;$staleDir;$missingDir;$aliasDir;$canonicalDir"
            function Fail { param([string]$Message) throw $Message }
            Invoke-Expression $physicalResolverFunction.Extent.Text
            Invoke-Expression $pathHealFunction.Extent.Text

            Remove-OtherTuiOnPath -CanonicalPath (Join-Path $canonicalDir 'lingtai-tui.exe')
            Assert-True (Test-Path -LiteralPath (Join-Path $canonicalDir 'lingtai-tui.exe')) 'PowerShell PATH auto-heal preserves the canonical TUI'
            Assert-True (Test-Path -LiteralPath (Join-Path $aliasDir 'lingtai-tui.exe')) 'PowerShell PATH auto-heal preserves the canonical TUI through its junction alias'
            Assert-True (-not (Test-Path -LiteralPath (Join-Path $staleDir 'lingtai-tui.exe'))) 'PowerShell PATH auto-heal removes the stale TUI'

            Set-Content -LiteralPath (Join-Path $staleDir 'lingtai-tui.exe') -Value 'stale' -Encoding ASCII
            function Remove-Item {
                param([string]$LiteralPath, [switch]$Force, [string]$ErrorAction)
                throw 'controlled removal failure'
            }
            $healFailed = $false
            try {
                Remove-OtherTuiOnPath -CanonicalPath (Join-Path $canonicalDir 'lingtai-tui.exe')
            } catch {
                $healFailed = $true
                Assert-Contains $_.Exception.Message (Join-Path $staleDir 'lingtai-tui.exe') 'PowerShell PATH auto-heal failure names the exact stale path'
            }
            Assert-True $healFailed 'PowerShell PATH auto-heal fails when stale TUI removal fails'
        } finally {
            $env:PATH = $savedPath
            Microsoft.PowerShell.Management\Remove-Item -LiteralPath Function:\Remove-Item -Force -ErrorAction SilentlyContinue
            Microsoft.PowerShell.Management\Remove-Item -LiteralPath $pathHealRoot -Recurse -Force -ErrorAction SilentlyContinue
        }
    }

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
    Assert-Contains $psText 'Install-FromLocalArtifact -Archive $ArchivePath' 'explicit local-artifact mode remains available behind the incompatibility guard'
    Assert-Contains $psText 'only the TUI falls back to the latest GitHub source release' 'TUI provider fallback wording is component-scoped'
    Assert-True ([regex]::IsMatch($psText, 'The kernel is installed from\s+a verified local source archive selected from its release manifest')) 'kernel wording stays independent of TUI source choice'
    Assert-NotContains $psText 'Choose GitHub explicitly with -Source github' 'mirror helper errors do not prescribe a provider'
    Assert-Contains $psText 'lingtai.ai TUI source is unavailable; falling back to the latest GitHub TUI source release.' 'TUI caller retains its automatic fallback warning'
    Assert-Contains $psText 'lingtai.ai kernel release is unavailable; falling back to the latest GitHub kernel release.' 'kernel caller retains its automatic fallback warning'
} finally {
    if ($null -ne $testRoot -and (Test-Path -LiteralPath $testRoot)) { Remove-Item -LiteralPath $testRoot -Recurse -Force -ErrorAction SilentlyContinue }
}

Write-Host ''
Write-Host ("summary: {0} passed, {1} failed" -f $script:Passed, $script:Failures)
if ($script:Failures -gt 0) {
    Write-Host 'RESULT: FAIL'
    exit 1
}
Write-Host 'RESULT: PASS'
exit 0
