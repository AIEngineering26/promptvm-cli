package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/agentskill"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/gitutil"
	"github.com/spf13/cobra"
)

// agentPromptBlock is the canonical copy-paste block a user hands to Claude
// Code / Codex to have the agent perform the whole setup. The wording is a
// cross-repo contract — keep it semantically identical everywhere it appears
// (CLI, web app, docs).
const agentPromptBlock = `Set up PromptVM for me in this environment:
1. Install the CLI if missing: npm install -g @promptvm/cli (check with: promptvm version).
2. Authenticate: run promptvm auth login (opens a browser). If this environment is headless, run promptvm auth login --device and show me the code to enter. If I gave you an API key pair, run promptvm auth login --public-key <pk_...> --secret-key <sk_...>.
3. Run promptvm setup --yes to configure Context Sync (session-capture hooks + credential) and register the PromptVM MCP server with this agent.
4. Verify with promptvm sync status and show me the output.`

func newSetupCmd() *cobra.Command {
	var (
		yes              bool
		skipMCP          bool
		skipSync         bool
		workspace        string
		mode             string
		eventsCSV        string
		mcpURL           string
		device           bool
		printAgentPrompt bool
	)

	cmd := &cobra.Command{
		Use:   "setup",
		Short: "One-shot onboarding: login, Context Sync, MCP registration, agent skill",
		Long: `Runs the full PromptVM onboarding in one command:

  1. auth   — logs in if needed (browser SSO; --device for headless;
              --public-key/--secret-key for key pairs)
  2. sync   — configures Context Sync non-interactively (hooks + manifest +
              capture credential; workspace resolves to your default)
  3. mcp    — registers the hosted MCP server with detected clients
              (Claude Code, Codex); undetected clients are skipped
  4. skill  — ensures the promptvm agent skill is installed

Use --print-agent-prompt to get a copy-paste block you can hand to Claude Code
or Codex so the agent performs this setup for you.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if printAgentPrompt {
				fmt.Fprintln(cmd.OutOrStdout(), agentPromptBlock)
				return nil
			}

			out := cmd.OutOrStdout()

			// --yes defaults to true when stdin is not a TTY (CI / agents).
			if !cmd.Flags().Changed("yes") && !isTTYFunc() {
				yes = true
			}

			// ---- 1. Auth -------------------------------------------------
			publicKey, _ := cmd.Flags().GetString("public-key")
			secretKey, _ := cmd.Flags().GetString("secret-key")
			authed := false
			if _, err := client.ResolveCredentials(cmd); err == nil {
				authed = true
				fmt.Fprintln(out, "✓ auth: already authenticated")
			}
			if !authed {
				fmt.Fprintln(out, "→ auth: logging in…")
				var err error
				switch {
				case publicKey != "" && secretKey != "":
					err = runDualKeyLogin(cmd, publicKey, secretKey, "")
				case device || os.Getenv("PROMPTVM_HEADLESS") == "1" || (!isTTYFunc() && yes):
					err = runDeviceLogin(cmd, "")
				default:
					err = runBrowserLogin(cmd, "")
				}
				if err != nil {
					return fmt.Errorf("login failed: %w\n  Retry with `promptvm auth login` (or `--device` on headless machines), then re-run `promptvm setup`", err)
				}
			}

			// ---- 2. Context Sync ------------------------------------------
			if skipSync {
				fmt.Fprintln(out, "- sync: skipped (--skip-sync)")
			} else {
				scope := "project"
				if _, inRepo := gitutil.Detect(""); !inRepo {
					scope = "user"
					fmt.Fprintln(out, "note: not inside a git repository — configuring Context Sync at user scope")
				}
				if err := runSyncInit(cmd, syncInitOptions{
					Workspace: workspace,
					Directory: "captures",
					Scope:     scope,
					Mode:      mode,
					EventsCSV: eventsCSV,
				}); err != nil {
					return fmt.Errorf("context sync setup failed: %w", err)
				}
			}

			// ---- 3. MCP registration ---------------------------------------
			if skipMCP {
				fmt.Fprintln(out, "- mcp: skipped (--skip-mcp)")
			} else {
				results, err := runMCPInstall(cmd, mcpInstallOptions{
					Target:         "all",
					Scope:          "project",
					MCPURL:         mcpURL,
					SkipUndetected: true,
				})
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: MCP registration failed: %v\n", err)
				} else {
					_ = printMCPInstallResults(cmd, results)
				}
			}

			// ---- 4. Agent skill --------------------------------------------
			ensureAgentSkill(cmd)

			// ---- 5. Summary ---------------------------------------------
			fmt.Fprintln(out, "\nSetup complete. Verify with:")
			fmt.Fprintln(out, "  promptvm sync status")
			fmt.Fprintln(out, "  promptvm auth status")
			return nil
		},
	}

	cmd.Flags().BoolVar(&yes, "yes", false, "Assume defaults; never prompt (default true when stdin is not a TTY)")
	cmd.Flags().BoolVar(&skipMCP, "skip-mcp", false, "Skip MCP client registration")
	cmd.Flags().BoolVar(&skipSync, "skip-sync", false, "Skip Context Sync setup")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Target workspace UUID, slug, or name (default: your account default)")
	cmd.Flags().StringVar(&mode, "mode", "summary", "Capture mode: summary|metadata|transcript")
	cmd.Flags().StringVar(&eventsCSV, "events", "SessionEnd,PreCompact", "Capture events (comma-separated)")
	cmd.Flags().StringVar(&mcpURL, "mcp-url", "", "MCP endpoint override (default: derived from the API base URL; env PROMPTVM_MCP_URL)")
	cmd.Flags().BoolVar(&device, "device", false, "Use the device authorization grant for login (headless / SSH / CI)")
	cmd.Flags().BoolVar(&printAgentPrompt, "print-agent-prompt", false, "Print the copy-paste prompt for Claude Code/Codex and exit")

	return cmd
}

// ensureAgentSkill best-effort installs the bundled promptvm agent skill for
// all detected agents at user scope. An already-present skill (any version) is
// reported, never overwritten — `promptvm agent install --force` updates it.
func ensureAgentSkill(cmd *cobra.Command) {
	out := cmd.OutOrStdout()
	var installed []agentskill.InstalledTarget
	for _, t := range agentskill.AllTargets() {
		res, err := agentskill.Install(agentskill.ScopeUser, []agentskill.Target{t}, false)
		switch {
		case err == nil && len(res) > 0:
			installed = append(installed, res...)
			fmt.Fprintf(out, "✓ skill: %s → %s\n", t.Key, res[0].Path)
		case err != nil && strings.Contains(err.Error(), "already installed"):
			fmt.Fprintf(out, "✓ skill: %s already installed (update with `promptvm agent install --force`)\n", t.Key)
		case err != nil:
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not install the agent skill for %s: %v\n", t.Key, err)
		}
	}
	// Record the install in the tracker marker (keeps first-run auto-install
	// quiet). Only written when this run actually installed something, so an
	// existing marker's target list is never clobbered.
	if len(installed) == 0 {
		return
	}
	tracker := &agentskill.Tracker{
		Name:        agentskill.Name,
		Version:     agentskill.Version,
		Checksum:    agentskill.Checksum(),
		Status:      agentskill.StatusInstalled,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}
	for _, it := range installed {
		tracker.Targets = append(tracker.Targets, agentskill.TrackedTarget(it))
	}
	_ = tracker.Save()
}

func init() {
	rootCmd.AddCommand(newSetupCmd())
}
