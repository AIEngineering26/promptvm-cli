package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	"github.com/AIEngineering26/promptvm-cli/internal/skills"
	promptvmgosdk "github.com/AIEngineering26/promptvm-go-sdk"
	sdkclient "github.com/AIEngineering26/promptvm-go-sdk/client"
	"github.com/spf13/cobra"
)

// skillsUpdateBody is the JSON body for PATCH /api/v1/skills/:id. It carries a
// fresh SKILL.md + bundled-file manifest so the backend advances the skill's
// version in place (same id/slug — NOT a new skill). An optional base_version
// forwards the optimistic-concurrency guard.
type skillsUpdateBody struct {
	SkillMD     string           `json:"skill_md"`
	Files       []skillFileEntry `json:"files,omitempty"`
	BaseVersion *int             `json:"base_version,omitempty"`
}

// buildSkillsUpdateBody assembles the PATCH body from the already-read SKILL.md
// bytes + uploaded-file manifest, including base_version only when set (>0).
// Split out so the flag→body wiring is unit-testable without HTTP.
func buildSkillsUpdateBody(md []byte, manifest []skillFileEntry, baseVersion int) skillsUpdateBody {
	body := skillsUpdateBody{
		SkillMD: string(md),
		Files:   manifest,
	}
	if baseVersion > 0 {
		bv := baseVersion
		body.BaseVersion = &bv
	}
	return body
}

func newSkillsUpdateCmd() *cobra.Command {
	var baseVersion int

	cmd := &cobra.Command{
		Use:   "update <id> <folder>",
		Short: "Update a skill in place (advances its version)",
		Long: "Re-uploads a folder-shaped Agent Skill against an existing skill id,\n" +
			"advancing its version in place (same id/slug — NOT a new skill). The\n" +
			"folder must contain a SKILL.md at its root; every other regular file is\n" +
			"uploaded as a bundled resource and recorded in the skill's file manifest.\n\n" +
			"Pass --base-version to guard against a concurrent update (optimistic\n" +
			"concurrency): the PATCH is rejected if the server's current version does\n" +
			"not match.",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			folder := args[1]

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

			bundled, err := skills.Walk(folder)
			if err != nil {
				return err
			}

			// The workspace is fixed by the existing skill; bundled files are
			// uploaded into it. Resolve the caller's default workspace for the
			// resource-upload flow (the PATCH itself targets the skill by id).
			wsID, err := resolveDefaultWorkspace(cmd)
			if err != nil {
				return err
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}
			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			// Upload bundled files as resources (presigned-URL flow). Track each
			// successfully-uploaded resource so we can roll back on failure — a
			// failed PATCH would otherwise leave them orphaned as loose files.
			manifest := make([]skillFileEntry, 0, len(bundled))
			uploadedResourceIDs := make([]string, 0, len(bundled))
			for i, f := range bundled {
				if output.Format(cmd) == "table" {
					fmt.Fprintf(cmd.OutOrStdout(), "Uploading file %d/%d: %s (%s)\n",
						i+1, len(bundled), f.Path, resHumanBytes(f.Size))
				}
				resID, _, err := uploadFileResource(cmd, c, wsID, f.AbsPath, f.Path)
				if err != nil {
					rollbackOrphanUpdateResources(cmd, c, uploadedResourceIDs)
					return fmt.Errorf("upload %s: %w", f.Path, err)
				}
				manifest = append(manifest, skillFileEntry{Path: f.Path, ResourceID: resID})
				uploadedResourceIDs = append(uploadedResourceIDs, resID)
			}

			body := buildSkillsUpdateBody(md, manifest, baseVersion)

			var resp skillResponse
			if err := caller.Patch("/api/v1/skills/"+id, body, &resp); err != nil {
				rollbackOrphanUpdateResources(cmd, c, uploadedResourceIDs)
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			d := resp.Data
			fmt.Fprintf(cmd.OutOrStdout(), "Updated skill %s %q (slug: %s, status: %s, %d bundled file(s))\n",
				d.ID, d.Name, d.Slug, d.Status, len(manifest))
			return nil
		},
	}

	cmd.Flags().IntVar(&baseVersion, "base-version", 0, "Optimistic-concurrency guard: reject if the server's current version differs")

	return cmd
}

// rollbackOrphanUpdateResources best-effort deletes resources that were
// uploaded as would-be bundled files before the skill PATCH failed. Without
// this, the resources linger in the workspace root as loose files. Per-resource
// failures are logged but not surfaced — the caller already holds the
// underlying skill-update error, which is the meaningful signal.
func rollbackOrphanUpdateResources(cmd *cobra.Command, c *sdkclient.Client, resourceIDs []string) {
	if len(resourceIDs) == 0 {
		return
	}
	ctx := context.Background()
	if output.Format(cmd) == "table" {
		fmt.Fprintf(cmd.ErrOrStderr(),
			"Skill update failed after uploading %d file(s); rolling back to keep the workspace clean.\n",
			len(resourceIDs))
	}
	for _, id := range resourceIDs {
		if err := c.Resources.DeleteResource(ctx, &promptvmgosdk.DeleteResourceRequest{ResourceID: id}); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "  rollback: could not delete resource %s: %v\n", id, err)
		}
	}
}

func init() {
	skillsCmd.AddCommand(newSkillsUpdateCmd())
}
