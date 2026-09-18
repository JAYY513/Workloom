# M3: policy gates, claim quality gate, record propagation, last-known-good,
# workflow instances and approvals, repeatable.
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke-m3.ps1 [-Keep]
[CmdletBinding()]
param([switch]$Keep)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$repoRoot = Split-Path -Parent $PSScriptRoot
$workspace = Join-Path ([IO.Path]::GetTempPath()) ('smoke-m3-' + [guid]::NewGuid().ToString('N'))
$project = Join-Path $workspace 'demo-project'
$devsys = Join-Path $workspace 'devsys.exe'
$originalLocation = Get-Location
$originalConfig = $env:DEVSYS_CONFIG_DIR
$originalEncoding = [Console]::OutputEncoding
$success = $false
$PARENT = ''; $SIBLING = ''; $CHILD = ''; $LOWQ = ''
function Assert-Exit([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "$Step failed: exit $LASTEXITCODE" }
}
function Get-Version([string]$Id) {
    $json = & $script:devsys --json workitem get $Id | ConvertFrom-Json
    return $json.version
}
function Get-WorkItemID([string]$Title) {
    $out = & $script:devsys workitem create --title $Title --actor smoke --reason smoke
    Assert-Exit 'workitem create'
    foreach ($line in $out) {
        if ($line -match '^(WLM-[0-9]+)') { return $Matches[1] }
    }
    throw "could not parse the created id from: $out"
}
function Invoke-Helper([string[]]$HelperArgs) {
    Push-Location $script:repoRoot
    try { return (go run ./scripts/m3helper @HelperArgs) } finally { Pop-Location }
}
function Set-HelperVars([string[]]$Lines) {
    foreach ($line in $Lines) {
        if ($line -match '^([A-Z][A-Z0-9_]*)=(.*)$') {
            Set-Variable -Name $Matches[1] -Value $Matches[2] -Scope Script
        }
    }
}
function Step-To([string]$Id, [string]$Target) {
    & $script:devsys workitem transition --id $Id --to $Target --actor smoke --reason smoke --expect (Get-Version $Id) | Out-Null
    Assert-Exit "transition $Id -> $Target"
}
function Invoke-Expect([int]$WantCode, [string[]]$CmdArgs, [string]$What) {
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $out = @(& $script:devsys @CmdArgs 2>&1) | ForEach-Object { "$_" }
        $code = $LASTEXITCODE
    } finally { $ErrorActionPreference = $prev }
    if ($code -ne $WantCode) { throw "${What}: exit $code (want $WantCode): $($out -join ' | ')" }
    return ($out -join "`n")
}
try {
    [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding
    New-Item -ItemType Directory -Path $project | Out-Null
    $env:DEVSYS_CONFIG_DIR = Join-Path $workspace 'config'
    Set-Location $repoRoot
    go build -o $devsys ./cmd/devsys
    Assert-Exit 'build CLI'
    Set-Location $project

    # 1. Fixtures: parent + sibling + child on the quick-fix policy.
    git init -q $project
    Assert-Exit 'git init'
    & $devsys init | Out-Null
    Assert-Exit 'init'
    Copy-Item (Join-Path $repoRoot 'docs/examples/workflows/quick-fix.md') (Join-Path $project '.devsys/workflows/quick-fix.md')
    Set-HelperVars (Invoke-Helper @('setup', $project))
    if (-not $CHILD -or -not $PARENT -or -not $SIBLING) { throw 'helper setup failed' }

    # 2. The done gate blocks the advance and lists the missing artifact.
    $blocked = Invoke-Expect 4 @('workitem', 'transition', '--id', $CHILD, '--to', 'done', '--actor', 'smoke', '--reason', 'try', '--expect', (Get-Version $CHILD)) 'gate did not block'
    if ($blocked -notmatch 'artifact "test-results" is required') { throw "missing list incomplete: $blocked" }
    Write-Output 'PASS: done gate blocked the advance with the missing artifact list'

    # 3. Evidence releases the gate; completion propagates records.
    Invoke-Helper @('evidence', $project, $CHILD) | Out-Null
    & $devsys workitem transition --id $CHILD --to done --actor smoke --reason 'verified' --expect (Get-Version $CHILD) | Out-Null
    Assert-Exit 'transition to done'
    foreach ($target in @($PARENT, $SIBLING)) {
        $status = (Invoke-Helper @('status', $project, $target)) -join ' '
        if ($status -notmatch 'decision://decision-1') { throw "no decision ref on ${target}: $status" }
        if ($status -notmatch 'finding://finding-1') { throw "no finding ref on ${target}: $status" }
        if ($status -notmatch 'STATUS=draft') { throw "target status changed: $status" }
        $propagated = (Invoke-Helper @('propagated', $project, $target)) -join ' '
        if ($propagated -ne 'PROPAGATED=1') { throw "expected one propagation event on ${target}: $propagated" }
    }
    Write-Output 'PASS: completion propagated records to parent and sibling, statuses untouched'

    # 4. The claim quality gate blocks a thin task and names the improvements.
    Set-HelperVars (Invoke-Helper @('lowq', $project))
    $blocked = Invoke-Expect 4 @('workitem', 'claim', '--id', $LOWQ, '--owner', 'smoke', '--reason', 'try', '--expect', (Get-Version $LOWQ)) 'quality gate did not block'
    if ($blocked -notmatch 'quality_gate.min_score') { throw "no threshold location: $blocked" }
    if ($blocked -notmatch '标题过短') { throw "no improvement items: $blocked" }
    Invoke-Helper @('improve', $project, $LOWQ) | Out-Null
    & $devsys workitem claim --id $LOWQ --owner smoke --reason 'improved' --expect (Get-Version $LOWQ) | Out-Null
    Assert-Exit 'claim improved task'
    Write-Output 'PASS: quality gate blocked a thin task and released the improved one'

    # 5. Last-known-good: corrupt policy blocks claims, reads keep working.
    Set-HelperVars (Invoke-Helper @('lowq', $project)); $PRIMED = $LOWQ
    Invoke-Helper @('improve', $project, $PRIMED) | Out-Null
    Set-HelperVars (Invoke-Helper @('lowq', $project)); $BLOCKED_TASK = $LOWQ
    Invoke-Helper @('improve', $project, $BLOCKED_TASK) | Out-Null
    Set-Content -Path (Join-Path $project '.devsys/workflows/quick-fix.md') -Value "---`nname: 缺 id`nversion: `"x`"`n---" -Encoding UTF8
    $denied = Invoke-Expect 4 @('workitem', 'claim', '--id', $BLOCKED_TASK, '--owner', 'smoke', '--reason', 'try', '--expect', (Get-Version $BLOCKED_TASK)) 'invalid policy did not block the claim'
    if ($denied -notmatch 'is invalid' -or $denied -notmatch 'last-known-good') { throw "claim refusal lacks the policy reason: $denied" }
    $nextJson = (& $devsys --json next) -join ''
    if ($nextJson -notmatch 'invalid_policy') { throw "next lacks the invalid_policy risk: $nextJson" }
    $warned = (& $devsys workflow next --id $PRIMED) -join "`n"
    if ($warned -notmatch 'last-known-good') { throw "workflow next lacks the last-known-good notice: $warned" }
    Copy-Item (Join-Path $repoRoot 'docs/examples/workflows/quick-fix.md') (Join-Path $project '.devsys/workflows/quick-fix.md') -Force
    & $devsys workitem claim --id $BLOCKED_TASK --owner smoke --reason 'restored' --expect (Get-Version $BLOCKED_TASK) | Out-Null
    Assert-Exit 'claim after restore'
    Write-Output 'PASS: corrupt policy blocked claims, kept read paths on last-known-good, and recovery restored dispatch'

    # 6. Workflow instances: branch, refused jump, pause/resume, no scheduling.
    $flow = @"
---
id: flow
name: 流程
version: 1
steps:
  - id: inspect
    type: inspect
  - id: implement
    type: execute
  - id: verify
    type: verify
transitions:
  - from: inspect
    to: implement
    when: workitem.clarification_needed == false
  - from: inspect
    to: verify
    when: workitem.clarification_needed == true
  - from: implement
    to: verify
  - from: verify
    to: done
---
"@
    Set-Content -Path (Join-Path $project '.devsys/workflows/flow.md') -Value $flow -Encoding UTF8
    $FLOW_WI = Get-WorkItemID 'flow smoke task'
    Step-To $FLOW_WI 'backlog'; Step-To $FLOW_WI 'ready'
    & $devsys workflow start --id $FLOW_WI --policy flow --actor smoke --reason begin --expect (Get-Version $FLOW_WI) | Out-Null
    Assert-Exit 'workflow start'
    $nextOut = (& $devsys workflow next --id $FLOW_WI) -join "`n"
    if ($nextOut -notmatch 'candidate: to=implement' -or $nextOut -notmatch 'next: implement') { throw "workflow next candidates wrong: $nextOut" }
    $schedBefore = ((Get-ChildItem (Join-Path $project '.devsys/scheduling') -ErrorAction SilentlyContinue | ForEach-Object { Get-Content $_.FullName -Raw }) -join '|')
    $jump = Invoke-Expect 4 @('workflow', 'step-complete', '--id', $FLOW_WI, '--to', 'nope', '--actor', 'smoke', '--reason', 'try', '--expect', (Get-Version $FLOW_WI)) 'jump was not refused'
    if ($jump -notmatch 'not a declared transition' -or $jump -notmatch 'allowed next steps') { throw "jump refusal lacks the allowed list: $jump" }
    & $devsys workflow step-complete --id $FLOW_WI --to implement --actor smoke --reason go --expect (Get-Version $FLOW_WI) | Out-Null
    Assert-Exit 'step-complete to implement'
    & $devsys workflow pause --id $FLOW_WI --actor smoke --reason hold --expect (Get-Version $FLOW_WI) | Out-Null
    Assert-Exit 'pause'
    $paused = Invoke-Expect 4 @('workflow', 'step-complete', '--id', $FLOW_WI, '--actor', 'smoke', '--reason', 'try', '--expect', (Get-Version $FLOW_WI)) 'paused advance was not refused'
    if ($paused -notmatch 'paused') { throw "paused refusal lacks its reason: $paused" }
    & $devsys workflow resume --id $FLOW_WI --actor smoke --reason go --expect (Get-Version $FLOW_WI) | Out-Null
    Assert-Exit 'resume'
    $schedAfter = ((Get-ChildItem (Join-Path $project '.devsys/scheduling') -ErrorAction SilentlyContinue | ForEach-Object { Get-Content $_.FullName -Raw }) -join '|')
    if ($schedBefore -ne $schedAfter) { throw 'workflow instance operations touched scheduling material' }
    Write-Output 'PASS: workflow instance branched, refused a jump with the allowed list, held while paused, wrote no scheduling'

    # 7. Approvals: the gate holds until an approved approval is consumed.
    $approval = @"
---
id: approval-flow
name: 审批门禁
version: 1
steps:
  - id: implement
    type: execute
gates:
  exempt_stages:
    - draft
    - backlog
  stages:
    in_progress:
      require_approval: true
---
"@
    Set-Content -Path (Join-Path $project '.devsys/workflows/approval-flow.md') -Value $approval -Encoding UTF8
    $APPROVAL_WI = Get-WorkItemID 'approval smoke task'
    Step-To $APPROVAL_WI 'backlog'; Step-To $APPROVAL_WI 'ready'
    & $devsys workflow start --id $APPROVAL_WI --policy approval-flow --actor smoke --reason begin --expect (Get-Version $APPROVAL_WI) | Out-Null
    Assert-Exit 'approval workflow start'
    $gated = Invoke-Expect 4 @('workitem', 'transition', '--id', $APPROVAL_WI, '--to', 'in_progress', '--actor', 'dev', '--reason', 'try', '--expect', (Get-Version $APPROVAL_WI)) 'approval gate did not hold'
    if ($gated -notmatch 'approval is required') { throw "approval refusal lacks its reason: $gated" }
    & $devsys approval request --id $APPROVAL_WI --stage in_progress --actor dev --reason 'architecture change' | Out-Null
    Assert-Exit 'approval request'
    & $devsys approval approve --id approval-1 --by boss --comment ok | Out-Null
    Assert-Exit 'approval approve'
    & $devsys workitem transition --id $APPROVAL_WI --to in_progress --actor dev --reason go --expect (Get-Version $APPROVAL_WI) | Out-Null
    Assert-Exit 'approved transition'
    $consumed = (& $devsys --json approval list --workitem $APPROVAL_WI) -join ''
    if ($consumed -notmatch '"consumed_at":"2') { throw "approval was not consumed: $consumed" }
    $REJECT_WI = Get-WorkItemID 'reject smoke task'
    Step-To $REJECT_WI 'backlog'; Step-To $REJECT_WI 'ready'
    & $devsys workflow start --id $REJECT_WI --policy approval-flow --actor smoke --reason begin --expect (Get-Version $REJECT_WI) | Out-Null
    Assert-Exit 'reject workflow start'
    & $devsys approval request --id $REJECT_WI --stage in_progress --actor dev --reason change | Out-Null
    Assert-Exit 'reject approval request'
    & $devsys approval reject --id approval-2 --by boss --reason 'risk too high' | Out-Null
    Assert-Exit 'approval reject'
    $rejected = ((& $devsys workitem get $REJECT_WI) -join "`n")
    if ($rejected -notmatch 'blocked') { throw "rejected approval did not block the work item: $rejected" }
    $events = (Get-ChildItem (Join-Path $project '.devsys/events') -Filter '*.jsonl' | Get-Content | Out-String)
    if ($events -notmatch 'approval-2 rejected: risk too high') { throw 'status_changed lacks the approval reference' }
    Write-Output 'PASS: approval gate held until approval, consumed in the transition, and rejection blocked the work item'

    $success = $true
    Write-Output 'PASS: M3 gates / quality gate / propagation / last-known-good / workflow instances / approvals.'
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
