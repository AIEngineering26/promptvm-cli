package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"
)

const (
	defaultBaseURL = "https://api.promptvm.com"
	envAPIKey      = "PROMPTVM_API_KEY"
	envBaseURL     = "PROMPTVM_BASE_URL"
)

// Caller makes raw HTTP requests to the API for endpoints where the SDK
// does not decode response bodies.
type Caller struct {
	APIKey  string
	BaseURL string
}

// NewFromContext creates a Caller from CLI flags and environment.
func NewFromContext(cmd *cobra.Command) (*Caller, error) {
	apiKey := resolveFlag(cmd, "api-key")
	if apiKey == "" {
		apiKey = os.Getenv(envAPIKey)
	}
	if apiKey == "" {
		return nil, fmt.Errorf("API key required: set --api-key flag, %s env var, or run `promptvm auth login`", envAPIKey)
	}

	baseURL := resolveFlag(cmd, "base-url")
	if baseURL == "" {
		baseURL = os.Getenv(envBaseURL)
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return &Caller{APIKey: apiKey, BaseURL: baseURL}, nil
}

// Get performs a GET request and decodes JSON into result.
func (c *Caller) Get(path string, result interface{}) error {
	req, err := http.NewRequest(http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, result)
}

// Post performs a POST request with a JSON body.
func (c *Caller) Post(path string, body interface{}, result interface{}) error {
	return c.mutate(http.MethodPost, path, body, result)
}

// Delete performs a DELETE request.
func (c *Caller) Delete(path string, result interface{}) error {
	req, err := http.NewRequest(http.MethodDelete, c.BaseURL+path, nil)
	if err != nil {
		return err
	}
	return c.do(req, result)
}

// Patch performs a PATCH request with a JSON body.
func (c *Caller) Patch(path string, body interface{}, result interface{}) error {
	return c.mutate(http.MethodPatch, path, body, result)
}

func (c *Caller) mutate(method, path string, body interface{}, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshaling request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, bodyReader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.do(req, result)
}

func (c *Caller) do(req *http.Request, result interface{}) error {
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	if result != nil {
		if err := json.Unmarshal(body, result); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}
	return nil
}

func resolveFlag(cmd *cobra.Command, name string) string {
	val, _ := cmd.Root().PersistentFlags().GetString(name)
	return val
}
