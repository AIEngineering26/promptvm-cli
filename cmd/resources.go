package cmd

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	sdkclient "github.com/AIEngineering26/promptvm-go-sdk/client"
	"github.com/spf13/cobra"
)

var resourcesCmd = &cobra.Command{
	Use:     "resources",
	Aliases: []string{"res"},
	Short:   "Manage resources",
	Long:    "Upload, download, list, get, and delete file resources.",
}

func init() {
	rootCmd.AddCommand(resourcesCmd)
	resourcesCmd.AddCommand(newResListCmd())
	resourcesCmd.AddCommand(newResUploadCmd())
	resourcesCmd.AddCommand(newResGetCmd())
	resourcesCmd.AddCommand(newResDownloadCmd())
	resourcesCmd.AddCommand(newResDeleteCmd())
}

// --- list ---

func newResListCmd() *cobra.Command {
	var (
		workspace string
		promptID  string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List resources",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			if promptID != "" {
				resp, err := c.Resources.ListPromptResources(cmd.Context(), &sdk.ListPromptResourcesRequest{
					PromptID: promptID,
				})
				if err != nil {
					return err
				}
				return output.Print(cmd, resp, func(w io.Writer) error {
					return output.Table(w, []string{"ID", "NAME", "TYPE", "SIZE", "UPLOADED"}, func(tw *tabwriter.Writer) {
						for _, r := range resp.GetData() {
							fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
								r.GetID(), r.GetName(), r.GetContentType(),
								resHumanBytes(int64(r.GetSizeBytes())), output.HumanTime(r.GetCreatedAt()))
						}
					})
				})
			}

			wsID := workspace
			if wsID == "" {
				wsID, err = resolveDefaultWorkspace(cmd)
				if err != nil {
					return err
				}
			}

			resp, err := c.Resources.ListWorkspaceResources(cmd.Context(), &sdk.ListWorkspaceResourcesRequest{
				WorkspaceID: wsID,
			})
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "NAME", "TYPE", "SIZE", "UPLOADED"}, func(tw *tabwriter.Writer) {
					for _, r := range resp.GetData() {
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
							r.GetID(), r.GetName(), r.GetContentType(),
							resHumanBytes(int64(r.GetSizeBytes())), output.HumanTime(r.GetCreatedAt()))
					}
				})
			})
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "Filter by workspace")
	cmd.Flags().StringVar(&promptID, "prompt", "", "Filter by prompt")
	return cmd
}

// --- upload ---

func newResUploadCmd() *cobra.Command {
	var (
		promptID  string
		workspace string
	)

	cmd := &cobra.Command{
		Use:   "upload <file> [file...]",
		Short: "Upload file(s)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Expand globs
			var files []string
			for _, pattern := range args {
				matches, err := filepath.Glob(pattern)
				if err != nil {
					return fmt.Errorf("invalid glob pattern %q: %w", pattern, err)
				}
				if len(matches) == 0 {
					return fmt.Errorf("no files match %q", pattern)
				}
				files = append(files, matches...)
			}

			wsID := workspace
			if wsID == "" {
				var err error
				wsID, err = resolveDefaultWorkspace(cmd)
				if err != nil {
					return err
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			if len(files) > 1 {
				fmt.Fprintf(cmd.ErrOrStderr(), "Uploading %d files...\n", len(files))
			}

			var totalSize int64
			for _, file := range files {
				resID, size, err := uploadSingleFile(cmd, c, wsID, file)
				if err != nil {
					return fmt.Errorf("upload %s: %w", filepath.Base(file), err)
				}
				totalSize += size

				if promptID != "" {
					_, attachErr := c.Resources.AttachPromptResource(cmd.Context(), &sdk.AttachPromptResourceRequest{
						PromptID:   promptID,
						ResourceID: resID,
					})
					if attachErr != nil {
						fmt.Fprintf(cmd.ErrOrStderr(), "Warning: uploaded %s but failed to attach to prompt: %v\n", resID, attachErr)
					} else if output.Format(cmd) == "table" {
						fmt.Fprintf(cmd.OutOrStdout(), "Attached to prompt %s\n", promptID)
					}
				}
			}

			if len(files) > 1 && output.Format(cmd) == "table" {
				fmt.Fprintf(cmd.OutOrStdout(), "Uploaded %d resources (%s total)\n", len(files), resHumanBytes(totalSize))
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&promptID, "prompt", "", "Attach to prompt")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Target workspace")
	return cmd
}

func uploadSingleFile(cmd *cobra.Command, c *sdkclient.Client, wsID, filePath string) (string, int64, error) {
	resID, size, err := uploadFileResource(cmd, c, wsID, filePath, filepath.Base(filePath))
	if err != nil {
		return "", 0, err
	}
	if output.Format(cmd) == "table" {
		fmt.Fprintf(cmd.OutOrStdout(), "Uploaded resource %s (%s)\n", resID, resHumanBytes(size))
	}
	return resID, size, nil
}

// uploadFileResource runs the three-step resource upload flow (initiate →
// PUT to presigned URL → confirm) without printing a summary line, so it can
// be reused by commands that render their own per-file output (e.g. skills
// upload). displayName labels the progress bar and is used as the resource
// name.
func uploadFileResource(cmd *cobra.Command, c *sdkclient.Client, wsID, filePath, displayName string) (string, int64, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return "", 0, err
	}
	size := info.Size()
	name := displayName

	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// 1. Initiate upload — get presigned URL
	initResp, err := c.Resources.InitiateResourceUpload(cmd.Context(), &sdk.InitiateResourceUploadRequest{
		WorkspaceID: wsID,
		Name:        name,
		ContentType: contentType,
		SizeBytes:   int(size),
	})
	if err != nil {
		return "", 0, fmt.Errorf("initiate upload: %w", err)
	}

	data := initResp.GetData()
	if data == nil {
		return "", 0, fmt.Errorf("empty upload response")
	}

	// 2. Upload file bytes to presigned URL with progress
	fileData, err := os.ReadFile(filePath)
	if err != nil {
		return "", 0, err
	}

	pw := output.NewProgressWriter(io.Discard, size, name)
	bodyReader := io.TeeReader(bytes.NewReader(fileData), pw)

	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPut, data.GetPresignedURL(), bodyReader)
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", contentType)
	req.ContentLength = size

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("upload to S3: %w", err)
	}
	resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", 0, fmt.Errorf("S3 upload failed with status %d", resp.StatusCode)
	}

	// 3. Confirm upload
	_, err = c.Resources.ConfirmResourceUpload(cmd.Context(), &sdk.ConfirmResourceUploadRequest{
		ResourceID: data.GetResourceID(),
	})
	if err != nil {
		return "", 0, fmt.Errorf("confirm upload: %w", err)
	}

	return data.GetResourceID(), size, nil
}

// --- get ---

func newResGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get resource metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.Resources.GetResource(cmd.Context(), &sdk.GetResourceRequest{
				ResourceID: args[0],
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			d := resp.GetData()
			if d == nil {
				return fmt.Errorf("resource not found")
			}
			printField(cmd, "ID", d.GetID())
			printField(cmd, "Name", d.GetName())
			printField(cmd, "Content Type", d.GetContentType())
			printField(cmd, "Size", resHumanBytes(int64(d.GetSizeBytes())))
			printField(cmd, "Workspace", d.GetWorkspaceID())
			printField(cmd, "Created", output.HumanTime(d.GetCreatedAt()))
			if len(d.GetTags()) > 0 {
				printField(cmd, "Tags", strings.Join(d.GetTags(), ", "))
			}
			return nil
		},
	}
	return cmd
}

// --- download ---

func newResDownloadCmd() *cobra.Command {
	var outputPath string

	cmd := &cobra.Command{
		Use:   "download <id>",
		Short: "Download a resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			// Get resource metadata for filename
			meta, err := c.Resources.GetResource(cmd.Context(), &sdk.GetResourceRequest{
				ResourceID: args[0],
			})
			if err != nil {
				return err
			}
			d := meta.GetData()
			if d == nil {
				return fmt.Errorf("resource not found")
			}

			// Get download URL
			dlResp, err := c.Resources.GetResourceDownloadURL(cmd.Context(), &sdk.GetResourceDownloadURLRequest{
				ResourceID: args[0],
			})
			if err != nil {
				return err
			}
			dlData := dlResp.GetData()
			if dlData == nil {
				return fmt.Errorf("no download URL returned")
			}

			// Determine output file path. Always sanitize the server-supplied
			// resource name to prevent path-traversal if an attacker controls it.
			safeName := sanitizeFilename(d.GetName())
			destPath := safeName
			if outputPath != "" {
				info, statErr := os.Stat(outputPath)
				if statErr == nil && info.IsDir() {
					destPath = filepath.Join(outputPath, safeName)
				} else {
					destPath = outputPath
				}
			}

			// Download from presigned URL
			httpReq, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, dlData.GetURL(), nil)
			if err != nil {
				return err
			}
			httpResp, err := http.DefaultClient.Do(httpReq)
			if err != nil {
				return fmt.Errorf("download: %w", err)
			}
			defer httpResp.Body.Close()

			if httpResp.StatusCode >= 400 {
				return fmt.Errorf("download failed with status %d", httpResp.StatusCode)
			}

			outFile, err := os.Create(destPath)
			if err != nil {
				return err
			}
			defer outFile.Close()

			pw := output.NewProgressWriter(outFile, httpResp.ContentLength, filepath.Base(destPath))
			if _, err := io.Copy(pw, httpResp.Body); err != nil {
				return fmt.Errorf("writing file: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Saved to %s (%s)\n", destPath, resHumanBytes(httpResp.ContentLength))
			return nil
		},
	}

	cmd.Flags().StringVarP(&outputPath, "output", "O", "", "Output directory or file (default: current dir)")
	return cmd
}

// --- delete ---

func newResDeleteCmd() *cobra.Command {
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a resource",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !output.Confirm(fmt.Sprintf("Delete resource %s?", args[0])) {
					fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
					return nil
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			if err := c.Resources.DeleteResource(cmd.Context(), &sdk.DeleteResourceRequest{
				ResourceID: args[0],
			}); err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted resource %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	return cmd
}

// sanitizeFilename strips any directory component and empty/dot-only names from
// server-supplied filenames so they can be safely joined to a user-chosen
// output directory without risk of path traversal.
func sanitizeFilename(name string) string {
	// Replace any path separators and keep only the base component.
	base := filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	// Reject traversal and empty components.
	if base == "" || base == "." || base == ".." || base == "/" {
		return "resource"
	}
	return base
}

func resHumanBytes(b int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
