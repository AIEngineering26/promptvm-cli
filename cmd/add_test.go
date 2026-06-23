package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AIEngineering26/promptvm-cli/internal/prompt"
	"github.com/spf13/cobra"
)

func TestParseAddRef(t *testing.T) {
	cases := []struct {
		name        string
		in          string
		wantCreator string
		wantSlug    string
		wantErr     bool
	}{
		{"bare slug", "pdf-toolkit", "", "pdf-toolkit", false},
		{"creator slug", "acme/pdf-toolkit", "acme", "pdf-toolkit", false},
		{"trims whitespace", "  pdf-toolkit  ", "", "pdf-toolkit", false},
		{"empty", "", "", "", true},
		{"too many segments", "a/b/c", "", "", true},
		{"empty creator", "/pdf", "", "", true},
		{"empty slug", "acme/", "", "", true},
		{"path traversal slug", "../../etc", "", "", true},
		{"dot segment", "..", "", "", true},
		{"absolute path", "/etc/passwd", "", "", true},
		{"backslash traversal", "..\\windows", "", "", true},
		{"uppercase rejected", "PDF-Toolkit", "", "", true},
		{"traversal in creator", "../etc/pdf", "", "", true},
		{"leading hyphen rejected", "-pdf", "", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			creator, slug, err := parseAddRef(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if creator != tc.wantCreator || slug != tc.wantSlug {
				t.Errorf("got (%q, %q), want (%q, %q)", creator, slug, tc.wantCreator, tc.wantSlug)
			}
		})
	}
}

func TestResolveSkillPath(t *testing.T) {
	cases := []struct {
		creator, slug, want string
	}{
		{"", "pdf", "/api/v1/skills/s/pdf"},
		{"acme", "pdf", "/api/v1/skills/s/pdf?creator=acme"},
	}
	for _, tc := range cases {
		if got := resolveSkillPath(tc.creator, tc.slug); got != tc.want {
			t.Errorf("resolveSkillPath(%q,%q) = %q, want %q", tc.creator, tc.slug, got, tc.want)
		}
	}
}

func TestResolveSkillsRoot(t *testing.T) {
	if got, err := resolveSkillsRoot("user", "/override"); err != nil || got != "/override" {
		t.Errorf("override should win: %q %v", got, err)
	}
	if got, err := resolveSkillsRoot("project", ""); err != nil || got != filepath.Join(".claude", "skills") {
		t.Errorf("project scope = %q %v", got, err)
	}
	if _, err := resolveSkillsRoot("bogus", ""); err == nil {
		t.Error("bogus scope should error")
	}
	home, _ := os.UserHomeDir()
	if got, err := resolveSkillsRoot("user", ""); err != nil || got != filepath.Join(home, ".claude", "skills") {
		t.Errorf("user scope = %q %v", got, err)
	}
}

// newTestAddCmd wires a standalone add command with the persistent flags the
// resolver reads, so it can be exercised without the full root command.
func newTestAddCmd() *cobra.Command {
	cmd := newAddCmd()
	cmd.Flags().String("base-url", "", "base url")
	cmd.Flags().String("public-key", "", "")
	cmd.Flags().String("secret-key", "", "")
	cmd.Flags().String("api-key", "", "")
	return cmd
}

// fakeServer returns a test server: a skill resolve endpoint with one bundled
// file (whose download URL points back at this same server), the file download
// endpoint, a 404 for "missing", and a 204 install-counter endpoint. It records
// whether the install counter was hit.
func fakeServer(t *testing.T) (*httptest.Server, *bool) {
	t.Helper()
	counterHit := false
	mux := http.NewServeMux()
	// The download URL is filled in after the server is up via the closure
	// over srv (see below); we use a pointer the handler reads at request time.
	var baseURL string
	mux.HandleFunc("/api/v1/skills/s/found", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Public slug endpoint returns the skill object UN-wrapped (no `data`
		// envelope) — must match the real API contract.
		_, _ = w.Write([]byte(`{"slug":"found","name":"Found","raw_skill_md":"---\nname: found\n---\nbody","files":[{"path":"a.txt","downloadUrl":"` + baseURL + `/dl","sizeBytes":5}]}`))
	})
	mux.HandleFunc("/api/v1/skills/s/missing", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"Not Found"}`))
	})
	mux.HandleFunc("/dl", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})
	mux.HandleFunc("/api/v1/skills/s/found/install", func(w http.ResponseWriter, r *http.Request) {
		counterHit = true
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	baseURL = srv.URL
	t.Cleanup(srv.Close)
	return srv, &counterHit
}

// runAdd builds a fresh add command pointed at srv and runs it with args,
// returning combined output and any error.
func runAdd(t *testing.T, srv *httptest.Server, installDir string, args ...string) (string, error) {
	t.Helper()
	cmd := newTestAddCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	full := append([]string{"--base-url", srv.URL, "--skills-dir", installDir}, args...)
	cmd.SetArgs(full)
	err := cmd.Execute()
	return out.String(), err
}

func TestAddInstallsSkill(t *testing.T) {
	srv, _ := fakeServer(t)
	dir := t.TempDir()

	out, err := runAdd(t, srv, dir, "found")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Installed skill \"found\"") {
		t.Errorf("output missing success line: %s", out)
	}
	md, err := os.ReadFile(filepath.Join(dir, "found", "SKILL.md"))
	if err != nil || !strings.Contains(string(md), "name: found") {
		t.Errorf("SKILL.md not written: %v %q", err, md)
	}
	a, _ := os.ReadFile(filepath.Join(dir, "found", "a.txt"))
	if string(a) != "hello" {
		t.Errorf("bundled file = %q", a)
	}
}

func TestAddFiresInstallCounter(t *testing.T) {
	srv, counterHit := fakeServer(t)
	if _, err := runAdd(t, srv, t.TempDir(), "found"); err != nil {
		t.Fatalf("add: %v", err)
	}
	// The counter is best-effort and fired in the background context; give the
	// (synchronous) PostBestEffort a moment if needed — it runs inline here.
	if !*counterHit {
		t.Error("install counter should be hit on successful install")
	}
}

func TestAddDryRunSkipsInstallCounter(t *testing.T) {
	srv, counterHit := fakeServer(t)
	if _, err := runAdd(t, srv, t.TempDir(), "found", "--dry-run"); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if *counterHit {
		t.Error("dry-run must not hit the install counter")
	}
}

func TestAddNotFound(t *testing.T) {
	srv, _ := fakeServer(t)
	out, err := runAdd(t, srv, t.TempDir(), "missing")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if !strings.Contains(err.Error(), `Skill "missing" not found on the marketplace`) {
		t.Errorf("wrong error: %v (out: %s)", err, out)
	}
}

func TestAddDryRunWritesNothing(t *testing.T) {
	srv, _ := fakeServer(t)
	dir := t.TempDir()
	out, err := runAdd(t, srv, dir, "found", "--dry-run")
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if !strings.Contains(out, "Dry-run: would install 2 files") {
		t.Errorf("missing dry-run summary: %s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "found")); !os.IsNotExist(err) {
		t.Error("dry-run must not create the skill folder")
	}
}

func TestAddCollisionNonTTYAborts(t *testing.T) {
	srv, _ := fakeServer(t)
	dir := t.TempDir()
	// Pre-create the target.
	if err := os.MkdirAll(filepath.Join(dir, "found"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Force non-TTY.
	orig := isTTYFunc
	isTTYFunc = func() bool { return false }
	defer func() { isTTYFunc = orig }()

	_, err := runAdd(t, srv, dir, "found")
	if err == nil || !strings.Contains(err.Error(), `already exists. Pass --force`) {
		t.Errorf("want already-exists error, got %v", err)
	}
}

func TestAddCollisionForceOverwrites(t *testing.T) {
	srv, _ := fakeServer(t)
	dir := t.TempDir()
	stale := filepath.Join(dir, "found", "stale.txt")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(stale, []byte("old"), 0o644)

	out, err := runAdd(t, srv, dir, "found", "--force")
	if err != nil {
		t.Fatalf("force: %v\n%s", err, out)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("force should remove stale files from the old folder")
	}
}

func TestAddCollisionPromptDenied(t *testing.T) {
	srv, _ := fakeServer(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "found"), 0o755); err != nil {
		t.Fatal(err)
	}
	origTTY := isTTYFunc
	origConfirm := confirmOverwriteFunc
	isTTYFunc = func() bool { return true }
	confirmOverwriteFunc = func(string) (bool, error) { return false, nil }
	defer func() { isTTYFunc = origTTY; confirmOverwriteFunc = origConfirm }()

	_, err := runAdd(t, srv, dir, "found")
	if err == nil || err.Error() != "Installation cancelled." {
		t.Errorf("want 'Installation cancelled.', got %v", err)
	}
}

func TestAddCollisionPromptCancelledTreatedAsDenial(t *testing.T) {
	srv, _ := fakeServer(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "found"), 0o755); err != nil {
		t.Fatal(err)
	}
	origTTY := isTTYFunc
	origConfirm := confirmOverwriteFunc
	isTTYFunc = func() bool { return true }
	confirmOverwriteFunc = func(string) (bool, error) { return false, prompt.ErrCancelled }
	defer func() { isTTYFunc = origTTY; confirmOverwriteFunc = origConfirm }()

	_, err := runAdd(t, srv, dir, "found")
	if err == nil || err.Error() != "Installation cancelled." {
		t.Errorf("want 'Installation cancelled.', got %v", err)
	}
}
