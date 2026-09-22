# install.ps1: download a workloom release binary, verify sha256, install to %LOCALAPPDATA%/workloom.
# Also installs a devsys.exe alias copy (same bytes) for old scripts.
# Falls back to `git clone --branch <tag> + go build` when download fails and Go exists.
# Uses the system proxy via Invoke-WebRequest natively.
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/install.ps1 -Tag <tag> [-Repo JAYY513/Workloom] [-AddToPath]
param(
  [Parameter(Mandatory = $true)][string]$Tag,
  [string]$Repo = "JAYY513/Workloom",
  [switch]$AddToPath
)
$ErrorActionPreference = "Stop"
$dest = Join-Path $env:LOCALAPPDATA "workloom"
$arch = if ($env:PROCESSOR_ARCHITECTURE -match "ARM64") { "arm64" } else { "amd64" }
$asset = "workloom-windows-$arch.exe"
$base = "https://github.com/$Repo/releases/download/$Tag"
$tmp = Join-Path ([IO.Path]::GetTempPath()) ("workloom-install-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
# Public releases download anonymously. When `gh` is installed and logged in,
# try it first: that covers private forks and avoids anonymous rate limits.
# Fall back to the anonymous download otherwise.
function Get-ReleaseAsset([string]$Name, [string]$OutFile) {
  if (Get-Command gh -ErrorAction SilentlyContinue) {
    # gh writes diagnostics to stderr even for harmless states (not logged
    # in). Under $ErrorActionPreference="Stop", native stderr becomes an
    # ErrorRecord and would throw past the $LASTEXITCODE check, so redirect
    # stderr explicitly and decide on the exit code (#342).
    & gh auth status 2>$null | Out-Null
    if ($LASTEXITCODE -eq 0) {
      & gh release download $Tag --repo $Repo --pattern $Name --output $OutFile --clobber 2>$null | Out-Null
      if ($LASTEXITCODE -eq 0) { return }
    }
  }
  Invoke-WebRequest -Uri "$base/$Name" -OutFile $OutFile
}
try {
  $ok = $true
  try {
    Get-ReleaseAsset $asset (Join-Path $tmp $asset)
    Get-ReleaseAsset "checksums.txt" (Join-Path $tmp "checksums.txt")
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
    & git -c advice.detachedHead=false clone -q --branch $Tag --depth 1 "https://github.com/$Repo.git" (Join-Path $tmp "src")
    if ($LASTEXITCODE -ne 0) { throw "git clone failed" }
    $src = Join-Path $tmp "src"
    Push-Location $src
    try {
      $env:GOFLAGS = "-mod=vendor"
      # The version ldflags match scripts/build-release.sh, so a fallback build
      # reports the tag it was built from instead of the 0.1.0-dev default.
      & go build -ldflags "-s -w -X github.com/JAYY513/Workloom/internal/version.Version=$Tag" -o (Join-Path $tmp $asset) ./cmd/workloom
      if ($LASTEXITCODE -ne 0) { throw "go build failed" }
    } finally {
      Pop-Location
      Remove-Item Env:\GOFLAGS -ErrorAction SilentlyContinue
    }
  }
  New-Item -ItemType Directory -Force -Path $dest | Out-Null
  Copy-Item -Force (Join-Path $tmp $asset) (Join-Path $dest "workloom.exe")
  Copy-Item -Force (Join-Path $tmp $asset) (Join-Path $dest "devsys.exe")
  & (Join-Path $dest "workloom.exe") --version
  $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
  if ($null -eq $userPath) { $userPath = "" }
  $inPath = ($userPath -split ';' | ForEach-Object { $_.Trim() }) -contains $dest
  if ($inPath) {
    Write-Output "already on user PATH: $dest"
  } elseif ($AddToPath) {
    if ($userPath -eq "") { $newPath = $dest } else { $newPath = "$userPath;$dest" }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")
    Write-Output "added $dest to user PATH (reopen terminal)"
  } else {
    Write-Output "add to PATH: $dest (rerun with -AddToPath to set automatically)"
  }
} finally {
  Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
