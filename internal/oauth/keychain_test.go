package oauth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// TestKeychainRoundTrip saves, loads, and deletes tokens via go-keyring's
// MockInit, which is enough to cover the primary code path in CI without
// requiring an actual OS keychain.
func TestKeychainRoundTrip(t *testing.T) {
	keyring.MockInit()
	profile := "test-profile"

	st := &StoredTokens{
		AccessToken:  "at_123",
		RefreshToken: "rt_456",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
	}

	if err := SaveTokens(profile, st); err != nil {
		t.Fatalf("SaveTokens: %v", err)
	}

	got, err := LoadTokens(profile)
	if err != nil {
		t.Fatalf("LoadTokens: %v", err)
	}
	if got.AccessToken != st.AccessToken {
		t.Errorf("access = %q, want %q", got.AccessToken, st.AccessToken)
	}
	if got.RefreshToken != st.RefreshToken {
		t.Errorf("refresh = %q, want %q", got.RefreshToken, st.RefreshToken)
	}

	if err := DeleteTokens(profile); err != nil {
		t.Fatalf("DeleteTokens: %v", err)
	}

	if _, err := LoadTokens(profile); !errors.Is(err, ErrNoTokens) {
		t.Errorf("after delete, err = %v, want ErrNoTokens", err)
	}
}

// TestFileFallbackRoundTrip directly exercises the file fallback path,
// which is what runs on Linux servers without a Secret Service daemon.
// We override XDG_CONFIG_HOME to keep artifacts inside the test tempdir.
func TestFileFallbackRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// On macOS os.UserConfigDir uses ~/Library/Application Support; redirect
	// HOME too so we land inside the tempdir there as well.
	t.Setenv("HOME", dir)

	profile := "file-test"
	st := &StoredTokens{
		AccessToken:  "at_file",
		RefreshToken: "rt_file",
		ExpiresAt:    time.Now().Add(time.Hour).UTC().Truncate(time.Second),
	}

	if err := saveTokensToFile(profile, st); err != nil {
		t.Fatalf("saveTokensToFile: %v", err)
	}

	got, err := loadTokensFromFile(profile)
	if err != nil {
		t.Fatalf("loadTokensFromFile: %v", err)
	}
	if got.AccessToken != st.AccessToken {
		t.Errorf("access = %q, want %q", got.AccessToken, st.AccessToken)
	}
	if got.RefreshToken != st.RefreshToken {
		t.Errorf("refresh = %q, want %q", got.RefreshToken, st.RefreshToken)
	}

	// Sanity: the encrypted file should exist and NOT contain the plaintext token.
	path, _ := tokenFilePath(profile)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading encrypted file: %v", err)
	}
	if containsString(raw, "at_file") {
		t.Error("plaintext access token leaked into token file")
	}

	if err := deleteTokensFile(profile); err != nil {
		t.Fatalf("deleteTokensFile: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after delete: %v", err)
	}

	// Touch the master key path so the test is aware of where it lands
	// (handy if the test fails and we need to inspect artifacts).
	if _, err := os.Stat(filepath.Join(dir)); err != nil {
		t.Logf("tempdir missing: %v", err)
	}
}

// containsString reports whether needle appears as a substring of haystack.
func containsString(haystack []byte, needle string) bool {
	return len(needle) > 0 && indexBytes(haystack, []byte(needle)) >= 0
}

// indexBytes is bytes.Index, reimplemented inline to avoid an import.
func indexBytes(s, sep []byte) int {
	if len(sep) == 0 {
		return 0
	}
	for i := 0; i+len(sep) <= len(s); i++ {
		match := true
		for j := range sep {
			if s[i+j] != sep[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
