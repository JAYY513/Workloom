# build-release.ps1: matrix-build devsys release binaries + checksums.txt.
# Usage: powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build-release.ps1 [-Version <v>] [-Out dist]
param(
  [string]$Version = "",
  [string]$Out = "dist"
)
$ErrorActionPreference = "Stop"
$repoRoot = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
if ([string]::IsNullOrWhiteSpace($Version)) {
  $Version = (git -C $repoRoot describe --tags --always --dirty).Trim()
}
$commit = (git -C $repoRoot rev-parse --short HEAD).Trim()
$ldflags = "-s -w -X workloom/internal/version.Version=$Version -X workloom/internal/version.Commit=$commit"
New-Item -ItemType Directory -Force -Path (Join-Path $repoRoot $Out) | Out-Null
$matrix = @(
  @("windows", "amd64"), @("windows", "arm64"),
  @("linux", "amd64"), @("linux", "arm64"),
  @("darwin", "amd64"), @("darwin", "arm64")
)
foreach ($pair in $matrix) {
  $name = "devsys-$($pair[0])-$($pair[1])"
  if ($pair[0] -eq "windows") { $name += ".exe" }
  $env:GOOS = $pair[0]; $env:GOARCH = $pair[1]; $env:GOFLAGS = "-mod=vendor"
  & go build -ldflags $ldflags -o (Join-Path $repoRoot "$Out/$name") ./cmd/devsys
  if ($LASTEXITCODE -ne 0) { throw "go build failed for $($pair[0])/$($pair[1])" }
  Write-Output "built $Out/$name"
}
Remove-Item Env:\GOOS, Env:\GOARCH, Env:\GOFLAGS -ErrorAction SilentlyContinue
$lines = foreach ($f in Get-ChildItem (Join-Path $repoRoot "$Out/devsys-*") -File | Where-Object { $_.Name -ne "checksums.txt" }) {
  $h = (Get-FileHash -Algorithm SHA256 $f.FullName).Hash.ToLower()
  "$h  $($f.Name)"
}
$lines | Set-Content -Path (Join-Path $repoRoot "$Out/checksums.txt") -Encoding Ascii
Write-Output "wrote $Out/checksums.txt ($($lines.Count) files)"
