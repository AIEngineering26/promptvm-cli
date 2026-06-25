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

// Metadata is the structural provenance block sent with every capture.
type Metadata struct {
	RepoURL      string   `json:"repoUrl,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	HeadSha      string   `json:"headSha,omitempty"`
	FilesTouched []string `json:"filesTouched,omitempty"`
	Commands     []string `json:"commands,omitempty"`
	Outcome      string   `json:"outcome,omitempty"`
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
