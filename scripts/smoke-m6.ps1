# M6: execution layer — workspace invariants, the dispatch tick, retry and
# stall handling, the completion check, and the harness adapter chain (driven
# through a stub codex on PATH, so the smoke suite needs no model provider).
# Repeatable. Requires Go, Git and Python (Windows PowerShell).
#
# usage: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke-m6.ps1 [-Keep]
param([switch]$Keep)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$workspace = Join-Path ([System.IO.Path]::GetTempPath()) ("smoke-m6-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
$project = Join-Path $workspace 'demo-project'
$stubs = Join-Path $workspace 'bin'
$success = $false
$oldConfig = $env:DEVSYS_CONFIG_DIR
$oldPath = $env:PATH
$oldLocation = Get-Location

function Assert-Exit([string]$What, [int]$Code) {
  if ($LASTEXITCODE -ne $Code) { throw "$What exited $LASTEXITCODE, want $Code" }
}

function New-Py([string]$Name, [string]$Script) {
  $path = Join-Path $script:pyDir "$Name.py"
  Set-Content -Path $path -Value $Script -Encoding ASCII
  return $path
}

function Invoke-Py([string]$Path, [string[]]$Arguments) {
  $output = & python $Path @Arguments
  if ($LASTEXITCODE -ne 0) { throw "python check failed: $Path" }
  return $output
}

function Invoke-Expect([string]$What, [int]$Code, [scriptblock]$Run) {
  $old = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  try { & $Run 2>$null | Out-Null } finally { $ErrorActionPreference = $old }
  if ($LASTEXITCODE -ne $Code) { throw "$What exited $LASTEXITCODE, want $Code" }
}

try {
  New-Item -ItemType Directory -Path $project -Force | Out-Null
  New-Item -ItemType Directory -Path $stubs -Force | Out-Null
  $script:pyDir = Join-Path $workspace 'py'
  New-Item -ItemType Directory -Path $script:pyDir -Force | Out-Null
  $env:DEVSYS_CONFIG_DIR = Join-Path $workspace 'config'
  Push-Location $repoRoot
  & go build -o (Join-Path $workspace 'devsys.exe') ./cmd/devsys; Assert-Exit 'go build devsys' 0
  Pop-Location
  $devsys = Join-Path $workspace 'devsys.exe'

  # The stub stands in for the real CLI: the adapter chain (prompt on stdin,
  # workspace cwd, exit code) is pinned without a provider.
  $stub = @'
@echo off
powershell -NoProfile -Command "$p = [Console]::In.ReadToEnd(); if ($p.Length -lt 10) { exit 9 }; [Console]::Error.WriteLine('stub codex: prompt bytes=' + $p.Length)"
if "%STUB_CODEX_FAIL%"=="1" (echo stub: refusing to work 1>&2 & exit /b 7)
git add -A
git -c user.email=agent@test -c user.name=agent commit -q -m "agent work" --allow-empty
echo {"type":"turn.completed","stub":true}
'@
  Set-Content -Path (Join-Path $stubs 'codex.cmd') -Value $stub -Encoding ASCII
  $env:PATH = "$workspace;$stubs;$env:PATH"

  & git init -q $project; Assert-Exit 'git init' 0
  Set-Location $project
  & $devsys init | Out-Null; Assert-Exit 'devsys init' 0
  $policy = @'
---
id: task
name: 执行层剧本
version: 1
steps:
  - id: implement
    type: execute
    required: true
concurrency:
  global: 1
limits:
  max_attempts: 2
  backoff_max_seconds: 1
  stall_threshold_seconds: 3600
---
在仓库根创建 notes.txt（一行 hello）并提交：{{workitem.title}}
'@
  Set-Content -Path (Join-Path $project '.devsys/workflows/task.md') -Value $policy -Encoding UTF8
  Add-Content -Path (Join-Path $project '.devsys/config.yaml') -Value 'dispatch_command: "echo dispatched"' -Encoding UTF8

  function New-Item($Title, $Priority) {
    $out = & $devsys workitem create --title $Title --actor me --reason demo
    Assert-Exit "workitem create $Title" 0
    $id = ($out | Select-Object -First 1).Split("`t")[0]
    & $devsys workitem update --id $id --priority $Priority | Out-Null; Assert-Exit 'workitem update' 0
    & $devsys workflow start --id $id --policy task --actor me --reason demo | Out-Null; Assert-Exit 'workflow start' 0
    foreach ($target in @('backlog', 'ready')) {
      & $devsys workitem transition --id $id --to $target --actor me --reason demo | Out-Null; Assert-Exit "transition $target" 0
    }
    return $id
  }

  $low = New-Item '低优先级' 5
  $high = New-Item '高优先级' 9
  & git add -A; Assert-Exit 'git add' 0
  & git -c user.email=devsys@test -c user.name=devsys commit -q -m fixture; Assert-Exit 'git commit' 0

  Write-Host '== workspace invariants =='
  $list = (& $devsys worktree list) -join "`n"
  if ($list -notmatch 'no workspaces') { throw "worktree list is not empty: $list" }
  Write-Host 'PASS: list is empty before any prepare'
  Invoke-Expect 'out-of-root removal' 3 { & $devsys worktree remove --path $workspace --actor me --reason demo }
  Write-Host 'PASS: out-of-root removal refused'

  Write-Host '== dispatch tick (cap 1, ordered) =='
  $tick1 = (& $devsys dispatch --once --actor ops --reason tick) -join "`n"
  if ($tick1 -notmatch "started $high") { throw "the higher priority item did not start first: $tick1" }
  if ($tick1 -notmatch 'cap_global') { throw "the cap did not hold back the second item: $tick1" }
  Write-Host 'PASS: only the higher priority item started, the other was capped'
  $tick2 = (& $devsys dispatch --once --actor ops --reason tick) -join "`n"
  if ($tick2 -notmatch 'in flight: 1') { throw "the tick lost track of the in-flight attempt: $tick2" }
  $before = (Get-ChildItem (Join-Path $project '.devsys/runs') -Filter *.yaml).Count
  & $devsys dispatch --dry-run --actor ops --reason preview | Out-Null; Assert-Exit 'dispatch --dry-run' 0
  $after = (Get-ChildItem (Join-Path $project '.devsys/runs') -Filter *.yaml).Count
  if ($before -ne $after) { throw 'the dry run created runs' }
  Write-Host 'PASS: a repeated tick starts nothing; the dry run changes nothing'

  Write-Host '== harness adapter chain (stub codex) =='
  $runId = (& $devsys run create --workitem $high --actor me --reason harness) | Select-Object -First 1
  $runId = $runId.Split("`t")[0]
  & $devsys worktree prepare --workitem $high --run $runId --actor me --reason harness | Out-Null; Assert-Exit 'worktree prepare' 0
  & $devsys run exec --id $runId --harness codex --actor ops --reason acceptance --timeout 120s | Out-Null; Assert-Exit 'run exec --harness codex' 0
  $verify = (& $devsys run verify --id $runId) -join "`n"
  if ($verify -notmatch 'advanced') { throw "the completion check did not see the commit: $verify" }
  & $devsys run complete --id $runId --actor ops --reason acceptance | Out-Null; Assert-Exit 'run complete' 0
  $fieldsPy = New-Py 'fields' @"
import json, subprocess, sys
run = json.loads(subprocess.run(["devsys.exe", "--json", "run", "get", sys.argv[1]], capture_output=True, text=True).stdout)["run"]
assert run["agent"]["harness"] == "codex", run["agent"]
assert run["verification"]["advanced"] is True, run["verification"]
assert run["workspace"]["worktree"] == "$high", run["workspace"]
print("PASS: harness=%s workspace=%s advanced=%s" % (run["agent"]["harness"], run["workspace"]["worktree"], run["verification"]["advanced"]))
"@
  Invoke-Py $fieldsPy @($runId) | Write-Host

  Write-Host '== completion check refuses work that did not advance =='
  $runId = (& $devsys run create --workitem $low --actor me --reason check) | Select-Object -First 1
  $runId = $runId.Split("`t")[0]
  & $devsys worktree prepare --workitem $low --run $runId --actor me --reason check | Out-Null; Assert-Exit 'worktree prepare' 0
  Invoke-Expect 'completion refusal' 3 { & $devsys run complete --id $runId --actor agent --reason done }
  Write-Host 'PASS: a run that advanced nothing cannot be completed'
  $reviewPy = New-Py 'review' @"
import json, subprocess, sys
item = json.loads(subprocess.run(["devsys.exe", "--json", "workitem", "get", sys.argv[1]], capture_output=True, text=True).stdout)["item"]
assert item["status"] == "review", item["status"]
print("PASS: the refusal routed", item["id"], "to", item["status"])
"@
  Invoke-Py $reviewPy @($low) | Write-Host
  & $devsys run complete --id $runId --force --by reviewer --actor reviewer --reason reviewed | Out-Null; Assert-Exit 'forced completion' 0
  Write-Host 'PASS: a reviewer accepted it explicitly'

  Write-Host '== retry sweep =='
  $leasePy = New-Py 'lease' @"
import json, subprocess, sys
item = json.loads(subprocess.run(["devsys.exe", "--json", "workitem", "get", sys.argv[1]], capture_output=True, text=True).stdout)["item"]
print(item.get("lease_owner", ""), item.get("lease_token", ""))
"@
  $lease = (Invoke-Py $leasePy @($high)) -split ' '
  & $devsys workitem release --id $high --owner $lease[0] --token $lease[1] --actor ops --reason 'free the slot' | Out-Null; Assert-Exit 'release' 0
  $failId = New-Item '会失败的尝试' 1
  & $devsys workitem update --id $failId --assigned-harness codex | Out-Null; Assert-Exit 'assign harness' 0
  & git add -A; Assert-Exit 'git add' 0
  & git -c user.email=devsys@test -c user.name=devsys commit -q -m 'fixture: failing item'; Assert-Exit 'git commit' 0
  $env:STUB_CODEX_FAIL = '1'
  $tick3 = (& $devsys dispatch --once --actor ops --reason tick) -join "`n"
  if ($tick3 -notmatch "started $failId") { throw "the failing item was not dispatched: $tick3" }
  Start-Sleep -Seconds 3
  $env:STUB_CODEX_FAIL = ''
  $tick4 = (& $devsys dispatch --once --actor ops --reason tick) -join "`n"
  if ($tick4 -notmatch "swept $failId") { throw "the failed attempt was not swept: $tick4" }
  if ($tick4 -notmatch 'retry_queued') { throw "the sweep did not queue a retry: $tick4" }
  Write-Host 'PASS: the tick swept a failed attempt into the retry queue'

  Write-Host '== unavailable harness =='
  $runId = (& $devsys run create --workitem $low --actor me --reason harness-check) | Select-Object -First 1
  $runId = $runId.Split("`t")[0]
  & $devsys worktree prepare --workitem $low --run $runId --actor me --reason harness-check | Out-Null; Assert-Exit 'worktree prepare' 0
  Invoke-Expect 'unavailable harness' 3 { & $devsys run exec --id $runId --harness claude --actor ops --reason acceptance }
  Write-Host 'PASS: an unavailable harness is refused with its probe result'

  Write-Host '== read-only commands never dispatch =='
  $before = (Get-ChildItem (Join-Path $project '.devsys/runs') -Filter *.yaml).Count
  $old = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  try {
    & $devsys next | Out-Null
    & $devsys doctor | Out-Null
    & $devsys project status | Out-Null
  } finally { $ErrorActionPreference = $old }
  $after = (Get-ChildItem (Join-Path $project '.devsys/runs') -Filter *.yaml).Count
  if ($before -ne $after) { throw 'a read-only command started an attempt' }
  Write-Host 'PASS: next, doctor and status changed nothing'

  Write-Host 'PASS: M6 execution layer (workspaces, dispatch, retry, completion, harnesses).'
  $success = $true
} finally {
  Set-Location $oldLocation
  $env:DEVSYS_CONFIG_DIR = $oldConfig
  $env:PATH = $oldPath
  if ($success -and $Keep) {
    Write-Host "KEPT_PROJECT=$project"
  } else {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $workspace
  }
}
