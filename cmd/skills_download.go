package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/skills"
	"github.com/spf13/cobra"
)

func newSkillsDownloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download <id> <dir>",
		Short: "Download a skill folder",
		Long:  "Writes the skill's SKILL.md and every bundled file into <dir>,\nrecreating the original folder layout.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, dir := args[0], args[1]

			caller, err := api.NewFromContext(cmd)
			if err != nil {
				return err
			}

			var resp skillResponse
			if err := caller.Get("/api/v1/skills/"+id, &resp); err != nil {
				return err
			}
			d := resp.Data

			// Validate every manifest path before writing anything, so a
			// malicious path aborts the download instead of leaving a
			// partial folder behind.
			dests := make([]string, len(d.Files))
			for i, f := range d.Files {
				dest, err := skills.SafeJoin(dir, f.Path)
				if err != nil {
					return err
				}
				dests[i] = dest
			}

			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}

			// SKILL.md — raw bytes, verbatim.
			mdPath := filepath.Join(dir, "SKILL.md")
			if err := os.WriteFile(mdPath, []byte(d.RawSkillMD), 0o644); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (%s)\n", mdPath, resHumanBytes(int64(len(d.RawSkillMD))))

			for i, f := range d.Files {
				if f.DownloadURL == "" {
					return fmt.Errorf("no download URL for %s", f.Path)
				}
				if err := downloadToFile(cmd, f.DownloadURL, dests[i]); err != nil {
					return fmt.Errorf("download %s: %w", f.Path, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (%s)\n", dests[i], resHumanBytes(f.SizeBytes))
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Downloaded skill %q to %s (%d file(s) + SKILL.md)\n",
				d.Name, dir, len(d.Files))
			return nil
		},
	}

	return cmd
}

// downloadToFile fetches a presigned URL into dest, creating parent
// directories as needed.
func downloadToFile(cmd *cobra.Command, url, dest string) error {
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}
	return nil
}

func init() {
	skillsCmd.AddCommand(newSkillsDownloadCmd())
}
