#!/usr/bin/env node
"use strict";

// Distribution shim. It execs the platform package that npm installed as an
// optionalDependency. It never downloads a binary.

const { spawn } = require("child_process");
const fs = require("fs");
const path = require("path");

const targets = {
  "darwin-arm64": "@kaki317/workloom-darwin-arm64",
  "darwin-x64": "@kaki317/workloom-darwin-x64",
  "linux-arm64": "@kaki317/workloom-linux-arm64",
  "linux-x64": "@kaki317/workloom-linux-x64",
  "win32-arm64": "@kaki317/workloom-win32-arm64",
  "win32-x64": "@kaki317/workloom-win32-x64",
};

function packageFor(platform, arch) {
  return targets[platform + "-" + arch] || "";
}

function binaryName(platform) {
  return platform === "win32" ? "workloom.exe" : "workloom";
}

function resolveBinary(platform, arch) {
  const name = packageFor(platform, arch);
  if (!name) {
    throw new Error(
      "unsupported platform " + platform + "-" + arch + "; this package does not download a binary",
    );
  }
  let pkgJson;
  try {
    pkgJson = require.resolve(name + "/package.json");
  } catch {
    throw new Error(
      "no workloom binary for " +
        platform +
        "-" +
        arch +
        ": platform package " +
        name +
        " is not installed. Either that platform is not published on npm yet (npm currently ships Windows x64), or npm skipped optional dependencies (--omit=optional). This package does not download a binary; for other platforms see INSTALL.md (release script / go install).",
    );
  }
  const bin = path.join(path.dirname(pkgJson), binaryName(platform));
  if (!fs.existsSync(bin)) {
    throw new Error(
      name + " is installed but " + binaryName(platform) + " is missing. This package does not download a binary.",
    );
  }
  return bin;
}

function main() {
  let bin;
  try {
    bin = resolveBinary(process.platform, process.arch);
  } catch (err) {
    console.error("workloom: " + err.message);
    process.exit(1);
  }
  const child = spawn(bin, process.argv.slice(2), { stdio: "inherit" });
  child.on("error", (err) => {
    console.error("workloom: failed to start " + bin + ": " + err.message);
    process.exit(1);
  });
  child.on("exit", (code, signal) => {
    if (signal) {
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code == null ? 1 : code);
  });
}

if (require.main === module) {
  main();
}

module.exports = { packageFor, binaryName, resolveBinary };
