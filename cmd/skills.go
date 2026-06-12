package cmd

import (
	"time"

	"github.com/spf13/cobra"
)

var skillsCmd = &cobra.Command{
	Use:     "skills",
	Aliases: []string{"skill"},
	Short:   "Manage Agent Skills",
	Long:    "Upload, list, inspect, download, and delete folder-shaped Agent Skills\n(SKILL.md + bundled files, agentskills.io format).",
}

func init() {
	rootCmd.AddCommand(skillsCmd)
}

// skillFileEntry is one entry in a skill's file manifest, as accepted by
// POST /api/v1/skills and returned in SkillReadShape.files.
type skillFileEntry struct {
	Path        string `json:"path"`
	ResourceID  string `json:"resourceId"`
	SizeBytes   int64  `json:"sizeBytes,omitempty"`
	Sha256      string `json:"sha256,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
	DownloadURL string `json:"downloadUrl,omitempty"`
}

// skillDetail is the SkillReadShape returned by the skills endpoints.
type skillDetail struct {
	ID            string           `json:"id"`
	Slug          string           `json:"slug"`
	ContentKind   string           `json:"content_kind"`
	Name          string           `json:"name"`
	Description   string           `json:"description"`
	WhenToUse     string           `json:"when_to_use"`
	Tags          []string         `json:"tags"`
	Body          string           `json:"body"`
	RawSkillMD    string           `json:"raw_skill_md"`
	Sha256SkillMD string           `json:"sha256_skill_md"`
	Files         []skillFileEntry `json:"files"`
	WorkspaceID   string           `json:"workspaceId"`
	Status        string           `json:"status"`
	IsPublic      bool             `json:"isPublic"`
	CreatedAt     *time.Time       `json:"createdAt"`
	UpdatedAt     *time.Time       `json:"updatedAt"`
}

type skillResponse struct {
	Data skillDetail `json:"data"`
}
