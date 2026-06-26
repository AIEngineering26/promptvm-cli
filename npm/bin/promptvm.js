#!/usr/bin/env node
"use strict";

// Thin Node shim for the `promptvm` npm package (Option B —
// optionalDependencies / esbuild model). It does NOT download anything: npm
// installs exactly one matching `@promptvm/cli-<platform>-<arch>` sub-package
// (gated by its `os`/`cpu` fields), and this shim locates that package's
// prebuilt Go binary and execs it, forwarding argv and the exit code.
//
// The Go binary is the single source of truth; this file is pure delivery.

const { execFileSync } = require("node:child_process");

// Map Node's process.platform/process.arch to the sub-package naming used in
// optionalDependencies. Keep in sync with npm/package.json and the release
// generator (scripts/build-npm-packages.mjs).
const PLATFORM_PACKAGES = {
  "darwin-arm64": "@promptvm/cli-darwin-arm64",
  "darwin-x64": "@promptvm/cli-darwin-x64",
  "linux-x64": "@promptvm/cli-linux-x64",
  "linux-arm64": "@promptvm/cli-linux-arm64",
  "win32-x64": "@promptvm/cli-win32-x64",
};

function binaryName() {
  return process.platform === "win32" ? "promptvm.exe" : "promptvm";
}

function resolveBinary() {
  const key = `${process.platform}-${process.arch}`;
  const pkg = PLATFORM_PACKAGES[key];
  if (!pkg) {
    fail(
      `promptvm: no prebuilt binary for ${key}. Reinstall, or install via Homebrew/curl.`
    );
  }
  // The sub-package ships its binary at `<pkg>/bin/promptvm[.exe]`. Resolve via
  // its package.json so this works regardless of node_modules hoisting layout.
  try {
    const pkgJson = require.resolve(`${pkg}/package.json`);
    const path = require("node:path");
    return path.join(path.dirname(pkgJson), "bin", binaryName());
  } catch (_) {
    fail(
      `promptvm: no prebuilt binary for ${key}. Reinstall, or install via Homebrew/curl.`
    );
  }
}

function fail(message) {
  process.stderr.write(message + "\n");
  process.exit(1);
}

function main() {
  const binary = resolveBinary();

  const fs = require("node:fs");
  try {
    fs.accessSync(binary, fs.constants.X_OK);
  } catch (err) {
    if (err && err.code === "ENOENT") {
      fail(
        `promptvm: prebuilt binary missing at ${binary}. Reinstall, or install via Homebrew/curl.`
      );
    }
    // Exists but not executable — surface that distinctly.
    fail(`promptvm: binary at ${binary} is not executable: ${err.message}`);
  }

  try {
    execFileSync(binary, process.argv.slice(2), { stdio: "inherit" });
  } catch (err) {
    // execFileSync throws on a non-zero exit; forward the child's exit code.
    if (typeof err.status === "number") {
      process.exit(err.status);
    }
    fail(`promptvm: failed to run binary: ${err.message}`);
  }
}

main();
