package oauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/AIEngineering26/promptvm-cli/internal/config"
)

// File fallback layout:
//
//   ~/.config/promptvm/tokens/<profile>.enc   — AES-GCM ciphertext of JSON
//   ~/.config/promptvm/tokens/.master         — 32 random bytes used as key
//
// This fallback only activates when the OS keychain is unavailable (for
// example, Linux containers with no Secret Service running). On macOS and
// Windows, the real keychain is always used and these files are never
// created.

const tokenFilePerms = 0o600
const tokenDirPerms = 0o700

// tokensDir returns the directory where fallback token files live.
func tokensDir() (string, error) {
	cfgDir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cfgDir, "tokens"), nil
}

// masterKeyPath returns the path to the fallback master key file.
func masterKeyPath() (string, error) {
	dir, err := tokensDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".master"), nil
}

// tokenFilePath returns the path to an encrypted token file for profile.
func tokenFilePath(profile string) (string, error) {
	if err := config.ValidateProfileName(profile); err != nil {
		return "", err
	}
	dir, err := tokensDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, profile+".enc"), nil
}

// loadOrCreateMasterKey reads the fallback master key, creating one with
// 0600 permissions if it doesn't exist. The key never leaves disk and
// only protects data on the same machine — the OS keychain is always
// preferred when available.
func loadOrCreateMasterKey() ([]byte, error) {
	path, err := masterKeyPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != 32 {
			return nil, fmt.Errorf("master key has unexpected length %d", len(data))
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("reading master key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), tokenDirPerms); err != nil {
		return nil, fmt.Errorf("creating tokens directory: %w", err)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating master key: %w", err)
	}
	if err := os.WriteFile(path, key, tokenFilePerms); err != nil {
		return nil, fmt.Errorf("writing master key: %w", err)
	}
	return key, nil
}

func saveTokensToFile(profile string, tokens *StoredTokens) error {
	path, err := tokenFilePath(profile)
	if err != nil {
		return err
	}
	key, err := loadOrCreateMasterKey()
	if err != nil {
		return err
	}

	plaintext, err := json.Marshal(tokens)
	if err != nil {
		return fmt.Errorf("marshaling tokens: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	sealed := aead.Seal(nonce, nonce, plaintext, nil)

	if err := os.MkdirAll(filepath.Dir(path), tokenDirPerms); err != nil {
		return err
	}
	return os.WriteFile(path, sealed, tokenFilePerms)
}

func loadTokensFromFile(profile string) (*StoredTokens, error) {
	path, err := tokenFilePath(profile)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoTokens
		}
		return nil, fmt.Errorf("reading token file: %w", err)
	}

	key, err := loadOrCreateMasterKey()
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < aead.NonceSize() {
		return nil, errors.New("token file is corrupted")
	}
	nonce := raw[:aead.NonceSize()]
	ct := raw[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypting token file: %w", err)
	}
	var st StoredTokens
	if err := json.Unmarshal(plaintext, &st); err != nil {
		return nil, fmt.Errorf("parsing token file: %w", err)
	}
	return &st, nil
}

func deleteTokensFile(profile string) error {
	path, err := tokenFilePath(profile)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
