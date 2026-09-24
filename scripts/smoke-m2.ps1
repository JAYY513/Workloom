# M2.5: interrupt-recovery lifecycle, repeatable.
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke-m2.ps1 [-Keep]
[CmdletBinding()]
param([switch]$Keep)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
$workspace = Join-Path ([IO.Path]::GetTempPath()) ('smoke-m2-' + [guid]::NewGuid().ToString('N'))
$project = Join-Path $workspace 'demo-project'
$devsys = Join-Path $workspace 'workloom.exe'
$workloom = $devsys
$originalLocation = Get-Location
$originalConfig = $env:DEVSYS_CONFIG_DIR
$originalEncoding = [Console]::OutputEncoding
$success = $false
function Assert-Exit([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "$Step failed: exit $LASTEXITCODE" }
}
function Get-Version([string]$Id) {
    $json = & $script:workloom --json workitem get $Id | ConvertFrom-Json
    return $json.version
}
try {
    [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding
    New-Item -ItemType Directory -Path $project | Out-Null
    $env:DEVSYS_CONFIG_DIR = Join-Path $workspace 'config'
    Set-Location $repoRoot
    go build -o $devsys ./cmd/workloom
    Assert-Exit 'build CLI'
    Set-Location $project

    # 1. Normal lifecycle: create -> ready -> claim.
    git init -q $project
    Assert-Exit 'git init'
    & $workloom init | Out-Null
    Assert-Exit 'init'
    & $workloom workitem create --title 'M2 recovery probe' --actor main --reason 'smoke-m2' | Out-Null
    Assert-Exit 'create'
    $v = Get-Version 'WLM-1'
    & $workloom workitem transition --id WLM-1 --to ready --actor main --reason 'scoping done' --expect $v | Out-Null
    Assert-Exit 'transition to ready'
    $claimJson = & $workloom --json workitem claim --id WLM-1 --owner probe-agent --reason 'smoke claim'
    Assert-Exit 'claim'
    $claim = $claimJson | ConvertFrom-Json
    if ($claim.status -ne 'in_progress') { throw 'claim did not produce in_progress' }
    if ($claim.run_id -notlike 'run-*') { throw 'claim did not create a run' }
    Write-Output 'PASS: claim atomically created lease + run'

    # 2. Simulated crash: owner gone, lease stale, run file missing.
    $lease = @"
schema_version: 1
workitem_id: WLM-1
owner: dead-agent
token: deadtoken0000000000000000000000000000000000000000000000000000000
claimed_at: 2026-01-01T00:00:00Z
lease_until: 2026-01-01T00:01:00Z
heartbeat_at: 2026-01-01T00:00:00Z
run_id: run-20990101-999
head_sha: ""
agent_id: dead-agent
agent_harness: shell
"@
    Set-Content -Path (Join-Path $project '.devsys/scheduling/WLM-1.yaml') -Value $lease -Encoding UTF8
    Remove-Item -Path (Join-Path $project '.devsys/runs/*.yaml') -Force

    # 3. Doctor reports the orphan read-only.
    $lockBefore = (Get-FileHash (Join-Path $project '.devsys/local/lock') -Algorithm SHA256).Hash
    $docJson = & $workloom --json doctor
    Assert-Exit 'doctor'
    $doc = $docJson -join ''
    if ($doc -notmatch 'orphan') { throw 'doctor missed the orphan lease' }
    $lockAfter = (Get-FileHash (Join-Path $project '.devsys/local/lock') -Algorithm SHA256).Hash
    if ($lockBefore -ne $lockAfter) { throw 'doctor mutated the lock file' }
    Write-Output 'PASS: doctor reports orphan read-only'

    # 4. Recover releases the lease and records the event.
    & $devsys recover --actor operator --reason 'lease expired after crash' | Out-Null
    Assert-Exit 'recover'
    if (Test-Path (Join-Path $project '.devsys/scheduling/WLM-1.yaml')) { throw 'recover did not remove the lease' }
    $events = Get-Content (Join-Path $project '.devsys/events/*.jsonl') -ErrorAction SilentlyContinue
    if (-not ($events -join '')) { $events = Get-ChildItem (Join-Path $project '.devsys/events') -Filter '*.jsonl' | Get-Content }
    if (-not (($events -join '') -match 'lease_recovered')) { throw 'no lease_recovered event' }
    Write-Output 'PASS: recover released orphan lease with event'

    # 5. Confirmed repair: done + missing artifact -> in_progress.
    $wiPath = Join-Path $project '.devsys/workitems/WLM-1.yaml'
    $s = Get-Content $wiPath -Raw
    $s = $s -replace 'status: in_progress', 'status: done'
    $s = $s -replace 'scheduling_state: claimed', 'scheduling_state: released'
    $s = $s -replace 'artifact_refs: \[\]', "artifact_refs:`n  - artifact-999"
    Set-Content -Path $wiPath -Value $s -Encoding UTF8 -NoNewline
    $dry = & $devsys repair --dry-run --actor operator --reason 'missing artifact evidence'
    Assert-Exit 'dry-run'
    $digest = ($dry | Where-Object { $_ -like 'digest:*' }) -replace '^digest:\s*', ''
    & $devsys repair --apply --confirm $digest --actor operator --reason 'confirmed downgrade' | Out-Null
    Assert-Exit 'repair apply'
    $wi = Get-Content $wiPath -Raw
    if ($wi -notmatch 'status: in_progress') { throw 'repair did not downgrade' }
    if (-not (($events -join '') -or (Get-ChildItem (Join-Path $project '.devsys/events') -Filter '*.jsonl' | Get-Content | Out-String)) -match 'repair_applied') { throw 'no repair_applied event' }
    Write-Output 'PASS: confirmed repair downgraded done -> in_progress with event'

    # 6. Tracked state is text-only.
    git -c core.autocrlf=false add -- .devsys
    Assert-Exit 'git add'
    git -c core.quotepath=false status --porcelain | Out-Null
    Assert-Exit 'git status'
    $success = $true
    Write-Output 'PASS: M2 claim -> crash -> doctor -> recover -> confirmed repair; event stream complete.'
} finally {
    Set-Location $originalLocation
    $env:DEVSYS_CONFIG_DIR = $originalConfig
    [Console]::OutputEncoding = $originalEncoding
    if ($success -and $Keep) {
        Remove-Item -LiteralPath $devsys -Force
        Remove-Item -LiteralPath (Join-Path $workspace 'config') -Recurse -Force
        Write-Output "KEPT_PROJECT=$project"
    } elseif (Test-Path -LiteralPath $workspace) {
        Remove-Item -LiteralPath $workspace -Recurse -Force
    }
}
