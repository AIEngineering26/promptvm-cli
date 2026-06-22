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

			written, err := skills.Reconstruct(dir, skillBundle(d), downloaderFor(cmd))
			if err != nil {
				return err
			}
			for _, w := range written {
				fmt.Fprintf(cmd.OutOrStdout(), "Wrote %s (%s)\n", w.Dest, resHumanBytes(w.SizeBytes))
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Downloaded skill %q to %s (%d file(s) + SKILL.md)\n",
				d.Name, dir, len(d.Files))
			return nil
		},
	}

	return cmd
}

// skillBundle adapts a skillDetail (the skills API read shape) into the
// transport-agnostic skills.Bundle used by the shared reconstruction helper.
func skillBundle(d skillDetail) skills.Bundle {
	files := make([]skills.BundleFile, len(d.Files))
	for i, f := range d.Files {
		files[i] = skills.BundleFile{
			Path:        f.Path,
			DownloadURL: f.DownloadURL,
			SizeBytes:   f.SizeBytes,
		}
	}
	return skills.Bundle{RawSkillMD: d.RawSkillMD, Files: files}
}

// downloaderFor returns a skills.Downloader bound to the command's context so
// presigned-URL fetches honor cancellation/timeouts.
func downloaderFor(cmd *cobra.Command) skills.Downloader {
	return func(url, dest string) error { return downloadToFile(cmd, url, dest) }
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
