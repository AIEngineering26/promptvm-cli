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

// sessionDTO mirrors the shape returned by GET /api/v1/auth/cli/sessions.
// Unknown fields are ignored so the backend can evolve safely.
type sessionDTO struct {
	ID                string    `json:"id"`
	DeviceName        string    `json:"deviceName"`
	AccessTokenPrefix string    `json:"accessTokenPrefix"`
	Environment       string    `json:"environment"`
	CreatedAt         time.Time `json:"createdAt"`
	LastUsedAt        time.Time `json:"lastUsedAt"`
	ExpiresAt         time.Time `json:"expiresAt"`
}

func runAuthSessionsList(cmd *cobra.Command, _ []string) error {
	c, err := api.NewFromContext(cmd)
	if err != nil {
		return err
	}

	var sessions []sessionDTO
	if err := c.Get("/api/v1/auth/cli/sessions", &sessions); err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		fmt.Println("No active CLI sessions.")
		return nil
	}

	fmt.Printf("%-20s  %-30s  %-12s  %-20s  %-20s\n", "ID", "DEVICE", "PREFIX", "CREATED", "LAST USED")
	for _, s := range sessions {
		fmt.Printf("%-20s  %-30s  %-12s  %-20s  %-20s\n",
			truncateSession(s.ID, 20),
			truncateSession(s.DeviceName, 30),
			truncateSession(s.AccessTokenPrefix, 12),
			formatTime(s.CreatedAt),
			formatTime(s.LastUsedAt),
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

	if err := c.Delete("/api/v1/auth/cli/sessions/"+id, nil); err != nil {
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

