package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A share link is a PUBLIC projection. The backend deliberately withholds
// internal identifiers — prompt id, slug, status, isPublic, workspace and
// author ids — and pins that in "serializes only the public projection".
//
// This test exists because nothing covered what the command PRINTS. When the
// backend narrowed the response, the server stopped sending those fields and
// the SDK decoded the absent JSON into zero values. printField skips empty
// strings, so Prompt ID and Status silently vanished — but a false bool is not
// an empty string, so `share get` printed "Public: false" on every share link,
// asserting something the endpoint does not report at all. Nothing failed; the
// output was just wrong. Asserting on rendered output is what catches that.
func TestShareGet_PrintsOnlyPublicProjection(t *testing.T) {
	const body = `{"data":{
		"name":"Shared Prompt",
		"description":"a description",
		"kind":"instance",
		"contentKind":"prompt",
		"tags":["tag1"],
		"authorName":"Ada",
		"sharedVersionNumber":3,
		"createdAt":"2026-08-01T10:00:00Z",
		"currentVersion":{"versionNumber":3,"content":"Hello"}
	}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/share/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	withTestEnv(t, srv.URL)

	cmd := newShareGetCmd()
	cmd.SetContext(context.Background())
	addOutputFlags(cmd, "table")
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"tok123"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\noutput: %s", err, out.String())
	}

	got := out.String()

	for _, want := range []string{"Shared Prompt", "instance", "prompt", "Ada", "3"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q\n--- output ---\n%s", want, got)
		}
	}

	// The fields the endpoint does not return must not be rendered as labels
	// with nothing after them.
	for _, gone := range []string{"Prompt ID", "Status", "Public"} {
		if strings.Contains(got, gone) {
			t.Errorf("output still prints %q, which the public projection omits\n--- output ---\n%s", gone, got)
		}
	}
}
