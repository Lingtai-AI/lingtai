<#
.SYNOPSIS
    Lightweight smoke test for install.ps1 (no network, no install).

.DESCRIPTION
    Parses install.ps1 to confirm it is syntactically valid, then runs it with
    -DryRun so the control flow executes without downloading or modifying
    anything. Intended for CI (windows-latest) and local pwsh checks. Exits
    non-zero on any failure.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot
$installPs1 = Join-Path $root 'install.ps1'

if (-not (Test-Path -LiteralPath $installPs1)) {
    Write-Error "install.ps1 not found at $installPs1"
    exit 1
}

Write-Host "1. Syntax check (parse install.ps1)..."
$null = [ScriptBlock]::Create((Get-Content -Raw -LiteralPath $installPs1))
Write-Host "   ok"

Write-Host "2. Tokenize/parse via AST (surface parse errors)..."
$tokens = $null
$errors = $null
[System.Management.Automation.Language.Parser]::ParseFile($installPs1, [ref]$tokens, [ref]$errors) | Out-Null
if ($errors -and $errors.Count -gt 0) {
    foreach ($e in $errors) { Write-Error $e.Message }
    exit 1
}
Write-Host "   ok ($($tokens.Count) tokens, 0 parse errors)"

Write-Host "3. Dry run (-DryRun, no network/side effects)..."
& $installPs1 -DryRun -SkipVenv -Version 'v0.0.0-test'
if ($LASTEXITCODE -ne 0) {
    Write-Error "install.ps1 -DryRun exited $LASTEXITCODE"
    exit 1
}
Write-Host "   ok"

Write-Host ""
Write-Host "install.ps1 smoke test passed." -ForegroundColor Green
