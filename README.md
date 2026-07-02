# PromptVM CLI

The official command-line interface for the [PromptVM](https://promptvm.com) platform.

`promptvm` wraps the PromptVM REST API via the generated [Go SDK](https://github.com/AIEngineering26/promptvm-go-sdk) and gives you scriptable, repeatable access to prompts, versions, workspaces, organizations, collections, directories, templates, the marketplace, resources, sharing, and API keys.

---

## Quick start

Install a marketplace skill into your local Claude Code skills directory — no
login, no workspace:

```bash
npx @promptvm/cli add pdf
# or, with a native install:
promptvm add image-prompt-architect
```

This writes `SKILL.md` + every bundled file into `~/.claude/skills/<slug>/`.
See [`promptvm add`](#promptvm-add--install-a-marketplace-skill).

## Installation

### npm / npx (no native install)

```bash
npx @promptvm/cli add <slug>        # one-off, no global install
npm install -g promptvm        # or install the launcher globally
```

The `promptvm` npm package is a thin launcher: it resolves the matching
prebuilt Go binary via per-platform `optionalDependencies`
(`@promptvm/cli-<platform>-<arch>`) and execs it — the same binary shipped via
Homebrew/curl, no reimplementation and no `postinstall` download. See
[`npm/README.md`](./npm/README.md) for the layout and
[`docs/runbooks/npm-release.md`](./docs/runbooks/npm-release.md) for publishing.

### Install script (macOS / Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/AIEngineering26/promptvm-cli/main/install.sh | bash
```

### Homebrew

```bash
brew install AIEngineering26/tap/promptvm
```

### From source

```bash
go install github.com/AIEngineering26/promptvm-cli@latest
```

### Pre-built binaries

Download the archive for your OS/arch from [GitHub Releases](https://github.com/AIEngineering26/promptvm-cli/releases) and place the `promptvm` binary somewhere on your `PATH`.

Verify:

```bash
promptvm version
```

---

## One-shot setup (`promptvm setup`)

`promptvm setup` runs the entire onboarding in one command: login (if needed),
Context Sync (`sync init`, non-interactive), MCP registration for detected
clients (Claude Code / Codex), and the bundled agent skill.

```bash
promptvm setup                       # interactive login if needed, then everything
promptvm setup --yes                 # assume defaults (automatic when stdin is not a TTY)
promptvm setup --device              # headless login (device-code grant)
promptvm setup --workspace my-team   # UUID, slug, or name — normalized to the UUID
promptvm setup --skip-mcp            # only Context Sync + skill
promptvm setup --print-agent-prompt  # print the copy-paste block for Claude Code/Codex
```

Want an agent to do it? `promptvm setup --print-agent-prompt` emits a canonical
prompt you can paste into Claude Code or Codex to have it install, authenticate,
and configure everything, then verify with `promptvm sync status`.

---

## Authentication

The CLI supports two flows: **browser SSO** (default) and the **legacy API key**. Both persist named profiles at `~/.config/promptvm/profiles/<name>.yaml` with `0600` permissions.

### Browser SSO (default, recommended)

```bash
promptvm auth login
```

Opens your default browser to the PromptVM web app, authorizes via PKCE over a loopback redirect (RFC 8252), and stores the resulting access and refresh tokens in your OS keychain. The YAML profile only keeps metadata (expiry, user id/email) — **no secrets are ever written to disk in cleartext**.

### Device code flow (headless, SSH, CI)

```bash
promptvm auth login --device
# equivalently:
promptvm auth login --no-browser
PROMPTVM_HEADLESS=1 promptvm auth login
```

Prints a short user code and a URL you open on another device (RFC 8628). PromptVM polls the authorization server until you approve.

The CLI automatically suggests `--device` if it detects an SSH session, `CI=true`, or a GitHub Codespace.

### API key login (long-lived credentials)

The dashboard issues a **public key** (`pk_…`) and a **secret key** (`sk_…`) as a pair. Pass them as separate flags — this is the canonical form for non-interactive use:

```bash
promptvm auth login --public-key pk_live_... --secret-key sk_live_...
```

If you only pass `--public-key`, the CLI prompts for the matching secret with hidden input so the value never lands in your shell history:

```bash
promptvm auth login --public-key pk_live_...
# → Enter your secret key (sk_…):  ************
```

The legacy combined form is still accepted but **deprecated** and prints a stderr warning:

```bash
promptvm auth login --api-key pk_live_...:sk_live_...
# Warning: --api-key is deprecated; use --public-key/--secret-key
```

### Other flags

```bash
promptvm auth login --profile staging        # name the profile
promptvm auth login --base-url https://...   # custom API base URL
```

### Status, sessions, logout

```bash
promptvm auth status                 # show auth type (api-key|oauth), public-key prefix, org, base URL
promptvm auth status -o json         # same fields machine-readably (no secret_key field present)
promptvm auth sessions list          # list your active server-side CLI tokens
promptvm auth sessions revoke <id>   # revoke a session remotely (e.g. lost device)
promptvm auth logout                 # remove active profile AND keychain tokens
promptvm auth logout --all           # scrub every profile
```

`auth status` never prints the secret key — not whole, not truncated, not redacted-with-asterisks. The public key is shown as a 12-character prefix (`pk_554f77dcd…`).

### Credential resolution order

The CLI walks this precedence table on every command (first match wins):

1. `--public-key` + `--secret-key` flags (both required together)
2. `--api-key pk_…:sk_…` flag (deprecated; emits stderr warning)
3. `PROMPTVM_PUBLIC_KEY` + `PROMPTVM_SECRET_KEY` env vars (silent, long-term)
4. `PROMPTVM_API_KEY=pk_…:sk_…` env var (silent, backward-compat)
5. The active profile in `~/.config/promptvm/config.yaml` (api-key)
6. The active profile (OAuth — access token loaded from the keychain, refreshed transparently)

Setting only one half of a dual env pair (e.g. `PROMPTVM_PUBLIC_KEY` without `PROMPTVM_SECRET_KEY`) is a fatal error — the CLI **never** silently falls through to a single-string `PROMPTVM_API_KEY` when the dual pair is half-configured.

Switch profiles with `promptvm profile use <name>`.

### Profile storage

Profile YAML files at `~/.config/promptvm/profiles/<name>.yaml` are written atomically (temp + fsync + rename) with `0600` permissions on POSIX. On Windows the chmod is best-effort because NTFS does not honor POSIX permission bits; the rename step is still atomic on the same volume.

Profiles created by older CLI builds in the legacy `api_key: pk_…:sk_…` form are migrated transparently on first load — the file is rewritten with separate `public_key:` and `secret_key:` fields. If the rewrite fails (read-only FS, full disk), the CLI logs a warning and continues with the in-memory split for that session.

---

## Configuration

Global defaults live in `~/.config/promptvm/config.yaml`:

```bash
promptvm config list
promptvm config set defaults.output json
promptvm config set defaults.workspace ws_123
promptvm config set defaults.no_color true
```

Supported keys:

| Key                    | Values                 | Description                          |
|------------------------|------------------------|--------------------------------------|
| `active_profile`       | any profile name       | Profile selected by default          |
| `defaults.output`      | `table`, `json`, `yaml`| Default output format                |
| `defaults.no_color`    | `true`, `false`        | Disable ANSI colour in output        |
| `defaults.workspace`   | workspace ID           | Implicit `--workspace` for commands  |

---

## Usage

### Prompts

```bash
# Lifecycle
promptvm prompts list --workspace ws_123
promptvm prompts create --name "Support Reply" --workspace ws_123 --content "Hi {{name}}..."
promptvm prompts get pmt_abc123
promptvm prompts update pmt_abc123 --name "New name"
promptvm prompts delete pmt_abc123 --yes

# Resolve with variables (client-side {{var}} substitution)
promptvm prompts resolve pmt_abc123 --var name=Ada --var lang=Go
promptvm prompts resolve pmt_abc123 --vars-file vars.json

# Versions
promptvm prompts versions list pmt_abc123
promptvm prompts versions create pmt_abc123 --content "..."
promptvm prompts versions get pmt_abc123 v_456

# Rollback to a previous version (creates a new version that's a copy of the
# target and atomically advances the prompt's "current" pointer to it).
# Confirmation is interactive unless --yes is passed.
promptvm prompts rollback pmt_abc123 --to 1 --yes
promptvm prompts rollback pmt_abc123 --to 2 --idempotency-key $(uuidgen)

# Refs and dependents
promptvm prompts references pmt_abc123
promptvm prompts dependents pmt_abc123

# Move / fork / export
promptvm prompts move pmt_abc123 --directory dir_1
promptvm prompts fork pmt_abc123 --name "Copy"
promptvm prompts export pmt_abc123 --format md > prompt.md
```

### Contexts

```bash
# Discover the catalogue of context kinds the platform supports
# (e.g. prompt, skill). Use -o json or -o yaml for the full payload,
# including metadata, content, and file specs.
promptvm contexts list
promptvm contexts list -o json
```

### Context Sync — automatic session capture

`promptvm sync` wires Claude Code lifecycle hooks so that when a session **ends**
or is **compacted**, a distilled, **redacted** context artifact is uploaded into
the right PromptVM workspace — with no per-session glue work. Capture is opt-in,
redaction is on by default, and nothing becomes canonical without server-side
governance.

```bash
# One-time setup for a repo: writes the manifest + the capture hook + a
# workspace-bound capture credential, then flushes any pending spool.
# Zero prompts by default: the workspace resolves from --workspace → the config
# default → your account default, and names/slugs are normalized to the
# workspace UUID. Preview with --dry-run; opt in to a picker with --interactive.
promptvm sync init
promptvm sync init --scope project --dry-run
promptvm sync init --workspace my-team --mode summary   # slug/name → UUID
promptvm sync init --interactive                        # pick from a list

# Inspect the resolved config, manifests consulted, credential file, pending
# spool, installed hooks — plus a state-specific "Next:" hint
promptvm sync status
promptvm sync status -o json

# Diagnose + repair: normalize a non-UUID manifest workspace, re-mint a missing
# capture credential, reinstall missing hooks, flush the spool
promptvm sync doctor

# Manually capture (no hook needed) or backfill
promptvm sync push --last
promptvm sync push --session <session-id>

# Refresh the local context block (CLAUDE.md) with promoted captures
promptvm sync export

# Hook-invoked uploader (reads the event JSON from stdin; self-detaches). Not
# typically run by hand — `sync init` installs it for you.
promptvm sync run
```

**Scopes** (one vocabulary, `local | project | user`):

| Scope     | Manifest file                        | Settings file                  |
|-----------|--------------------------------------|--------------------------------|
| `local`   | `.promptvm/config.local.json` (gitignored) | `.claude/settings.local.json` |
| `project` | `.promptvm/config.json` (committable)      | `.claude/settings.json`        |
| `user`    | `<config dir>/promptvm/config.json`        | `~/.claude/settings.json`      |

Resolution precedence is **local → project → user** (most specific wins).
Capture-policy arrays (`events`, `excludePaths`) **replace** rather than merge,
so a repo can drop `PreCompact`.

- **Events:** `SessionEnd` (primary) + `PreCompact` (checkpoint) by default; a
  `SessionStart` reconcile hook is always installed to flush anything that never
  uploaded.
- **Non-blocking:** `sync run` self-detaches and exits 0 immediately; on failure
  the capture is spooled (0600 files in a 0700 dir) and retried on reconcile.
- **Redaction before egress:** provider token patterns + high-entropy detection +
  path excludes run client-side; the server runs an authoritative scan too.
- **Managed settings:** `sync init` aborts and `sync run` no-ops when Claude Code
  managed settings set `disableAllHooks`.

### MCP — connect AI clients to the hosted server

```bash
# Register the hosted PromptVM MCP server with local clients.
#   claude → `claude mcp add --transport http promptvm <mcp-url>` (or .mcp.json
#            when the binary is absent)
#   codex  → merges [mcp_servers.promptvm] into ~/.codex/config.toml, preserving
#            existing content; auth headers reuse the active api-key profile or
#            (OAuth-only) a freshly minted "codex mcp" read/write key
promptvm mcp install                 # --target claude|codex|all (default all)
promptvm mcp install --dry-run
promptvm mcp print                   # print the snippets only; writes nothing
```

The MCP endpoint derives from the API base URL (`dev-api.promptvm.ai` →
`dev-mcp.promptvm.ai`; `api.promptvm.ai` → `mcp.promptvm.ai`); override with
`--mcp-url` or `PROMPTVM_MCP_URL`.

### `promptvm add` — install a marketplace skill

`add` is the one-command path for installing a **public marketplace skill** into
your local Claude Code skills directory. It resolves the skill by slug
**anonymously** (no `auth login`, no workspace) and writes `SKILL.md` plus every
bundled file into `~/.claude/skills/<slug>/`.

```bash
promptvm add pdf-toolkit                 # → ~/.claude/skills/pdf-toolkit/
promptvm add acme/pdf-toolkit            # disambiguate by creator/slug
promptvm add pdf-toolkit --dry-run       # list files that would be written; writes nothing
promptvm add pdf-toolkit --force         # overwrite an existing install without prompting
promptvm add pdf-toolkit --scope project # write to ./.claude/skills/ instead of ~/.claude/skills/
```

- **Default scope is user-global** (`~/.claude/skills/`) — a skill teaches an
  agent globally.
- **Collision:** if the target already exists, `add` prompts `Overwrite existing
  skill '<slug>'? (y/N)`. `--force` overwrites; in a non-interactive shell
  (no TTY) without `--force` it aborts with a `--force` hint.
- **Best-effort install counter:** on success `add` pings a public install
  counter; a counter failure never fails the install (and `--dry-run` never
  increments it).
- Works with credentials too — if you are logged in they ride along, but they
  are never required.

This shares the same reconstruction + path-escape guard as `skills download`.

### Skills

```bash
# Skills are folder-shaped contexts (agentskills.io format): a SKILL.md with
# YAML frontmatter plus optional bundled files (scripts, references, assets).
# Upload a folder — SKILL.md is sent verbatim, every other file becomes a
# bundled resource under its relative path.
promptvm skills upload ./pdf-tools --workspace ws_123 --tags pdf,docs
promptvm skills upload ./pdf-tools --status published --public

promptvm skills list --workspace ws_123
promptvm skills get skl_123                  # frontmatter summary + file manifest
promptvm skills get skl_123 --raw            # literal SKILL.md to stdout
promptvm skills download skl_123 ./pdf-tools # recreate the skill folder locally
promptvm skills delete skl_123 --yes
```

### Search

```bash
# Org-wide search returns a table with name, kind, workspace, score, id.
# --org may be omitted if a profile-default organization is set.
promptvm search "support reply" --org org_abc
promptvm search "embeddings" --kind prompt --limit 50
promptvm search "onboarding" --workspace ws_123 -o json
```

### Workspaces and Organizations

```bash
promptvm workspaces list
promptvm workspaces create --name "Platform" --visibility private
promptvm workspaces pin ws_123
promptvm workspaces transfer ws_123 --new-owner user_456

promptvm orgs list
promptvm orgs members list org_abc
promptvm orgs invite org_abc --email new@example.com --role member
```

### Collections, Directories, Templates

```bash
promptvm collections create --name "Best of"
promptvm collections add col_1 pmt_abc123

promptvm dirs list --workspace ws_123
promptvm dirs create --workspace ws_123 --name "Marketing"

promptvm tpl convert pmt_abc123
promptvm tpl instantiate tpl_42 --name "My Copy" --workspace ws_123 --vars key=value
```

### Marketplace

```bash
promptvm marketplace browse search --q "copywriting"
promptvm marketplace browse featured
promptvm marketplace browse categories

# Publish from exactly one source: a prompt, skill, hook, collection, or directory.
# Skill, hook, and collection listings are free-only.
promptvm marketplace listings create --prompt pmt_abc123 --name "Pro Copy Prompt" --description "..."
promptvm marketplace listings create --skill skl_abc123 --name "Code Review Skill" --description "..."
promptvm marketplace listings create --hook hk_abc123  --name "Pre-commit Hook"  --description "..."
promptvm marketplace listings create --collection col_1 --name "Startup Kit"     --description "..."

# Claim any kind into a workspace; prints a per-kind imported manifest, e.g.
#   Imported: 2 prompts, 1 skill, 1 hook, 1 file → collection col_9
promptvm marketplace listings claim lst_123 --workspace ws_123

promptvm marketplace creator dashboard

promptvm marketplace subscribe creator_abc
promptvm marketplace rate lst_123 --stars 5 --review "Great!"
promptvm marketplace comment lst_123 --message "Nice work"
promptvm marketplace follow creator_abc
promptvm marketplace feed
```

### Resources

```bash
promptvm resources list --workspace ws_123
promptvm resources upload ./docs/*.pdf --prompt pmt_abc123
promptvm resources get res_123
promptvm resources download res_123 --output ./downloads
promptvm resources delete res_123 --yes
```

### Sharing

```bash
promptvm share create pmt_abc123 --permission view --expires 30d
promptvm share get share_link_id
promptvm share revoke share_link_id

promptvm share collaborators list pmt_abc123
promptvm share collaborators add pmt_abc123 --email teammate@example.com --role edit
promptvm share collaborators remove pmt_abc123 user_456
```

### API Keys

```bash
promptvm apikeys list
promptvm apikeys create --name "CI" --scopes read,write --environment live
promptvm apikeys get ak_123
promptvm apikeys revoke ak_123
promptvm apikeys usage ak_123
```

### Shell completion

```bash
promptvm completion bash > /etc/bash_completion.d/promptvm
promptvm completion zsh  > "${fpath[1]}/_promptvm"
promptvm completion fish > ~/.config/fish/completions/promptvm.fish
promptvm completion powershell | Out-String | Invoke-Expression
```

---

## Agent skill

The CLI bundles a canonical **`promptvm` Agent Skill** that teaches coding
agents how to drive PromptVM. It is installed automatically the first time you
run any `promptvm` command (opt-out), writing the same folder-shaped skill into
both agents' skill directories:

| Agent       | User scope (default)                       | Project scope (`--scope project`) |
|-------------|--------------------------------------------|-----------------------------------|
| Claude Code | `~/.claude/skills/promptvm/`               | `./.claude/skills/promptvm/`      |
| Codex       | `$CODEX_HOME/skills/promptvm/` or `~/.agents/skills/promptvm/` | `./.agents/skills/promptvm/` |

Manage it explicitly with the `agent` command family:

```bash
promptvm agent install                 # all targets, user scope (default)
promptvm agent install --target claude --scope project
promptvm agent install --dry-run       # list paths it would write
promptvm agent install --force         # overwrite an existing/older skill
promptvm agent status                  # bundled vs installed version + paths
promptvm agent uninstall               # remove the skill folders it installed
```

Disable the first-run auto-install (and the `install.sh` step) by setting
`PROMPTVM_NO_AGENT_SKILL=1`.

---

## Output formats

Every read command supports JSON and YAML for scripting:

```bash
promptvm prompts list --output json | jq '.data[].id'
promptvm workspaces list --output yaml
promptvm prompts get pmt_abc123 -o json --compact
```

Hide the header / expand all columns in table output:

```bash
promptvm prompts list --no-header
promptvm prompts list --wide
```

Disable colour explicitly (useful in CI):

```bash
promptvm prompts list --no-color
```

---

## Environment variables

| Variable               | Purpose                                                          |
|------------------------|------------------------------------------------------------------|
| `PROMPTVM_PUBLIC_KEY`  | API public key (`pk_…`); paired with `PROMPTVM_SECRET_KEY`       |
| `PROMPTVM_SECRET_KEY`  | API secret key (`sk_…`); paired with `PROMPTVM_PUBLIC_KEY`       |
| `PROMPTVM_API_KEY`     | Legacy combined `pk_…:sk_…` form (silent backward-compat)        |
| `PROMPTVM_BASE_URL`    | Override the API base URL (staging, self-host)                   |
| `PROMPTVM_APP_URL`     | Override the web app URL used by `auth login` (derived otherwise)|
| `PROMPTVM_HEADLESS`    | Set to `1` to force `auth login` into the device-code flow       |
| `PROMPTVM_MCP_URL`     | Override the hosted MCP endpoint (derived from the base URL otherwise) |
| `PROMPTVM_DEVICE_NAME` | Label sent to the server when authorizing a CLI session          |
| `XDG_CONFIG_HOME`      | Root for `promptvm/` config directory                            |
| `CODEX_HOME`           | Codex home; its `skills/` dir is the user-scope Codex skill target|
| `PROMPTVM_NO_AGENT_SKILL` | Set to `1` to disable first-run agent-skill auto-install      |

---

## Development

```bash
git clone https://github.com/AIEngineering26/promptvm-cli
cd promptvm-cli
make deps
make build       # build ./bin/promptvm
make test        # run unit tests
make lint        # run golangci-lint
make snapshot    # local GoReleaser snapshot build
```

Source layout:

```
cmd/             cobra commands, one file per resource
internal/
  api/           raw HTTP helper for endpoints not covered by the SDK
  client/        SDK client factory (flag → env → profile → default)
  config/        on-disk config + profile store (~/.config/promptvm)
  errors/        user-facing CLIError with hints
  ioutil/        shared helpers (e.g. reading --content / --file)
  oauth/         PKCE, loopback redirect, device-code grant, keychain store
  output/        table / json / yaml formatters, colour, spinner, progress, time
  prompt/        interactive confirm / select / input (huh)
main.go          entrypoint calling cmd.Execute()
.goreleaser.yml  release configuration
```

The CLI imports the generated SDK from `github.com/AIEngineering26/promptvm-go-sdk`. Dependabot bumps it on every release.

---

## Releasing

Releases are driven by [GoReleaser](https://goreleaser.com). Tag the commit and push:

```bash
git tag v0.1.0
git push origin v0.1.0
```

GitHub Actions will build archives for darwin/linux/windows × amd64/arm64, generate SBOMs, sign checksums, publish GitHub Releases, update the Homebrew tap, and push the install script.

---

## License

MIT — see [LICENSE](./LICENSE).
