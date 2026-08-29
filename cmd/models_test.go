package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Suggested models from the command line.
//
// The thing worth pinning is the model REFERENCE. Model ids are
// gen_random_uuid() per environment, so the CLI has to speak provider/slug or a
// script written against one environment silently stops matching in another.
// These tests assert on what goes on the wire and what comes back out.

var lastPutPath string

func modelsServer(t *testing.T, capture *[]string) *httptest.Server {
	t.Helper()
	const catalog = `{"data":[
		{"id":"p1","slug":"anthropic","name":"Anthropic","models":[
			{"id":"m1","slug":"claude-opus-5","name":"Claude Opus 5","modality":"text"},
			{"id":"m2","slug":"claude-sonnet-5","name":"Claude Sonnet 5","modality":"text"}]},
		{"id":"p2","slug":"openai","name":"OpenAI","models":[
			{"id":"m3","slug":"gpt-image-1","name":"GPT Image 1","modality":"image"}]}
	]}`
	const versions = `{"data":[
		{"id":"v1","promptId":"p_1","versionNumber":1,"content":"x","isCurrentVersion":false,"isPublished":true,"createdAt":"2026-08-01T10:00:00Z"},
		{"id":"v2","promptId":"p_1","versionNumber":2,"content":"x","isCurrentVersion":true,"isPublished":true,"createdAt":"2026-08-02T10:00:00Z"}
	],"pagination":{"hasMore":false}}`
	const models = `{"data":[
		{"id":"m1","slug":"claude-opus-5","name":"Claude Opus 5","providerId":"p1","providerSlug":"anthropic","providerName":"Anthropic","position":0}
	]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/marketplace/ai-models":
			_, _ = w.Write([]byte(catalog))
		case strings.HasSuffix(r.URL.Path, "/recommended-models"):
			if r.Method == http.MethodPut {
				lastPutPath = r.URL.Path
			}
			if r.Method == http.MethodPut && capture != nil {
				var body struct {
					ModelIDs []string `json:"modelIds"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				*capture = body.ModelIDs
			}
			_, _ = w.Write([]byte(models))
		case strings.HasSuffix(r.URL.Path, "/versions"):
			_, _ = w.Write([]byte(versions))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	withTestEnv(t, srv.URL)
	return srv
}

func run(t *testing.T, cmd interface {
	SetContext(context.Context)
	SetArgs([]string)
	Execute() error
}, out *bytes.Buffer, args []string) {
	t.Helper()
	cmd.SetContext(context.Background())
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, out.String())
	}
}

func TestMarketplaceModels_ListsPortableSlugs(t *testing.T) {
	modelsServer(t, nil)
	cmd := newMarketplaceModelsCmd()
	addOutputFlags(cmd, "table")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	run(t, cmd, &out, nil)

	got := out.String()
	// provider/slug, not the uuid — the uuid does not survive an environment hop.
	for _, want := range []string{"anthropic/claude-opus-5", "openai/gpt-image-1", "Anthropic"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\tm1\t") {
		t.Errorf("output leads with a per-environment id:\n%s", got)
	}
}

func TestMarketplaceModels_FiltersByModality(t *testing.T) {
	modelsServer(t, nil)
	cmd := newMarketplaceModelsCmd()
	addOutputFlags(cmd, "table")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	run(t, cmd, &out, []string{"--modality", "image"})

	got := out.String()
	if !strings.Contains(got, "openai/gpt-image-1") {
		t.Errorf("image model missing:\n%s", got)
	}
	if strings.Contains(got, "claude-opus-5") {
		t.Errorf("text model survived a --modality image filter:\n%s", got)
	}
}

func TestPromptModelsSet_SendsRefsInOrderAgainstTheCurrentVersion(t *testing.T) {
	var sent []string
	modelsServer(t, &sent)

	cmd := newPromptModelsSetCmd()
	addOutputFlags(cmd, "table")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	run(t, cmd, &out, []string{"p_1", "--models", "anthropic/claude-opus-5,openai/gpt-image-1"})

	// Order is preserved: it becomes `position`, which is display order.
	if len(sent) != 2 || sent[0] != "anthropic/claude-opus-5" || sent[1] != "openai/gpt-image-1" {
		t.Errorf("modelIds = %v, want the two slugs in order", sent)
	}
	// v2 is the current version; v1 is not. Writing to the wrong one would look
	// like it worked and change nothing a listing will ever inherit.
	if !strings.Contains(lastPutPath, "/versions/v2/") {
		t.Errorf("wrote to %q, want the current version v2", lastPutPath)
	}
}

func TestPromptModelsSet_RejectsAnEmptySelection(t *testing.T) {
	modelsServer(t, nil)
	cmd := newPromptModelsSetCmd()
	addOutputFlags(cmd, "table")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetContext(context.Background())
	cmd.SetArgs([]string{"p_1"})

	// `set` with nothing to set is a mistake, not a way to clear — clearing has
	// its own verb, so the intent is always visible in the command.
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error when --models is omitted")
	}
}

func TestPromptModelsClear_SendsAnEmptyList(t *testing.T) {
	sent := []string{"sentinel"}
	modelsServer(t, &sent)

	cmd := newPromptModelsClearCmd()
	addOutputFlags(cmd, "table")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	run(t, cmd, &out, []string{"p_1"})

	if len(sent) != 0 {
		t.Errorf("modelIds = %v, want an empty list", sent)
	}
}

func TestSplitModelRefs(t *testing.T) {
	// Repeated flags and comma lists have to mean the same thing; the CLI uses
	// both idioms elsewhere (--tags is comma, --category-ids is repeatable).
	got := splitModelRefs([]string{"a/b, c/d", "", "  e/f  "})
	want := []string{"a/b", "c/d", "e/f"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestValidateModelRefs(t *testing.T) {
	// The server enforces the same shape, but answers with a schema violation
	// carrying the raw regex. A person who typed a bare slug should read a
	// sentence, not a pattern.
	for _, ok := range []string{
		"anthropic/claude-opus-5",
		"anthropic/claude-opus-4.8",
		"3f2504e0-4f89-41d3-9a0c-0305e82c3301",
	} {
		if err := validateModelRefs([]string{ok}); err != nil {
			t.Errorf("validateModelRefs(%q) = %v, want nil", ok, err)
		}
	}

	err := validateModelRefs([]string{"claude-opus-5"})
	if err == nil {
		t.Fatal("a bare slug must be refused — slugs are unique only per provider")
	}
	for _, want := range []string{"missing its provider", "provider/slug", "anthropic/claude-opus-5"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q lacks %q", err.Error(), want)
		}
	}

	if err := validateModelRefs([]string{"/no-provider"}); err == nil {
		t.Error("an empty provider must be refused")
	}
}
