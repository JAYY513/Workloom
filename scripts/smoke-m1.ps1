# M1.7: build in the source module, exercise an isolated Git project.
# Usage: powershell -NoProfile -File scripts/smoke-m1.ps1 [-Keep]
[CmdletBinding()]
param([switch]$Keep)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
$workspace = Join-Path ([IO.Path]::GetTempPath()) ('smoke-m1-' + [guid]::NewGuid().ToString('N'))
$project = Join-Path $workspace 'demo-project'
$devsys = Join-Path $workspace 'workloom.exe'
$helper = Join-Path $workspace 'smoke-helper.exe'
$originalLocation = Get-Location
$originalConfig = $env:DEVSYS_CONFIG_DIR
$originalEncoding = [Console]::OutputEncoding
$success = $false
function Assert-Exit([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "$Step failed: exit $LASTEXITCODE" }
}
try {
    [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding
    New-Item -ItemType Directory -Path $project | Out-Null
    $env:DEVSYS_CONFIG_DIR = Join-Path $workspace 'config'
    Set-Location $repoRoot
    go build -o $devsys ./cmd/workloom
    Assert-Exit 'build CLI'
    go build -o $helper ./scripts/smoke-m1-helper.go
    Assert-Exit 'build helper'
    git init -q $project
    Assert-Exit 'git init'
    Set-Location $project
    & $workloom init
    Assert-Exit 'init'
    & $helper $project
    Assert-Exit 'create scenario'
    & $workloom --json config check
    Assert-Exit 'config check'
    $json = & $workloom --json search 'M1'
    Assert-Exit 'search'
    $result = $json | ConvertFrom-Json
    if (-not $result.ok -or -not ($result.matches | Where-Object { $_.path -eq 'workitems/WLM-1.yaml' })) {
        throw 'search did not return the work item'
    }
    $json | Write-Output
    git -c core.autocrlf=false add -- .devsys
    Assert-Exit 'git add'
    git -c core.quotepath=false status --porcelain
    Assert-Exit 'git status'
    & $helper --verify $project
    Assert-Exit 'verify scenario and tracked text'
    $success = $true
    Write-Output 'PASS: M1 create -> record -> complete; all tracked state is UTF-8 text.'
} finally {
    Set-Location $originalLocation
    $env:DEVSYS_CONFIG_DIR = $originalConfig
    [Console]::OutputEncoding = $originalEncoding
    if ($success -and $Keep) {
        Remove-Item -LiteralPath $devsys, $helper -Force
        Remove-Item -LiteralPath (Join-Path $workspace 'config') -Recurse -Force
        Write-Output "KEPT_PROJECT=$project"
    } elseif (Test-Path -LiteralPath $workspace) {
        Remove-Item -LiteralPath $workspace -Recurse -Force
    }
}
