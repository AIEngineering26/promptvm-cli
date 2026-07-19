package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AIEngineering26/promptvm-cli/internal/hooks"
	"github.com/AIEngineering26/promptvm-cli/internal/skills"
	"github.com/spf13/cobra"
)

// ─── skill ───────────────────────────────────────────────────────────────────

// installSkillKind writes a skill bundle to .claude/skills/<name>/ (SKILL.md +
// bundled files), reusing the shared skills.Reconstruct path so behavior is
// identical to the previous skills-only `add` and `skills download`.
func installSkillKind(cmd *cobra.Command, resp resolveResponse, opts installOptions) error {
	name, err := installName(resp)
	if err != nil {
		return err
	}
	root, err := resolveClaudeRoot(opts.scope, opts.baseDir)
	if err != nil {
		return err
	}
	target := filepath.Join(root, "skills", name)

	var d skillDetail
	if err := json.Unmarshal(resp.Content, &d); err != nil {
		return fmt.Errorf("Skill %q returned malformed content: %s", name, err) //nolint:staticcheck // PRD-mandated user-facing message
	}
	if d.RawSkillMD == "" {
		return fmt.Errorf("Skill %q returned no content", name) //nolint:staticcheck // PRD-mandated user-facing message
	}
	bundle := skillBundle(d)

	// Collision handling (folder target).
	if _, statErr := os.Stat(target); statErr == nil && !opts.dryRun {
		ok, decideErr := decideOverwrite(cmd, name, "skill", opts.force)
		if decideErr != nil {
			return decideErr
		}
		if !ok {
			return errInstallCancelled
		}
		if err := os.RemoveAll(target); err != nil {
			return mapWriteError(err, target)
		}
	}

	if opts.dryRun {
		_, fileDests, planErr := skills.PlanReconstruct(target, bundle)
		if planErr != nil {
			return planErr
		}
		total := len(fileDests) + 1
		fmt.Fprintf(cmd.OutOrStdout(), "Dry-run: would install %d files to %s\n", total, target)
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", filepath.Join(target, "SKILL.md"))
		for _, dest := range fileDests {
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", dest)
		}
		return nil
	}

	if _, err := skills.Reconstruct(target, bundle, downloaderFor(cmd)); err != nil {
		return mapWriteError(err, target)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Installed skill %q to %s (%d file(s) + SKILL.md)\n", name, target, len(bundle.Files))
	recordInstall(cmd, root, resp, canonicalRef(resp, name), target)
	return nil
}

// ─── agent / command (frontmatter-md, single file) ──────────────────────────

// installMarkdownKind writes a frontmatter-md kind (agent, command) as a single
// .claude/<subdir>/<name>.md file, sourced from the `content[rawField]` verbatim
// markdown. It also lays down any bundled files alongside (agents may bundle
// resources) using the shared skills reconstruction machinery.
func installMarkdownKind(cmd *cobra.Command, resp resolveResponse, opts installOptions, subdir, rawField, kind string) error {
	name, err := installName(resp)
	if err != nil {
		return err
	}
	root, err := resolveClaudeRoot(opts.scope, opts.baseDir)
	if err != nil {
		return err
	}
	target := filepath.Join(root, subdir, name+".md")

	var raw struct {
		Body  string           `json:"body"`
		Files []skillFileEntry `json:"files"`
	}
	// Decode the verbatim markdown from the kind-specific raw field
	// (raw_agent_md / raw_command_md) plus the shared body/files.
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(resp.Content, &envelope); err != nil {
		return fmt.Errorf("%s %q returned malformed content: %s", kind, name, err) //nolint:staticcheck // PRD-mandated user-facing message
	}
	_ = json.Unmarshal(resp.Content, &raw)

	var md string
	if rawMsg, ok := envelope[rawField]; ok {
		_ = json.Unmarshal(rawMsg, &md)
	}
	if md == "" {
		md = raw.Body // fall back to the body when the raw field is absent
	}
	if md == "" {
		return fmt.Errorf("%s %q returned no content", kind, name) //nolint:staticcheck // PRD-mandated user-facing message
	}

	// Bundled files (agents may carry resources) written next to the .md in a
	// sibling folder named after the item, mirroring the skill layout.
	bundleFiles := make([]skills.BundleFile, 0, len(raw.Files))
	for _, f := range raw.Files {
		bundleFiles = append(bundleFiles, skills.BundleFile{Path: f.Path, DownloadURL: f.DownloadURL, SizeBytes: f.SizeBytes})
	}
	filesDir := filepath.Join(root, subdir, name)

	// Collision handling (single .md file).
	if _, statErr := os.Stat(target); statErr == nil && !opts.dryRun {
		ok, decideErr := decideOverwrite(cmd, name, kind, opts.force)
		if decideErr != nil {
			return decideErr
		}
		if !ok {
			return errInstallCancelled
		}
	}

	if opts.dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "Dry-run: would write %s\n", target)
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", target)
		for _, f := range bundleFiles {
			dest, perr := skills.SafeJoin(filesDir, f.Path)
			if perr != nil {
				return perr
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", dest)
		}
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return mapWriteError(err, target)
	}
	if err := os.WriteFile(target, []byte(md), 0o644); err != nil {
		return mapWriteError(err, target)
	}
	for _, f := range bundleFiles {
		dest, perr := skills.SafeJoin(filesDir, f.Path)
		if perr != nil {
			return perr
		}
		if f.DownloadURL == "" {
			return fmt.Errorf("no download URL for bundled file %s", f.Path)
		}
		if err := downloadToFile(cmd, f.DownloadURL, dest); err != nil {
			return mapWriteError(err, dest)
		}
	}

	if len(bundleFiles) > 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "Installed %s %q to %s (+%d bundled file(s))\n", kind, name, target, len(bundleFiles))
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Installed %s %q to %s\n", kind, name, target)
	}
	recordInstall(cmd, root, resp, canonicalRef(resp, name), target)
	return nil
}

// ─── prompt (free-text body) ─────────────────────────────────────────────────

// installPromptKind writes a free-text prompt to .claude/prompts/<name>.md, or
// prints it to stdout with --stdout (a pipe-friendly one-shot).
func installPromptKind(cmd *cobra.Command, resp resolveResponse, opts installOptions) error {
	name, err := installName(resp)
	if err != nil {
		return err
	}

	var raw struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(resp.Content, &raw); err != nil {
		return fmt.Errorf("Prompt %q returned malformed content: %s", name, err) //nolint:staticcheck // PRD-mandated user-facing message
	}
	if raw.Content == "" {
		return fmt.Errorf("Prompt %q returned no content", name) //nolint:staticcheck // PRD-mandated user-facing message
	}

	if opts.stdout {
		if opts.dryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Dry-run: would print prompt %q to stdout\n", name)
			return nil
		}
		fmt.Fprintln(cmd.OutOrStdout(), raw.Content)
		return nil
	}

	root, err := resolveClaudeRoot(opts.scope, opts.baseDir)
	if err != nil {
		return err
	}
	target := filepath.Join(root, "prompts", name+".md")

	if _, statErr := os.Stat(target); statErr == nil && !opts.dryRun {
		ok, decideErr := decideOverwrite(cmd, name, "prompt", opts.force)
		if decideErr != nil {
			return decideErr
		}
		if !ok {
			return errInstallCancelled
		}
	}

	if opts.dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "Dry-run: would write %s\n", target)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return mapWriteError(err, target)
	}
	if err := os.WriteFile(target, []byte(raw.Content), 0o644); err != nil {
		return mapWriteError(err, target)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Installed prompt %q to %s\n", name, target)
	recordInstall(cmd, root, resp, canonicalRef(resp, name), target)
	return nil
}

// ─── hook (settings.json hooks merge) ────────────────────────────────────────

// installHookKind merges a hook's event config into .claude/settings.json,
// reusing the hooks package merge + tracker used by `promptvm hooks install`.
func installHookKind(cmd *cobra.Command, resp resolveResponse, opts installOptions) error {
	name, err := installName(resp)
	if err != nil {
		return err
	}
	root, err := resolveClaudeRoot(opts.scope, opts.baseDir)
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(root, "settings.json")

	var raw struct {
		Config map[string]interface{} `json:"config"`
		Events []string               `json:"events"`
	}
	if err := json.Unmarshal(resp.Content, &raw); err != nil {
		return fmt.Errorf("Hook %q returned malformed content: %s", name, err) //nolint:staticcheck // PRD-mandated user-facing message
	}
	if len(raw.Config) == 0 {
		return fmt.Errorf("Hook %q has no config to install", name) //nolint:staticcheck // PRD-mandated user-facing message
	}

	settings, err := hooks.ReadSettings(settingsPath)
	if err != nil {
		return err
	}

	// Inject _slug ownership metadata into each matcher, keyed by the canonical
	// ref so uninstall/upgrade is stable regardless of the alias typed.
	slug := canonicalRef(resp, name)
	for eventName, matchers := range raw.Config {
		matcherList, ok := matchers.([]interface{})
		if !ok {
			continue
		}
		for _, m := range matcherList {
			if mMap, ok := m.(map[string]interface{}); ok {
				mMap["_slug"] = slug
			}
		}
		raw.Config[eventName] = matcherList
	}

	if opts.dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "Dry-run: would merge hook %q into %s\n", name, settingsPath)
		for eventName, matchers := range raw.Config {
			count := 0
			if ml, ok := matchers.([]interface{}); ok {
				count = len(ml)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  Event: %s (%d matchers)\n", eventName, count)
		}
		return nil
	}

	settings.MergeHook(raw.Config, slug, opts.force)
	if err := settings.Write(settingsPath); err != nil {
		return fmt.Errorf("saving settings: %w", err)
	}

	// Track in the hooks sidecar so `hooks list/uninstall` see it too.
	trackerPath := filepath.Join(root, ".promptvm-hooks.json")
	tracker, terr := hooks.LoadTrackerFromPath(trackerPath)
	if terr == nil {
		tracker.Add(hooks.TrackedHook{
			Slug:      slug,
			SourceURL: "promptvm.ai/s/" + slug,
			Events:    raw.Events,
			Checksum:  hooks.Checksum(raw.Config),
		})
		_ = tracker.Save()
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Installed hook %q into %s\n", name, settingsPath)
	recordInstall(cmd, root, resp, slug, settingsPath)
	return nil
}

// ─── settings (settings.json deep-merge) ─────────────────────────────────────

// installSettingsKind deep-merges a settings fragment into
// .claude/settings.json. Existing user keys are preserved unless --force is
// passed; without --force, a conflicting scalar/array key is skipped (and
// reported) rather than clobbered.
func installSettingsKind(cmd *cobra.Command, resp resolveResponse, opts installOptions) error {
	name, err := installName(resp)
	if err != nil {
		return err
	}
	root, err := resolveClaudeRoot(opts.scope, opts.baseDir)
	if err != nil {
		return err
	}
	settingsPath := filepath.Join(root, "settings.json")

	var raw struct {
		Settings map[string]interface{} `json:"settings"`
	}
	if err := json.Unmarshal(resp.Content, &raw); err != nil {
		return fmt.Errorf("Settings %q returned malformed content: %s", name, err) //nolint:staticcheck // PRD-mandated user-facing message
	}
	if len(raw.Settings) == 0 {
		return fmt.Errorf("Settings %q returned no keys to merge", name) //nolint:staticcheck // PRD-mandated user-facing message
	}

	existing := map[string]interface{}{}
	if data, rerr := os.ReadFile(settingsPath); rerr == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &existing); err != nil {
			return fmt.Errorf("parsing %s: %w", settingsPath, err)
		}
	}

	merged, skipped := deepMergeSettings(existing, raw.Settings, opts.force)

	if opts.dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "Dry-run: would merge settings %q into %s\n", name, settingsPath)
		for k := range raw.Settings {
			fmt.Fprintf(cmd.OutOrStdout(), "  key: %s\n", k)
		}
		for _, k := range skipped {
			fmt.Fprintf(cmd.OutOrStdout(), "  (skipped existing key %q — pass --force to overwrite)\n", k)
		}
		return nil
	}

	data, merr := json.MarshalIndent(merged, "", "  ")
	if merr != nil {
		return merr
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return mapWriteError(err, settingsPath)
	}
	tmp := settingsPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return mapWriteError(err, settingsPath)
	}
	if err := os.Rename(tmp, settingsPath); err != nil {
		os.Remove(tmp)
		return mapWriteError(err, settingsPath)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Installed settings %q into %s\n", name, settingsPath)
	for _, k := range skipped {
		fmt.Fprintf(cmd.OutOrStderr(), "note: kept existing key %q (pass --force to overwrite)\n", k)
	}
	recordInstall(cmd, root, resp, canonicalRef(resp, name), settingsPath)
	return nil
}

// deepMergeSettings recursively merges src into dst. Nested objects merge
// key-by-key; a scalar/array collision is overwritten only when force is set,
// otherwise the existing value is kept and its key reported in skipped. dst is
// returned (mutated in place) along with the list of preserved-conflict keys.
func deepMergeSettings(dst, src map[string]interface{}, force bool) (map[string]interface{}, []string) {
	var skipped []string
	for k, sv := range src {
		dv, exists := dst[k]
		if !exists {
			dst[k] = sv
			continue
		}
		dm, dok := dv.(map[string]interface{})
		sm, sok := sv.(map[string]interface{})
		if dok && sok {
			merged, sub := deepMergeSettings(dm, sm, force)
			dst[k] = merged
			skipped = append(skipped, sub...)
			continue
		}
		// Leaf conflict (scalar or array vs. existing value).
		if force {
			dst[k] = sv
		} else {
			skipped = append(skipped, k)
		}
	}
	return dst, skipped
}

// ─── mcp (.mcp.json mcpServers merge) ────────────────────────────────────────

// installMCPKind merges an MCP server entry into .mcp.json's mcpServers map,
// keyed by the item name. The server value is derived from the resolved config:
// stdio (command/args/env) or http/sse (url/type/headers), dropping the
// registry-only schema_version/name fields. Existing entries are preserved
// unless --force is passed (they are skipped and reported otherwise).
func installMCPKind(cmd *cobra.Command, resp resolveResponse, opts installOptions) error {
	name, err := installName(resp)
	if err != nil {
		return err
	}
	root, err := resolveClaudeRoot(opts.scope, opts.baseDir)
	if err != nil {
		return err
	}
	// .mcp.json lives at the project root (the parent of .claude), matching
	// where Claude Code reads it and `promptvm mcp install`.
	mcpPath := filepath.Join(filepath.Dir(root), ".mcp.json")

	var config map[string]interface{}
	if err := json.Unmarshal(resp.Content, &config); err != nil {
		return fmt.Errorf("MCP %q returned malformed content: %s", name, err) //nolint:staticcheck // PRD-mandated user-facing message
	}
	// The mcp `content` nests the raw server JSON under `config`.
	rawServer, _ := config["config"].(map[string]interface{})
	if rawServer == nil {
		return fmt.Errorf("MCP %q returned no server config", name) //nolint:staticcheck // PRD-mandated user-facing message
	}
	entry := mcpServerEntry(rawServer)
	// Prefer the entry's own name if present, else the resolved name.
	serverKey := name
	if n, ok := rawServer["name"].(string); ok && slugPattern.MatchString(n) {
		serverKey = n
	}

	doc := map[string]interface{}{}
	if data, rerr := os.ReadFile(mcpPath); rerr == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parsing %s: %w", mcpPath, err)
		}
	}
	servers, _ := doc["mcpServers"].(map[string]interface{})
	if servers == nil {
		servers = map[string]interface{}{}
	}

	if _, exists := servers[serverKey]; exists && !opts.force {
		if opts.dryRun {
			fmt.Fprintf(cmd.OutOrStdout(), "Dry-run: mcpServers[%q] already exists in %s (pass --force to overwrite)\n", serverKey, mcpPath)
			return nil
		}
		return fmt.Errorf("MCP server %q already exists in %s. Pass --force to overwrite.", serverKey, mcpPath) //nolint:staticcheck // PRD-mandated user-facing message
	}

	if opts.dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "Dry-run: would merge mcpServers[%q] into %s\n", serverKey, mcpPath)
		return nil
	}

	servers[serverKey] = entry
	doc["mcpServers"] = servers
	data, merr := json.MarshalIndent(doc, "", "  ")
	if merr != nil {
		return merr
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(mcpPath), 0o755); err != nil {
		return mapWriteError(err, mcpPath)
	}
	tmp := mcpPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return mapWriteError(err, mcpPath)
	}
	if err := os.Rename(tmp, mcpPath); err != nil {
		os.Remove(tmp)
		return mapWriteError(err, mcpPath)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Installed MCP server %q into %s\n", serverKey, mcpPath)
	recordInstall(cmd, root, resp, canonicalRef(resp, name), mcpPath)
	return nil
}

// mcpServerEntry builds the .mcp.json server value from the resolved raw config,
// keeping only the transport-relevant fields Claude Code understands (stdio:
// command/args/env; http/sse: type/url/headers) and dropping the registry-only
// schema_version/name.
func mcpServerEntry(raw map[string]interface{}) map[string]interface{} {
	entry := map[string]interface{}{}
	for _, k := range []string{"command", "args", "env", "type", "url", "headers"} {
		if v, ok := raw[k]; ok && v != nil {
			entry[k] = v
		}
	}
	// Default an http transport type when a url is present but type is omitted,
	// matching how `promptvm mcp install` writes {"type":"http","url":…}.
	if _, hasURL := entry["url"]; hasURL {
		if _, hasType := entry["type"]; !hasType {
			entry["type"] = "http"
		}
	}
	return entry
}
