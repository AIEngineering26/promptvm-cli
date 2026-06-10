package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	promptvmgosdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

var templatesCmd = &cobra.Command{
	Use:     "templates",
	Aliases: []string{"tpl"},
	Short:   "Convert and instantiate prompt templates",
}

var tplConvertCmd = &cobra.Command{
	Use:   "convert <prompt-id>",
	Short: "Convert a prompt to a template",
	Args:  cobra.ExactArgs(1),
	RunE:  runTplConvert,
}

var tplInstantiateCmd = &cobra.Command{
	Use:   "instantiate <template-id>",
	Short: "Create a prompt from a template",
	Args:  cobra.ExactArgs(1),
	RunE:  runTplInstantiate,
}

var tplListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available templates",
	RunE:  runTplList,
}

func init() {
	rootCmd.AddCommand(templatesCmd)
	templatesCmd.AddCommand(tplConvertCmd)
	templatesCmd.AddCommand(tplInstantiateCmd)
	templatesCmd.AddCommand(tplListCmd)

	// instantiate flags
	tplInstantiateCmd.Flags().String("name", "", "Name for the new prompt (required)")
	tplInstantiateCmd.Flags().String("workspace", "", "Target workspace ID (required)")
	tplInstantiateCmd.Flags().String("directory", "", "Target directory ID")
	tplInstantiateCmd.Flags().StringToString("vars", nil, "Variable values as key=value pairs")

	// list flags
	tplListCmd.Flags().String("workspace", "", "Workspace ID (required)")
	tplListCmd.Flags().String("cursor", "", "Pagination cursor")
	tplListCmd.Flags().String("limit", "", "Max results to return")
}

func runTplConvert(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	resp, err := c.Templates.ConvertPromptToTemplate(cmd.Context(), &promptvmgosdk.ConvertPromptToTemplateRequest{
		PromptID: args[0],
	})
	if err != nil {
		return fmt.Errorf("converting prompt to template: %w", err)
	}

	data := resp.GetData()
	fmt.Printf("Converted prompt %q to template %s\n", data.Name, data.ID)
	fmt.Printf("Kind:   %s\n", data.Kind)
	fmt.Printf("Status: %s\n", data.Status)
	if len(data.Tags) > 0 {
		fmt.Printf("Tags:   %s\n", strings.Join(data.Tags, ", "))
	}
	return nil
}

func runTplInstantiate(cmd *cobra.Command, args []string) error {
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

	req := &promptvmgosdk.CreatePromptFromTemplateRequest{
		TemplateID:  args[0],
		Name:        name,
		WorkspaceID: workspace,
	}

	if dirID, _ := cmd.Flags().GetString("directory"); dirID != "" {
		req.SetDirectoryID(&dirID)
	}

	if vars, _ := cmd.Flags().GetStringToString("vars"); len(vars) > 0 {
		varValues := make(map[string]interface{}, len(vars))
		for k, v := range vars {
			varValues[k] = v
		}
		req.SetVariableValues(varValues)
	}

	resp, err := c.Templates.CreatePromptFromTemplate(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("instantiating template: %w", err)
	}

	data := resp.GetData()
	fmt.Printf("Created prompt %s %q from template %s\n", data.ID, data.Name, args[0])
	return nil
}

func runTplList(cmd *cobra.Command, args []string) error {
	c, err := client.NewFromContext(cmd)
	if err != nil {
		return err
	}

	workspace, _ := cmd.Flags().GetString("workspace")
	if workspace == "" {
		return fmt.Errorf("--workspace is required")
	}

	req := &promptvmgosdk.ListTemplatesRequest{
		WorkspaceID: workspace,
	}
	if v, _ := cmd.Flags().GetString("cursor"); v != "" {
		req.SetCursor(&v)
	}
	if v, _ := cmd.Flags().GetString("limit"); v != "" {
		req.SetLimit(&v)
	}

	resp, err := c.Templates.ListTemplates(cmd.Context(), req)
	if err != nil {
		return fmt.Errorf("listing templates: %w", err)
	}

	items := resp.GetData()
	if len(items) == 0 {
		fmt.Println("No templates found.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tKIND\tSTATUS\tUPDATED")
	for _, t := range items {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			t.ID, t.Name, t.Kind, t.Status, t.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return w.Flush()
}
