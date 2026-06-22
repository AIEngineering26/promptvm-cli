#!/usr/bin/env node
// @ts-nocheck
"use strict";

/**
 * Generate the npm distribution packages for the PromptVM CLI at release time.
 *
 * goreleaser has NO native npm publisher (.goreleaser.yml does binaries +
 * Homebrew only), so this script is the custom release step the PRD (US-007)
 * calls for. It:
 *
 *   1. Reads the version (--version or the git tag) and strips a leading "v".
 *   2. For each target platform, materializes an `@promptvm/cli-<plat>-<arch>`
 *      package under dist-npm/ from npm/platform-template/, sets its name,
 *      version, os/cpu, and copies the matching goreleaser-built binary into
 *      its bin/ directory.
 *   3. Materializes the root `promptvm` package with optionalDependencies
 *      pinned to the EXACT same version.
 *   4. With --publish, runs `npm publish` for every package (requires
 *      NPM_TOKEN). Without it (the default), it only stages + `npm pack`-style
 *      dry-runs so the layout can be verified before a real publish.
 *
 * This script never runs automatically; it is invoked by the release workflow
 * job (see .github/workflows/release.yml) AFTER the publish gate is lifted, or
 * manually for local verification.
 *
 * Usage:
 *   node scripts/build-npm-packages.mjs --version 1.2.3 [--dist dist] [--dry-run] [--publish]
 */

import { execFileSync } from "node:child_process";
import { existsSync, mkdirSync, cpSync, copyFileSync, readFileSync, writeFileSync, rmSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const cliRoot = resolve(__dirname, "..");

// Target matrix — keep in sync with npm/package.json optionalDependencies and
// the shim's PLATFORM_PACKAGES map. `goosArch` is how goreleaser names the dist
// directory (`promptvm_<os>_<arch>`); adjust if the goreleaser name_template
// changes.
const TARGETS = [
  { pkg: "@promptvm/cli-darwin-arm64", os: "darwin", cpu: "arm64", goos: "darwin", goarch: "arm64", exe: "promptvm" },
  { pkg: "@promptvm/cli-darwin-x64", os: "darwin", cpu: "x64", goos: "darwin", goarch: "amd64", exe: "promptvm" },
  { pkg: "@promptvm/cli-linux-x64", os: "linux", cpu: "x64", goos: "linux", goarch: "amd64", exe: "promptvm" },
  { pkg: "@promptvm/cli-linux-arm64", os: "linux", cpu: "arm64", goos: "linux", goarch: "arm64", exe: "promptvm" },
  { pkg: "@promptvm/cli-win32-x64", os: "win32", cpu: "x64", goos: "windows", goarch: "amd64", exe: "promptvm.exe" },
];

function arg(name, fallback) {
  const i = process.argv.indexOf(`--${name}`);
  if (i === -1) return fallback;
  const v = process.argv[i + 1];
  return v && !v.startsWith("--") ? v : true;
}

function gitTagVersion() {
  try {
    return execFileSync("git", ["describe", "--tags", "--abbrev=0"], { cwd: cliRoot }).toString().trim();
  } catch {
    return "";
  }
}

function main() {
  const dryRun = arg("dry-run", false) === true;
  const publish = arg("publish", false) === true;
  const distName = typeof arg("dist") === "string" ? arg("dist") : "dist";
  const goreleaserDist = resolve(cliRoot, distName);

  let version = arg("version", gitTagVersion());
  if (!version || version === true) {
    console.error("error: --version is required (or run from a tagged commit)");
    process.exit(1);
  }
  version = String(version).replace(/^v/, "");

  const outRoot = resolve(cliRoot, "dist-npm");
  rmSync(outRoot, { recursive: true, force: true });
  mkdirSync(outRoot, { recursive: true });

  const stagedDirs = [];

  // 1. Per-platform sub-packages.
  for (const t of TARGETS) {
    const dest = join(outRoot, t.pkg.replace("/", "-").replace("@", ""));
    cpSync(join(cliRoot, "npm", "platform-template"), dest, { recursive: true });

    const pkgPath = join(dest, "package.json");
    const pkg = JSON.parse(readFileSync(pkgPath, "utf8"));
    pkg.name = t.pkg;
    pkg.version = version;
    pkg.os = [t.os];
    pkg.cpu = [t.cpu];
    pkg.files = ["bin/" + t.exe];
    pkg.description = `Prebuilt PromptVM CLI binary for ${t.os}-${t.cpu}. Installed automatically as an optional dependency of \`promptvm\`.`;
    writeFileSync(pkgPath, JSON.stringify(pkg, null, 2) + "\n");

    // Copy the goreleaser-built binary in. goreleaser lays binaries out under
    // dist/<build-id>_<goos>_<goarch>[_v1]/promptvm; resolve loosely.
    const binSrc = findBinary(goreleaserDist, t);
    mkdirSync(join(dest, "bin"), { recursive: true });
    if (binSrc) {
      copyFileSync(binSrc, join(dest, "bin", t.exe));
    } else if (!dryRun) {
      console.error(`error: binary for ${t.goos}/${t.goarch} not found under ${goreleaserDist}`);
      console.error("       run goreleaser (or `make snapshot`) first.");
      process.exit(1);
    } else {
      console.warn(`warn: [dry-run] binary for ${t.goos}/${t.goarch} not found — staging package.json only`);
    }
    stagedDirs.push(dest);
  }

  // 2. Root package with pinned optionalDependencies.
  const rootDest = join(outRoot, "promptvm");
  mkdirSync(join(rootDest, "bin"), { recursive: true });
  copyFileSync(join(cliRoot, "npm", "bin", "promptvm.js"), join(rootDest, "bin", "promptvm.js"));
  const rootPkg = JSON.parse(readFileSync(join(cliRoot, "npm", "package.json"), "utf8"));
  rootPkg.version = version;
  rootPkg.optionalDependencies = Object.fromEntries(TARGETS.map((t) => [t.pkg, version]));
  writeFileSync(join(rootDest, "package.json"), JSON.stringify(rootPkg, null, 2) + "\n");
  stagedDirs.push(rootDest);

  console.log(`Staged ${stagedDirs.length} npm packages at version ${version} under ${outRoot}`);

  // 3. Publish (gated) or dry-run pack.
  for (const dir of stagedDirs) {
    if (publish && !dryRun) {
      if (!process.env.NPM_TOKEN) {
        console.error("error: --publish requires NPM_TOKEN in the environment");
        process.exit(1);
      }
      console.log(`publishing ${dir} ...`);
      execFileSync("npm", ["publish", "--access", "public"], { cwd: dir, stdio: "inherit" });
    } else {
      console.log(`dry-run pack ${dir}`);
      execFileSync("npm", ["pack", "--dry-run"], { cwd: dir, stdio: "inherit" });
    }
  }

  if (!publish) {
    console.log("\nNo --publish flag: nothing was published. Layout staged for verification.");
  }
}

function findBinary(distRoot, t) {
  if (!existsSync(distRoot)) return null;
  // Common goreleaser dir shapes: promptvm_<goos>_<goarch>, with possible
  // _v1 / _amd64v1 suffixes. Probe a few candidates.
  const candidates = [
    `promptvm_${t.goos}_${t.goarch}`,
    `promptvm_${t.goos}_${t.goarch}_v1`,
  ];
  for (const c of candidates) {
    const p = join(distRoot, c, t.exe);
    if (existsSync(p)) return p;
  }
  return null;
}

main();
