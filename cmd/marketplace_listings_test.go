package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// addOutputFlags wires the persistent output flags the output package reads,
// so a command built directly (not via root) won't panic on Format().
func addOutputFlags(cmd *cobra.Command, format string) {
	cmd.Flags().StringP("output", "o", format, "Output format")
	cmd.Flags().Bool("compact", false, "compact json")
	cmd.Flags().Bool("no-header", false, "no header")
	cmd.Flags().Bool("wide", false, "wide")
}

// TestValidateSingleSource covers the exactly-one-source rule across all
// listing sources, including mutual exclusivity of --skill/--hook with the
// other source flags.
func TestValidateSingleSource(t *testing.T) {
	cases := []struct {
		name     string
		sources  []listingSource
		wantFlag string
		wantErr  string
	}{
		{
			name:     "prompt only",
			sources:  []listingSource{{"prompt", "p_1"}, {"collection", ""}, {"skill", ""}, {"hook", ""}, {"directory", ""}},
			wantFlag: "prompt",
		},
		{
			name:     "skill only",
			sources:  []listingSource{{"prompt", ""}, {"collection", ""}, {"skill", "sk_1"}, {"hook", ""}, {"directory", ""}},
			wantFlag: "skill",
		},
		{
			name:     "hook only",
			sources:  []listingSource{{"prompt", ""}, {"collection", ""}, {"skill", ""}, {"hook", "hk_1"}, {"directory", ""}},
			wantFlag: "hook",
		},
		{
			name:     "directory only",
			sources:  []listingSource{{"prompt", ""}, {"collection", ""}, {"skill", ""}, {"hook", ""}, {"directory", "dir_1"}},
			wantFlag: "directory",
		},
		{
			name:    "none set",
			sources: []listingSource{{"prompt", ""}, {"collection", ""}, {"skill", ""}, {"hook", ""}, {"directory", ""}},
			wantErr: "exactly one",
		},
		{
			name:    "skill and hook conflict",
			sources: []listingSource{{"prompt", ""}, {"collection", ""}, {"skill", "sk_1"}, {"hook", "hk_1"}, {"directory", ""}},
			wantErr: "mutually exclusive",
		},
		{
			name:    "skill and prompt conflict",
			sources: []listingSource{{"prompt", "p_1"}, {"collection", ""}, {"skill", "sk_1"}, {"hook", ""}, {"directory", ""}},
			wantErr: "mutually exclusive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := validateSingleSource(tc.sources)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.flag != tc.wantFlag {
				t.Errorf("source flag = %q, want %q", got.flag, tc.wantFlag)
			}
		})
	}
}

// runCreateAndCapture runs `listings create` against a stub server and
// returns the request body the server received.
func runCreateAndCapture(t *testing.T, args []string) map[string]any {
	t.Helper()
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/marketplace/listings" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"lst_1","title":"T"}}`))
	}))
	t.Cleanup(srv.Close)
	withTestEnv(t, srv.URL)

	cmd := newListingsCreateCmd()
	cmd.SetContext(context.Background())
	addOutputFlags(cmd, "table")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, out.String())
	}
	return body
}

// TestListingsCreate_Skill verifies --skill builds a skillId source body.
func TestListingsCreate_Skill(t *testing.T) {
	body := runCreateAndCapture(t, []string{
		"--skill", "sk_42", "--name", "My Skill", "--description", "desc",
	})
	if body["skillId"] != "sk_42" {
		t.Errorf("skillId = %v, want sk_42", body["skillId"])
	}
	if _, ok := body["promptId"]; ok {
		t.Errorf("promptId should be omitted, body = %v", body)
	}
	if _, ok := body["priceCents"]; ok {
		t.Errorf("priceCents should be omitted for free listing, body = %v", body)
	}
}

// TestListingsCreate_Hook verifies --hook builds a hookId source body.
func TestListingsCreate_Hook(t *testing.T) {
	body := runCreateAndCapture(t, []string{
		"--hook", "hk_7", "--name", "My Hook", "--description", "desc",
	})
	if body["hookId"] != "hk_7" {
		t.Errorf("hookId = %v, want hk_7", body["hookId"])
	}
	if _, ok := body["skillId"]; ok {
		t.Errorf("skillId should be omitted, body = %v", body)
	}
}

// TestListingsCreate_ExactlyOneSource verifies the command rejects zero and
// multiple sources before making any request.
func TestListingsCreate_ExactlyOneSource(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no source",
			args: []string{"--name", "n", "--description", "d"},
			want: "source is required",
		},
		{
			name: "skill and hook",
			args: []string{"--skill", "sk_1", "--hook", "hk_1", "--name", "n", "--description", "d"},
			want: "mutually exclusive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withTestEnv(t, "http://unused.test")
			cmd := newListingsCreateCmd()
			cmd.SetContext(context.Background())
			addOutputFlags(cmd, "table")
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

// TestFormatClaimManifest covers the createdItems manifest rendering and the
// legacy fallback.
func TestFormatClaimManifest(t *testing.T) {
	str := func(s string) *string { return &s }

	mixed := &claimResult{}
	mixed.Data.CreatedItems = &claimCreatedItems{
		Prompts:      []claimCreatedItem{{}, {}},
		Skills:       []claimCreatedItem{{}},
		Hooks:        []claimCreatedItem{{}},
		Resources:    []claimCreatedItem{{}},
		CollectionID: str("col_9"),
	}

	skillOnly := &claimResult{}
	skillOnly.Data.CreatedItems = &claimCreatedItems{Skills: []claimCreatedItem{{}}}

	legacy := &claimResult{}
	legacy.Data.ImportedPromptID = str("p_1")

	collectionLegacy := &claimResult{}
	collectionLegacy.Data.ImportedCollectionID = str("col_2")

	emptyManifestWithCollection := &claimResult{}
	emptyManifestWithCollection.Data.CreatedItems = &claimCreatedItems{CollectionID: str("col_3")}

	cases := []struct {
		name string
		in   *claimResult
		want []string
	}{
		{
			name: "mixed bundle",
			in:   mixed,
			want: []string{"Imported: 2 prompts, 1 skill, 1 hook, 1 file → collection col_9"},
		},
		{
			name: "skill only",
			in:   skillOnly,
			want: []string{"Imported: 1 skill"},
		},
		{
			name: "legacy prompt",
			in:   legacy,
			want: []string{"Imported prompt: p_1"},
		},
		{
			name: "legacy collection",
			in:   collectionLegacy,
			want: []string{"Imported collection: col_2"},
		},
		{
			name: "empty manifest with collection",
			in:   emptyManifestWithCollection,
			want: []string{"Imported collection col_3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatClaimManifest(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("lines = %#v, want %#v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("line %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestListingsClaim_Manifest exercises the claim command end to end against a
// stub server returning a mixed createdItems manifest.
func TestListingsClaim_Manifest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/claim") || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"purchaseId":"pur_1","createdItems":{"prompts":[{"newFileId":"f1"}],"skills":[{"newFileId":"s1"}],"hooks":[],"resources":[{"newResourceId":"r1"}],"collectionId":"col_5"}}}`))
	}))
	t.Cleanup(srv.Close)
	withTestEnv(t, srv.URL)

	cmd := newListingsClaimCmd()
	cmd.SetContext(context.Background())
	addOutputFlags(cmd, "table")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"lst_1", "--workspace", "ws_1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, out.String())
	}
	got := out.String()
	for _, want := range []string{"Claimed listing lst_1", "Imported: 1 prompt, 1 skill, 1 file → collection col_5"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\ngot:\n%s", want, got)
		}
	}
}
