package client

import (
	"fmt"
	"os"

	sdkclient "github.com/AIEngineering26/promptvm-go-sdk/client"
	"github.com/AIEngineering26/promptvm-go-sdk/option"
	"github.com/spf13/cobra"
)

const (
	defaultBaseURL = "https://api.promptvm.com"
	envAPIKey      = "PROMPTVM_API_KEY"
	envBaseURL     = "PROMPTVM_BASE_URL"
)

// NewFromContext creates an SDK client from CLI context.
// Resolution order: flag → environment variable → config file → default.
func NewFromContext(cmd *cobra.Command) (*sdkclient.Client, error) {
	apiKey, err := resolveAPIKey(cmd)
	if err != nil {
		return nil, err
	}

	baseURL := resolveBaseURL(cmd)

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}
	if baseURL != "" {
		opts = append(opts, option.WithBaseURL(baseURL))
	}

	return sdkclient.NewClient(opts...), nil
}

func resolveAPIKey(cmd *cobra.Command) (string, error) {
	// 1. Flag
	if key, _ := cmd.Flags().GetString("api-key"); key != "" {
		return key, nil
	}

	// 2. Environment variable
	if key := os.Getenv(envAPIKey); key != "" {
		return key, nil
	}

	// 3. Config file support will be added in PRD-002
	return "", fmt.Errorf("API key required: set --api-key flag, %s env var, or run `promptvm auth login`", envAPIKey)
}

func resolveBaseURL(cmd *cobra.Command) string {
	// 1. Flag
	if url, _ := cmd.Flags().GetString("base-url"); url != "" {
		return url
	}

	// 2. Environment variable
	if url := os.Getenv(envBaseURL); url != "" {
		return url
	}

	return defaultBaseURL
}
