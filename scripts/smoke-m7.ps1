# M7: the read-only view — aggregation on a read-only copy, the offline static
# site, the local read-only service, and the freshness hints. No provider, no
# network: scratch project only.
# Repeatable. Requires Go, Git and Python (Windows PowerShell).
#
# usage: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/smoke-m7.ps1 [-Keep]
param([switch]$Keep)
$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$workspace = Join-Path ([System.IO.Path]::GetTempPath()) ("smoke-m7-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
$project = Join-Path $workspace 'demo-project'
$success = $false
$serves = @()
$oldConfig = $env:DEVSYS_CONFIG_DIR
$oldPath = $env:PATH
$oldLocation = Get-Location

function Assert-Exit([string]$What, [int]$Code) {
  if ($LASTEXITCODE -ne $Code) { throw "$What exited $LASTEXITCODE, want $Code" }
}

function New-Py([string]$Name, [string]$Script) {
  $path = Join-Path $workspace ("$Name.py")
  Set-Content -Path $path -Value $Script -Encoding UTF8
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

function Assert-Clean([string]$What) {
  $dirty = (& git status --short) -join "`n"
  if ($dirty) { throw "$What left the tree dirty: $dirty" }
}

function Wait-Banner([string]$File) {
  for ($i = 0; $i -lt 100; $i++) {
    if (Test-Path $File) {
      $m = Select-String -Path $File -Pattern 'serving http://([^/]+)/' | Select-Object -First 1
      if ($m) { return $m.Matches[0].Groups[1].Value }
    }
    Start-Sleep -Milliseconds 100
  }
  throw "no serve banner in $File"
}

function Stop-Serve([System.Diagnostics.Process]$Proc) {
  try { Stop-Process -Id $Proc.Id -Force -ErrorAction SilentlyContinue } catch {}
  try { $Proc.WaitForExit(5000) | Out-Null } catch {}
}

try {
  New-Item -ItemType Directory -Path $project -Force | Out-Null
  $env:DEVSYS_CONFIG_DIR = Join-Path $workspace 'config'
  Push-Location $repoRoot
  & go build -ldflags "-s -w" -o (Join-Path $workspace 'devsys.exe') ./cmd/devsys; Assert-Exit 'go build devsys' 0
  Pop-Location
  $devsys = Join-Path $workspace 'devsys.exe'
  $env:PATH = "$workspace;$env:PATH"

  & git init -q $project; Assert-Exit 'git init' 0
  Set-Location $project
  & $devsys init | Out-Null; Assert-Exit 'devsys init' 0
  $itemId = ((& $devsys workitem create --title '看板任务' --actor me --reason smoke) | Select-Object -First 1).Split("`t")[0]

  function New-Page($Rel, $Commit) {
    $path = Join-Path $project $Rel
    New-Item -ItemType Directory -Path (Split-Path -Parent $path) -Force | Out-Null
    $lines = @('---', 'status: stable', 'type: module', 'triggers:', '  - storage', 'description: store page', "source_commit: $Commit", 'sources:', '  - internal/store/**', '---', '', '# store', '', 'body.')
    Set-Content -Path $path -Value $lines -Encoding UTF8
  }

  New-Item -ItemType Directory -Path (Join-Path $project 'internal/store') -Force | Out-Null
  Set-Content -Path (Join-Path $project 'internal/store/a.go') -Value 'package store' -Encoding ASCII
  & git add -A; Assert-Exit 'git add' 0
  & git -c user.email=devsys@test -c user.name=devsys commit -q -m fixture; Assert-Exit 'git commit' 0
  New-Page 'docs/repowiki/knowledge/store.md' (& git rev-parse HEAD)
  & git add -A; Assert-Exit 'git add' 0
  & git -c user.email=devsys@test -c user.name=devsys commit -q -m pages; Assert-Exit 'git commit' 0

  Write-Host '== view is read-only and complete (M7.1) =='
  Assert-Clean 'fixture'
  $view = (& $devsys workspace view) -join "`n"; Assert-Exit 'workspace view' 0
  foreach ($w in @('progress:', 'readiness:', 'knowledge: fresh', 'sources:')) {
    if (-not $view.Contains($w)) { throw "view misses $w" }
  }
  $modelPy = New-Py 'model' @'
import json, subprocess, sys
m = json.loads(subprocess.run(["devsys.exe", "--json", "workspace", "view"], capture_output=True, text=True).stdout)
assert m["ok"] is True, m
assert m["schema_version"] == 1, m["schema_version"]
assert m["trust"]["state"] == "ok", m["trust"]
assert len(m["progress"]["items"]) == 1, m["progress"]
assert m["knowledge"]["status"] == "fresh", m["knowledge"]
assert ".devsys/project.yaml" in m["sources"], m["sources"]
print("model: items=1 knowledge=fresh trust=ok")
'@
  Invoke-Py $modelPy @() | Write-Host
  Assert-Clean 'workspace view'
  # workitem create took the lock (a write); view itself must change no managed file — same sha check as smoke-m7.sh.
  Write-Host 'PASS: the view names its facts, sources and baseline, and writes nothing'
  $empty = Join-Path $workspace 'empty'
  New-Item -ItemType Directory -Path $empty -Force | Out-Null
  & git init -q $empty; Assert-Exit 'git init empty' 0
  Push-Location $empty
  Invoke-Expect 'view outside a project' 3 { & $devsys workspace view }
  Pop-Location
  Write-Host 'PASS: view outside a project is a precondition failure (exit 3)'

  Write-Host '== static site is offline and complete (M7.2) =='
  $site = Join-Path $workspace 'site'
  $buildOut = (& $devsys workspace build --static --out $site) -join "`n"; Assert-Exit 'workspace build' 0
  if (-not $buildOut.Contains('6 pages')) { throw "build did not report 6 pages: $buildOut" }
  foreach ($name in @('index.html', 'tasks.html', 'workflows.html', 'runs.html', 'records.html', 'knowledge.html', 'assets/style.css', 'data/model.json')) {
    if (-not (Test-Path (Join-Path $site $name))) { throw "missing $name" }
  }
  Write-Host 'PASS: all 8 site files land below --out'
  $hits = Get-ChildItem $site -Recurse -File | Select-String -Pattern 'https?://|@import|<script'
  if ($hits) { throw "the site references the network: $($hits -join '; ')" }
  if (-not (Select-String -Path (Join-Path $site 'index.html') -Pattern '基线提交' -SimpleMatch)) { throw 'index.html lost the baseline footer' }
  if (-not (Select-String -Path (Join-Path $site 'index.html') -Pattern 'data/model.json' -SimpleMatch)) { throw 'index.html lost the model link' }
  Write-Host 'PASS: the site is offline (no http refs, no @import, no script) and names the baseline'
  Assert-Clean 'explicit --out build'
  Write-Host 'PASS: an explicit --out leaves the project tree clean'
  & $devsys workspace build --static | Out-Null; Assert-Exit 'default build' 0
  foreach ($name in @('index.html', 'tasks.html', 'workflows.html', 'runs.html', 'records.html', 'knowledge.html', 'assets/style.css', 'data/model.json')) {
    if (-not (Test-Path (Join-Path $project ".devsys/dist/site/$name"))) { throw "default out misses $name" }
  }
  Write-Host 'PASS: the default out is .devsys/dist/site/ (git-ignored)'
  Remove-Item -Recurse -Force (Join-Path $project '.devsys/dist')

  Write-Host '== serve is read-only and loopback-only (M7.3) =='
  $banner = Join-Path $workspace 'serve-banner.txt'
  $serve = Start-Process -FilePath $devsys -ArgumentList @('workspace', 'serve', '--port', '0') -WorkingDirectory $project -RedirectStandardOutput $banner -RedirectStandardError (Join-Path $workspace 'serve-err.txt') -NoNewWindow -PassThru
  $serves += $serve
  $addr = Wait-Banner $banner
  if (-not $addr.StartsWith('127.0.0.1:')) { throw "serve bound $addr, want 127.0.0.1" }
  $servePy = New-Py 'serve' @'
import json, sys, urllib.request, urllib.error
addr = sys.argv[1]
def get(path, ctype):
    req = urllib.request.Request("http://%s%s" % (addr, path))
    with urllib.request.urlopen(req, timeout=10) as r:
        assert r.status == 200, (path, r.status)
        assert r.headers["Content-Type"].startswith(ctype), (path, r.headers["Content-Type"])
        assert r.headers["Cache-Control"] == "no-store", (path, r.headers)
        return r.read().decode("utf-8")
for page in ["/", "/tasks.html", "/workflows.html", "/runs.html", "/records.html", "/knowledge.html"]:
    body = get(page, "text/html")
    assert "数据来源" in body and "基线提交" in body, page
get("/assets/style.css", "text/css")
model = json.loads(get("/data/model.json", "application/json"))
assert model["schema_version"] == 1 and len(model["sources"]) > 0 and model["baseline"]["available"], model
api = json.loads(get("/api/view", "application/json"))
assert api["ok"] is True and api["generated_at"] and len(api["sources"]) > 0, api
health = json.loads(get("/healthz", "application/json"))
assert health == {"ok": True}, health
for method in ["POST", "PUT", "PATCH", "DELETE"]:
    for path in ["/", "/api/view", "/data/model.json"]:
        req = urllib.request.Request("http://%s%s" % (addr, path), method=method)
        try:
            urllib.request.urlopen(req, timeout=10)
            raise SystemExit("FAIL: %s %s was not refused" % (method, path))
        except urllib.error.HTTPError as e:
            assert e.code == 405, (method, path, e.code)
            assert e.headers["Allow"] == "GET, HEAD", (method, path, e.headers["Allow"])
            assert e.read().decode() == "read-only: %s not allowed\n" % method, (method, path)
print("routes: 6 pages + css + model + api + healthz; writes refused with Allow: GET, HEAD")
'@
  Invoke-Py $servePy @($addr) | Write-Host
  Write-Host 'PASS: every read route answers; every write method is a 405'
  Stop-Serve $serve
  Assert-Clean 'workspace serve'
  Write-Host 'PASS: the server wrote nothing while running'
  Invoke-Expect 'non-loopback bind' 2 { & $devsys workspace serve --host 0.0.0.0 --port 0 }
  Write-Host 'PASS: a non-loopback bind without --allow-remote is a usage error (exit 2)'

  Write-Host '== freshness hints name the fix (M7.4) =='
  Set-Content -Path (Join-Path $project 'internal/store/a.go') -Value 'package store // edited' -Encoding ASCII
  $stale = (& $devsys workspace view) -join "`n"; Assert-Exit 'stale view' 0
  foreach ($w in @('knowledge: stale', 'affected: docs/repowiki/knowledge/store.md', 'hint: run `devsys knowledge refresh` to regenerate affected pages')) {
    if (-not $stale.Contains($w)) { throw "stale view misses $w" }
  }
  Write-Host 'PASS: a covered change marks the page stale, names it, and points at refresh (exit 0)'
  $old = $ErrorActionPreference
  $ErrorActionPreference = 'Continue'
  try { & $devsys knowledge status | Out-Null } finally { $ErrorActionPreference = $old }
  if ($LASTEXITCODE -ne 10) { throw "knowledge status exited $LASTEXITCODE, want 10" }
  Write-Host 'PASS: the same state through knowledge status exits 10 (one verdict, two faces)'
  $staleSite = Join-Path $workspace 'stale-site'
  & $devsys workspace build --static --out $staleSite | Out-Null; Assert-Exit 'stale build' 0
  if (-not (Select-String -Path (Join-Path $staleSite 'index.html') -Pattern '知识已过期' -SimpleMatch)) { throw 'the stale site lost its banner' }
  if (-not (Select-String -Path (Join-Path $staleSite 'knowledge.html') -Pattern 'devsys knowledge refresh' -SimpleMatch)) { throw 'the stale knowledge page lost its hint' }
  Write-Host 'PASS: the stale static site carries the banner and the page hint'
  $staleBanner = Join-Path $workspace 'serve-stale.txt'
  $serve2 = Start-Process -FilePath $devsys -ArgumentList @('workspace', 'serve', '--port', '0') -WorkingDirectory $project -RedirectStandardOutput $staleBanner -RedirectStandardError (Join-Path $workspace 'serve-stale-err.txt') -NoNewWindow -PassThru
  $serves += $serve2
  $staleAddr = Wait-Banner $staleBanner
  $staleServePy = New-Py 'serve-stale' @'
import sys, urllib.request
addr = sys.argv[1]
body = urllib.request.urlopen("http://%s/" % addr, timeout=10).read().decode("utf-8")
assert "知识已过期" in body, body[:500]
print("serve renders the stale banner from the same state")
'@
  Invoke-Py $staleServePy @($staleAddr) | Write-Host
  Stop-Serve $serve2
  & git checkout -q -- internal/store/a.go; Assert-Exit 'git checkout source' 0
  Remove-Item -Recurse -Force (Join-Path $project 'docs/repowiki')
  $missing = (& $devsys workspace view) -join "`n"; Assert-Exit 'missing view' 0
  foreach ($w in @('knowledge: missing', 'hint: configure `knowledge_generator`, then run `devsys knowledge refresh --full`')) {
    if (-not $missing.Contains($w)) { throw "missing view misses $w" }
  }
  Write-Host 'PASS: a missing page layer names the supported degradation and --full'
  & git checkout -q -- docs/repowiki; Assert-Exit 'git checkout pages' 0
  New-Item -ItemType Directory -Path (Join-Path $project '.devsys/local/txn/txn-smoke') -Force | Out-Null
  $pending = (& $devsys workspace view) -join "`n"; Assert-Exit 'pending view' 0
  foreach ($w in @('pending: txn-smoke', 'hint: run `devsys doctor` to inspect before `devsys recover`')) {
    if (-not $pending.Contains($w)) { throw "pending view misses $w" }
  }
  if ($pending.Contains('hint: run `devsys knowledge refresh`')) { throw 'the pending view suggests refresh against untrusted state' }
  Write-Host 'PASS: pending transactions point at doctor, never at refresh'
  Remove-Item -Recurse -Force (Join-Path $project '.devsys/local/txn/txn-smoke')
  Remove-Item -Recurse -Force (Join-Path $project '.devsys/dist') -ErrorAction SilentlyContinue

  Write-Host 'PASS: M7 read-only view (aggregation, static site, local service, freshness hints).'
  $success = $true
} finally {
  foreach ($s in $serves) { try { Stop-Serve $s } catch {} }
  Set-Location $oldLocation
  $env:DEVSYS_CONFIG_DIR = $oldConfig
  $env:PATH = $oldPath
  if ($success -and $Keep) {
    Write-Host "KEPT_PROJECT=$project"
  } else {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue $workspace
  }
}
