package cmd

import (
	"fmt"
	"time"

	"github.com/AIEngineering26/promptvm-cli/internal/api"
	"github.com/spf13/cobra"
)

var authSessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List or revoke server-side CLI sessions",
	Long: `List the CLI sessions (access tokens) currently authorized on the server.

Use "sessions revoke <id>" to invalidate a specific session remotely
— useful if you lose a device.`,
	RunE: runAuthSessionsList,
}

var authSessionsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List active CLI sessions",
	RunE:  runAuthSessionsList,
}

var authSessionsRevokeCmd = &cobra.Command{
	Use:   "revoke <id>",
	Short: "Revoke a CLI session by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runAuthSessionsRevoke,
}

func init() {
	authSessionsCmd.AddCommand(authSessionsListCmd)
	authSessionsCmd.AddCommand(authSessionsRevokeCmd)
}

// sessionDTO mirrors the shape we expect from GET /api/auth/cli/sessions.
// Unknown fields are ignored so the backend can evolve safely.
type sessionDTO struct {
	ID         string    `json:"id"`
	DeviceName string    `json:"device_name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

type sessionsResponse struct {
	Sessions []sessionDTO `json:"sessions"`
}

func runAuthSessionsList(cmd *cobra.Command, _ []string) error {
	c, err := api.NewFromContext(cmd)
	if err != nil {
		return err
	}

	var out sessionsResponse
	if err := c.Get("/api/auth/cli/sessions", &out); err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(out.Sessions) == 0 {
		fmt.Println("No active CLI sessions.")
		return nil
	}

	fmt.Printf("%-20s  %-30s  %-20s  %-20s  %s\n", "ID", "DEVICE", "CREATED", "LAST USED", "")
	for _, s := range out.Sessions {
		marker := ""
		if s.Current {
			marker = "(current)"
		}
		fmt.Printf("%-20s  %-30s  %-20s  %-20s  %s\n",
			truncateSession(s.ID, 20),
			truncateSession(s.DeviceName, 30),
			formatTime(s.CreatedAt),
			formatTime(s.LastUsedAt),
			marker,
		)
	}
	return nil
}

func runAuthSessionsRevoke(cmd *cobra.Command, args []string) error {
	c, err := api.NewFromContext(cmd)
	if err != nil {
		return err
	}
	id := args[0]

	if err := c.Delete("/api/auth/cli/sessions/"+id, nil); err != nil {
		return fmt.Errorf("revoking session %q: %w", id, err)
	}
	fmt.Printf("Revoked session %q.\n", id)
	return nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02 15:04")
}

func truncateSession(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

