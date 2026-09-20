# install.ps1: download a devsys release binary, verify sha256, install to %LOCALAPPDATA%/devsys.
# Falls back to `git clone --branch <tag> + go build` when download fails and Go exists.
# Uses the system proxy via Invoke-WebRequest natively.
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install.ps1 -Tag <tag> [-Repo JAYY513/Workloom] [-AddToPath]
param(
  [Parameter(Mandatory = $true)][string]$Tag,
  [string]$Repo = "JAYY513/Workloom",
  [switch]$AddToPath
)
$ErrorActionPreference = "Stop"
$dest = Join-Path $env:LOCALAPPDATA "devsys"
$arch = if ($env:PROCESSOR_ARCHITECTURE -match "ARM64") { "arm64" } else { "amd64" }
$asset = "devsys-windows-$arch.exe"
$base = "https://github.com/$Repo/releases/download/$Tag"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("devsys-install-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
  $ok = $true
  try {
    Invoke-WebRequest -Uri "$base/$asset" -OutFile (Join-Path $tmp $asset)
    Invoke-WebRequest -Uri "$base/checksums.txt" -OutFile (Join-Path $tmp "checksums.txt")
    # Tolerate both checksum dialects: `<hash>  <asset>` (GNU text mode) and
    # `<hash> *<asset>` (shasum / MSYS binary mode, as in the v0.1.0 release).
    $line = Select-String -Path (Join-Path $tmp "checksums.txt") -Pattern ("[ \t]\*?" + [regex]::Escape($asset) + "$") | Select-Object -First 1
    if (-not $line) { throw "checksum entry missing for $asset" }
    $want = ($line.Line -split '\s+')[0].ToLower()
    $got = (Get-FileHash -Algorithm SHA256 (Join-Path $tmp $asset)).Hash.ToLower()
    if ($want -ne $got) { throw "checksum mismatch for $asset" }
  } catch {
    Write-Warning "download/verify failed: $($_.Exception.Message)"
    $ok = $false
  }
  if (-not $ok) {
    if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw "download failed and no Go for fallback build" }
    if (-not (Get-Command git -ErrorAction SilentlyContinue)) { throw "download failed and no git for fallback build" }
    Write-Output "falling back to git clone + go build"
    & git clone -q --branch $Tag --depth 1 "https://github.com/$Repo.git" (Join-Path $tmp "src")
    if ($LASTEXITCODE -ne 0) { throw "git clone failed" }
    $src = Join-Path $tmp "src"
    Push-Location $src
    try {
      $env:GOFLAGS = "-mod=vendor"
      # The version ldflags match scripts/build-release.sh, so a fallback build
      # reports the tag it was built from instead of the 0.1.0-dev default.
      & go build -ldflags "-s -w -X workloom/internal/version.Version=$Tag" -o (Join-Path $tmp $asset) ./cmd/devsys
      if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    } finally {
      Pop-Location
      Remove-Item Env:\GOFLAGS -ErrorAction SilentlyContinue
    }
  }
  New-Item -ItemType Directory -Force -Path $dest | Out-Null
  Copy-Item -Force (Join-Path $tmp $asset) (Join-Path $dest "devsys.exe")
  & (Join-Path $dest "devsys.exe") --version
  $inPath = ($env:Path -split ';') -contains $dest
  if (-not $inPath) {
    if ($AddToPath) {
      [Environment]::SetEnvironmentVariable("Path", "$env:Path;$dest", "User")
      Write-Output "added $dest to user PATH (reopen terminal)"
    } else {
      Write-Output "add to PATH: $dest (rerun with -AddToPath to set automatically)"
    }
  }
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
