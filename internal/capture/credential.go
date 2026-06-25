package capture

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/AIEngineering26/promptvm-cli/internal/config"
)

// Credential is the workspace-bound, least-privilege capture key the detached
// hook uses. It is NEVER the OS keychain (DX-3): a detached background process
// cannot reliably unlock the keychain, so `sync init` mints this key and stores
// it as a 0600 file in a 0700 per-user dir that `sync run` reads directly.
type Credential struct {
	PublicKey string
	SecretKey string
}

// safeWorkspace bounds a workspace id/slug to filesystem-safe characters so it
// can be used as a credential filename without path traversal.
var safeWorkspace = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// credentialDir returns the 0700 directory holding capture credentials.
func credentialDir() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sync"), nil
}

// CredentialPath returns the 0600 env file path for a workspace's capture key.
func CredentialPath(workspace string) (string, error) {
	dir, err := credentialDir()
	if err != nil {
		return "", err
	}
	name := safeWorkspace.ReplaceAllString(workspace, "_")
	if name == "" {
		name = "default"
	}
	return filepath.Join(dir, name+".env"), nil
}

// SaveCredential writes the capture credential for a workspace, creating the
// 0700 dir and a 0600 env file the hook can source.
func SaveCredential(workspace string, cred Credential) (string, error) {
	dir, err := credentialDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating credential dir: %w", err)
	}
	path, err := CredentialPath(workspace)
	if err != nil {
		return "", err
	}
	body := fmt.Sprintf("PROMPTVM_PUBLIC_KEY=%s\nPROMPTVM_SECRET_KEY=%s\n", cred.PublicKey, cred.SecretKey)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return "", fmt.Errorf("writing credential: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("renaming credential: %w", err)
	}
	return path, nil
}

// LoadCredential reads a workspace's capture credential. A missing file returns
// (nil, nil) so `sync run` can degrade to spooling rather than failing.
func LoadCredential(workspace string) (*Credential, error) {
	path, err := CredentialPath(workspace)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	cred := &Credential{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		k, v, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		switch strings.TrimSpace(k) {
		case "PROMPTVM_PUBLIC_KEY":
			cred.PublicKey = strings.TrimSpace(v)
		case "PROMPTVM_SECRET_KEY":
			cred.SecretKey = strings.TrimSpace(v)
		}
	}
	if cred.PublicKey == "" || cred.SecretKey == "" {
		return nil, nil
	}
	return cred, scanner.Err()
}
