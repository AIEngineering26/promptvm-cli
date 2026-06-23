# npm distribution for the PromptVM CLI

This directory holds the npm packaging that makes `npx @promptvm/cli add <slug>` work.
It follows the **`optionalDependencies` pattern** used by esbuild / Turbo / swc
(Option B in the PRD) — the Go binary stays the single source of truth, and npm
is a thin delivery shim. There is **no `postinstall` download script**.

## Layout

```
npm/
├── package.json            ← root `promptvm` package (bin shim + optionalDependencies)
├── bin/promptvm.js         ← Node shim: resolves the platform binary and execs it
├── platform-template/      ← template for ONE per-platform sub-package
│   ├── package.json        ← @promptvm/cli-<platform>-<arch> (os/cpu + bin layout)
│   └── bin/.gitkeep        ← the prebuilt Go binary lands here at release time
└── README.md               ← this file
```

## How it works

1. A user runs `npm i -g promptvm` (or `npx @promptvm/cli …`). npm reads the root
   package's `optionalDependencies`, which list one package per platform:

   - `@promptvm/cli-darwin-arm64`
   - `@promptvm/cli-darwin-x64`
   - `@promptvm/cli-linux-x64`
   - `@promptvm/cli-linux-arm64`
   - `@promptvm/cli-win32-x64`

2. Each sub-package declares matching `os` and `cpu` fields, so npm installs
   **only** the one for the current machine and skips the rest.

3. The sub-package contains exactly one prebuilt Go binary at
   `bin/promptvm` (`bin/promptvm.exe` on Windows) — the same artifact goreleaser
   builds for Homebrew/curl.

4. The root `bin/promptvm.js` shim maps `process.platform`/`process.arch` to the
   sub-package name, `require.resolve`s its `package.json` to find the binary,
   and `execFileSync`s it — forwarding argv and the child exit code. If no
   matching package is installed it exits non-zero with:

   ```
   promptvm: no prebuilt binary for <platform>-<arch>. Reinstall, or install via Homebrew/curl.
   ```

   A present-but-not-executable binary is surfaced distinctly.

## Versioning

All packages share the git-tag version. At release time the root package's
`version` and every sub-package `version` are set to `{{.Version}}`, and the root
`optionalDependencies` pin **exact** sub-package versions (no `^`/`~`) so `npx`
never resolves a mismatched binary. See
[`scripts/build-npm-packages.mjs`](../scripts/build-npm-packages.mjs).

## Publishing is GATED — do not publish yet

Per the PRD prerequisite, `npm publish` is **blocked** until ownership of the
`promptvm` package name and the `@promptvm` org on npm is confirmed.

- Verify with `npm owner ls promptvm` and `npm access ls-packages @promptvm`.
- If unavailable, fall back to an all-scoped layout: rename the root package to
  `@promptvm/cli` and keep the `@promptvm/cli-*` sub-packages (the shim's
  `PLATFORM_PACKAGES` map is the only code change required).
- Only after confirming ownership, run the release publish step (guarded behind
  `NPM_TOKEN`, with a dry-run path) — see
  [`docs/runbooks/npm-release.md`](../docs/runbooks/npm-release.md).

## Local verification (without publishing)

```bash
# Build the binary, stage the packages at a test version, dry-run pack:
node scripts/build-npm-packages.mjs --version 0.0.0-dev --dry-run

# Then `npm link` the staged root package, or `npm pack` each and install the
# tarballs into a scratch project to confirm the shim resolves and execs.
```
