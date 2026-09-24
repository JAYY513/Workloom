# M4: MCP surface (profiles, health, records, session, error taxonomy),
# CLI/MCP write parity, exchange protocol (exit codes, jsonl), wire
# idempotency. Repeatable. Requires Go, Git and Python (Windows PowerShell).
#
# usage: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke-m4.ps1 [-Keep]
param([switch]$Keep)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$workspace = Join-Path ([System.IO.Path]::GetTempPath()) ("smoke-m4-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
$project = Join-Path $workspace 'demo-project'
$success = $false
$oldConfig = $env:DEVSYS_CONFIG_DIR
$oldLocation = Get-Location

$OutputEncoding = [System.Text.UTF8Encoding]::new()
function Assert-Exit([string]$What, [int]$Code) {
  if ($LASTEXITCODE -ne $Code) { throw "$What exited $LASTEXITCODE, want $Code" }
}

# Expected failures write to stderr; under ErrorActionPreference=Stop that
# would terminate the script, so the preference is relaxed for the call.
function Invoke-Expect([string]$What, [int]$Code, [scriptblock]$Run) {
  $old = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  try { & $Run 2>$null | Out-Null } finally { $ErrorActionPreference = $old }
  if ($LASTEXITCODE -ne $Code) { throw "$What exited $LASTEXITCODE, want $Code" }
}

# Python helpers live in temp files (python -c and stdin cannot both carry
# data), and their scripts stay ASCII so console encoding cannot break them.
function New-Py([string]$Name, [string]$Script) {
  $path = Join-Path $script:pyDir "$Name.py"
  Set-Content -Path $path -Value $Script -Encoding ASCII
  return $path
}

function Run-Py([string]$Path, [string[]]$Data) {
  if ($null -eq $Data) { $Data = @() }
  $output = $Data | python $Path
  if ($LASTEXITCODE -ne 0) { throw "python check failed: $Path" }
  return $output
}

try {
  New-Item -ItemType Directory -Path $project -Force | Out-Null
  $script:pyDir = Join-Path $workspace 'py'
  New-Item -ItemType Directory -Path $script:pyDir -Force | Out-Null
  $env:DEVSYS_CONFIG_DIR = Join-Path $workspace 'config'
  Push-Location $repoRoot
  & go build -o (Join-Path $workspace 'workloom.exe') ./cmd/workloom; Assert-Exit 'go build devsys' 0
  & go build -o (Join-Path $workspace 'm4helper.exe') ./scripts/m4helper; Assert-Exit 'go build m4helper' 0
  Pop-Location
  $devsys = Join-Path $workspace 'workloom.exe'
$workloom = $devsys
  $helper = Join-Path $workspace 'm4helper.exe'
  & git init -q $project; Assert-Exit 'git init' 0
  Set-Location $project
  & $workloom init | Out-Null; Assert-Exit 'workloom init' 0

  Write-Host '== MCP surface =='
  & $helper $devsys $project
  Assert-Exit 'm4helper' 0

  Write-Host '== write parity =='
  $idPy = New-Py 'id' "import json,sys`nprint(json.loads(sys.stdin.readline())['id'])"
  $verPy = New-Py 'ver' "import json,sys`nprint(json.load(sys.stdin)['version'])"
  $decisionId = (Run-Py $idPy (& $workloom --jsonl decision list)).Trim()
  if (-not $decisionId) { throw 'no decision found over jsonl' }
  $cliVersion = (Run-Py $verPy (& $workloom --json decision get $decisionId)).Trim()
  if (-not $cliVersion) { throw 'CLI could not read the MCP-created decision' }
  Write-Host "PASS: CLI reads the MCP-created decision $decisionId (version $cliVersion)"

  $sessionPath = Join-Path $workspace 'session.json'
  $sessionProc = Start-Process -FilePath $workloom -ArgumentList @('--json', 'session', 'start', '--harness', 'smoke', '--agent', 'smoke', '--intent', 'acceptance') -WorkingDirectory $project -RedirectStandardOutput $sessionPath -NoNewWindow -Wait -PassThru
  if ($sessionProc.ExitCode -ne 0) { throw "session start exited $($sessionProc.ExitCode)" }
  $actionPy = New-Py 'action' "import json,sys`nv=json.load(open(sys.argv[1],encoding='utf-8'))`nassert v['ok'] is True`nassert v['recommended_next_action']['type']`nprint(v['recommended_next_action']['type'])"
  $action = & python $actionPy $sessionPath
  if ($LASTEXITCODE -ne 0) { throw 'session JSON validation failed' }
  Write-Host "PASS: session start recommends $($action.Trim())"

  Write-Host '== exchange =='
  & $workloom workitem list | Out-Null; Assert-Exit 'workitem list' 0
  Write-Host 'PASS: exit 0: workitem list'
  Invoke-Expect 'bogus command' 2 { & $devsys bogus-command }
  Write-Host 'PASS: exit 2: bogus command'
  Invoke-Expect 'missing work item' 3 { & $workloom workitem get NOPE-1 }
  Write-Host 'PASS: exit 3: missing work item'
  & $workloom workitem create --title 'smoke task' --actor smoke --reason smoke | Out-Null
  Assert-Exit 'workitem create' 0
  $jsonlPy = New-Py 'jsonl' "import json,sys`nrows=[json.loads(l) for l in sys.stdin if l.strip()]`nassert len(rows)==1, rows`nprint('PASS: jsonl streamed', rows[0]['id'])"
  Run-Py $jsonlPy (& $workloom --jsonl workitem list) | Write-Host

  Write-Host '== wire =='
  $agents = @'
# 手写说明

这段内容必须原样保留。

<!-- repowiki:begin | 由 repowiki 管理 -->
## repowiki

- docs/repowiki/

<!-- repowiki:end -->
'@
  Set-Content -Path 'AGENTS.md' -Value $agents -Encoding UTF8
  & $devsys wire | Out-Null; Assert-Exit 'wire' 0
  $first = (Get-FileHash AGENTS.md -Algorithm MD5).Hash
  & $devsys wire | Out-Null
  & $devsys wire | Out-Null
  $second = (Get-FileHash AGENTS.md -Algorithm MD5).Hash
  if ($first -ne $second) { throw 'wire is not idempotent' }
  $content = Get-Content AGENTS.md -Raw
  if ($content -notmatch '这段内容必须原样保留。') { throw 'wire dropped hand-written content' }
  if ($content -notmatch '<!-- repowiki:begin') { throw 'wire dropped the repowiki block' }
  Write-Host 'PASS: wire is idempotent and preserves hand-written content'

  Write-Host '== knowledge =='
  $knowledgePath = Join-Path $workspace 'knowledge.json'
  $knowledgeProc = Start-Process -FilePath $workloom -ArgumentList @('--json', 'knowledge', 'status') -WorkingDirectory $project -RedirectStandardOutput $knowledgePath -NoNewWindow -Wait -PassThru
  if ($knowledgeProc.ExitCode -ne 11) { throw "knowledge status exited $($knowledgeProc.ExitCode), want 11" }
  $knowledgePy = New-Py 'knowledge' "import json,sys`nv=json.load(open(sys.argv[1],encoding='utf-8'))`nassert v['ok'] is True, v`nassert v['status']=='missing', v`nprint('PASS: knowledge status reports missing pages honestly')"
  & python $knowledgePy $knowledgePath | Write-Host
  if ($LASTEXITCODE -ne 0) { throw 'knowledge JSON validation failed' }

  Write-Host 'PASS: M4 MCP surface / write parity / session / exchange / wire / knowledge.'
  $success = $true
} finally {
  Set-Location $oldLocation
  $env:DEVSYS_CONFIG_DIR = $oldConfig
  if ($success -and $Keep) {
    Write-Host "KEPT_PROJECT=$project"
  } else {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $workspace
  }
}
