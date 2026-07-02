---
name: promptvm
description: Use PromptVM — a version manager for prompts, agent skills, and Claude Code hooks — from the `promptvm` CLI or hosted MCP server. Use when the user wants to create, version, resolve, share, search, or publish prompts/skills/hooks, manage workspaces and collections, or install marketplace content.
---

# PromptVM

PromptVM is a **version manager for AI context**: prompts, agent skills, and
Claude Code hooks. Think "git + package registry" for the things that drive
agents. Content lives in **workspaces**, is versioned, can be grouped into
**collections** and **directories**, and can be published to a **marketplace**.

Drive it through the `promptvm` CLI (installed locally) or the hosted **MCP
server** (tools prefixed `promptvm_*`).

## When to use

- Create / read / update / version a **prompt** and resolve it with variables.
- Roll back, fork, or export a prompt; inspect its version history.
- Upload or download a folder-shaped **agent skill** (`SKILL.md` + bundled files).
- Install a Claude Code **hook** from the registry into local settings.
- Organize content with **workspaces**, **collections**, **directories**.
- **Search** across an org, or browse / claim **marketplace** listings.
- **Onboard an environment** end-to-end (`promptvm setup`), enable **Context
  Sync** session capture, or connect this agent to the hosted **MCP server**
  (`promptvm mcp install`).

## One-shot setup

`promptvm setup` runs the whole onboarding: login (if needed) → Context Sync
(`sync init`, non-interactive) → MCP registration for detected clients
(Claude Code, Codex) → this agent skill. Flags: `--yes` (assume defaults;
automatic when stdin is not a TTY), `--workspace <uuid|slug|name>`,
`--device` (headless login), `--skip-mcp`, `--skip-sync`, `--mcp-url <url>`.

```bash
promptvm setup --yes                  # full onboarding, zero prompts
promptvm setup --print-agent-prompt  # print the copy-paste block for an agent
```

If the CLI is missing, install it first: `npm install -g @promptvm/cli`
(`npx @promptvm/cli <cmd>` also works). Verify with `promptvm version`.

## Authentication

Pick whichever is available; the CLI resolves credentials in this order:

1. `--public-key pk_… --secret-key sk_…` flags
2. `PROMPTVM_PUBLIC_KEY` + `PROMPTVM_SECRET_KEY` env vars
3. The active profile (run `promptvm auth login` for browser SSO, or
   `promptvm auth login --device` for headless/CI)

Check state with `promptvm auth status`. Override the API base with
`--base-url` or `PROMPTVM_BASE_URL`. Add `-o json` to almost any command for
machine-readable output.

## The three content kinds

- **prompt** — a versioned text template/instance, with `{{variable}}`
  placeholders resolved client-side.
- **skill** — a folder-shaped Agent Skill (agentskills.io): a `SKILL.md` with
  YAML frontmatter (`name`, `description`) plus optional bundled files.
- **hook** — a Claude Code lifecycle hook (events + matchers + handlers) merged
  into `.claude/settings.json`.

## Command map

### Workspaces / orgs
```bash
promptvm workspaces list                 # alias: ws
promptvm workspaces create|get|update|delete <id>
promptvm workspaces transfer|pin|unpin <id>
promptvm orgs list
```

### Prompts
```bash
promptvm prompts list --workspace <id>            # --workspace is required
promptvm prompts create --name <n> --workspace <id> --kind template|instance
promptvm prompts get <id>
promptvm prompts update <id>
promptvm prompts versions list <id>      # version history (versions is a parent: list|get|create)
promptvm prompts resolve <id> --var key=value   # {{var}} substitution (repeatable; also --vars-file)
promptvm prompts rollback <id> --to <version> [--yes]
promptvm prompts fork <id>
promptvm prompts export <id>
promptvm prompts move <id>
promptvm prompts references <id>         # what this prompt references
promptvm prompts dependents <id>         # what depends on it
```
Note: `prompts create --kind` takes the *prompt* kind (`template`|`instance`).
For skills/hooks use the `skills` / `hooks` command families.

### Skills (folder-shaped Agent Skills)
```bash
promptvm skills upload <folder>          # SKILL.md sent verbatim + bundled files
promptvm skills list [--workspace <id>]
promptvm skills get <id> [--raw]         # --raw prints SKILL.md
promptvm skills download <id> <dir>      # recreate the folder locally
promptvm skills delete <id> [--yes]
```

### Hooks (Claude Code)
```bash
promptvm hooks install <slug> [--scope project|user] [--dry-run] [--force]
promptvm hooks browse --workspace <id>
promptvm hooks list                      # locally installed
promptvm hooks uninstall <slug>
```

### Collections / directories / resources
```bash
promptvm collections list|create|get|update|delete <id>   # alias: col
promptvm collections add <collection-id> <prompt-id>
promptvm collections remove <collection-id> <item-id>
promptvm directories ...                  # alias: dirs
promptvm resources ...                    # alias: res — bundled files
```

### Marketplace
```bash
promptvm marketplace browse search [query]   # browse is a parent: search|featured|categories
promptvm marketplace listings create --prompt|--skill|--hook|--collection|--directory <id> \
    --name <title> --description <desc>
promptvm marketplace listings get|update|delete <id>
promptvm marketplace listings claim <id> [--workspace <id>]   # defaults to configured workspace
```

### Search / contexts / share
```bash
promptvm search <query> [--org <id>]
promptvm contexts list                   # catalogue of supported context kinds
promptvm share ...                       # share links
```

### Context Sync (automatic session capture)
```bash
promptvm sync init [--workspace <uuid|slug|name>] [--interactive]
                                         # zero prompts by default: writes the manifest,
                                         # installs Claude Code capture hooks, mints a
                                         # workspace-bound capture credential, flushes
                                         # any pending spool. Workspace names/slugs are
                                         # normalized to the UUID automatically.
promptvm sync run                        # hook-invoked uploader (stdin JSON; never blocks)
promptvm sync status                     # resolved config, manifests consulted, credential
                                         # file, pending spool, and a state-specific Next hint
promptvm sync doctor                     # diagnose + repair: normalize workspace → UUID,
                                         # re-mint a missing credential, reinstall hooks,
                                         # flush the spool (each check: ok/fixed/failed)
promptvm sync push                       # capture the current session manually
promptvm sync export                     # refresh the local context block from promoted captures
```
Lifecycle: `init` once per repo → hooks fire `run` on SessionEnd/PreCompact →
captures upload (or spool offline) → `status` to inspect → `doctor` to repair.

### MCP server connection
```bash
promptvm mcp install [--target claude|codex|all] [--scope user|project] [--mcp-url <url>] [--dry-run]
                                         # claude → `claude mcp add --transport http promptvm <url>`
                                         #   (or .mcp.json when the binary is absent)
                                         # codex → merges [mcp_servers.promptvm] into ~/.codex/config.toml
promptvm mcp print [--target claude|codex|all]
                                         # print the copy-paste config snippets only
```
The MCP endpoint derives from the API base URL (dev-api.promptvm.ai →
dev-mcp.promptvm.ai/mcp; api.promptvm.ai → mcp.promptvm.ai/mcp — the hosted
server speaks MCP only at the /mcp path); override with `--mcp-url` or
`PROMPTVM_MCP_URL` (full endpoint, path included).

## Using the hosted MCP server

When connected to PromptVM over MCP instead of the CLI, the same workflows are
exposed as `promptvm_*` tools (e.g. `promptvm_list_workspaces`,
`promptvm_list_prompts`, `promptvm_get_prompt`, `promptvm_create_prompt`,
`promptvm_create_prompt_version`, `promptvm_resolve_prompt`,
`promptvm_rollback_prompt`, `promptvm_search`, `promptvm_list_context_kinds`).
Workspace context is available via MCP resources. Prefer these tools when the
CLI is not installed locally.

## Tips

- Add `-o json` (or `-o yaml`) for structured output you can parse.
- `--dry-run` previews `hooks install` without writing.
- Most write commands accept `--yes` to skip interactive confirmation in CI.
- Scope `hooks install`/`hooks list`/`hooks uninstall` with `--scope project` (cwd) or
  `--scope user` (home). (Skills are placed via `skills download <id> <dir>`, not `--scope`.)
