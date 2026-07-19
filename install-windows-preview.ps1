[CmdletBinding()]
param(
    [string]$Version,
    [string]$ArchivePath,
    [string]$ChecksumPath,
    [string]$InstallDir = (Join-Path $env:LOCALAPPDATA "LingTai\preview")
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Fail([string]$Message) {
    throw "LingTai Windows preview bootstrap failed: $Message"
}

$isWindowsVariable = Get-Variable -Name IsWindows -ErrorAction SilentlyContinue
if ($isWindowsVariable -and -not [bool]$isWindowsVariable.Value) {
    Fail "this preview is Windows amd64 only"
}
$arch = if ($env:PROCESSOR_ARCHITEW6432) { $env:PROCESSOR_ARCHITEW6432 } else { $env:PROCESSOR_ARCHITECTURE }
if ($arch -ne "AMD64" -or -not [Environment]::Is64BitOperatingSystem) {
    Fail "this preview is amd64 only; detected '$arch'"
}
if ([string]::IsNullOrWhiteSpace($ArchivePath) -xor [string]::IsNullOrWhiteSpace($ChecksumPath)) {
    Fail "-ArchivePath and -ChecksumPath must be supplied together"
}

$temp = Join-Path ([IO.Path]::GetTempPath()) ("lingtai-preview-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $temp | Out-Null
try {
    if ([string]::IsNullOrWhiteSpace($ArchivePath)) {
        if ($Version -notmatch '^v[0-9][A-Za-z0-9._-]*$') {
            Fail "pass a release version such as -Version v0.X.Y"
        }
        $name = "lingtai-$Version-windows-amd64-preview.zip"
        $ArchivePath = Join-Path $temp $name
        $ChecksumPath = "$ArchivePath.sha256"
        $base = "https://github.com/Lingtai-AI/lingtai/releases/download/$Version"
        Invoke-WebRequest -Uri "$base/$name" -OutFile $ArchivePath
        Invoke-WebRequest -Uri "$base/$name.sha256" -OutFile $ChecksumPath
    }

    if (-not (Test-Path -LiteralPath $ArchivePath -PathType Leaf)) { Fail "missing archive: $ArchivePath" }
    if (-not (Test-Path -LiteralPath $ChecksumPath -PathType Leaf)) { Fail "missing checksum: $ChecksumPath" }
    $archiveName = Split-Path -Leaf $ArchivePath
    $line = (Get-Content -LiteralPath $ChecksumPath -Raw).Trim()
    if ($line -match "^([0-9a-fA-F]{64})\s+\*?$([regex]::Escape($archiveName))$") {
        $expectedHash = $Matches[1].ToLowerInvariant()
    } else {
        Fail "checksum file must name $archiveName"
    }
    $actualHash = (Get-FileHash -LiteralPath $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $expectedHash) { Fail "SHA256 mismatch for $archiveName" }

    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [IO.Compression.ZipFile]::OpenRead($ArchivePath)
    try {
        $members = @($zip.Entries | ForEach-Object { $_.FullName })
    } finally {
        $zip.Dispose()
    }
    $expectedMembers = @("lingtai-tui.exe", "lingtai-portal.exe", "WINDOWS-PREVIEW.md")
    if ($members.Count -ne 3 -or (Compare-Object $members $expectedMembers)) {
        Fail "archive members must be exactly: $($expectedMembers -join ', ')"
    }

    $running = @(Get-Process -Name "lingtai-tui", "lingtai-portal" -ErrorAction SilentlyContinue)
    if ($running.Count -gt 0) {
        Fail "stop lingtai-tui.exe and lingtai-portal.exe before updating"
    }

    $stage = Join-Path $temp "stage"
    Expand-Archive -LiteralPath $ArchivePath -DestinationPath $stage
    $parent = Split-Path -Parent $InstallDir
    if ([string]::IsNullOrWhiteSpace($parent)) { Fail "invalid install directory: $InstallDir" }
    New-Item -ItemType Directory -Force -Path $parent | Out-Null
    $backup = $null
    if (Test-Path -LiteralPath $InstallDir) {
        $backup = "$InstallDir.backup-$([guid]::NewGuid().ToString('N'))"
        Move-Item -LiteralPath $InstallDir -Destination $backup
    }
    try {
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
        foreach ($member in $expectedMembers) {
            Copy-Item -LiteralPath (Join-Path $stage $member) -Destination (Join-Path $InstallDir $member)
        }
    } catch {
        Remove-Item -LiteralPath $InstallDir -Recurse -Force -ErrorAction SilentlyContinue
        if ($backup -and (Test-Path -LiteralPath $backup)) {
            Move-Item -LiteralPath $backup -Destination $InstallDir
        }
        throw
    }
    if ($backup) {
        try { Remove-Item -LiteralPath $backup -Recurse -Force }
        catch { Write-Warning "installed the new preview but could not remove backup: $backup" }
    }

    Write-Host "Installed experimental Windows amd64 preview to $InstallDir"
    Write-Host "This does not install Python/kernel files, PATH entries, services, signing, or updater metadata."
} finally {
    Remove-Item -LiteralPath $temp -Recurse -Force -ErrorAction SilentlyContinue
}
