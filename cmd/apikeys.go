package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/client"
	"github.com/AIEngineering26/promptvm-cli/internal/output"
	sdk "github.com/AIEngineering26/promptvm-go-sdk"
	"github.com/spf13/cobra"
)

var apikeysCmd = &cobra.Command{
	Use:   "apikeys",
	Short: "Manage API keys",
	Long:  "Create, list, get, update, revoke, and view usage for API keys.",
}

func init() {
	rootCmd.AddCommand(apikeysCmd)
	apikeysCmd.AddCommand(newApikeysListCmd())
	apikeysCmd.AddCommand(newApikeysCreateCmd())
	apikeysCmd.AddCommand(newApikeysGetCmd())
	apikeysCmd.AddCommand(newApikeysUpdateCmd())
	apikeysCmd.AddCommand(newApikeysRevokeCmd())
	apikeysCmd.AddCommand(newApikeysUsageCmd())
}

// --- list ---

func newApikeysListCmd() *cobra.Command {
	var (
		status string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List API keys",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.ListAPIKeysRequest{}
			if status != "" {
				s, err := sdk.NewListAPIKeysRequestStatusFromString(status)
				if err != nil {
					return err
				}
				req.Status = s.Ptr()
			}

			resp, err := c.APIKeys.ListAPIKeys(cmd.Context(), req)
			if err != nil {
				return err
			}

			return output.Print(cmd, resp, func(w io.Writer) error {
				return output.Table(w, []string{"ID", "NAME", "PUBLIC KEY", "STATUS", "SCOPES", "CREATED"}, func(tw *tabwriter.Writer) {
					for _, k := range resp.GetData() {
						name := "-"
						if k.GetName() != nil {
							name = *k.GetName()
						}
						scopes := scopeStrings(k.GetScopes())
						fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
							k.GetID(), name, k.GetPublicKey(),
							string(k.GetStatus()),
							scopes, output.HumanTime(k.GetCreatedAt()))
					}
				})
			})
		},
	}

	cmd.Flags().StringVar(&status, "status", "", "Filter by status: active|revoked")
	return cmd
}

// --- create ---

func newApikeysCreateCmd() *cobra.Command {
	var (
		name      string
		scopes    string
		expiresAt string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create an API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			// Parse scopes
			scopeParts := strings.Split(scopes, ",")
			var sdkScopes []sdk.CreateAPIKeyRequestScopesItem
			for _, s := range scopeParts {
				s = strings.TrimSpace(s)
				if s == "" {
					continue
				}
				scope, err := sdk.NewCreateAPIKeyRequestScopesItemFromString(s)
				if err != nil {
					return err
				}
				sdkScopes = append(sdkScopes, scope)
			}

			req := &sdk.CreateAPIKeyRequest{
				Name:   name,
				Scopes: sdkScopes,
			}

			if expiresAt != "" {
				t, err := time.Parse(time.RFC3339, expiresAt)
				if err != nil {
					return fmt.Errorf("invalid --expires format (use RFC3339, e.g. 2025-12-31T23:59:59Z): %w", err)
				}
				req.ExpiresAt = &t
			}

			resp, err := c.APIKeys.CreateAPIKey(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "API Key created:")
			printField(cmd, "ID", resp.GetID())
			printField(cmd, "Name", resp.GetKeyName())
			printField(cmd, "Public Key", resp.GetPublicKey())
			printField(cmd, "Secret Key", resp.GetSecretKey())
			scopeStrs := make([]string, len(resp.GetScopes()))
			for i, s := range resp.GetScopes() {
				scopeStrs[i] = string(s)
			}
			printField(cmd, "Scopes", strings.Join(scopeStrs, ", "))

			fmt.Fprintln(cmd.OutOrStdout())
			fmt.Fprintln(os.Stderr, output.Warn("The secret key will not be shown again. Store it securely."))
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "API key name (required)")
	cmd.MarkFlagRequired("name")
	cmd.Flags().StringVar(&scopes, "scopes", "read", "Comma-separated scopes: read,write,delete,admin")
	cmd.Flags().StringVar(&expiresAt, "expires", "", "Expiration date (RFC3339)")
	return cmd
}

// --- get ---

func newApikeysGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get API key details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			resp, err := c.APIKeys.GetAPIKey(cmd.Context(), &sdk.GetAPIKeyRequest{
				APIKeyID: args[0],
			})
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			d := resp.GetData()
			if d == nil {
				return fmt.Errorf("API key not found")
			}
			printField(cmd, "ID", d.GetID())
			if d.GetKeyName() != nil {
				printField(cmd, "Name", *d.GetKeyName())
			}
			printField(cmd, "Public Key", d.GetPublicKey())
			printField(cmd, "Status", string(d.GetStatus()))
			getScopes := make([]string, len(d.GetScopes()))
			for i, s := range d.GetScopes() {
				getScopes[i] = string(s)
			}
			printField(cmd, "Scopes", strings.Join(getScopes, ", "))
			printField(cmd, "Created", output.HumanTime(d.GetCreatedAt()))
			if d.GetLastUsedAt() != nil {
				printField(cmd, "Last Used", output.HumanTime(*d.GetLastUsedAt()))
			}
			if d.GetRevokedAt() != nil {
				printField(cmd, "Revoked", output.HumanTime(*d.GetRevokedAt()))
			}
			if d.GetRevokedReason() != nil {
				printField(cmd, "Revoke Reason", *d.GetRevokedReason())
			}
			return nil
		},
	}
	return cmd
}

// --- update ---

func newApikeysUpdateCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update an API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.UpdateAPIKeyRequest{
				APIKeyID: args[0],
			}
			if name != "" {
				req.Name = &name
			}

			resp, err := c.APIKeys.UpdateAPIKey(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Updated API key %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "New name for the API key")
	return cmd
}

// --- revoke ---

func newApikeysRevokeCmd() *cobra.Command {
	var (
		yes    bool
		reason string
	)

	cmd := &cobra.Command{
		Use:   "revoke <id>",
		Short: "Revoke an API key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !yes {
				if !output.Confirm(fmt.Sprintf("Revoke API key %s? This cannot be undone.", args[0])) {
					fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
					return nil
				}
			}

			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.RevokeAPIKeyRequest{
				APIKeyID: args[0],
			}
			if reason != "" {
				req.Reason = &reason
			}

			resp, err := c.APIKeys.RevokeAPIKey(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Revoked API key %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip confirmation prompt")
	cmd.Flags().StringVar(&reason, "reason", "", "Reason for revocation")
	return cmd
}

// --- usage ---

func newApikeysUsageCmd() *cobra.Command {
	var period string

	cmd := &cobra.Command{
		Use:   "usage <id>",
		Short: "View API key usage stats",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := client.NewFromContext(cmd)
			if err != nil {
				return err
			}

			req := &sdk.GetAPIKeyUsageRequest{
				APIKeyID: args[0],
			}
			if period != "" {
				p, err := sdk.NewGetAPIKeyUsageRequestPeriodFromString(period)
				if err != nil {
					return err
				}
				req.Period = p.Ptr()
			}

			resp, err := c.APIKeys.GetAPIKeyUsage(cmd.Context(), req)
			if err != nil {
				return err
			}

			if output.Format(cmd) != "table" {
				return output.Print(cmd, resp, nil)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "API Key: %s\n", resp.GetAPIKeyID())
			fmt.Fprintln(cmd.OutOrStdout())

			// Period stats table
			if stats := resp.GetPeriodStats(); len(stats) > 0 {
				w := cmd.OutOrStdout()
				_ = output.Table(w, []string{"PERIOD", "REQUESTS", "ERRORS", "BYTES", "AVG LATENCY"}, func(tw *tabwriter.Writer) {
					for _, s := range stats {
						fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%.0fms\n",
							string(s.GetPeriod()), s.GetRequests(), s.GetErrors(),
							resHumanBytes(int64(s.GetBytesTransferred())),
							s.GetAverageLatencyMs())
					}
				})
				fmt.Fprintln(w)
			}

			// All-time summary
			if allTime := resp.GetAllTime(); allTime != nil {
				fmt.Fprintln(cmd.OutOrStdout(), "All-time:")
				printField(cmd, "Total Requests", allTime.GetTotalRequests())
				printField(cmd, "Total Errors", allTime.GetTotalErrors())
				printField(cmd, "Avg Latency", fmt.Sprintf("%.0fms", allTime.GetAverageLatencyMs()))
				if allTime.GetFirstUsedAt() != nil {
					printField(cmd, "First Used", output.HumanTime(*allTime.GetFirstUsedAt()))
				}
				if allTime.GetLastUsedAt() != nil {
					printField(cmd, "Last Used", output.HumanTime(*allTime.GetLastUsedAt()))
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&period, "period", "7d", "Stats period: 1h|24h|7d|30d")
	return cmd
}

func scopeStrings(scopes []sdk.ListAPIKeysResponseDataItemScopesItem) string {
	if len(scopes) == 0 {
		return "-"
	}
	parts := make([]string, len(scopes))
	for i, s := range scopes {
		parts[i] = string(s)
	}
	return strings.Join(parts, ", ")
}
