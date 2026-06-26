package redact

import (
	"strings"
	"testing"
)

func TestRedactProviderTokens(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"aws", "key is AKIAIOSFODNN7EXAMPLE here"},
		{"github", "token ghp_1234567890abcdefghijklmnopqrstuvwxyz done"},
		{"openai", "use sk-abcdefghijklmnopqrstuvwxyz12345 now"},
		{"stripe", "secret sk_live_abcdefghijklmnop1234 ok"},
		{"pem", "-----BEGIN RSA PRIVATE KEY-----"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Redact(c.in, nil)
			if !r.Applied {
				t.Fatalf("expected redaction for %q", c.in)
			}
			if strings.Contains(r.Text, "AKIAIOSFODNN7EXAMPLE") ||
				strings.Contains(r.Text, "ghp_1234567890") ||
				strings.Contains(r.Text, "sk-abcdefghij") ||
				strings.Contains(r.Text, "sk_live_abcdefghij") {
				t.Errorf("secret leaked: %q", r.Text)
			}
		})
	}
}

func TestRedactAssignmentKeepsKey(t *testing.T) {
	r := Redact(`API_SECRET = "hunter2supersecret"`, nil)
	if !r.Applied {
		t.Fatal("expected redaction")
	}
	if !strings.Contains(r.Text, "API_SECRET") {
		t.Errorf("key should be preserved, got %q", r.Text)
	}
	if strings.Contains(r.Text, "hunter2supersecret") {
		t.Errorf("value leaked: %q", r.Text)
	}
}

func TestRedactConnectionStringCredentials(t *testing.T) {
	cases := []struct {
		name, in, leaked, keep string
	}{
		{"postgres", "db at postgres://acme_api:pgX7f2Qv9LmZ0kPw@db.internal:5432/app done",
			"pgX7f2Qv9LmZ0kPw", "postgres://acme_api:"},
		{"mysql", "mysql://root:S3cr3tPass@10.0.0.4/orders", "S3cr3tPass", "mysql://root:"},
		{"redis-no-user", "redis://:r3disPass99@cache:6379", "r3disPass99", "redis://:"},
		{"mongodb", "mongodb://svc:m0ngoPwd123@cluster0.mongodb.net/db", "m0ngoPwd123", "mongodb://svc:"},
		{"https-basic", "curl https://user:tok3nABC@api.example.com/v1", "tok3nABC", "https://user:"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := Redact(c.in, nil)
			if !r.Applied {
				t.Fatalf("expected redaction for %q", c.in)
			}
			if strings.Contains(r.Text, c.leaked) {
				t.Errorf("password leaked: %q", r.Text)
			}
			if !strings.Contains(r.Text, c.keep) {
				t.Errorf("scheme/user context should be preserved, got %q", r.Text)
			}
			if !strings.Contains(r.Text, placeholder) {
				t.Errorf("expected placeholder, got %q", r.Text)
			}
		})
	}
}

func TestRedactPlainURLUntouched(t *testing.T) {
	// A URL with no embedded credentials must not be altered.
	in := "see https://docs.example.com/guide and http://localhost:3000/health"
	r := Redact(in, nil)
	if r.Applied || r.Text != in {
		t.Errorf("plain URLs should be untouched, got applied=%v text=%q", r.Applied, r.Text)
	}
}

func TestRedactHighEntropyToken(t *testing.T) {
	// A long random-looking base64 blob with no provider prefix.
	secret := "Zm9vYmFyYmF6cXV4MTIzNDU2Nzg5MGFiY2RlZmdoaWprbG1ub3A"
	r := Redact("value: "+secret, nil)
	if !r.Applied {
		t.Fatal("expected entropy redaction")
	}
	if strings.Contains(r.Text, secret) {
		t.Errorf("high-entropy token leaked: %q", r.Text)
	}
}

func TestRedactLeavesProseAlone(t *testing.T) {
	in := "The quick brown fox jumps over the lazy dog repeatedly today."
	r := Redact(in, nil)
	if r.Applied {
		t.Errorf("prose should not be redacted, got %q", r.Text)
	}
	if r.Text != in {
		t.Errorf("prose mutated: %q", r.Text)
	}
}

func TestRedactPathExcludeDropsLine(t *testing.T) {
	in := "line one\ncat .env > out\nline three"
	r := Redact(in, []string{"**/.env*"})
	if !r.Applied {
		t.Fatal("expected path-exclude redaction")
	}
	if strings.Contains(r.Text, ".env") {
		t.Errorf(".env line not dropped: %q", r.Text)
	}
	if !strings.Contains(r.Text, "line one") || !strings.Contains(r.Text, "line three") {
		t.Errorf("non-matching lines should survive: %q", r.Text)
	}
}

func TestShannonEntropy(t *testing.T) {
	if shannonEntropy("aaaaaaaa") > 0.01 {
		t.Errorf("uniform string should have ~0 entropy")
	}
	if shannonEntropy("abcdefABCDEF012345") < 3.0 {
		t.Errorf("varied string should have high entropy")
	}
}
