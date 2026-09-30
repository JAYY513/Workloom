# Native Windows entry. Does not require Node on PATH.
# The npm bin field stays bin/workloom.js so MCP registration (node + wrapper)
# is unchanged. Git Bash without node cannot run that shim (#!/usr/bin/env node
# fails before any JavaScript); invoke this script instead, or set WORKLOOM_BIN
# to the platform workloom.exe.
$ErrorActionPreference = "Stop"

function Fail([string]$Message) {
  [Console]::Error.WriteLine($Message)
  exit 1
}

$override = $env:WORKLOOM_BIN
if ($override) {
  if (-not (Test-Path -LiteralPath $override)) {
    Fail ("WORKLOOM_BIN is set to '" + $override + "' but that file does not exist.`n" +
      "Unset it, or point it at the platform workloom.exe (npm optional package @kaki317/workloom-win32-x64).")
  }
  $bin = (Resolve-Path -LiteralPath $override).Path
} else {
  $here = Split-Path -Parent $MyInvocation.MyCommand.Path
  $names = @(
    "workloom-win32-x64",
    "workloom-win32-arm64"
  )
  $roots = @(
    (Join-Path $here "..\node_modules\@kaki317"),
    (Join-Path $here "..\.."),
    (Join-Path $here "..\..\..")
  )
  $bin = $null
  foreach ($root in $roots) {
    foreach ($name in $names) {
      $candidate = Join-Path (Join-Path $root $name) "workloom.exe"
      if (Test-Path -LiteralPath $candidate) {
        $bin = (Resolve-Path -LiteralPath $candidate).Path
        break
      }
    }
    if ($bin) { break }
  }
  if (-not $bin) {
    Fail @"
workloom: no platform binary found next to this script, and this entry does not need Node.
Git Bash reports 'node: No such file or directory' when the npm shim (#!/usr/bin/env node) runs and node is not on that PATH. The JavaScript wrapper never starts, so it cannot diagnose this.

Set WORKLOOM_BIN to the platform executable and re-run, for example from PowerShell:
  `$env:WORKLOOM_BIN = Join-Path (npm root -g) '@kaki317\workloom-win32-x64\workloom.exe'
or point it at a release / go-install workloom.exe.

Git Bash and WSL are different environments. If bash.exe resolves to WSL, Windows node/npm are not on that PATH; use Git Bash, or set WORKLOOM_BIN and run this script from PowerShell.
This package does not download a binary. The npm bin field stays bin/workloom.js so MCP registration is unchanged.
"@
  }
}

& $bin @args
if ($null -ne $LASTEXITCODE) { exit $LASTEXITCODE }
exit 0
