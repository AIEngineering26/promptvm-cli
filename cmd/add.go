package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/installs"
	"github.com/AIEngineering26/promptvm-cli/internal/prompt"
	"github.com/spf13/cobra"
)

// slugPattern bounds each add reference segment to lowercase kebab-case. This is
// the single security chokepoint that keeps a hostile reference (e.g.
// "../../etc") from ever reaching filepath.Join when building the install
// target — only [a-z0-9-] segments are accepted, so "/", "..", "\" and
// absolute paths are rejected before the name is used as a file/directory name.
var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)

const (
	// resolveTimeout bounds the anonymous resolve GET. A slow network surfaces
	// as a friendly "could not reach the marketplace" message rather than a
	// hang.
	resolveTimeout = 10 * time.Second
	// installCounterTimeout bounds the best-effort install-counter POST. The
	// install has already succeeded by the time it fires, so it stays short.
	installCounterTimeout = 2 * time.Second
)

// isTTYFunc is indirected so tests can force the interactive / non-interactive
// branch without a real terminal. It reports whether stdin is a character
// device (a terminal), which is true for interactive shells and false when
// piped or run under CI / npx without a TTY.
var isTTYFunc = func() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// confirmOverwriteFunc is indirected so tests can simulate the y/N answer.
var confirmOverwriteFunc = prompt.Confirm

// errInstallCancelled is returned when the user declines an overwrite prompt.
var errInstallCancelled = errors.New("Installation cancelled.") //nolint:staticcheck // PRD-mandated user-facing message

// resolveResponse is the unified marketplace resolve payload
// (GET /api/v1/marketplace/resolve?ref=…). Every content kind shares this
// envelope; the kind-specific fields live in Content, which the per-kind
// installer decodes.
type resolveResponse struct {
	Ref         string          `json:"ref"`
	Kind        string          `json:"kind"`
	ListingID   string          `json:"listingId"`
	FileID      string          `json:"fileId"`
	Name        string          `json:"name"`
	ResolvedVia string          `json:"resolvedVia"`
	Creator     resolveCreator  `json:"creator"`
	Content     json.RawMessage `json:"content"`
}

type resolveCreator struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// ambiguousError is the 409 AMBIGUOUS_REF body: a bare name owned by multiple
// creators, with the disambiguating candidate refs to retry.
type ambiguousError struct {
	Err        string   `json:"error"`
	Ref        string   `json:"ref"`
	Candidates []string `json:"candidates"`
	Message    string   `json:"message"`
}

func newAddCmd() *cobra.Command {
	var (
		force   bool
		dryRun  bool
		scope   string
		baseDir string
		stdout  bool
	)

	cmd := &cobra.Command{
		Use:   "add <ref>",
		Short: "Install any marketplace content into your local Claude Code config",
		Long: "Resolves a marketplace item by reference (no login required) and installs\n" +
			"it into the right Claude Code target for its kind:\n\n" +
			"  skill    → .claude/skills/<name>/SKILL.md (+ bundled files)\n" +
			"  agent    → .claude/agents/<name>.md\n" +
			"  command  → .claude/commands/<name>.md\n" +
			"  prompt   → .claude/prompts/<name>.md (or --stdout)\n" +
			"  hook     → merged into .claude/settings.json\n" +
			"  settings → merged into .claude/settings.json\n" +
			"  mcp      → merged into .mcp.json\n\n" +
			"Accepts a bare <name>, a legacy <name>-<id8> slug, or the namespaced\n" +
			"creator/name form (e.g. claude-code-templates/frontend-design).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, err := parseAddRef(args[0])
			if err != nil {
				return err
			}

			// Resolve the content anonymously via the unified endpoint.
			caller := api.AnonymousFromContext(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), resolveTimeout)
			defer cancel()

			var resp resolveResponse
			if err := caller.GetWithContext(ctx, resolvePath(ref), &resp); err != nil {
				return mapResolveError(err, args[0])
			}

			opts := installOptions{
				scope:   scope,
				baseDir: baseDir,
				force:   force,
				dryRun:  dryRun,
				stdout:  stdout,
			}
			if err := dispatchInstall(cmd, resp, opts); err != nil {
				return err
			}

			// Best-effort install counter — never fails the install. Skipped on
			// dry-run (nothing was written). Uses the canonical ref echoed by the
			// server so the count is attributed to the resolved item.
			if !dryRun {
				fireInstallCounter(cmd, caller, canonicalRef(resp, ref))
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite existing content without prompting")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the changes without writing anything")
	cmd.Flags().StringVar(&scope, "scope", "user", "Install scope: user (~/.claude) or project (./.claude)")
	cmd.Flags().BoolVar(&stdout, "stdout", false, "For prompt kind: print the body to stdout instead of writing a file")
	// Hidden override for tests; defaults to the resolved scope root's .claude dir.
	cmd.Flags().StringVar(&baseDir, "skills-dir", "", "Override the install root (.claude) directory (advanced/testing)")
	_ = cmd.Flags().MarkHidden("skills-dir")

	return cmd
}

// installOptions carries the shared install knobs to each per-kind installer.
type installOptions struct {
	scope   string
	baseDir string
	force   bool
	dryRun  bool
	stdout  bool
}

// dispatchInstall routes a resolved item to the installer for its kind. Unknown
// kinds are rejected with a clear message rather than silently ignored.
func dispatchInstall(cmd *cobra.Command, resp resolveResponse, opts installOptions) error {
	switch resp.Kind {
	case "skill":
		return installSkillKind(cmd, resp, opts)
	case "agent":
		return installMarkdownKind(cmd, resp, opts, "agents", "raw_agent_md", "agent")
	case "command":
		return installMarkdownKind(cmd, resp, opts, "commands", "raw_command_md", "command")
	case "prompt":
		return installPromptKind(cmd, resp, opts)
	case "hook":
		return installHookKind(cmd, resp, opts)
	case "settings":
		return installSettingsKind(cmd, resp, opts)
	case "mcp":
		return installMCPKind(cmd, resp, opts)
	default:
		return fmt.Errorf("Unsupported content kind %q for %q — upgrade the CLI to install it.", resp.Kind, resp.Name) //nolint:staticcheck // PRD-mandated user-facing message
	}
}

// parseAddRef validates an add argument and returns the normalized reference to
// pass to the unified resolve endpoint. It accepts a bare "name" (legacy vanity
// or name-<id8> slug) or the disambiguating "creator/name" form (FR-12). More
// than one slash, or an empty/invalid segment, is rejected locally so a hostile
// reference never reaches the filesystem or the URL builder.
func parseAddRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", errors.New("a content reference is required")
	}
	parts := strings.Split(ref, "/")
	switch len(parts) {
	case 1:
		if !slugPattern.MatchString(parts[0]) {
			return "", invalidRefError(ref)
		}
		return parts[0], nil
	case 2:
		// Both segments must be clean kebab-case. Validating them here is what
		// prevents path traversal in the install target and keeps the URL safe.
		if !slugPattern.MatchString(parts[0]) || !slugPattern.MatchString(parts[1]) {
			return "", invalidRefError(ref)
		}
		return parts[0] + "/" + parts[1], nil
	default:
		return "", invalidRefError(ref)
	}
}

// capitalize upper-cases the first byte of an ASCII kind name for user messages
// ("skill" → "Skill"). Kinds are always lowercase ASCII, so byte-wise is safe.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}
	return s
}

func invalidRefError(ref string) error {
	return fmt.Errorf("invalid reference %q: expected <name> or <creator>/<name> using lowercase letters, numbers, and hyphens", ref)
}

// resolvePath builds the unified resolve GET path. The whole ref (bare or
// namespaced) is url-encoded into the ?ref= query param.
func resolvePath(ref string) string {
	return "/api/v1/marketplace/resolve?ref=" + url.QueryEscape(ref)
}

// canonicalRef prefers the canonical ref the server echoed (namespaced when
// composable) so the install counter and tracker key stay stable regardless of
// which alias the user typed; it falls back to the input ref.
func canonicalRef(resp resolveResponse, input string) string {
	if resp.Ref != "" {
		return resp.Ref
	}
	return input
}

// resolveClaudeRoot returns the .claude root for the given scope. user →
// ~/.claude, project → ./.claude. An explicit override (baseDir) wins and is
// treated as the .claude root itself, so tests can point at a temp dir.
func resolveClaudeRoot(scope, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	switch scope {
	case "", "user":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		}
		return filepath.Join(home, ".claude"), nil
	case "project":
		return ".claude", nil
	default:
		return "", fmt.Errorf("invalid --scope %q: expected user or project", scope)
	}
}

// decideOverwrite resolves the collision policy: --force always overwrites;
// otherwise an interactive TTY gets a y/N prompt and a non-TTY aborts with a
// --force hint. Returns (true, nil) to overwrite, (false, nil) to cancel via
// prompt denial, or a terminal error for the non-TTY-no-force case.
func decideOverwrite(cmd *cobra.Command, name, kind string, force bool) (bool, error) {
	if force {
		return true, nil
	}
	if !isTTYFunc() {
		return false, fmt.Errorf("%s %q already exists. Pass --force to overwrite.", capitalize(kind), name) //nolint:staticcheck // PRD-mandated user-facing message
	}
	ok, err := confirmOverwriteFunc(fmt.Sprintf("Overwrite existing %s '%s'? (y/N)", kind, name))
	if err != nil {
		// A cancelled prompt is a denial, not a hard error.
		if errors.Is(err, prompt.ErrCancelled) {
			return false, nil
		}
		return false, err
	}
	return ok, nil
}

// mapResolveError translates a resolve failure into the PRD-mandated, stack-free
// user message: 409 → ambiguous (with candidates); 404/other HTTP → not found;
// transport/timeout → unreachable.
func mapResolveError(err error, ref string) error {
	var se *api.StatusError
	if errors.As(err, &se) {
		if se.StatusCode == 409 {
			if amb := parseAmbiguous(se.Body); amb != nil && len(amb.Candidates) > 0 {
				return fmt.Errorf("%q matches multiple creators. Did you mean: %s?", ref, strings.Join(amb.Candidates, ", ")) //nolint:staticcheck // PRD-mandated user-facing message
			}
			return fmt.Errorf("%q is ambiguous — retry with a creator/name reference.", ref) //nolint:staticcheck // PRD-mandated user-facing message
		}
		// 404 (not public / missing) and any other HTTP-level rejection map to
		// "not found" — the ref is the user's only handle on the content.
		return fmt.Errorf("%q not found on the marketplace", ref) //nolint:staticcheck // PRD-mandated user-facing message
	}
	// Any transport error (DNS, refused connection, context deadline) maps to
	// the connectivity message.
	return errors.New("Could not reach the marketplace — check your connection.") //nolint:staticcheck // PRD-mandated user-facing message
}

// parseAmbiguous decodes a 409 AMBIGUOUS_REF body; returns nil if it does not
// parse as one.
func parseAmbiguous(body string) *ambiguousError {
	var a ambiguousError
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		return nil
	}
	if a.Err != "AMBIGUOUS_REF" {
		return nil
	}
	return &a
}

// mapWriteError translates a filesystem failure during an install into the
// "Cannot write to <path>: <reason>" form.
func mapWriteError(err error, target string) error {
	return fmt.Errorf("Cannot write to %s: %s", target, err) //nolint:staticcheck // PRD-mandated user-facing message
}

// fireInstallCounter best-effort increments the unified public install counter.
// Any failure (including the endpoint not existing yet) is swallowed; with
// --verbose a single debug line is printed to stderr.
func fireInstallCounter(cmd *cobra.Command, caller *api.Caller, ref string) {
	ctx, cancel := context.WithTimeout(context.Background(), installCounterTimeout)
	defer cancel()

	path := "/api/v1/marketplace/resolve/install?ref=" + url.QueryEscape(ref)
	if err := caller.PostBestEffort(ctx, path, nil); err != nil {
		if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
			fmt.Fprintf(cmd.ErrOrStderr(), "debug: install counter call failed (non-fatal): %v\n", err)
		}
	}
}

// recordInstall writes a best-effort entry to the generic install tracker so a
// future `promptvm list/remove/update` has provenance. Never fails the install.
func recordInstall(cmd *cobra.Command, root string, resp resolveResponse, ref, target string) {
	if err := installs.Record(root, installs.Entry{
		Name:        resp.Name,
		Ref:         ref,
		Kind:        resp.Kind,
		Target:      target,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
			fmt.Fprintf(cmd.ErrOrStderr(), "debug: install tracker write failed (non-fatal): %v\n", err)
		}
	}
}

// installName is the safe on-disk name for a resolved item: the server's `name`
// (already a clean name segment) validated against slugPattern as defense in
// depth before it is ever joined into a path. Falls back to the ref's last
// segment when the server omits a name.
func installName(resp resolveResponse) (string, error) {
	name := strings.TrimSpace(resp.Name)
	if name == "" {
		if resp.Ref != "" {
			parts := strings.Split(resp.Ref, "/")
			name = parts[len(parts)-1]
		}
	}
	if name == "" || !slugPattern.MatchString(name) {
		return "", fmt.Errorf("the marketplace returned an unsafe name %q for this item", resp.Name)
	}
	return name, nil
}

func init() {
	rootCmd.AddCommand(newAddCmd())
}
