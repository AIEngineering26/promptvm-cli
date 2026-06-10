package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	promptvmgosdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
)

var collectionsCmd = &cobra.Command{
	Use:     "collections",
	Aliases: []string{"col"},
	Short:   "Manage collections of prompts",
}

var colListCmd = &cobra.Command{
	Use:   "list",
	Short: "List collections",
	RunE:  runColList,
}

var colCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a collection",
	RunE:  runColCreate,
}

var colGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get collection with items",
	Args:  cobra.ExactArgs(1),
	RunE:  runColGet,
}

var colUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update collection metadata",
	Args:  cobra.ExactArgs(1),
	RunE:  runColUpdate,
}

var colDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a collection",
	Args:  cobra.ExactArgs(1),
	RunE:  runColDelete,
}

var colAddCmd = &cobra.Command{
	Use:   "add <collection-id> <prompt-id>",
	Short: "Add a prompt to a collection",
	Args:  cobra.ExactArgs(2),
	RunE:  runColAdd,
}

var colRemoveCmd = &cobra.Command{
	Use:   "remove <collection-id> <item-id>",
	Short: "Remove a prompt from a collection",
	Args:  cobra.ExactArgs(2),
	RunE:  runColRemove,
}

func init() {
	rootCmd.AddCommand(collectionsCmd)
	collectionsCmd.AddCommand(colListCmd)
	collectionsCmd.AddCommand(colCreateCmd)
	collectionsCmd.AddCommand(colGetCmd)
	collectionsCmd.AddCommand(colUpdateCmd)
	collectionsCmd.AddCommand(colDeleteCmd)
	collectionsCmd.AddCommand(colAddCmd)
	collectionsCmd.AddCommand(colRemoveCmd)

	// list flags
	colListCmd.Flags().String("cursor", "", "Pagination cursor")
	colListCmd.Flags().String("limit", "", "Max results to return")

	// create flags
	colCreateCmd.Flags().String("name", "", "Collection name (required)")
	colCreateCmd.Flags().String("description", "", "Collection description")

	// update flags
	colUpdateCmd.Flags().String("name", "", "New collection name")
	colUpdateCmd.Flags().String("description", "", "New collection description")

	// add flags
	colAddCmd.Flags().String("note", "", "Note for the collection item")

	// delete flags
	colDeleteCmd.Flags().Bool("yes", false, "Skip confirmation prompt")

	// remove flags
	colRemoveCmd.Flags().Bool("yes", false, "Skip confirmation prompt")
}

func runColList(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	req := &promptvmgosdk.ListCollectionsRequest{}
	if v, _ := cmd.Flags().GetString("cursor"); v != "" {
		req.SetCursor(&v)
	}
	if v, _ := cmd.Flags().GetString("limit"); v != "" {
		req.SetLimit(&v)
	}

	resp, err := c.Collections.ListCollections(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("listing collections: %w", err)
	}

	if len(resp.GetData()) == 0 {
		fmt.Println("No collections found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tDESCRIPTION\tSYSTEM\tUPDATED")
	for _, col := range resp.GetData() {
		desc := ""
		if col.Description != nil {
			desc = *col.Description
		}
		system := ""
		if col.IsSystem {
			system = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			col.ID, col.Name, truncate(desc, 30), system, col.UpdatedAt.Format("2006-01-02 15:04"))
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if p := resp.GetPagination(); p != nil && p.HasMore {
		if cursor := p.GetCursor(); cursor != nil {
			fmt.Printf("\nMore results available. Use --cursor %q\n", *cursor)
		}
	}
	return nil
}

func runColCreate(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	name, _ := cmd.Flags().GetString("name")
	if name == "" {
		return fmt.Errorf("--name is required")
	}

	req := &promptvmgosdk.CreateCollectionRequest{
		Name: name,
	}
	if desc, _ := cmd.Flags().GetString("description"); desc != "" {
		req.SetDescription(&desc)
	}

	resp, err := c.Collections.CreateCollection(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("creating collection: %w", err)
	}

	data := resp.GetData()
	fmt.Printf("Created collection %s %q\n", data.ID, data.Name)
	return nil
}

func runColGet(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	resp, err := c.Collections.GetCollection(cmd.Context(), &promptvmgosdk.GetCollectionRequest{
		CollectionID: args[0],
	})
	if err != nil {
		return fmt.Errorf("getting collection: %w", err)
	}

	data := resp.GetData()
	name := ""
	if data.Name != nil {
		name = *data.Name
	}
	id := ""
	if data.ID != nil {
		id = *data.ID
	}
	fmt.Printf("Name:   %s\n", name)
	fmt.Printf("ID:     %s\n", id)
	if data.Description != nil {
		fmt.Printf("Desc:   %s\n", *data.Description)
	}
	fmt.Printf("Items:  %d\n", len(data.GetItems()))

	items := data.GetItems()
	if len(items) > 0 {
		fmt.Println()
		w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
		fmt.Fprintln(w, "ITEM ID\tFILE ID\tNAME\tTYPE")
		for _, item := range items {
			fileName := ""
			fileType := ""
			if f := item.GetFile(); f != nil {
				if f.Name != nil {
					fileName = *f.Name
				}
				if f.Type != nil {
					fileType = *f.Type
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", item.ID, item.FileID, fileName, fileType)
		}
		w.Flush()
	}
	return nil
}

func runColUpdate(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	req := &promptvmgosdk.UpdateCollectionRequest{
		CollectionID: args[0],
	}

	name, _ := cmd.Flags().GetString("name")
	desc, _ := cmd.Flags().GetString("description")

	if name == "" && desc == "" {
		return fmt.Errorf("at least one of --name or --description is required")
	}

	if name != "" {
		req.SetName(&name)
	}
	if desc != "" {
		req.SetDescription(&desc)
	}

	resp, err := c.Collections.UpdateCollection(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("updating collection: %w", err)
	}

	fmt.Printf("Updated collection %s\n", resp.GetData().ID)
	return nil
}

func runColDelete(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		prompt := promptui.Prompt{
			Label:     fmt.Sprintf("Delete collection %s", args[0]),
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

	resp, err := c.Collections.DeleteCollection(cmd.Context(), &promptvmgosdk.DeleteCollectionRequest{
		CollectionID: args[0],
	})
	if err != nil {
		return fmt.Errorf("deleting collection: %w", err)
	}

	fmt.Println(resp.GetMessage())
	return nil
}

func runColAdd(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	req := &promptvmgosdk.AddCollectionItemRequest{
		CollectionID: args[0],
		FileID:       args[1],
	}
	if note, _ := cmd.Flags().GetString("note"); note != "" {
		req.SetNote(&note)
	}

	resp, err := c.Collections.AddCollectionItem(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("adding item to collection: %w", err)
	}

	data := resp.GetData()
	fileName := data.FileID
	if f := data.GetFile(); f != nil && f.Name != nil {
		fileName = *f.Name
	}
	fmt.Printf("Added %q to collection %s\n", fileName, args[0])
	return nil
}

func runColRemove(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		prompt := promptui.Prompt{
			Label:     fmt.Sprintf("Remove item %s from collection %s", args[1], args[0]),
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

	resp, err := c.Collections.RemoveCollectionItem(cmd.Context(), &promptvmgosdk.RemoveCollectionItemRequest{
		CollectionID: args[0],
		ItemID:       args[1],
	})
	if err != nil {
		return fmt.Errorf("removing item from collection: %w", err)
	}

	fmt.Println(resp.GetMessage())
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
