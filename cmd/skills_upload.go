package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/AIEngineering26/promptvm-cli/internal/skills"
	"github.com/spf13/cobra"
)

// skillsCreateBody is the JSON body for POST /api/v1/skills.
type skillsCreateBody struct {
	SkillMD     string           `json:"skill_md"`
	WorkspaceID string           `json:"workspaceId"`
	Files       []skillFileEntry `json:"files,omitempty"`
	DirectoryID string           `json:"directoryId,omitempty"`
	IsPublic    *bool            `json:"isPublic,omitempty"`
	Status      string           `json:"status,omitempty"`
	Tags        []string         `json:"tags,omitempty"`
}

func newSkillsUploadCmd() *cobra.Command {
	var (
		workspace string
		directory string
		status    string
		tags      string
		isPublic  bool
	)

	cmd := &cobra.Command{
		Use:     "upload <folder>",
		Aliases: []string{"create"},
		Short:   "Upload a skill folder",
		Long: "Uploads a folder-shaped Agent Skill. The folder must contain a SKILL.md\n" +
			"at its root; every other regular file is uploaded as a bundled resource\n" +
			"and recorded in the skill's file manifest under its relative path.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			folder := args[0]
			info, err := os.Stat(folder)
			if err != nil {
				return err
			}
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory — skills are folder-shaped (SKILL.md + bundled files)", folder)
			}

			// Read SKILL.md literally (byte-preserving) and validate just
			// enough frontmatter before uploading anything.
			md, err := skills.ReadSkillMD(folder)
			if err != nil {
				return err
			}
			fm, err := skills.ParseFrontmatter(md)
			if err != nil {
				return err
			}
			if err := skills.ValidateName(fm.Name); err != nil {
				return err
			}
			if status != "draft" && status != "published" {
				return fmt.Errorf("invalid --status %q: must be draft or published", status)
			}

			bundled, err := skills.Walk(folder)
			if err != nil {
				return err
			}

			wsID := workspace
			if wsID == "" {
				wsID, err = resolveDefaultWorkspace(cmd)
				if err != nil {
					return err
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}
			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			// Upload bundled files as resources (presigned-URL flow).
			manifest := make([]skillFileEntry, 0, len(bundled))
			for i, f := range bundled {
				if output.Format(cmd) == "table" {
					fmt.Fprintf(cmd.OutOrStdout(), "Uploading file %d/%d: %s (%s)\n",
						i+1, len(bundled), f.Path, resHumanBytes(f.Size))
				}
				resID, _, err := uploadFileResource(cmd, c, wsID, f.AbsPath, f.Path)
				if err != nil {
					return fmt.Errorf("upload %s: %w", f.Path, err)
				}
				manifest = append(manifest, skillFileEntry{Path: f.Path, ResourceID: resID})
			}

			body := skillsCreateBody{
				SkillMD:     string(md),
				WorkspaceID: wsID,
				Files:       manifest,
				DirectoryID: directory,
				Status:      status,
			}
			if cmd.Flags().Changed("public") {
				body.IsPublic = &isPublic
			}
			if tags != "" {
				body.Tags = strings.Split(tags, ",")
			}

			var resp skillResponse
			if err := caller.Post("/api/v1/skills", body, &resp); err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			d := resp.Data
			fmt.Fprintf(cmd.OutOrStdout(), "Created skill %s %q (slug: %s, status: %s, %d bundled file(s))\n",
				d.ID, d.Name, d.Slug, d.Status, len(manifest))
			if d.Description != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Description: %s\n", d.Description)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Target workspace ID (default: config defaults.workspace)")
	cmd.Flags().StringVar(&directory, "directory", "", "Target directory ID")
	cmd.Flags().StringVar(&status, "status", "draft", "Initial status (draft|published)")
	cmd.Flags().BoolVar(&isPublic, "public", false, "Make skill public")
	cmd.Flags().StringVar(&tags, "tags", "", "Comma-separated tags")

	return cmd
}

func init() {
	skillsCmd.AddCommand(newSkillsUploadCmd())
}
