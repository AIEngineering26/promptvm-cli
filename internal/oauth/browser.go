package oauth

import "github.com/pkg/browser"

// Open opens the user's default browser pointed at url.
// Returns the underlying error if the browser could not be launched.
// Callers should treat failure as non-fatal — the URL should also be
// printed to stderr so the user can paste it manually.
func Open(url string) error {
	return browser.OpenURL(url)
}
