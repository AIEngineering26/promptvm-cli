package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ─── pure-function unit tests ────────────────────────────────────────────────

func TestKebabPackName(t *testing.T) {
	cases := map[string]string{
		"my-pack":     "my-pack",
		"My Pack":     "my-pack",
		"My_Pack 2":   "my-pack-2",
		"UPPER":       "upper",
		"a  b":        "a-b",
		"--edge--":    "edge",
		"日本語":         "promptvm-pack", // unicode-only → fallback
		"":            "promptvm-pack",
		".":           "promptvm-pack",
		"!!!":         "promptvm-pack",
		"trailing---": "trailing",
	}
	for in, want := range cases {
		if got := kebabPackName(in); got != want {
			t.Errorf("kebabPackName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPersonaHasSentinel(t *testing.T) {
	with := "---\nname: x\nskills: []\n---\n" + buzzManagedSentinelLine + "\n\nbody"
	if !personaHasSentinel(with) {
		t.Error("expected sentinel detected in body")
	}
	// A sentinel-looking string inside frontmatter must NOT count.
	inFM := "---\nname: x\ndescription: " + buzzManagedSentinel + "\n---\nbody"
	if personaHasSentinel(inFM) {
		t.Error("sentinel inside frontmatter should not count")
	}
	without := "---\nname: x\n---\njust a user persona"
	if personaHasSentinel(without) {
		t.Error("no sentinel expected")
	}
}

func TestSpliceSkillsList(t *testing.T) {
	// Inline empty → block list, other frontmatter + body preserved byte-for-byte.
	in := "---\nname: scout\nmodel: anthropic:claude-opus-4-8\nskills: []\ntemperature: 0.4\n---\n" +
		buzzManagedSentinelLine + "\n\nSystem prompt body.\n"
	out, changed, err := spliceSkillsList(in, []string{"skills/b", "skills/a"})
	if err != nil || !changed {
		t.Fatalf("splice: changed=%v err=%v", changed, err)
	}
	if !strings.Contains(out, "skills:\n  - skills/a\n  - skills/b\n") {
		t.Errorf("skills block not rendered sorted:\n%s", out)
	}
	for _, keep := range []string{"model: anthropic:claude-opus-4-8", "temperature: 0.4", "System prompt body."} {
		if !strings.Contains(out, keep) {
			t.Errorf("lost line %q:\n%s", keep, out)
		}
	}
	// Foreign (non skills/) entry is preserved; a stale skills/ entry is dropped.
	in2 := "---\nname: s\nskills:\n  - skills/old\n  - custom/ref\n---\nbody"
	out2, _, err := spliceSkillsList(in2, []string{"skills/new"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "- custom/ref") {
		t.Errorf("foreign entry dropped:\n%s", out2)
	}
	if strings.Contains(out2, "skills/old") {
		t.Errorf("stale skills/ entry kept:\n%s", out2)
	}
	if !strings.Contains(out2, "- skills/new") {
		t.Errorf("new skill missing:\n%s", out2)
	}
	// No skills key present → one is inserted before the frontmatter close.
	in3 := "---\nname: s\ndescription: d\n---\nbody"
	out3, changed3, err := spliceSkillsList(in3, []string{"skills/x"})
	if err != nil || !changed3 {
		t.Fatalf("insert: changed=%v err=%v", changed3, err)
	}
	if !strings.Contains(out3, "skills:\n  - skills/x\n---") {
		t.Errorf("skills not inserted before close:\n%s", out3)
	}
	// Empty union → inline [].
	out4, _, _ := spliceSkillsList("---\nname: s\nskills:\n  - skills/gone\n---\nb", nil)
	if !strings.Contains(out4, "skills: []") {
		t.Errorf("empty union should be inline []:\n%s", out4)
	}
}

// ─── AC behavior tests (no real buzz binary needed) ──────────────────────────

func readManaged(t *testing.T, pack string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(pack, "agents"))
	if err != nil {
		t.Fatalf("read agents: %v", err)
	}
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(pack, "agents", e.Name()))
		if personaHasSentinel(string(b)) {
			return string(b)
		}
	}
	return ""
}

func TestBuzzPackScaffoldsAndInstallsSkill(t *testing.T) { // AC1
	pack := filepath.Join(t.TempDir(), "my-pack")
	fixtures := map[string]resolveFixture{}
	srv, _ := fakeServer(t, fixtures)
	fixtures["found"] = skillFixture(srv.URL)

	out, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "found")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	// Manifest present + valid JSON with required fields.
	mb, err := os.ReadFile(filepath.Join(pack, ".plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(mb, &m); err != nil {
		t.Fatalf("manifest json: %v", err)
	}
	for _, k := range []string{"id", "name", "version"} {
		if s, _ := m[k].(string); strings.TrimSpace(s) == "" {
			t.Errorf("manifest missing required %q: %s", k, mb)
		}
	}
	// Skill written to <pack>/skills/found/SKILL.md.
	if _, err := os.Stat(filepath.Join(pack, "skills", "found", "SKILL.md")); err != nil {
		t.Errorf("skill not written: %v", err)
	}
	// Managed persona lists the skill.
	if p := readManaged(t, pack); !strings.Contains(p, "- skills/found") {
		t.Errorf("managed persona missing skill:\n%s", p)
	}
	if !strings.Contains(out, "buzz pack validate") {
		t.Errorf("missing validate hint: %s", out)
	}
	// No installs tracker sidecar in the pack.
	if _, err := os.Stat(filepath.Join(pack, ".promptvm-installs.json")); err == nil {
		t.Error("tracker sidecar leaked into pack")
	}
}

func TestBuzzPackSecondSkillUnionsSorted(t *testing.T) { // AC2
	pack := filepath.Join(t.TempDir(), "p")
	fixtures := map[string]resolveFixture{}
	srv, _ := fakeServer(t, fixtures)
	fixtures["found"] = skillFixture(srv.URL)
	fixtures["another"] = resolveFixture{kind: "skill", name: "another", content: map[string]interface{}{
		"raw_skill_md": "---\nname: another\n---\nbody",
	}}
	if _, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "found"); err != nil {
		t.Fatal(err)
	}
	if _, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "another"); err != nil {
		t.Fatal(err)
	}
	p := readManaged(t, pack)
	if !strings.Contains(p, "- skills/another\n  - skills/found") {
		t.Errorf("skills not unioned+sorted:\n%s", p)
	}
}

func TestBuzzPackInstallsMCPAtPackRoot(t *testing.T) { // AC3
	parent := t.TempDir()
	pack := filepath.Join(parent, "p")
	fixtures := map[string]resolveFixture{
		"my-mcp": {kind: "mcp", name: "my-mcp", content: map[string]interface{}{
			"config": map[string]interface{}{"type": "http", "url": "https://x/mcp", "name": "my-mcp"},
		}},
	}
	srv, _ := fakeServer(t, fixtures)
	if _, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "my-mcp"); err != nil {
		t.Fatal(err)
	}
	// .mcp.json is at the pack root, NOT the parent.
	if _, err := os.Stat(filepath.Join(pack, ".mcp.json")); err != nil {
		t.Errorf(".mcp.json not at pack root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, ".mcp.json")); err == nil {
		t.Error(".mcp.json leaked into parent dir")
	}
}

func TestBuzzPackCollectionWarnSkipsUnsupported(t *testing.T) { // AC4
	pack := filepath.Join(t.TempDir(), "p")
	fixtures := map[string]resolveFixture{}
	srv, _ := fakeServer(t, fixtures)
	fixtures["bundle"] = collectionFixture("bundle", "Bundle", nil, []map[string]interface{}{
		{"kind": "skill", "name": "sk", "content": map[string]interface{}{"raw_skill_md": "---\nname: sk\n---\nb"}},
		{"kind": "prompt", "name": "pr", "content": map[string]interface{}{"content": "hi"}},
	}, nil)
	out, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "bundle")
	if err != nil {
		t.Fatalf("collection: %v\n%s", err, out)
	}
	if !strings.Contains(out, `Skipped prompt "pr" (not supported in a Buzz pack)`) {
		t.Errorf("prompt not warn-skipped: %s", out)
	}
	if _, err := os.Stat(filepath.Join(pack, "skills", "sk", "SKILL.md")); err != nil {
		t.Errorf("skill member not installed: %v", err)
	}
	// A collection of ONLY unsupported kinds errors and writes no pack.
	pack2 := filepath.Join(t.TempDir(), "p2")
	fixtures["allprompt"] = collectionFixture("allprompt", "AllPrompt", nil, []map[string]interface{}{
		{"kind": "prompt", "name": "pr", "content": map[string]interface{}{"content": "hi"}},
	}, nil)
	_, err = runAddRaw(t, srv.URL, "--buzz-pack", pack2, "allprompt")
	if err == nil {
		t.Error("expected error for zero supported members")
	}
}

func TestBuzzPackPreExistingNonMutation(t *testing.T) { // AC5
	pack := filepath.Join(t.TempDir(), "p")
	// User-authored manifest + user persona (no sentinel).
	must(t, os.MkdirAll(filepath.Join(pack, ".plugin"), 0o755))
	must(t, os.MkdirAll(filepath.Join(pack, "agents"), 0o755))
	manifest := `{"id":"mine","name":"Mine","version":"9.9.9","personas":["agents/mine.persona.md"]}`
	persona := "---\nname: mine\ndisplay_name: Mine\ndescription: my agent\n---\nHand-written body\n"
	must(t, os.WriteFile(filepath.Join(pack, ".plugin", "plugin.json"), []byte(manifest), 0o644))
	must(t, os.WriteFile(filepath.Join(pack, "agents", "mine.persona.md"), []byte(persona), 0o644))

	fixtures := map[string]resolveFixture{}
	srv, _ := fakeServer(t, fixtures)
	fixtures["found"] = skillFixture(srv.URL)
	out, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "found")
	if err != nil {
		t.Fatalf("add: %v\n%s", err, out)
	}
	// Manifest + user persona byte-unchanged.
	if b, _ := os.ReadFile(filepath.Join(pack, ".plugin", "plugin.json")); string(b) != manifest {
		t.Errorf("manifest mutated:\n%s", b)
	}
	if b, _ := os.ReadFile(filepath.Join(pack, "agents", "mine.persona.md")); string(b) != persona {
		t.Errorf("user persona mutated:\n%s", b)
	}
	// Skill still installed; a hint was printed (no managed persona to update).
	if _, err := os.Stat(filepath.Join(pack, "skills", "found", "SKILL.md")); err != nil {
		t.Errorf("skill not installed: %v", err)
	}
	if !strings.Contains(out, "add it to a persona's `skills:` list") {
		t.Errorf("expected no-managed-persona hint: %s", out)
	}
	if _, err := os.Stat(filepath.Join(pack, ".promptvm-installs.json")); err == nil {
		t.Error("tracker sidecar leaked into pack")
	}
}

func TestBuzzPackManagedPersonaPreservesUserFrontmatter(t *testing.T) { // AC6
	pack := filepath.Join(t.TempDir(), "p")
	must(t, os.MkdirAll(filepath.Join(pack, ".plugin"), 0o755))
	must(t, os.MkdirAll(filepath.Join(pack, "agents"), 0o755))
	must(t, os.WriteFile(filepath.Join(pack, ".plugin", "plugin.json"),
		[]byte(`{"id":"p","name":"P","version":"0.1.0","personas":["agents/p.persona.md"]}`), 0o644))
	persona := "---\nname: p\ndisplay_name: P\ndescription: d\nmodel: anthropic:claude-opus-4-8\ntemperature: 0.4\nskills: []\ntriggers:\n  mentions: true\n  keywords:\n    - hi\n---\n" +
		buzzManagedSentinelLine + "\n\nBody stays.\n"
	must(t, os.WriteFile(filepath.Join(pack, "agents", "p.persona.md"), []byte(persona), 0o644))

	fixtures := map[string]resolveFixture{}
	srv, _ := fakeServer(t, fixtures)
	fixtures["found"] = skillFixture(srv.URL)
	if _, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "found"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(filepath.Join(pack, "agents", "p.persona.md"))
	for _, keep := range []string{"model: anthropic:claude-opus-4-8", "temperature: 0.4", "triggers:", "  mentions: true", "    - hi", "Body stays."} {
		if !strings.Contains(string(got), keep) {
			t.Errorf("lost %q after regen:\n%s", keep, got)
		}
	}
	if !strings.Contains(string(got), "- skills/found") {
		t.Errorf("skill not added:\n%s", got)
	}
}

func TestBuzzPackFlagMutualExclusion(t *testing.T) { // AC7
	pack := filepath.Join(t.TempDir(), "p")
	srv, _ := fakeServer(t, map[string]resolveFixture{})
	for _, bad := range [][]string{
		{"--buzz-pack", pack, "--scope", "user", "x"},
		{"--buzz-pack", pack, "--skills-dir", pack, "x"},
		{"--buzz-pack", pack, "--stdout", "x"},
	} {
		if _, err := runAddRaw(t, srv.URL, bad...); err == nil {
			t.Errorf("expected exclusion error for %v", bad)
		}
	}
}

func TestBuzzPackUnsupportedDirectKind(t *testing.T) { // AC8
	pack := filepath.Join(t.TempDir(), "p")
	fixtures := map[string]resolveFixture{
		"h": {kind: "hook", name: "h", content: map[string]interface{}{"config": map[string]interface{}{"PreToolUse": []interface{}{}}}},
	}
	srv, _ := fakeServer(t, fixtures)
	out, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "h")
	if err == nil || !strings.Contains(err.Error()+out, "not supported with --buzz-pack") {
		t.Errorf("expected unsupported-kind error, got err=%v out=%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(pack, ".plugin")); err == nil {
		t.Error("nothing should be written for an unsupported direct kind")
	}
}

func TestBuzzPackDryRunWritesNothing(t *testing.T) { // AC9
	pack := filepath.Join(t.TempDir(), "p")
	fixtures := map[string]resolveFixture{}
	srv, _ := fakeServer(t, fixtures)
	fixtures["found"] = skillFixture(srv.URL)
	out, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "--dry-run", "found")
	if err != nil {
		t.Fatalf("dry-run: %v\n%s", err, out)
	}
	if _, err := os.Stat(pack); err == nil {
		t.Error("dry-run created the pack dir")
	}
}

func TestBuzzPackMultipleManagedPersonasError(t *testing.T) { // AC12
	pack := filepath.Join(t.TempDir(), "p")
	must(t, os.MkdirAll(filepath.Join(pack, ".plugin"), 0o755))
	must(t, os.MkdirAll(filepath.Join(pack, "agents"), 0o755))
	must(t, os.WriteFile(filepath.Join(pack, ".plugin", "plugin.json"),
		[]byte(`{"id":"p","name":"P","version":"0.1.0","personas":["agents/a.persona.md"]}`), 0o644))
	dup := "---\nname: a\ndescription: d\nskills: []\n---\n" + buzzManagedSentinelLine + "\nbody"
	must(t, os.WriteFile(filepath.Join(pack, "agents", "a.persona.md"), []byte(dup), 0o644))
	must(t, os.WriteFile(filepath.Join(pack, "agents", "b.persona.md"), []byte(strings.Replace(dup, "name: a", "name: b", 1)), 0o644))

	fixtures := map[string]resolveFixture{}
	srv, _ := fakeServer(t, fixtures)
	fixtures["found"] = skillFixture(srv.URL)
	_, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "found")
	if err == nil || !strings.Contains(err.Error(), "multiple promptvm-managed personas") {
		t.Errorf("expected multiple-managed-persona error, got %v", err)
	}
}

// ─── real-binary oracle (skipped when `buzz` is absent) ──────────────────────

func TestBuzzPackValidatesWithRealBinary(t *testing.T) {
	bin, err := exec.LookPath("buzz")
	if err != nil {
		t.Skip("buzz binary not on PATH; skipping validate oracle")
	}
	if v, verr := exec.Command(bin, "--help").CombinedOutput(); verr == nil {
		_ = v // presence is enough; version is logged below if available
	}
	t.Logf("using buzz at %s", bin)

	pack := filepath.Join(t.TempDir(), "oracle-pack")
	fixtures := map[string]resolveFixture{}
	srv, _ := fakeServer(t, fixtures)
	fixtures["found"] = skillFixture(srv.URL)
	fixtures["my-mcp"] = resolveFixture{kind: "mcp", name: "my-mcp", content: map[string]interface{}{
		"config": map[string]interface{}{"type": "http", "url": "https://x/mcp", "name": "my-mcp"},
	}}
	if out, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "found"); err != nil {
		t.Fatalf("install skill: %v\n%s", err, out)
	}
	if out, err := runAddRaw(t, srv.URL, "--buzz-pack", pack, "my-mcp"); err != nil {
		t.Fatalf("install mcp: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "pack", "validate", pack).CombinedOutput()
	if err != nil {
		t.Fatalf("buzz pack validate failed: %v\n%s", err, out)
	}
	t.Logf("buzz pack validate: %s", out)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// runAddRaw runs a fresh add command against baseURL with only --base-url
// prepended, letting the caller control every other flag (needed for the
// --buzz-pack path, which is incompatible with runAdd's --skills-dir).
func runAddRaw(t *testing.T, baseURL string, args ...string) (string, error) {
	t.Helper()
	cmd := newTestAddCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(append([]string{"--base-url", baseURL}, args...))
	err := cmd.Execute()
	return out.String(), err
}
