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
│   └── prompt/             ← Interactive input (huh TUI)
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
