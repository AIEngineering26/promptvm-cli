# CLAUDE.md — promptvm-cli

## Submodule Position

```
framework                    → github.com/AIEngineering26/framework
└── projects/promptvm        → github.com/AIEngineering26/promptvm
    ├── services/cli/        ← YOU ARE HERE (github.com/AIEngineering26/promptvm-cli)
    ├── services/backend/    → github.com/AIEngineering26/promptvm-backend
    ├── services/frontend/   → github.com/AIEngineering26/promptvm-frontend
    └── ...
```

## Stack

**Go 1.23+ · Cobra · Huh (TUI) · Lipgloss · go-keyring**

```bash
make build       # CGO_ENABLED=0 go build -o bin/promptvm
make test        # go test -race -cover ./...
make lint        # golangci-lint run ./...
go vet ./...     # static analysis
```

## Project Structure

```
cli/
├── cmd/                     ← One file per command/subcommand
│   ├── root.go             ← Root command + global flags
│   ├── hooks.go            ← hooks parent command
│   ├── hooks_install.go    ← hooks install <slug>
│   ├── hooks_browse.go     ← hooks browse (list from API)
│   ├── hooks_list.go       ← hooks list (local settings)
│   ├── hooks_uninstall.go  ← hooks uninstall <slug>
│   ├── prompts.go          ← prompts parent
│   ├── prompts_*.go        ← prompts subcommands
│   ├── skills.go           ← skills parent + shared API shapes
│   ├── skills_upload.go    ← skills upload <folder> (alias: create)
│   ├── skills_list.go      ← skills list
│   ├── skills_get.go       ← skills get <id> [--raw]
│   ├── skills_download.go  ← skills download <id> <dir>
│   ├── skills_delete.go    ← skills delete <id>
│   ├── marketplace.go      ← marketplace parent
│   ├── marketplace_listings.go ← listings create/get/update/delete/claim (raw HTTP)
│   └── ...
├── internal/
│   ├── api/                ← Raw HTTP caller (for endpoints not in SDK)
│   ├── client/             ← SDK client factory (credential resolution)
│   ├── config/             ← Profile & config storage (~/.config/promptvm/)
│   ├── errors/             ← CLIError with hints
│   ├── hooks/              ← Claude Code settings.json merge + tracker
│   ├── ioutil/             ← Content reading helpers
│   ├── oauth/              ← PKCE, device-code grant, keychain
│   ├── output/             ← Table/JSON/YAML formatters
│   ├── prompt/             ← Interactive input (huh TUI)
│   └── skills/             ← Agent Skills folder walk, frontmatter, safe paths
├── main.go
├── go.mod / go.sum
└── Makefile
```

## Authentication

Credential resolution precedence (in `internal/client/client.go`):
1. `--public-key` + `--secret-key` flags
2. `--api-key pk_…:sk_…` flag (deprecated)
3. `PROMPTVM_PUBLIC_KEY` + `PROMPTVM_SECRET_KEY` env vars
4. `PROMPTVM_API_KEY` env var (backward-compat)
5. Active profile (api-key)
6. Active profile (OAuth with auto-refresh from keychain)

## Hooks Commands

Manage Claude Code lifecycle hooks from the PromptVM registry.

```bash
promptvm hooks install <slug>     # Fetch + merge into .claude/settings.json
promptvm hooks browse --workspace <id>  # List hooks from workspace
promptvm hooks list               # Show locally installed hooks
promptvm hooks uninstall <slug>   # Remove managed hook from settings
```

**Install flow:**
1. Fetches hook via public slug endpoint (`GET /api/v1/hooks/s/:slug`)
2. Merges the hook's `config` (events/matchers/handlers) into `.claude/settings.json`
3. Tracks installation in sidecar `.claude/.promptvm-hooks.json`
4. Injects `_slug` metadata into matchers for ownership tracking

**Key files:**
- `internal/hooks/settings.go` — Read/write/merge settings.json (atomic writes)
- `internal/hooks/tracker.go` — Sidecar `.promptvm-hooks.json` management
- `internal/hooks/settings_test.go` — 14 unit tests

**Flags common to hooks commands:**
- `--scope project|user` — Target `.claude/settings.json` in cwd or `~/.claude/`
- `--dry-run` — Preview changes (install only)
- `--force` — Overwrite existing (install only)

Uses raw HTTP (`internal/api.Caller`) since the Go SDK doesn't have hooks endpoints yet.

## Skills Commands

Manage folder-shaped Agent Skills (SKILL.md + bundled files, agentskills.io format).

```bash
promptvm skills upload <folder>        # Upload SKILL.md verbatim + bundled files as resources
promptvm skills list [--workspace id]  # NAME, SLUG, STATUS, FILES, UPDATED
promptvm skills get <id> [--raw]       # Frontmatter summary + manifest; --raw prints SKILL.md
promptvm skills download <id> <dir>    # Recreate the skill folder locally
promptvm skills delete <id> [--yes]    # Delete (y/N confirm)
```

**Upload flow:**
1. Reads `SKILL.md` literally (byte-preserving); validates frontmatter `name` (kebab rule `^[a-z0-9][a-z0-9-]{0,63}$`) client-side
2. Walks the folder (skips root SKILL.md, dotfiles, dot-dirs); each file uploads via the resources presigned-URL flow (`uploadFileResource` in `cmd/resources.go`)
3. `POST /api/v1/skills` with `skill_md` + files manifest (relative forward-slash paths)

**Key files:**
- `internal/skills/skills.go` — folder walk, frontmatter parse/validate, `SafeJoin` path-escape guard for downloads
- `internal/skills/skills_test.go` — table-driven unit tests

Uses raw HTTP (`internal/api.Caller`) since the Go SDK doesn't have skills endpoints yet. Note: `prompts create --kind` takes the *prompt* kind (template|instance); passing `skill`/`hook` errors with a pointer to the right command family.

## Marketplace Listings Commands

Publish and claim marketplace listings for any content kind.

```bash
# Create from exactly one source (mutually exclusive). Skill/hook/collection are free-only.
promptvm marketplace listings create --prompt <id>     --name <t> --description <d>
promptvm marketplace listings create --skill <id>      --name <t> --description <d>
promptvm marketplace listings create --hook <id>       --name <t> --description <d>
promptvm marketplace listings create --collection <id> --name <t> --description <d>
promptvm marketplace listings create --directory <id>  --name <t> --description <d>

# Claim a listing of any kind into a workspace; prints a per-kind imported manifest.
promptvm marketplace listings claim <id> --workspace <id>
```

**Source flags** (`create`): `--prompt`, `--collection`, `--skill`, `--hook`, `--directory` — exactly one
is required and they are mutually exclusive (`validateSingleSource`). `--skill`/`--hook` are sent as
`skillId`/`hookId` in the request body; the backend maps them to the underlying `promptId`. Price stays
free by default (`--price free`); skill/hook/collection listings reject a non-zero price server-side.

**Claim manifest:** `claim` reads the backend `createdItems` response (`prompts`/`skills`/`hooks`/`resources`
arrays + `collectionId`) and prints a summary like `Imported: 2 prompts, 1 skill, 1 hook, 1 file → collection <id>`
(`formatClaimManifest`), falling back to the legacy `importedPromptId`/`importedCollectionId` fields for
older prompt/collection listings.

**Key files:**
- `cmd/marketplace_listings.go` — create/get/update/delete/claim. `create` + `claim` use raw HTTP (`internal/api.Caller`) because the generated Go SDK doesn't yet model `skillId`/`hookId`/`directoryId` or the `createdItems` manifest (SDK regenerates via Fern after merge).
- `cmd/marketplace_listings_test.go` — table-driven tests for source validation, request-body building, and manifest formatting.
