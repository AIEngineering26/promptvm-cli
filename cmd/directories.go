package cmd

import (
	"fmt"
	"net/url"
	"os"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/AIEngineering26/promptvm-cli/internal/client"
	promptvmgosdk "github.com/AIEngineering26/promptvm-go-sdk"
	sdkclient "github.com/AIEngineering26/promptvm-go-sdk/client"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var directoriesCmd = &cobra.Command{
	Use:     "directories",
	Aliases: []string{"dirs"},
	Short:   "Manage directory hierarchy",
}

var dirsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List directories (tree view by default)",
	RunE:  runDirsList,
}

var dirsCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a directory",
	RunE:  runDirsCreate,
}

var dirsGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get directory details",
	Args:  cobra.ExactArgs(1),
	RunE:  runDirsGet,
}

var dirsGetWorkspace string

var dirsUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a directory",
	Args:  cobra.ExactArgs(1),
	RunE:  runDirsUpdate,
}

var dirsDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a directory",
	Args:  cobra.ExactArgs(1),
	RunE:  runDirsDelete,
}

func init() {
	rootCmd.AddCommand(directoriesCmd)
	directoriesCmd.AddCommand(dirsListCmd)
	directoriesCmd.AddCommand(dirsCreateCmd)
	directoriesCmd.AddCommand(dirsGetCmd)
	directoriesCmd.AddCommand(dirsUpdateCmd)
	directoriesCmd.AddCommand(dirsDeleteCmd)

	// list flags
	dirsListCmd.Flags().String("workspace", "", "Workspace ID (required)")
	dirsListCmd.Flags().Bool("flat", false, "Flat list instead of tree view")
	dirsListCmd.Flags().Int("depth", 0, "Max tree depth (0 = unlimited)")

	// create flags
	dirsCreateCmd.Flags().String("name", "", "Directory name (required)")
	dirsCreateCmd.Flags().String("parent", "", "Parent directory ID (omit for root)")
	dirsCreateCmd.Flags().String("workspace", "", "Workspace ID (required)")

	// get flags
	dirsGetCmd.Flags().StringVar(&dirsGetWorkspace, "workspace", "", "Workspace ID containing the directory (required)")

	// update flags
	dirsUpdateCmd.Flags().String("name", "", "New directory name")
	dirsUpdateCmd.Flags().String("parent", "", "New parent directory ID")

	// delete flags
	dirsDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
}

func runDirsList(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	workspace, _ := cmd.Flags().GetString("workspace")
	if workspace == "" {
		return fmt.Errorf("--workspace is required")
	}

	resp, err := c.Directories.ListDirectories(cmd.Context(), &promptvmgosdk.ListDirectoriesRequest{
		WorkspaceID: workspace,
	})
	if err != nil {
		return fmt.Errorf("listing directories: %w", err)
	}

	dirs := resp.GetData()
	if len(dirs) == 0 {
		fmt.Println("No directories found.")
		return nil
	}

	flat, _ := cmd.Flags().GetBool("flat")
	if flat {
		return printDirsFlat(dirs)
	}

	maxDepth, _ := cmd.Flags().GetInt("depth")
	printDirsTree(dirs, maxDepth)
	return nil
}

func printDirsFlat(dirs []*promptvmgosdk.ListDirectoriesResponseDataItem) error {
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSLUG\tPARENT\tUPDATED")
	for _, d := range dirs {
		parent := "(root)"
		if d.ParentID != nil {
			parent = *d.ParentID
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			d.ID, d.Name, d.Slug, parent, d.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return w.Flush()
}

type dirNode struct {
	dir      *promptvmgosdk.ListDirectoriesResponseDataItem
	children []*dirNode
}

func printDirsTree(dirs []*promptvmgosdk.ListDirectoriesResponseDataItem, maxDepth int) {
	byID := make(map[string]*dirNode, len(dirs))
	for _, d := range dirs {
		byID[d.ID] = &dirNode{dir: d}
	}

	var roots []*dirNode
	for _, d := range dirs {
		node := byID[d.ID]
		if d.ParentID != nil {
			if parent, ok := byID[*d.ParentID]; ok {
				parent.children = append(parent.children, node)
				continue
			}
		}
		roots = append(roots, node)
	}

	for i, root := range roots {
		isLast := i == len(roots)-1
		printTreeNode(root, "", isLast, 1, maxDepth)
	}
}

func printTreeNode(node *dirNode, prefix string, isLast bool, depth, maxDepth int) {
	connector := "├── "
	if isLast {
		connector = "└── "
	}
	fmt.Printf("%s%s%s/\t(%s)\n", prefix, connector, node.dir.Name, node.dir.ID)

	if maxDepth > 0 && depth >= maxDepth {
		return
	}

	childPrefix := prefix + "│   "
	if isLast {
		childPrefix = prefix + "    "
	}

	for i, child := range node.children {
		childIsLast := i == len(node.children)-1
		printTreeNode(child, childPrefix, childIsLast, depth+1, maxDepth)
	}
}

func runDirsCreate(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	workspace, _ := cmd.Flags().GetString("workspace")

	if name == "" {
		return fmt.Errorf("--name is required")
	}
	if workspace == "" {
		return fmt.Errorf("--workspace is required")
	}

	req := &promptvmgosdk.CreateDirectoryRequest{
		WorkspaceID: workspace,
		Name:        name,
	}
	if parent, _ := cmd.Flags().GetString("parent"); parent != "" {
		req.SetParentID(&parent)
	}

	resp, err := c.Directories.CreateDirectory(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	data := resp.GetData()
	parentInfo := ""
	if data.ParentID != nil {
		parentInfo = fmt.Sprintf(" under %s", *data.ParentID)
	}
	fmt.Printf("Created directory %s %q%s\n", data.ID, data.Name, parentInfo)
	return nil
}

func runDirsGet(cmd *cobra.Command, args []string) error {
	// The SDK does not expose a dedicated GetDirectory endpoint, so we fetch
	// the workspace's directory tree and filter by ID. This matches the same
	// data the UI shows on a directory detail page.
	if dirsGetWorkspace == "" {
		return fmt.Errorf("--workspace is required")
	}

	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	dirsResp, err := c.Directories.ListDirectories(cmd.Context(), &promptvmgosdk.ListDirectoriesRequest{
		WorkspaceID: dirsGetWorkspace,
	})
	if err != nil {
		return fmt.Errorf("listing directories: %w", err)
	}

	var match *promptvmgosdk.ListDirectoriesResponseDataItem
	subDirs := 0
	for _, d := range dirsResp.GetData() {
		if d.ID == args[0] {
			match = d
		}
		if d.ParentID != nil && *d.ParentID == args[0] {
			subDirs++
		}
	}
	if match == nil {
		return fmt.Errorf("directory %s not found in workspace %s", args[0], dirsGetWorkspace)
	}

	// Count non-directory children (prompts, skills, hooks, resources) whose
	// directoryId matches. Without this, `directories get` reports Children: 0
	// even after uploads succeed — misleading because the linkage IS working;
	// only child directories were being counted.
	caller, err := api.NewFromContext(cmd)
	if err != nil {
		return err
	}
	counts, err := countDirectoryChildren(cmd, c, caller, dirsGetWorkspace, args[0])
	if err != nil {
		return err
	}
	total := subDirs + counts.prompts + counts.skills + counts.hooks + counts.resources

	parent := "(root)"
	if match.ParentID != nil {
		parent = *match.ParentID
	}
	fmt.Fprintf(cmd.OutOrStdout(), "ID:        %s\n", match.ID)
	fmt.Fprintf(cmd.OutOrStdout(), "Name:      %s\n", match.Name)
	fmt.Fprintf(cmd.OutOrStdout(), "Slug:      %s\n", match.Slug)
	fmt.Fprintf(cmd.OutOrStdout(), "Workspace: %s\n", dirsGetWorkspace)
	fmt.Fprintf(cmd.OutOrStdout(), "Parent:    %s\n", parent)
	fmt.Fprintf(cmd.OutOrStdout(), "Children:  %d (dirs: %d, prompts: %d, skills: %d, hooks: %d, resources: %d)\n",
		total, subDirs, counts.prompts, counts.skills, counts.hooks, counts.resources)
	fmt.Fprintf(cmd.OutOrStdout(), "Updated:   %s\n", match.UpdatedAt.Format("2006-01-02 15:04"))
	return nil
}

type dirChildCounts struct {
	prompts   int
	skills    int
	hooks     int
	resources int
}

// countDirectoryChildren returns the number of prompts, skills, hooks, and
// resources whose directoryId matches dirID. Each list endpoint is paginated
// via cursor so a single directory with >100 items is counted correctly.
// Hooks go through the raw HTTP caller because the pinned Go SDK doesn't yet
// expose a Hooks client.
func countDirectoryChildren(cmd *cobra.Command, c *sdkclient.Client, caller *api.Caller, workspaceID, dirID string) (dirChildCounts, error) {
	var out dirChildCounts
	ctx := cmd.Context()
	limit := "100"

	// Prompts
	var pCursor *string
	for {
		req := &promptvmgosdk.ListPromptsRequest{WorkspaceID: workspaceID, Limit: &limit}
		if pCursor != nil {
			req.Cursor = pCursor
		}
		resp, err := c.Prompts.ListPrompts(ctx, req)
		if err != nil {
			return out, fmt.Errorf("listing prompts: %w", err)
		}
		for _, p := range resp.GetData() {
			if p.DirectoryID != nil && *p.DirectoryID == dirID {
				out.prompts++
			}
		}
		pg := resp.GetPagination()
		if pg == nil || !pg.GetHasMore() || pg.GetCursor() == nil {
			break
		}
		pCursor = pg.GetCursor()
	}

	// Skills
	var sCursor *string
	for {
		req := &promptvmgosdk.ListSkillsRequest{WorkspaceID: workspaceID, Limit: &limit}
		if sCursor != nil {
			req.Cursor = sCursor
		}
		resp, err := c.Skills.ListSkills(ctx, req)
		if err != nil {
			return out, fmt.Errorf("listing skills: %w", err)
		}
		for _, s := range resp.GetData() {
			if s.DirectoryID != nil && *s.DirectoryID == dirID {
				out.skills++
			}
		}
		pg := resp.GetPagination()
		if pg == nil || !pg.GetHasMore() || pg.GetCursor() == nil {
			break
		}
		sCursor = pg.GetCursor()
	}

	// Hooks — raw HTTP, the pinned SDK lacks a Hooks client.
	hCursor := ""
	for {
		params := url.Values{}
		params.Set("workspaceId", workspaceID)
		params.Set("limit", limit)
		if hCursor != "" {
			params.Set("cursor", hCursor)
		}
		var resp struct {
			Data []struct {
				DirectoryID *string `json:"directoryId"`
			} `json:"data"`
			Pagination struct {
				Cursor  string `json:"cursor"`
				HasMore bool   `json:"hasMore"`
			} `json:"pagination"`
		}
		if err := caller.Get("/api/v1/hooks?"+params.Encode(), &resp); err != nil {
			return out, fmt.Errorf("listing hooks: %w", err)
		}
		for _, h := range resp.Data {
			if h.DirectoryID != nil && *h.DirectoryID == dirID {
				out.hooks++
			}
		}
		if !resp.Pagination.HasMore || resp.Pagination.Cursor == "" {
			break
		}
		hCursor = resp.Pagination.Cursor
	}

	// Resources (workspace listing — bundled skill files already hidden by the backend)
	resResp, err := c.Resources.ListWorkspaceResources(ctx, &promptvmgosdk.ListWorkspaceResourcesRequest{
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return out, fmt.Errorf("listing resources: %w", err)
	}
	for _, r := range resResp.GetData() {
		if r.DirectoryID != nil && *r.DirectoryID == dirID {
			out.resources++
		}
	}

	return out, nil
}

func runDirsUpdate(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	req := &promptvmgosdk.UpdateDirectoryRequest{
		DirectoryID: args[0],
	}

	name, _ := cmd.Flags().GetString("name")
	parent, _ := cmd.Flags().GetString("parent")

	if name == "" && parent == "" {
		return fmt.Errorf("at least one of --name or --parent is required")
	}

	if name != "" {
		req.SetName(&name)
	}
	if parent != "" {
		req.SetParentID(&parent)
	}

	resp, err := c.Directories.UpdateDirectory(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("updating directory: %w", err)
	}

	data := resp.GetData()
	fmt.Printf("Updated directory %s %q\n", data.ID, data.Name)
	return nil
}

func runDirsDelete(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		prompt := promptui.Prompt{
			Label:     fmt.Sprintf("Delete directory %s", args[0]),
			IsConfirm: true,
		}
		if _, err := prompt.Run(); err != nil {
			fmt.Println("Cancelled.")
			return nil
		}
	}

	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	resp, err := c.Directories.DeleteDirectory(cmd.Context(), &promptvmgosdk.DeleteDirectoryRequest{
		DirectoryID: args[0],
	})
	if err != nil {
		return fmt.Errorf("deleting directory: %w", err)
	}

	fmt.Println(resp.GetMessage())
	return nil
}
