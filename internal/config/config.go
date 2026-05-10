package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Auth type constants for Profile.AuthType.
const (
	AuthTypeAPIKey = "api_key"
	AuthTypeOAuth  = "oauth"
)

// profileNamePattern restricts profile names to safe filesystem characters.
// This prevents directory traversal when loading/saving profiles by name.
var profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// ValidateProfileName rejects profile names that contain path separators,
// parent references, or other unsafe characters.
func ValidateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("invalid profile name %q", name)
	}
	if !profileNamePattern.MatchString(name) {
		return fmt.Errorf("invalid profile name %q: allowed characters are letters, digits, dot, dash, underscore", name)
	}
	return nil
}

// Config represents the global CLI configuration.
type Config struct {
	ActiveProfile string   `yaml:"active_profile"`
	Defaults      Defaults `yaml:"defaults"`
}

// Defaults holds default CLI settings.
type Defaults struct {
	Output    string `yaml:"output"`
	NoColor   bool   `yaml:"no_color"`
	Workspace string `yaml:"workspace,omitempty"`
}

// Profile represents a named authentication profile.
//
// Profiles support two authentication modes selected by AuthType:
//   - "api_key" (legacy): the credential pair (PublicKey + SecretKey) is
//     stored explicitly. The combined APIKey field is retained for
//     backward compatibility with profiles written by older CLI builds.
//   - "oauth" (SSO): no access/refresh tokens live in this file; tokens
//     are stored in the OS keychain keyed by TokenRef. Only metadata
//     (expiry, user id/email) is persisted here.
type Profile struct {
	Name         string `yaml:"name"`
	APIKey       string `yaml:"api_key,omitempty"`
	PublicKey    string `yaml:"public_key,omitempty"`
	SecretKey    string `yaml:"secret_key,omitempty"`
	BaseURL      string `yaml:"base_url"`
	Environment  string `yaml:"environment"`
	Organization string `yaml:"organization,omitempty"`

	// OAuth / SSO metadata. Empty for legacy API-key profiles.
	AuthType  string    `yaml:"auth_type,omitempty"`
	TokenRef  string    `yaml:"token_ref,omitempty"`
	ExpiresAt time.Time `yaml:"expires_at,omitempty"`
	UserID    string    `yaml:"user_id,omitempty"`
	UserEmail string    `yaml:"user_email,omitempty"`
}

// IsOAuth reports whether the profile uses OAuth/SSO authentication.
func (p *Profile) IsOAuth() bool {
	return p.AuthType == AuthTypeOAuth
}

// dirOverride allows tests to redirect config storage.
var dirOverride string

// Dir returns the configuration directory path (~/.config/promptvm/).
func Dir() (string, error) {
	if dirOverride != "" {
		return dirOverride, nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("unable to determine config directory: %w", err)
	}
	return filepath.Join(configDir, "promptvm"), nil
}

// configPath returns the path to config.yaml.
func configPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// profilesDir returns the path to the profiles directory.
func profilesDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "profiles"), nil
}

// profilePath returns the path for a named profile file.
// The profile name is validated to prevent path traversal attacks.
func profilePath(name string) (string, error) {
	if err := ValidateProfileName(name); err != nil {
		return "", err
	}
	dir, err := profilesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".yaml"), nil
}

// Load reads the global config from disk. Returns defaults if the file doesn't exist.
func Load() (*Config, error) {
	cfg := &Config{
		ActiveProfile: "default",
		Defaults: Defaults{
			Output: "table",
		},
	}

	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return cfg, nil
}

// Save writes the global config to disk.
func (c *Config) Save() error {
	path, err := configPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

// Get returns a config value by dot-notation key.
func (c *Config) Get(key string) (string, error) {
	switch key {
	case "active_profile":
		return c.ActiveProfile, nil
	case "defaults.output":
		return c.Defaults.Output, nil
	case "defaults.no_color":
		return fmt.Sprintf("%t", c.Defaults.NoColor), nil
	case "defaults.workspace":
		return c.Defaults.Workspace, nil
	default:
		return "", fmt.Errorf("unknown config key: %s", key)
	}
}

// Set updates a config value by dot-notation key.
func (c *Config) Set(key, value string) error {
	switch key {
	case "active_profile":
		c.ActiveProfile = value
	case "defaults.output":
		if value != "table" && value != "json" && value != "yaml" {
			return fmt.Errorf("invalid output format %q: must be table, json, or yaml", value)
		}
		c.Defaults.Output = value
	case "defaults.no_color":
		if value != "true" && value != "false" {
			return fmt.Errorf("invalid value %q: must be true or false", value)
		}
		c.Defaults.NoColor = value == "true"
	case "defaults.workspace":
		c.Defaults.Workspace = value
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
	return nil
}

// AllSettings returns all config keys and their values.
func (c *Config) AllSettings() map[string]string {
	return map[string]string{
		"active_profile":    c.ActiveProfile,
		"defaults.output":   c.Defaults.Output,
		"defaults.no_color": fmt.Sprintf("%t", c.Defaults.NoColor),
		"defaults.workspace": c.Defaults.Workspace,
	}
}

// ActiveProfileData loads the currently active profile.
func (c *Config) ActiveProfileData() (*Profile, error) {
	return LoadProfile(c.ActiveProfile)
}

// SetActiveProfile updates the active profile name and saves.
func (c *Config) SetActiveProfile(name string) error {
	// Verify the profile exists
	if _, err := LoadProfile(name); err != nil {
		return err
	}
	c.ActiveProfile = name
	return c.Save()
}

// migrationWarnWriter is the destination for migration warning messages
// (non-fatal disk-failure paths). Tests swap this out so they can assert
// on emitted output. Defaults to os.Stderr.
var migrationWarnWriter io.Writer = os.Stderr

// LoadProfile reads a profile by name from disk. If the profile is in
// the legacy single-string `api_key: pk:sk` shape and there are no
// `public_key`/`secret_key` fields, LoadProfile transparently splits
// the credential and rewrites the file atomically with the dual-key
// fields populated.
//
// A migration write failure (disk full, perms, read-only FS) is
// non-fatal: a warning is logged and the in-memory Profile carries the
// split values for the duration of the session. The next start retries.
func LoadProfile(name string) (*Profile, error) {
	path, err := profilePath(name)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("profile %q not found", name)
	}
	if err != nil {
		return nil, fmt.Errorf("reading profile %q: %w", name, err)
	}

	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parsing profile %q: %w", name, err)
	}

	// Migrate legacy `api_key: pk:sk` profiles to the dual-key shape.
	if migrateLegacyAPIKey(&p) {
		if err := SaveProfile(&p); err != nil {
			fmt.Fprintf(migrationWarnWriter,
				"Warning: failed to migrate profile %q to dual-key form (%v). "+
					"Continuing with in-memory split for this session.\n",
				name, err)
		}
	}
	return &p, nil
}

// migrateLegacyAPIKey detects the legacy `api_key: pk_xxx:sk_xxx` shape
// (no `public_key`/`secret_key` fields populated) and splits the
// combined value into the new fields. Returns true if a migration was
// performed (caller should persist), false otherwise.
//
// Profiles using a non-pk:sk legacy bearer token (e.g. older `pvk_…`
// long-lived keys) are left unchanged — they're handled by the bearer
// fallback in client.ResolveCredentials.
func migrateLegacyAPIKey(p *Profile) bool {
	if p == nil {
		return false
	}
	if p.IsOAuth() {
		return false
	}
	if p.PublicKey != "" || p.SecretKey != "" {
		return false
	}
	if p.APIKey == "" {
		return false
	}
	parts := strings.Split(p.APIKey, ":")
	if len(parts) != 2 {
		return false
	}
	pk, sk := parts[0], parts[1]
	if !strings.HasPrefix(pk, "pk_") || !strings.HasPrefix(sk, "sk_") {
		return false
	}
	p.PublicKey = pk
	p.SecretKey = sk
	// Clear the legacy combined field so future loads don't keep
	// re-running the migration. The new fields are the canonical home.
	p.APIKey = ""
	return true
}

// SaveProfile writes a profile to disk atomically, with 0600
// permissions on POSIX systems (best-effort on Windows).
//
// Atomic write semantics: the YAML is written to a sibling .tmp file,
// fsync'd, and renamed over the destination. A partial profile file is
// never observable to a concurrent reader.
func SaveProfile(p *Profile) error {
	path, err := profilePath(p.Name)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("creating profiles directory: %w", err)
	}

	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("marshaling profile: %w", err)
	}

	return atomicWriteFile(path, data, 0600)
}

// atomicWriteFile writes data to path atomically: it creates a sibling
// `<path>.tmp` file with the requested permissions, writes the bytes,
// fsyncs both the file and its parent directory, then renames over the
// final destination. On Windows the chmod is best-effort because NTFS
// does not honor POSIX permission bits; the rename is still atomic on
// the same volume.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// On any failure path, do our best to clean up the temp file.
	cleanup := func() { _ = os.Remove(tmpPath) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("closing temp file: %w", err)
	}
	// Apply the requested perm. On Windows this is best-effort.
	if err := os.Chmod(tmpPath, perm); err != nil && runtime.GOOS != "windows" {
		cleanup()
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("renaming temp file: %w", err)
	}
	// fsync the directory so the rename is durable.
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// DeleteProfile removes a profile file from disk.
func DeleteProfile(name string) error {
	path, err := profilePath(name)
	if err != nil {
		return err
	}

	if err := os.Remove(path); os.IsNotExist(err) {
		return fmt.Errorf("profile %q not found", name)
	} else if err != nil {
		return fmt.Errorf("removing profile %q: %w", name, err)
	}
	return nil
}

// ListProfiles returns all saved profiles.
func ListProfiles() ([]*Profile, error) {
	dir, err := profilesDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("listing profiles: %w", err)
	}

	var profiles []*Profile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		name := entry.Name()[:len(entry.Name())-len(".yaml")]
		p, err := LoadProfile(name)
		if err != nil {
			continue // skip malformed profiles
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// MaskAPIKey returns a masked version of an API key for display.
func MaskAPIKey(key string) string {
	if len(key) <= 8 {
		return "****"
	}
	return key[:8] + "****" + key[len(key)-6:]
}
