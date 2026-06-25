package cmd

import (
	"fmt"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/capture"
	"github.com/AIEngineering26/promptvm-cli/internal/config"
	"github.com/AIEngineering26/promptvm-cli/internal/hooks"
	"github.com/AIEngineering26/promptvm-cli/internal/manifest"
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

// resolveSyncWorkspace resolves the target workspace for context-sync with the
// precedence: --workspace flag → config.Defaults.Workspace → GET
// /api/v1/me/workspaces default (DX-8, a net-new fallback). The /me/workspaces
// lookup is best-effort: a failure surfaces the original "no workspace" error.
func resolveSyncWorkspace(cmd *cobra.Command, caller *api.Caller) (string, error) {
	if v, _ := cmd.Flags().GetString("workspace"); v != "" {
		return v, nil
	}
	if cfg, err := config.Load(); err == nil && cfg.Defaults.Workspace != "" {
		return cfg.Defaults.Workspace, nil
	}
	if caller != nil {
		var resp struct {
			Data []workspaceItem `json:"data"`
		}
		if err := caller.Get("/api/v1/me/workspaces", &resp); err == nil {
			if ws := pickDefaultWorkspace(resp.Data); ws != "" {
				return ws, nil
			}
		}
	}
	return "", fmt.Errorf("no workspace specified. Use --workspace, set a default " +
		"(promptvm config set defaults.workspace <id>), or run `promptvm auth login`")
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
