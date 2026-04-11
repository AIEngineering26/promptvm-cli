package oauth

import (
	"crypto/sha256"
	"encoding/base64"
	"testing"
)

func TestGenerateVerifierLength(t *testing.T) {
	v, err := GenerateVerifier()
	if err != nil {
		t.Fatalf("GenerateVerifier: %v", err)
	}
	// 32 random bytes → 43 base64url chars (no padding).
	if got := len(v); got != 43 {
		t.Errorf("verifier length = %d, want 43", got)
	}
}

func TestGenerateVerifierUnique(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		v, err := GenerateVerifier()
		if err != nil {
			t.Fatalf("GenerateVerifier: %v", err)
		}
		if _, dup := seen[v]; dup {
			t.Fatalf("verifier collision at iteration %d", i)
		}
		seen[v] = struct{}{}
	}
}

func TestChallengeMatchesSHA256(t *testing.T) {
	v := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk" // RFC 7636 §4.5 example
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := Challenge(v); got != want {
		t.Errorf("Challenge = %q, want %q", got, want)
	}
}

func TestChallengeRoundTrip(t *testing.T) {
	v, err := GenerateVerifier()
	if err != nil {
		t.Fatal(err)
	}
	c := Challenge(v)

	sum := sha256.Sum256([]byte(v))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if c != want {
		t.Errorf("Challenge = %q, want %q", c, want)
	}
}
