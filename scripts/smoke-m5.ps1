<#
M5: the knowledge layer — page contract and validation, the file index, the
freshness verdicts and their reserved exit codes, human protection, resumable
refreshes, the layered context assembly, and the generator contract (driven here
through a stub generator, so the smoke suite needs no generator installed).
Repeatable. Requires Go and Git.
#>
[CmdletBinding()]
param([switch]$Keep)

$ErrorActionPreference = 'Stop'
# devsys writes UTF-8: read it as UTF-8 so the located Chinese messages survive
# capture, and keep native stderr from being promoted to a terminating error.
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8
$repoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$workspace = Join-Path ([System.IO.Path]::GetTempPath()) ("smoke-m5-" + [System.Guid]::NewGuid().ToString('N').Substring(0, 8))
$project = Join-Path $workspace 'demo-project'
$stubs = Join-Path $workspace 'bin'
$success = $false
$keepProject = $Keep

function Cleanup {
  if ($success -and $keepProject) {
    Write-Host "KEPT_PROJECT=$project"
  } elseif (Test-Path $workspace) {
    Remove-Item -Recurse -Force $workspace -ErrorAction SilentlyContinue
  }
}

function Fail($message) {
  Write-Host "FAIL: $message" -ForegroundColor Red
  throw $message
}

# Invoke-Devsys runs the built binary inside the project and keeps its exit code
# visible: the CLI resolves the project from the working directory, and a
# PowerShell 5.1 native command's stderr must not become a terminating error.
function Invoke-Devsys {
  param([string[]]$Arguments)
  Push-Location $project
  $previous = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  try {
    $output = & $script:D @Arguments 2>&1
    $code = $LASTEXITCODE
  } finally {
    $ErrorActionPreference = $previous
    Pop-Location
  }
  return @{ Code = $code; Text = (($output | ForEach-Object { "$_" }) -join "`n") }
}

try {
  New-Item -ItemType Directory -Force -Path $project, $stubs | Out-Null
  $env:DEVSYS_CONFIG_DIR = Join-Path $workspace 'config'
  Push-Location $repoRoot
  # Some endpoint protection flags the unstripped command binary as a false
  # positive; the smoke suite is about behaviour, not debug symbols.
  & go build -ldflags '-s -w' -o (Join-Path $workspace 'workloom.exe') ./cmd/workloom
  if ($LASTEXITCODE -ne 0) { Fail 'go build failed' }
  Pop-Location
  $script:D = Join-Path $workspace 'workloom.exe'

  # The stub generator implements the contract: it records the argv and the
  # scope file it was handed, then fails, sleeps or records a page on request.
  $stubBody = @'
param([Parameter(ValueFromRemainingArguments = $true)][string[]]$GeneratorArgs)
$ErrorActionPreference = 'Stop'
$log = Join-Path $env:STUB_GENERATOR_DIR 'generator.argv'
Set-Content -Path $log -Value ($GeneratorArgs -join ' ') -Encoding UTF8
$scope = ''
for ($i = 0; $i -lt $GeneratorArgs.Count; $i++) {
  if ($GeneratorArgs[$i] -eq '--scope-file' -and $i + 1 -lt $GeneratorArgs.Count) { $scope = $GeneratorArgs[$i + 1] }
}
if ($scope -ne '') {
  Copy-Item -Force $scope (Join-Path $env:STUB_GENERATOR_DIR 'generator.scope')
}
if ($env:STUB_GENERATOR_SLEEP) { Start-Sleep -Seconds ([int]$env:STUB_GENERATOR_SLEEP) }
Write-Output 'stub generator ran'
if ($env:STUB_GENERATOR_FAIL -eq '1') { Write-Error 'stub generator: refusing'; exit 7 }
if ($env:STUB_GENERATOR_WRITE) {
  $root = $env:STUB_GENERATOR_ROOT
  $page = Join-Path $root ($env:STUB_GENERATOR_WRITE -replace '/', '\')
  $stateFile = Join-Path $root '.devsys\knowledge\state.json'
  if (Test-Path $stateFile) { $state = Get-Content -Raw -Encoding UTF8 $stateFile | ConvertFrom-Json } else {
    $state = [pscustomobject]@{ schema_version = 1; baseline = [pscustomobject]@{ commit = '' }; pages = [pscustomobject]@{}; generator = 'stub' }
  }
  $hash = (Get-FileHash -Algorithm SHA256 -Path $page).Hash.ToLower()
  $commit = (& git -C $root rev-parse HEAD).Trim()
  $entries = @{}
  $state.pages.PSObject.Properties | ForEach-Object { $entries[$_.Name] = $_.Value }
  $entries[$env:STUB_GENERATOR_WRITE] = [pscustomobject]@{ content_hash = $hash; sources = @('internal/store/**') }
  $state.pages = [pscustomobject]$entries
  $state.baseline = [pscustomobject]@{ commit = $commit; branch = 'master' }
  # No BOM: managed state is strict JSON, and PowerShell 5.1's -Encoding UTF8
  # would add one.
  [System.IO.File]::WriteAllText($stateFile, ($state | ConvertTo-Json -Depth 6), (New-Object System.Text.UTF8Encoding($false)))
}
exit 0
'@
  Set-Content -Path (Join-Path $stubs 'generator.ps1') -Value $stubBody -Encoding UTF8
  $stubCmd = @"
@echo off
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0generator.ps1" %*
"@
  Set-Content -Path (Join-Path $stubs 'generator.cmd') -Value $stubCmd -Encoding ASCII
  # Forward slashes: the value lands in a double-quoted YAML scalar, where a
  # backslash would start an escape sequence.
  $generatorCmd = (Join-Path $stubs 'generator.cmd') -replace '\\', '/'
  $env:STUB_GENERATOR_ROOT = $project
  $env:STUB_GENERATOR_DIR = $workspace
  $env:PATH = "$workspace;$stubs;$env:PATH"

  function Invoke-Git {
    param([string[]]$Arguments)
    & git -C $project @Arguments | Out-Null
  }
  function Commit-All($message) {
    Invoke-Git @('add', '-A')
    & git -C $project -c user.email=t@example.com -c user.name=t commit -q -m $message --allow-empty | Out-Null
  }
  function New-Page {
    param([string]$Relative, [string]$Commit, [string[]]$Sources = @())
    $path = Join-Path $project ($Relative -replace '/', '\')
    New-Item -ItemType Directory -Force -Path (Split-Path -Parent $path) | Out-Null
    $lines = @('---', 'status: stable', 'type: module', 'triggers:', '  - 存储层', 'description: 可靠文本存储的定位与边界', "source_commit: $Commit")
    if ($Sources.Count -gt 0) {
      $lines += 'sources:'
      foreach ($source in $Sources) { $lines += "  - $source" }
    }
    $lines += @('---', '', '# 存储层', '', '正文。')
    Set-Content -Path $path -Value ($lines -join "`n") -Encoding UTF8
  }

  Invoke-Git @('init', '-q')
  Push-Location $project
  $initResult = Invoke-Devsys @('init')
  if ($initResult.Code -ne 0) { Fail "init failed: $($initResult.Text)" }
  Pop-Location

  Write-Host '== page contract and validation =='
  New-Item -ItemType Directory -Force -Path (Join-Path $project 'internal\store'), (Join-Path $project 'docs\repowiki\knowledge') | Out-Null
  Set-Content -Path (Join-Path $project 'internal\store\a.go') -Value 'package store' -Encoding UTF8
  Commit-All 'source'
  $head1 = (& git -C $project rev-parse HEAD).Trim()
  New-Page 'docs/repowiki/knowledge/存储.md' $head1 @('internal/store/**')
  $missing = "---`nstatus: stable`ntype: module`ndescription: 没有 triggers 的页面`nsource_commit: $head1`nsources:`n  - internal/store/**`n---`n`n# 缺触发词`n"
  Set-Content -Path (Join-Path $project 'docs\repowiki\knowledge\缺触发词.md') -Value $missing -Encoding UTF8
  Commit-All 'pages'
  $result = Invoke-Devsys @('knowledge', 'validate')
  if ($result.Code -ne 4) { Fail "validate of a page without triggers: exit=$($result.Code) want 4`n$($result.Text)" }
  if ($result.Text -notmatch '缺触发词\.md:1: triggers: 缺少必填字段') { Fail "validate output misses the located problem:`n$($result.Text)" }
  Write-Host 'PASS: a page without triggers is refused, located, and exits 4'
  Remove-Item (Join-Path $project 'docs\repowiki\knowledge\缺触发词.md')
  Commit-All 'fix'
  $result = Invoke-Devsys @('knowledge', 'validate')
  if ($result.Code -ne 0) { Fail "validate of a good page: exit=$($result.Code)`n$($result.Text)" }
  Write-Host 'PASS: the remaining page validates (exit 0)'

  Write-Host '== index scan and snapshot =='
  Set-Content -Path (Join-Path $project '.env') -Value 'TOKEN=1' -Encoding UTF8
  $big = New-Object byte[] (1024 * 1024 + 1)
  [System.IO.File]::WriteAllBytes((Join-Path $project 'big.bin'), $big)
  Set-Content -Path (Join-Path $project '.gitignore') -Value '.env' -Encoding UTF8
  $result = Invoke-Devsys @('--json', 'knowledge', 'scan')
  if ($result.Code -ne 0) { Fail "scan: exit=$($result.Code)`n$($result.Text)" }
  $scan = $result.Text | ConvertFrom-Json
  $reasons = @{}
  foreach ($entry in $scan.excluded) { $reasons[$entry.path] = $entry.reason }
  if (-not $reasons.ContainsKey('.env') -or $reasons['.env'] -notmatch '密钥名模式') { Fail "exclusion reasons: $($result.Text)" }
  if (-not $reasons.ContainsKey('big.bin') -or $reasons['big.bin'] -notmatch '字节上限') { Fail "exclusion reasons: $($result.Text)" }
  if ($scan.files -lt 1) { Fail "scan found no files: $($result.Text)" }
  $rescan = Invoke-Devsys @('knowledge', 'scan')
  if ($rescan.Code -ne 0) { Fail "rescan: exit=$($rescan.Code)" }
  Write-Host 'PASS: secrets and oversized files land in the exclusion list, and the snapshot rebuilds'

  Write-Host '== freshness and the reserved exit codes =='
  $result = Invoke-Devsys @('knowledge', 'status')
  if ($result.Code -ne 0) { Fail "fresh status: exit=$($result.Code)`n$($result.Text)" }
  Write-Host 'PASS: an unchanged tree is fresh (exit 0)'
  Set-Content -Path (Join-Path $project 'internal\store\a.go') -Value 'package store // edited' -Encoding UTF8
  $result = Invoke-Devsys @('--json', 'knowledge', 'status')
  if ($result.Code -ne 10) { Fail "stale status: exit=$($result.Code) want 10`n$($result.Text)" }
  $status = $result.Text | ConvertFrom-Json
  if ($status.status -ne 'stale') { Fail "status: $($result.Text)" }
  if (($status.affected_pages -join ',') -ne 'docs/repowiki/knowledge/存储.md') { Fail "affected: $($result.Text)" }
  Write-Host 'PASS: a change makes exactly the covering page stale (exit 10)'
  $quiet = Invoke-Devsys @('--quiet', 'knowledge', 'status')
  if ($quiet.Text -notmatch 'affected: docs/repowiki/knowledge/存储.md') { Fail "--quiet dropped the affected list:`n$($quiet.Text)" }
  Write-Host 'PASS: the affected pages stay visible under --quiet'
  Rename-Item (Join-Path $project 'docs\repowiki') 'repowiki.away'
  $result = Invoke-Devsys @('knowledge', 'status')
  if ($result.Code -ne 11) { Fail "missing page layer: exit=$($result.Code) want 11" }
  Rename-Item (Join-Path $project 'docs\repowiki.away') 'repowiki'
  Write-Host 'PASS: no page layer answers missing (exit 11) without failing'

  Write-Host '== generator contract and refresh =='
  Add-Content -Path (Join-Path $project '.devsys\config.yaml') -Value ("knowledge_generator: `"$generatorCmd`"") -Encoding UTF8
  Commit-All 'configured'
  $result = Invoke-Devsys @('--json', 'knowledge', 'refresh', '--affected')
  if ($result.Code -ne 0) { Fail "refresh: exit=$($result.Code)`n$($result.Text)" }
  $scope = Get-Content -Raw -Encoding UTF8 (Join-Path $workspace 'generator.scope') | ConvertFrom-Json
  if ($scope.format_version -ne 1) { Fail "scope format_version: $($result.Text)" }
  if ($scope.mode -ne 'affected') { Fail "scope mode: $($result.Text)" }
  if (($scope.pages -join ',') -ne 'docs/repowiki/knowledge/存储.md') { Fail "scope pages: $($scope.pages -join ',')" }
  $argvLine = Get-Content -Raw -Encoding UTF8 (Join-Path $workspace 'generator.argv')
  foreach ($flag in @('--project', '--scope-file', '--format-version')) {
    if ($argvLine -notmatch [regex]::Escape($flag)) { Fail "generator argv misses $flag : $argvLine" }
  }
  Write-Host 'PASS: the generator is handed the scope through the contract argv'

  Write-Host '== human protection =='
  New-Page 'docs/repowiki/knowledge/锁定.md' $head1 @('internal/store/**')
  $lockedPath = Join-Path $project 'docs\repowiki\knowledge\锁定.md'
  $locked = (Get-Content -Raw -Encoding UTF8 $lockedPath) -replace "---`n`n# 存储层", "protected: true`n---`n`n# 存储层"
  Set-Content -Path $lockedPath -Value $locked -Encoding UTF8
  New-Page 'docs/repowiki/knowledge/手改.md' $head1 @('internal/store/**')
  Commit-All 'protection'
  $env:STUB_GENERATOR_WRITE = 'docs/repowiki/knowledge/手改.md'
  $recorded = Invoke-Devsys @('knowledge', 'refresh', '--affected')
  if ($recorded.Code -ne 0) { Fail "recording refresh: exit=$($recorded.Code)`n$($recorded.Text)" }
  $env:STUB_GENERATOR_WRITE = ''
  Add-Content -Path (Join-Path $project 'docs\repowiki\knowledge\手改.md') -Value '人工补充。' -Encoding UTF8
  Set-Content -Path (Join-Path $project 'internal\store\b.go') -Value 'package store // new' -Encoding UTF8
  $result = Invoke-Devsys @('--json', 'knowledge', 'refresh', '--affected')
  if ($result.Code -ne 0) { Fail "protected refresh: exit=$($result.Code)`n$($result.Text)" }
  $view = $result.Text | ConvertFrom-Json
  $skipped = @{}
  foreach ($skip in $view.skipped) { $skipped[$skip.path] = $skip.reason }
  if ($skipped['docs/repowiki/knowledge/锁定.md'] -notmatch 'protected') { Fail "locked page not reported: $($result.Text)" }
  if ($skipped['docs/repowiki/knowledge/手改.md'] -notmatch '手工修改') { Fail "hand edit not reported: $($result.Text)" }
  if (($view.pages -join ',') -ne 'docs/repowiki/knowledge/存储.md') { Fail "scope kept a protected page: $($result.Text)" }
  Write-Host 'PASS: locked and hand-edited pages stay out of the generator scope and are reported'
  $result = Invoke-Devsys @('--json', 'knowledge', 'refresh', '--affected', '--force')
  if ($result.Code -ne 0) { Fail "--force refresh: exit=$($result.Code)`n$($result.Text)" }
  $view = $result.Text | ConvertFrom-Json
  if (-not $view.ran) { Fail "--force did not run: $($result.Text)" }
  if ($view.pages.Count -ne 3) { Fail "--force scope: $($result.Text)" }
  $overridden = @($view.skipped | Where-Object { $_.overridden })
  if ($overridden.Count -ne 2) { Fail "--force did not report its overrides: $($result.Text)" }
  Write-Host 'PASS: --force regenerates them and still reports the override'

  Write-Host '== resumable refreshes and the mutex =='
  Remove-Item -Force (Join-Path $project '.devsys\knowledge\state.json'), (Join-Path $project '.devsys\knowledge\run.json') -ErrorAction SilentlyContinue
  Set-Content -Path (Join-Path $project 'internal\store\a.go') -Value 'package store // changed again' -Encoding UTF8
  $env:STUB_GENERATOR_FAIL = '1'
  Invoke-Devsys @('knowledge', 'refresh', '--affected') | Out-Null
  $env:STUB_GENERATOR_FAIL = ''
  $checkpoint = Get-Content -Raw -Encoding UTF8 (Join-Path $project '.devsys\knowledge\run.json') | ConvertFrom-Json
  if ($checkpoint.phase -ne 'failed') { Fail "checkpoint after failure: $($checkpoint | ConvertTo-Json -Compress)" }
  if ($checkpoint.pages.Count -lt 1) { Fail "checkpoint lists no pages" }
  $result = Invoke-Devsys @('--json', 'knowledge', 'refresh', '--affected')
  if ($result.Code -ne 0) { Fail "resume: exit=$($result.Code)`n$($result.Text)" }
  $view = $result.Text | ConvertFrom-Json
  if (-not $view.resumed -or -not $view.ran) { Fail "resume did not continue the run: $($result.Text)" }
  if (($view.pages -join ',') -ne 'docs/repowiki/knowledge/存储.md,docs/repowiki/knowledge/手改.md') { Fail "resume scope: $($view.pages -join ',')" }
  if (Test-Path (Join-Path $project '.devsys\knowledge\run.json')) { Fail 'a clean run must clear its checkpoint' }
  Write-Host 'PASS: a failed run leaves a checkpoint and the next run resumes it, then clears it'

  Set-Content -Path (Join-Path $project 'internal\store\a.go') -Value 'package store // again' -Encoding UTF8
  $env:STUB_GENERATOR_SLEEP = '4'
  $holder = Start-Process -PassThru -NoNewWindow -FilePath $script:D -ArgumentList @('knowledge', 'refresh', '--affected') -WorkingDirectory $project
  Start-Sleep -Seconds 2
  $result = Invoke-Devsys @('knowledge', 'refresh', '--affected')
  $env:STUB_GENERATOR_SLEEP = ''
  if ($result.Code -ne 3) {
    if (-not $holder.HasExited) { $holder.Kill() }
    Fail "concurrent refresh: exit=$($result.Code) want 3`n$($result.Text)"
  }
  if ($result.Text -notmatch 'another refresh is already running') { Fail "refusal misses its reason:`n$($result.Text)" }
  if (-not $holder.HasExited) { $holder.WaitForExit(30000) | Out-Null }
  Write-Host 'PASS: a second refresh is refused while another holds the mutex'

  Write-Host '== layered context assembly =='
  Invoke-Devsys @('workitem', 'create', '--title', '整理存储层的写入路径', '--actor', 'smoke', '--reason', 'fixture') | Out-Null
  $items = (Invoke-Devsys @('--json', 'workitem', 'list')).Text | ConvertFrom-Json
  $item = $items.items[0].id
  $first = Invoke-Devsys @('--json', 'context', 'get', '--task', $item)
  if ($first.Code -ne 0) { Fail "context get --task: exit=$($first.Code)`n$($first.Text)" }
  $second = Invoke-Devsys @('--json', 'context', 'get', '--task', $item)
  if ($first.Text -ne $second.Text) { Fail 'two assemblies of one task differ' }
  $assembly = $first.Text | ConvertFrom-Json
  $matched = @($assembly.task.knowledge.pages | Where-Object { $_.match -eq 'trigger' -and $_.reason -eq '存储层' })
  if ($matched.Count -lt 1) { Fail "no trigger match in the assembly: $($first.Text)" }
  Write-Host 'PASS: the task context carries the pages it touches, twice identically'

  Write-Host '== degradation without a generator =='
  $configPath = Join-Path $project '.devsys\config.yaml'
  (Get-Content -Encoding UTF8 $configPath) | Where-Object { $_ -notmatch '^knowledge_generator:' } | Set-Content $configPath -Encoding UTF8
  $result = Invoke-Devsys @('--json', 'knowledge', 'refresh', '--affected')
  if ($result.Code -ne 11) { Fail "refresh without a generator: exit=$($result.Code) want 11`n$($result.Text)" }
  $view = $result.Text | ConvertFrom-Json
  if (-not $view.missing -or $view.ran) { Fail "degradation view: $($result.Text)" }
  if ($view.reason -notmatch 'knowledge_generator') { Fail "degradation reason: $($result.Text)" }
  $status = Invoke-Devsys @('--json', 'knowledge', 'status')
  if ($status.Code -notin @(0, 10)) { Fail "status without a generator: exit=$($status.Code)`n$($status.Text)" }
  Write-Host "PASS: without a generator the layer degrades instead of failing (refresh exit 11, status exit $($status.Code))"

  $success = $true
  Write-Host 'PASS: M5 knowledge layer (page contract, index, freshness, protection, resume, context, generator contract).'
} finally {
  Cleanup
}
