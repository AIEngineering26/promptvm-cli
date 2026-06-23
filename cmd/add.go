package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/prompt"
	"github.com/AIEngineering26/promptvm-cli/internal/skills"
	"github.com/spf13/cobra"
)

// slugPattern bounds an add reference segment to lowercase kebab-case. This is
// the single security chokepoint that keeps a hostile reference (e.g.
// "../../etc") from ever reaching filepath.Join when building the install
// target — only [a-z0-9-] segments are accepted, so "/", "..", "\" and
// absolute paths are rejected before the slug is used as a directory name.
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

func newAddCmd() *cobra.Command {
	var (
		force   bool
		dryRun  bool
		scope   string
		baseDir string
	)

	cmd := &cobra.Command{
		Use:   "add <slug>",
		Short: "Install a marketplace skill into your local Claude Code skills directory",
		Long: "Resolves a marketplace skill by slug (no login required) and writes\n" +
			"SKILL.md plus every bundled file into ~/.claude/skills/<slug>/.\n\n" +
			"Accepts a bare <slug> or the disambiguating creator/slug form.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			creator, slug, err := parseAddRef(args[0])
			if err != nil {
				return err
			}

			// Target install directory: ~/.claude/skills/<slug>/ by default.
			root, err := resolveSkillsRoot(scope, baseDir)
			if err != nil {
				return err
			}
			target := filepath.Join(root, slug)

			// Resolve the skill bundle anonymously.
			caller := api.AnonymousFromContext(cmd)
			ctx, cancel := context.WithTimeout(cmd.Context(), resolveTimeout)
			defer cancel()

			// The PUBLIC slug endpoint returns the skill object at the top
			// level (un-wrapped) — unlike the authenticated by-id endpoint used
			// by `skills download`, which nests it under `data`. Decode directly
			// into skillDetail.
			var resp skillDetail
			if err := caller.GetWithContext(ctx, resolveSkillPath(creator, slug), &resp); err != nil {
				return mapResolveError(err, args[0])
			}
			if resp.RawSkillMD == "" {
				return fmt.Errorf("Skill %q returned no content", args[0])
			}
			bundle := skillBundle(resp)

			// Collision handling.
			if _, statErr := os.Stat(target); statErr == nil && !dryRun {
				ok, decideErr := decideOverwrite(cmd, slug, force)
				if decideErr != nil {
					return decideErr
				}
				if !ok {
					return errors.New("Installation cancelled.")
				}
				// Overwrite: clear the existing folder so stale files don't
				// linger from a previous version.
				if err := os.RemoveAll(target); err != nil {
					return fmt.Errorf("Cannot write to %s: %s", target, err)
				}
			}

			if dryRun {
				_, fileDests, planErr := skills.PlanReconstruct(target, bundle)
				if planErr != nil {
					return planErr
				}
				// SKILL.md + bundled files.
				total := len(fileDests) + 1
				fmt.Fprintf(cmd.OutOrStdout(), "Dry-run: would install %d files to %s\n", total, target)
				fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", filepath.Join(target, "SKILL.md"))
				for _, d := range fileDests {
					fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", d)
				}
				return nil
			}

			if _, err := skills.Reconstruct(target, bundle, downloaderFor(cmd)); err != nil {
				return mapWriteError(err, target)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Installed skill %q to %s (%d file(s) + SKILL.md)\n",
				slug, target, len(bundle.Files))

			// Best-effort install counter — never fails the install.
			fireInstallCounter(cmd, caller, creator, slug)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing skill without prompting")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview the files that would be written without writing anything")
	cmd.Flags().StringVar(&scope, "scope", "user", "Install scope: user (~/.claude/skills) or project (./.claude/skills)")
	// Hidden override for tests; defaults to the resolved scope root.
	cmd.Flags().StringVar(&baseDir, "skills-dir", "", "Override the skills root directory (advanced/testing)")
	_ = cmd.Flags().MarkHidden("skills-dir")

	return cmd
}

// parseAddRef splits an add argument into an optional creator segment and the
// skill slug. It accepts a bare "slug" or the disambiguating "creator/slug"
// form (FR-12). More than one slash, or an empty segment, is rejected.
func parseAddRef(ref string) (creator, slug string, err error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", "", errors.New("a skill slug is required")
	}
	parts := strings.Split(ref, "/")
	switch len(parts) {
	case 1:
		if !slugPattern.MatchString(parts[0]) {
			return "", "", invalidRefError(ref)
		}
		return "", parts[0], nil
	case 2:
		// Both segments must be clean kebab-case. Validating the slug here is
		// what prevents path traversal in the install target; the creator is
		// held to the same rule for consistency and safe URL building.
		if !slugPattern.MatchString(parts[0]) || !slugPattern.MatchString(parts[1]) {
			return "", "", invalidRefError(ref)
		}
		return parts[0], parts[1], nil
	default:
		return "", "", invalidRefError(ref)
	}
}

func invalidRefError(ref string) error {
	return fmt.Errorf("invalid reference %q: expected <slug> or <creator>/<slug> using lowercase letters, numbers, and hyphens", ref)
}

// resolveSkillPath builds the public resolve path. The bare-slug form hits
// GET /api/v1/skills/s/:slug; a creator segment is passed through as a query
// param so the backend can disambiguate when slugs aren't globally unique.
func resolveSkillPath(creator, slug string) string {
	base := "/api/v1/skills/s/" + url.PathEscape(slug)
	if creator != "" {
		return base + "?creator=" + url.QueryEscape(creator)
	}
	return base
}

// resolveSkillsRoot returns the skills root directory for the given scope.
// user → ~/.claude/skills, project → ./.claude/skills. An explicit override
// (baseDir) wins, used by tests.
func resolveSkillsRoot(scope, override string) (string, error) {
	if override != "" {
		return override, nil
	}
	switch scope {
	case "", "user":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("cannot resolve home directory: %w", err)
		}
		return filepath.Join(home, ".claude", "skills"), nil
	case "project":
		return filepath.Join(".claude", "skills"), nil
	default:
		return "", fmt.Errorf("invalid --scope %q: expected user or project", scope)
	}
}

// decideOverwrite resolves the collision policy: --force always overwrites;
// otherwise an interactive TTY gets a y/N prompt and a non-TTY aborts with a
// --force hint. Returns (true, nil) to overwrite, (false, nil) to cancel via
// prompt denial, or a terminal error for the non-TTY-no-force case.
func decideOverwrite(cmd *cobra.Command, slug string, force bool) (bool, error) {
	if force {
		return true, nil
	}
	if !isTTYFunc() {
		return false, fmt.Errorf("Skill %q already exists. Pass --force to overwrite.", slug)
	}
	ok, err := confirmOverwriteFunc(fmt.Sprintf("Overwrite existing skill '%s'? (y/N)", slug))
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
// user message: 404 → not found; transport/timeout → unreachable.
func mapResolveError(err error, ref string) error {
	var se *api.StatusError
	if errors.As(err, &se) {
		// 404 (not public / missing) and any other HTTP-level rejection map
		// to "not found" — the slug is the user's only handle on the skill.
		return fmt.Errorf("Skill %q not found on the marketplace", ref)
	}
	// Any transport error (DNS, refused connection, context deadline) maps to
	// the connectivity message.
	return errors.New("Could not reach the marketplace — check your connection.")
}

// mapWriteError translates a filesystem failure during reconstruction into the
// "Cannot write to <path>: <reason>" form.
func mapWriteError(err error, target string) error {
	return fmt.Errorf("Cannot write to %s: %s", target, err)
}

// fireInstallCounter best-effort increments the public install counter. Any
// failure (including the endpoint not existing yet) is swallowed; with
// --verbose a single debug line is printed to stderr.
func fireInstallCounter(cmd *cobra.Command, caller *api.Caller, creator, slug string) {
	ctx, cancel := context.WithTimeout(context.Background(), installCounterTimeout)
	defer cancel()

	path := "/api/v1/skills/s/" + url.PathEscape(slug) + "/install"
	if creator != "" {
		path += "?creator=" + url.QueryEscape(creator)
	}
	if err := caller.PostBestEffort(ctx, path, nil); err != nil {
		if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
			fmt.Fprintf(cmd.ErrOrStderr(), "debug: install counter call failed (non-fatal): %v\n", err)
		}
	}
}

func init() {
	rootCmd.AddCommand(newAddCmd())
}
