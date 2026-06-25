package capture

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestComputeContentHashStableAndIgnoresOccurredAt(t *testing.T) {
	base := IngestRequest{
		WorkspaceID:     "ws_1",
		ClaudeSessionID: "sess_abc",
		Source:          "claude-code",
		CaptureMode:     ModeSummary,
		Summary:         "did the thing",
		Metadata:        Metadata{RepoURL: "git@x", Branch: "main"},
		OccurredAt:      "2026-06-25T00:00:00Z",
	}
	h1 := base.ComputeContentHash()

	other := base
	other.OccurredAt = "2026-06-26T11:22:33Z" // different timestamp
	h2 := other.ComputeContentHash()
	if h1 != h2 {
		t.Errorf("hash changed with occurredAt; should be stable\n%s\n%s", h1, h2)
	}

	changed := base
	changed.Summary = "did something else"
	if changed.ComputeContentHash() == h1 {
		t.Errorf("hash should change when summary changes")
	}
}

type fakePoster struct {
	gotPath string
	gotBody *IngestRequest
	resp    IngestResponse
	err     error
}

func (f *fakePoster) Post(path string, body, result interface{}) error {
	f.gotPath = path
	if r, ok := body.(*IngestRequest); ok {
		f.gotBody = r
	}
	if f.err != nil {
		return f.err
	}
	if out, ok := result.(*IngestResponse); ok {
		*out = f.resp
	}
	return nil
}

func TestIngestFillsHashAndPostsToCanonicalPath(t *testing.T) {
	fp := &fakePoster{resp: IngestResponse{Status: "accepted", CaptureID: "cap_1"}}
	req := &IngestRequest{WorkspaceID: "ws_1", ClaudeSessionID: "s1", Source: "claude-code", CaptureMode: ModeSummary}
	resp, err := Ingest(fp, req)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if fp.gotPath != IngestPath {
		t.Errorf("path = %q, want %q", fp.gotPath, IngestPath)
	}
	if req.ContentHash == "" {
		t.Errorf("ContentHash not filled before POST")
	}
	if resp.Status != "accepted" || resp.CaptureID != "cap_1" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestIngestPropagatesError(t *testing.T) {
	fp := &fakePoster{err: errors.New("boom")}
	_, err := Ingest(fp, &IngestRequest{ClaudeSessionID: "s"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCredentialRoundTrip(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	path, err := SaveCredential("ws_demo", Credential{PublicKey: "pk_x", SecretKey: "sk_y"})
	if err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}
	// 0600 perms on the file.
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("credential perms = %v, want 0600", fi.Mode().Perm())
	}
	// 0700 perms on the dir.
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("credential dir perms = %v, want 0700", di.Mode().Perm())
	}

	cred, err := LoadCredential("ws_demo")
	if err != nil {
		t.Fatalf("LoadCredential: %v", err)
	}
	if cred == nil || cred.PublicKey != "pk_x" || cred.SecretKey != "sk_y" {
		t.Errorf("loaded credential mismatch: %+v", cred)
	}
}

func TestLoadCredentialMissingReturnsNil(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("HOME", cfgHome)
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	cred, err := LoadCredential("absent")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cred != nil {
		t.Errorf("expected nil credential, got %+v", cred)
	}
}
