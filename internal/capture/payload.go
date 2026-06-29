// Package capture builds and uploads context-sync session captures. It owns
// the wire contract for POST /api/v1/contexts/sessions, the canonical content
// hash, the non-interactive capture credential, and the ingest client.
package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Mode is the capture granularity.
type Mode string

const (
	ModeSummary    Mode = "summary"
	ModeMetadata   Mode = "metadata"
	ModeTranscript Mode = "transcript"
)

// Metadata is the structural provenance block sent with every capture. Field
// names are camelCase to match the canonical v1 wire contract.
type Metadata struct {
	RepoURL      string   `json:"repoUrl,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	HeadSha      string   `json:"headSha,omitempty"`
	FilesTouched []string `json:"filesTouched,omitempty"`
	Commands     []string `json:"commands,omitempty"`
	Outcome      string   `json:"outcome,omitempty"`

	// Semantic identity fields (FR-1/FR-5/FR-7). All sanitized + redacted before
	// assignment. The server persists Title→captures.title, Description→
	// captures.description, TaskAtHand→captures.first_prompt, ProjectKey→
	// captures.project_key, RepoSlug→captures.repo_slug, and prefers these over
	// its own distillation when present.
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	TaskAtHand  string `json:"taskAtHand,omitempty"`
	ProjectKey  string `json:"projectKey,omitempty"`
	RepoSlug    string `json:"repoSlug,omitempty"`
}

// IngestRequest is the POST /api/v1/contexts/sessions body. Field names match
// the canonical v1 contract exactly.
type IngestRequest struct {
	WorkspaceID     string   `json:"workspaceId"`
	DirectoryID     string   `json:"directoryId,omitempty"`
	ClaudeSessionID string   `json:"claudeSessionId"`
	Source          string   `json:"source"`
	CaptureMode     Mode     `json:"captureMode"`
	Summary         string   `json:"summary,omitempty"`
	Metadata        Metadata `json:"metadata"`
	ContentHash     string   `json:"contentHash"`
	OccurredAt      string   `json:"occurredAt"`
	// RedactionApplied records whether client-side redaction actually replaced
	// any secret span in this capture (provenance for the governance UI).
	RedactionApplied bool `json:"redactionApplied"`
	// LowSignal is a top-level governance routing hint (FR-Q4): true when the
	// session has no real user turn and no tool work. The backend maps it to
	// governance_status='low_signal' so it is excluded from the default inbox.
	// It is deliberately a sibling of summary/occurredAt — NOT under metadata —
	// and is excluded from the canonical content hash so it never perturbs dedup.
	LowSignal bool `json:"lowSignal,omitempty"`
}

// IngestResponse is the server reply. Status is one of accepted | deduped |
// superseded | disabled_by_policy | excluded.
type IngestResponse struct {
	Status           string `json:"status"`
	CaptureID        string `json:"captureId"`
	FileID           string `json:"fileId"`
	VersionID        string `json:"versionId"`
	GovernanceStatus string `json:"governanceStatus"`
}

// ComputeContentHash computes the canonical content hash for idempotency. The
// server recomputes and validates this (SEC-9: a client hash is advisory), but
// the CLI sends a matching value so retries dedupe deterministically.
//
// The hash covers the semantic content (session id, source, mode, summary,
// metadata) and deliberately excludes occurredAt so a re-run of the same
// session produces the same hash.
func (r *IngestRequest) ComputeContentHash() string {
	canonical := struct {
		ClaudeSessionID string   `json:"claudeSessionId"`
		Source          string   `json:"source"`
		CaptureMode     Mode     `json:"captureMode"`
		Summary         string   `json:"summary"`
		Metadata        Metadata `json:"metadata"`
	}{
		ClaudeSessionID: r.ClaudeSessionID,
		Source:          r.Source,
		CaptureMode:     r.CaptureMode,
		Summary:         r.Summary,
		Metadata:        r.Metadata,
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
