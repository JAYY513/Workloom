# M8: device handoff (sync status), conflict annotation + dispatch gate,
# conservative archiving, and the Contrabass-board fixture loop.
# Repeatable. Requires Go, Git and Python (Windows PowerShell).
#
# usage: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke-m8.ps1 [-Keep]
param([switch]$Keep)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$workspace = Join-Path ([IO.Path]::GetTempPath()) ('smoke-m8-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
$project = Join-Path $workspace 'demo-project'
$devsys = Join-Path $workspace 'workloom.exe'
$workloom = $devsys
$originalLocation = Get-Location
$originalConfig = $env:DEVSYS_CONFIG_DIR
$originalEncoding = [Console]::OutputEncoding
$originalPath = $env:PATH
$success = $false
function Assert-Exit([string]$Step) {
    if ($LASTEXITCODE -ne 0) { throw "$Step failed: exit $LASTEXITCODE" }
}
function Assert-Clean([string]$What) {
    $dirty = (& git status --short) -join "`n"
    if ($dirty) { throw "$What left the tree dirty: $dirty" }
}
function Invoke-Expect([string]$What, [int]$Code, [scriptblock]$Run) {
    $old = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try { & $Run 2>$null | Out-Null } finally { $ErrorActionPreference = $old }
    if ($LASTEXITCODE -ne $Code) { throw "$What exited $LASTEXITCODE, want $Code" }
}
function Get-Version([string]$Id) {
    $json = & $script:workloom --json workitem get $Id | ConvertFrom-Json
    return $json.version
}
function Move-To([string]$Id, [string[]]$Targets) {
    foreach ($t in $Targets) {
        & $script:workloom workitem transition --id $Id --to $t --actor me --reason smoke --expect (Get-Version $Id) | Out-Null
        if ($LASTEXITCODE -ne 0) { throw "transition $Id -> $t failed: exit $LASTEXITCODE" }
    }
}
try {
    [Console]::OutputEncoding = New-Object System.Text.UTF8Encoding
    New-Item -ItemType Directory -Path $project -Force | Out-Null
    $env:DEVSYS_CONFIG_DIR = Join-Path $workspace 'config'
    Set-Location $repoRoot
    go build -ldflags '-s -w' -o $devsys ./cmd/workloom
    Assert-Exit 'build CLI'
    $env:PATH = "$workspace;$env:PATH"
    Set-Location $project

    git init -q $project
    Assert-Exit 'git init'
    git config user.email devsys@test
    git config user.name devsys
    git config core.autocrlf false
    & $workloom init | Out-Null
    Assert-Exit 'init'
    $itemId = ((& $workloom workitem create --title '接力任务' --actor me --reason smoke) | Select-Object -First 1).Split("`t")[0]
    Move-To $itemId @('backlog', 'ready')
    git branch -M main
    Assert-Exit 'branch -M'
    git add -A
    git -c user.email=devsys@test -c user.name=devsys commit -q -m fixture
    Assert-Exit 'git commit fixture'
    $remote = Join-Path $workspace 'origin.git'
    git init -q --bare $remote
    Assert-Exit 'bare remote'
    git remote add origin $remote
    Assert-Exit 'remote add'
    git push -q -u origin main
    Assert-Exit 'push'

    Write-Output '== sync status classifies the handoff (M8.1) =='
    $clean = (& $devsys sync status) -join "`n"
    Assert-Exit 'sync clean'
    if (-not $clean.Contains('handoff: ready')) { throw "clean tree not ready: $clean" }
    $ready = (& $workloom --json sync status | ConvertFrom-Json)
    if (-not $ready.handoff_ready -or $ready.blockers.Count -ne 0) { throw "json not ready: $($ready | ConvertTo-Json -Compress)" }
    Write-Output 'PASS: a clean pushed tree is ready'
    & $devsys event record --type note --subject-type workitem --subject $itemId --actor me --content dirty-probe | Out-Null
    Assert-Exit 'dirty probe'
    $dirty = (& $devsys sync status) -join "`n"
    if (-not $dirty.Contains('blocked [uncommitted-devsys]')) { throw "dirty not classified: $dirty" }
    if (-not $dirty.Contains('handoff: NOT ready')) { throw "dirty tree reported ready" }
    git checkout -q -- .devsys
    Assert-Exit 'checkout .devsys'
    Write-Output 'PASS: uncommitted .devsys/ blocks with uncommitted-devsys, then recovers'
    $claim = (& $workloom --json workitem claim --id $itemId --owner old-device --reason handoff | ConvertFrom-Json)
    if ($claim.status -ne 'in_progress') { throw 'claim did not produce in_progress' }
    $leased = (& $devsys sync status) -join "`n"
    if (-not $leased.Contains('blocked [active-leases]')) { throw "lease not classified: $leased" }
    if (-not $leased.Contains('handoff: NOT ready')) { throw 'leased tree reported ready' }
    $token = $claim.token
    & $workloom workitem release --id $itemId --owner old-device --token $token --actor me --reason handoff-done --latest | Out-Null
    Assert-Exit 'release'
    git add -A
    git -c user.email=devsys@test -c user.name=devsys commit -q -m "chore(workloom): claim and release $itemId"
    Assert-Exit 'commit release'
    git push -q origin main
    Assert-Exit 'push release'
    if (-not ((& $devsys sync status) -join "`n").Contains('handoff: ready')) { throw 'release did not restore ready' }
    Write-Output 'PASS: an active lease blocks with active-leases; release restores ready'

    # clash.txt 用无 BOM UTF-8：与 sh 版裸字节一致（Set-Content -Encoding UTF8 在 PS5.1 带 BOM，会污染冲突内容的字节对等）。
    [IO.File]::WriteAllText((Join-Path $project 'clash.txt'), "base`n", [Text.UTF8Encoding]::new($false))
    git add -A
    git -c user.email=devsys@test -c user.name=devsys commit -q -m 'clash base'
    git push -q origin main
    git checkout -q -b side
    Assert-Exit 'checkout side'
    [IO.File]::WriteAllText((Join-Path $project 'clash.txt'), "side`n", [Text.UTF8Encoding]::new($false))
    git add -A
    git commit -q -m 'side clash'
    Assert-Exit 'side clash'
    git checkout -q main
    Assert-Exit 'checkout main'
    [IO.File]::WriteAllText((Join-Path $project 'clash.txt'), "main`n", [Text.UTF8Encoding]::new($false))
    git add -A
    git commit -q -m 'main clash'
    Assert-Exit 'main clash'
    Invoke-Expect 'merge must conflict' 1 { git merge side }
    $repair = (& $devsys repair --dry-run --actor me --reason conflict) -join "`n"
    Assert-Exit 'repair dry-run'
    if (-not $repair.Contains('note_unmerged_paths')) { throw "repair missed unmerged: $repair" }
    if (-not $repair.Contains('MERGE_HEAD')) { throw "repair missed machinery: $repair" }
    $d1 = ($repair -split "`n" | Where-Object { $_ -like 'digest:*' })
    $d2 = ((& $devsys repair --dry-run --actor me --reason conflict) -join "`n" -split "`n" | Where-Object { $_ -like 'digest:*' })
    if ($d1 -ne $d2) { throw "digest unstable: $d1 vs $d2" }
    $digest = $d1 -replace '^digest:\s*', ''
    Write-Output 'PASS: the dry run annotates clash.txt + MERGE_HEAD with a stable digest'
    $treeBefore = (git rev-parse 'HEAD:.devsys')
    $apply = (& $devsys repair --apply --confirm $digest --actor me --reason conflict) -join "`n"
    Assert-Exit 'repair apply'
    if (-not $apply.Contains('rejected clash.txt: requires human resolution; no automatic merge')) { throw "apply did not reject: $apply" }
    $treeAfter = (git rev-parse 'HEAD:.devsys')
    if ($treeBefore -ne $treeAfter) { throw 'apply touched .devsys under conflict' }
    Write-Output 'PASS: apply rejects every note and writes nothing under conflict'
    Invoke-Expect 'dispatch blocked under merge' 3 { & $devsys dispatch --dry-run --actor me --reason tick }
    $oldErr = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $blocked = (& $devsys dispatch --dry-run --actor me --reason tick 2>&1) -join "`n"
    $ErrorActionPreference = $oldErr
    if (-not $blocked.Contains('dispatch blocked')) { throw "dispatch did not name the block: $blocked" }
    Write-Output 'PASS: dispatch refuses the tick (exit 3) while the merge is open'
    [IO.File]::WriteAllText((Join-Path $project 'clash.txt'), "resolved`n", [Text.UTF8Encoding]::new($false))
    git add -A
    git -c user.email=devsys@test -c user.name=devsys commit -q -m 'resolve: keep main'
    Assert-Exit 'commit resolve'
    git push -q origin main
    Assert-Exit 'push resolve'
    & $devsys dispatch --dry-run --actor me --reason tick | Out-Null
    Assert-Exit 'dispatch recovers'
    if (-not ((& $devsys sync status) -join "`n").Contains('handoff: ready')) { throw 'sync not ready after resolve' }
    Write-Output 'PASS: a human resolve restores dispatch and the handoff verdict'

    Write-Output '== archiving keeps history queryable and shrinks live (M8.3) =='
    & $devsys event record --type note --subject-type workitem --subject $itemId --actor me --content live-event | Out-Null
    Assert-Exit 'live event'
    $nBefore = (& $workloom --json event list | ConvertFrom-Json).events.Count
    $liveBefore = (Get-ChildItem (Join-Path $project '.devsys/events') -Recurse -Filter '*.jsonl' | Measure-Object -Property Length -Sum).Sum
    $dryOut = (& $devsys archive events --before 2999-01 --dry-run --actor me --reason trim) -join "`n"
    Assert-Exit 'archive dry-run'
    if (-not $dryOut.Contains('dry-run: nothing was moved')) { throw "dry header missing: $dryOut" }
    $statusOut = (& git status --short) -join "`n"
    $untracked = $statusOut -split "`n" | Where-Object { $_.StartsWith('??') }
    if ($untracked) { throw "archive dry run created files: $($untracked -join '; ')" }
    Write-Output 'PASS: the archive dry run lists the move and writes nothing'
    $archOut = (& $devsys archive events --before 2999-01 --actor me --reason trim) -join "`n"
    Assert-Exit 'archive apply'
    if (-not $archOut.Contains("live bytes: $liveBefore -> 0 ")) { throw "live bytes did not drain: $archOut" }
    if (-not (Test-Path (Join-Path $project '.devsys/archive/manifest.yaml'))) { throw 'manifest missing' }
    $nAfter = (& $workloom --json event list | ConvertFrom-Json).events.Count
    if ($nBefore -ne $nAfter) { throw "events $nBefore -> $nAfter across archive" }
    Write-Output "PASS: $nAfter events still queryable after live drained to 0"
    $guardId = ((& $workloom workitem create --title '归档守卫' --actor me --reason smoke) | Select-Object -First 1).Split("`t")[0]
    Move-To $guardId @('backlog', 'ready')
    $runId = (& $workloom --json workitem claim --id $guardId --owner archivist --reason runs-guard | ConvertFrom-Json).run_id
    Invoke-Expect 'running stream refused' 1 { & $devsys archive runs --id $runId --actor me --reason trim }
    $oldErr2 = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    $refusal = (& $devsys archive runs --id $runId --actor me --reason trim 2>&1) -join "`n"
    $ErrorActionPreference = $oldErr2
    if (-not $refusal.Contains('only terminal runs may be archived')) { throw "refusal unexplained: $refusal" }
    Write-Output 'PASS: a running stream is refused with only-terminal-runs'
    $gtoken = (Get-Content (Join-Path $project ".devsys/local/leases/$guardId.token") -Raw).Trim()
    & $workloom workitem release --id $guardId --owner archivist --token $gtoken --actor me --reason runs-guard-done --latest | Out-Null
    Assert-Exit 'guard release'
    git add -A
    git -c user.email=devsys@test -c user.name=devsys commit -q -m "chore(workloom): archive segment $itemId"
    Assert-Exit 'commit archive'
    git push -q origin main
    Assert-Exit 'push archive'

    Write-Output '== board fixture loop flows back through the CLI only (M8.4) =='
    Move-To $itemId @('review', 'verification')
    git add -A
    git -c user.email=devsys@test -c user.name=devsys commit -q -m "chore(workloom): $itemId to verification"
    Assert-Exit 'commit verification'
    $export = (& python (Join-Path $repoRoot 'scripts/contrabass/export.py') --root $project --out (Join-Path $workspace 'board') --devsys $devsys) -join "`n"
    if (-not $export.Contains('verification -> review')) { throw "export missed the card: $export" }
    $cardPath = Join-Path $workspace "board/issues/$itemId.json"
    $card = Get-Content $cardPath -Raw | ConvertFrom-Json
    $card.board_status = 'done'
    $card | Add-Member -NotePropertyName result -NotePropertyValue (@{summary = 'board executed OK'; log = 'evidence' }) -Force
    $cardJson = $card | ConvertTo-Json -Depth 6
    [IO.File]::WriteAllText($cardPath, $cardJson, [Text.UTF8Encoding]::new($false))
    $dryImport = (& python (Join-Path $repoRoot 'scripts/contrabass/import.py') --root $project --board (Join-Path $workspace 'board') --dry-run --actor board --reason verdict --devsys $devsys) -join "`n"
    if (-not $dryImport.Contains('dry-run would')) { throw "import dry run silent: $dryImport" }
    $devsysDirty = ((& git status --short -- .devsys) -join "`n")
    if ($devsysDirty) { throw "import dry run touched .devsys: $devsysDirty" }
    Write-Output 'PASS: import --dry-run previews and leaves .devsys alone'
    $import = (& python (Join-Path $repoRoot 'scripts/contrabass/import.py') --root $project --board (Join-Path $workspace 'board') --actor board --reason verdict --devsys $devsys) -join "`n"
    if (-not $import.Contains('verification -> done')) { throw "import did not transition: $import" }
    $got = (& $workloom --json workitem get $itemId | ConvertFrom-Json)
    if ($got.item.status -ne 'done') { throw 'board verdict did not land' }
    Write-Output 'workitem is done'
    $evs = (& $workloom --json event list --subject-type workitem --subject $itemId | ConvertFrom-Json).events
    $hasTransition = @($evs | Where-Object { $_.type -eq 'status_changed' -and $_.content.Contains('verification -> done') }).Count -gt 0
    $hasComment = @($evs | Where-Object { $_.type -eq 'comment' -and $_.content.Contains('[board] board executed OK') }).Count -gt 0
    if (-not $hasTransition) { throw 'transition evidence missing' }
    if (-not $hasComment) { throw 'board comment missing' }
    Write-Output 'events carry the transition and the [board] comment'
    Write-Output 'PASS: the board verdict lands as a transition plus a [board] comment'
    git add -A
    git -c user.email=devsys@test -c user.name=devsys commit -q -m "chore(workloom): board verdict $itemId"
    Assert-Exit 'commit verdict'
    git push -q origin main
    Assert-Exit 'push verdict'
    if (-not ((& $devsys sync status) -join "`n").Contains('handoff: ready')) { throw 'final handoff not ready' }
    Assert-Clean 'final tree'

    $success = $true
    Write-Output 'PASS: M8 device handoff (sync verdicts, conflict annotation, archive, board loop).'
} finally {
    Set-Location $originalLocation
    $env:DEVSYS_CONFIG_DIR = $originalConfig
    $env:PATH = $originalPath
    [Console]::OutputEncoding = $originalEncoding
    if ($success -and $Keep) {
        Remove-Item -LiteralPath $devsys -Force
        Remove-Item -LiteralPath (Join-Path $workspace 'config') -Recurse -Force
        Write-Output "KEPT_PROJECT=$project"
    } elseif (Test-Path -LiteralPath $workspace) {
        Remove-Item -LiteralPath $workspace -Recurse -Force
    }
}
