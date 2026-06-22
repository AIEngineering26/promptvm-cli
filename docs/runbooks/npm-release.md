# Runbook: publishing the PromptVM CLI to npm

`npx promptvm add <slug>` is delivered via npm using the **`optionalDependencies`
pattern** (esbuild/Turbo/swc model). The Go binary remains the single source of
truth; npm only ships the prebuilt artifacts goreleaser already builds. See
[`npm/README.md`](../../npm/README.md) for the package layout.

## Packages

| Package | Contents |
|---------|----------|
| `promptvm` (root) | `bin/promptvm.js` shim + `optionalDependencies` on all platform packages |
| `@promptvm/cli-darwin-arm64` | macOS arm64 binary |
| `@promptvm/cli-darwin-x64` | macOS x64 binary |
| `@promptvm/cli-linux-x64` | Linux x64 binary |
| `@promptvm/cli-linux-arm64` | Linux arm64 binary |
| `@promptvm/cli-win32-x64` | Windows x64 binary |

All share the git-tag version; the root pins exact sub-package versions.

## One-time gate — confirm npm ownership BEFORE first publish

Publishing is **blocked** until PromptVM controls both names:

```bash
npm owner ls promptvm            # must succeed / be claimable
npm access ls-packages @promptvm # @promptvm org must exist + be owned
```

If either is unavailable, fall back to the all-scoped layout (rename the root
package to `@promptvm/cli`; update `PLATFORM_PACKAGES` in `npm/bin/promptvm.js`
only if sub-package names change — they do not in this fallback).

## Wiring the release

1. Create an automation `NPM_TOKEN` (publish scope) and add it as a repo secret.
2. Set the repo variable `PUBLISH_NPM=true`.
3. Tag a release as usual (`make release V=X.Y.Z`). The `Release` workflow:
   - builds binaries + Homebrew via goreleaser,
   - uploads `dist/` as an artifact,
   - runs `scripts/build-npm-packages.mjs --version vX.Y.Z --dry-run` (always),
   - runs the same script with `--publish` **only** when `PUBLISH_NPM=true` and
     `NPM_TOKEN` is set.

Until the gate is lifted the job runs the dry-run only and publishes nothing.

## Local verification (no publish)

```bash
make snapshot                                            # build binaries into dist/
node scripts/build-npm-packages.mjs --version 0.0.0-dev --dry-run
# inspect dist-npm/ ; optionally `npm pack` each and install the tarballs into a
# scratch project to confirm the shim resolves + execs the right binary.
```

## Verify after a real publish

On a clean machine (matching one of the published platforms):

```bash
npx promptvm@X.Y.Z version
npx promptvm@X.Y.Z add <known-slug>   # writes ~/.claude/skills/<slug>/
```
