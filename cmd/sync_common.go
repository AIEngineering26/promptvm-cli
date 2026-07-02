package cmd

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/capture"
	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/hooks"
	"github.com/AIEngineering26/promptvm-cli/internal/manifest"
	"github.com/AIEngineering26/promptvm-cli/internal/spool"
	"github.com/spf13/cobra"
)

// validSyncScopes is the single scope vocabulary used across the manifest and
// the settings writer (DX-4).
var validSyncScopes = []string{"local", "project", "user"}

// scopeToManifest maps the user-facing scope to a manifest.Scope.
func scopeToManifest(scope string) (manifest.Scope, error) {
	switch scope {
	case "local":
		return manifest.ScopeLocal, nil
	case "project":
		return manifest.ScopeProject, nil
	case "user":
		return manifest.ScopeUser, nil
	default:
		return "", fmt.Errorf("invalid --scope %q: must be one of local|project|user", scope)
	}
}

// scopeToHooks maps the user-facing scope to a hooks.Scope. The user manifest
// tier pairs with the user settings.json; project ↔ project, local ↔ local.
func scopeToHooks(scope string) (hooks.Scope, error) {
	switch scope {
	case "local":
		return hooks.ScopeLocal, nil
	case "project":
		return hooks.ScopeProject, nil
	case "user":
		return hooks.ScopeUser, nil
	default:
		return "", fmt.Errorf("invalid --scope %q: must be one of local|project|user", scope)
	}
}

// workspaceItem is the minimal shape returned by GET /api/v1/me/workspaces.
type workspaceItem struct {
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

// uuidPattern matches a canonical 8-4-4-4-12 UUID (any variant, case-insensitive).
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// isUUID reports whether s is a canonical UUID. Workspace identifiers are
// ALWAYS persisted/transmitted as UUIDs (cross-repo contract); names/slugs
// must be normalized before anything is written to disk.
func isUUID(s string) bool {
	return uuidPattern.MatchString(s)
}

// fetchWorkspaces returns the caller's workspaces via GET /api/v1/me/workspaces.
func fetchWorkspaces(caller *api.Caller) ([]workspaceItem, error) {
	if caller == nil {
		return nil, fmt.Errorf("not authenticated")
	}
	var resp struct {
		Data []workspaceItem `json:"data"`
	}
	if err := caller.Get("/api/v1/me/workspaces", &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// fetchDefaultWorkspaceID returns the caller's default workspace UUID via
// GET /api/v1/me (defaultWorkspaceId), tolerating both the enveloped
// ({data:{…}}) and flat response shapes.
func fetchDefaultWorkspaceID(caller *api.Caller) string {
	if caller == nil {
		return ""
	}
	var resp struct {
		DefaultWorkspaceID string `json:"defaultWorkspaceId"`
		Data               struct {
			DefaultWorkspaceID string `json:"defaultWorkspaceId"`
		} `json:"data"`
	}
	if err := caller.Get("/api/v1/me", &resp); err != nil {
		return ""
	}
	if resp.DefaultWorkspaceID != "" {
		return resp.DefaultWorkspaceID
	}
	return resp.Data.DefaultWorkspaceID
}

// normalizeWorkspace guarantees the returned workspace identifier is a UUID.
// A non-UUID value is looked up against GET /api/v1/me/workspaces by slug or
// case-insensitive name. The returned display name is best-effort ("" when the
// listing was unavailable but the value was already a UUID).
func normalizeWorkspace(caller *api.Caller, value string) (id, name string, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", fmt.Errorf("no workspace specified")
	}
	items, listErr := fetchWorkspaces(caller)
	if isUUID(value) {
		for _, w := range items {
			if strings.EqualFold(w.ID, value) {
				return w.ID, w.Name, nil
			}
		}
		if listErr != nil {
			return value, "", nil // trust the UUID when the listing is unavailable
		}
		// The listing succeeded and the UUID is not in it (e.g. a stale
		// defaults.workspace from another org): fail now with the available
		// workspaces instead of writing a manifest the backend will reject.
		return "", "", fmt.Errorf("workspace %q not found in your organization. Available workspaces:\n%s", value, formatWorkspaceList(items))
	}
	if listErr != nil {
		return "", "", fmt.Errorf("workspace %q is not a UUID and the workspace list could not be fetched to resolve it: %w", value, listErr)
	}
	var matches []workspaceItem
	for _, w := range items {
		if w.Slug != "" && strings.EqualFold(w.Slug, value) {
			matches = append(matches, w)
			continue
		}
		if strings.EqualFold(w.Name, value) {
			matches = append(matches, w)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0].ID, matches[0].Name, nil
	case 0:
		return "", "", fmt.Errorf("workspace %q not found. Available workspaces:\n%s", value, formatWorkspaceList(items))
	default:
		return "", "", fmt.Errorf("workspace %q is ambiguous (matches %d workspaces). Use the UUID instead:\n%s", value, len(matches), formatWorkspaceList(matches))
	}
}

// formatWorkspaceList renders workspaces as "  name (slug) — uuid" lines.
func formatWorkspaceList(items []workspaceItem) string {
	if len(items) == 0 {
		return "  (none)"
	}
	var b strings.Builder
	for i, w := range items {
		if i > 0 {
			b.WriteString("\n")
		}
		label := w.Name
		if w.Slug != "" {
			label += " (" + w.Slug + ")"
		}
		if w.IsDefault {
			label += " [default]"
		}
		b.WriteString("  " + label + " — " + w.ID)
	}
	return b.String()
}

// resolveSyncWorkspace resolves the target workspace for context-sync with the
// precedence: explicit value (--workspace flag) → config.Defaults.Workspace →
// GET /api/v1/me defaultWorkspaceId → GET /api/v1/me/workspaces default. The
// resolved value is ALWAYS normalized to a UUID (names/slugs are looked up via
// /me/workspaces); only UUIDs are ever persisted.
func resolveSyncWorkspace(caller *api.Caller, explicit string) (id, name string, err error) {
	raw := ""
	if strings.TrimSpace(explicit) != "" {
		raw = strings.TrimSpace(explicit)
	} else if cfg, cfgErr := config.Load(); cfgErr == nil && cfg.Defaults.Workspace != "" {
		raw = cfg.Defaults.Workspace
	} else if v := fetchDefaultWorkspaceID(caller); v != "" {
		raw = v
	} else if items, listErr := fetchWorkspaces(caller); listErr == nil {
		raw = pickDefaultWorkspace(items)
	}
	if raw == "" {
		return "", "", fmt.Errorf("no workspace specified. Use --workspace, set a default " +
			"(promptvm config set defaults.workspace <uuid>), or run `promptvm auth login`")
	}
	return normalizeWorkspace(caller, raw)
}

// flushSpoolForWorkspace uploads every pending spool entry for the workspace
// using its stored capture credential. Returns (flushed, remaining-for-ws).
func flushSpoolForWorkspace(cmd *cobra.Command, workspaceID string) (int, int) {
	entries, err := spool.List()
	if err != nil {
		return 0, 0
	}
	flushed, remaining := 0, 0
	for _, e := range entries {
		if workspaceID != "" && e.WorkspaceID != workspaceID {
			continue
		}
		caller, cerr := captureCaller(cmd, e.WorkspaceID)
		if cerr != nil || caller == nil {
			remaining++
			continue
		}
		if _, ierr := capture.Ingest(caller, e.Payload); ierr == nil {
			markCaptured(e.ClaudeSessionID)
			_ = e.Remove()
			flushed++
		} else {
			_ = e.MarkAttempt()
			remaining++
		}
	}
	return flushed, remaining
}

// pickDefaultWorkspace chooses the default workspace (isDefault), falling back
// to the first one.
func pickDefaultWorkspace(items []workspaceItem) string {
	for _, w := range items {
		if w.IsDefault {
			return w.ID
		}
	}
	if len(items) > 0 {
		return items[0].ID
	}
	return ""
}

// loadCaptureCredential reports whether a stored capture credential exists for
// the workspace (without exposing the secret).
func loadCaptureCredential(workspace string) (bool, error) {
	cred, err := capture.LoadCredential(workspace)
	if err != nil {
		return false, err
	}
	return cred != nil, nil
}

// captureCaller builds an api.Caller bound to the workspace's stored capture
// credential (DX-3). It never touches the OS keychain. Returns (nil, nil) when
// no credential is stored so the caller can decide to spool.
func captureCaller(cmd *cobra.Command, workspace string) (*api.Caller, error) {
	cred, err := capture.LoadCredential(workspace)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, nil
	}
	base := api.AnonymousFromContext(cmd) // resolves base URL only
	base.PublicKey = cred.PublicKey
	base.SecretKey = cred.SecretKey
	base.APIKey = cred.PublicKey + ":" + cred.SecretKey
	base.BearerToken = ""
	return base, nil
}
