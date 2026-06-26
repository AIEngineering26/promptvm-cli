// Package redact applies layered, client-side secret redaction to captured
// session content BEFORE anything leaves the machine (SEC-3 / FR-12).
//
// Three layers run in order:
//
//  1. Provider patterns — known token shapes (AWS, GitHub, OpenAI, Stripe,
//     Slack, Google, private-key blocks, JWTs, generic "key = value" assigns).
//  2. High-entropy tokens — long base64/hex-ish runs whose Shannon entropy
//     exceeds a threshold (catches secrets in tool output, diffs, pasted blobs
//     that provider patterns miss).
//  3. Path excludes — lines referencing a glob in excludePaths are dropped.
//
// Redaction is best-effort by design: the server runs an authoritative scanner
// too (SEC-3b). This layer minimizes egress; it is not the sole defense.
package redact

import (
	"math"
	"path"
	"regexp"
	"strings"
)

// placeholder replaces every redacted secret span.
const placeholder = "«redacted»"

// providerPatterns matches well-known credential shapes.
var providerPatterns = []*regexp.Regexp{
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),                                              // AWS access key id
	regexp.MustCompile(`ASIA[0-9A-Z]{16}`),                                              // AWS temp key id
	regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{20,255}`),                                 // GitHub tokens
	regexp.MustCompile(`github_pat_[A-Za-z0-9_]{50,255}`),                               // GitHub fine-grained PAT
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),                                           // OpenAI / generic sk-
	regexp.MustCompile(`sk-ant-[A-Za-z0-9-]{20,}`),                                      // Anthropic
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`),                                  // Slack
	regexp.MustCompile(`(?:r|s)k_(?:live|test)_[A-Za-z0-9]{16,}`),                       // Stripe
	regexp.MustCompile(`AIza[0-9A-Za-z\-_]{35}`),                                        // Google API key
	regexp.MustCompile(`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`), // JWT
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),                            // PEM private key header
}

// assignmentPattern matches `SECRET = "value"` / `password: value` style
// lines and redacts the value side only.
var assignmentPattern = regexp.MustCompile(
	`(?i)\b([A-Za-z0-9_\-.]*(?:secret|token|password|passwd|api[_-]?key|access[_-]?key|private[_-]?key|client[_-]?secret|auth)[A-Za-z0-9_\-.]*)\s*[:=]\s*['"]?([^\s'"]{6,})['"]?`)

// urlCredentialPattern matches `scheme://user:password@host` connection strings
// and redacts the password span only (keeping scheme/user/host for context).
// Connection strings — postgres/mysql/redis/mongodb/amqp/https-basic-auth — embed
// credentials that none of the other layers reliably catch: the password isn't a
// known provider shape, isn't an assignment (`key=value`), and DB passwords are
// frequently shorter than the 24-char high-entropy threshold. The user segment is
// optional so `redis://:pass@host` is covered too.
var urlCredentialPattern = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^:/?#@\s]*:)([^@/?#\s]+)(@)`)

// entropyTokenPattern isolates candidate high-entropy runs for layer 2.
var entropyTokenPattern = regexp.MustCompile(`[A-Za-z0-9+/=_\-]{24,}`)

// Result reports what Redact produced.
type Result struct {
	Text    string
	Applied bool // true if any redaction (any layer) fired
}

// entropyThreshold is the Shannon-entropy (bits/char) above which a long token
// is treated as a likely secret. English prose sits well below ~4.0; random
// base64 approaches ~6.0.
const entropyThreshold = 4.0

// shannonEntropy returns the per-character Shannon entropy of s in bits.
func shannonEntropy(s string) float64 {
	if s == "" {
		return 0
	}
	counts := make(map[rune]int)
	for _, r := range s {
		counts[r]++
	}
	n := float64(len(s))
	var h float64
	for _, c := range counts {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// matchesAnyGlob reports whether p matches any of the provided globs. It tries
// both the full path and the basename so `**/.env*` and `.env` both hit.
func matchesAnyGlob(p string, globs []string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	base := path.Base(p)
	for _, g := range globs {
		// Normalize a leading **/ so path.Match (which has no ** support)
		// still catches the common "**/.env*" case via the basename.
		trimmed := strings.TrimPrefix(g, "**/")
		if ok, _ := path.Match(trimmed, base); ok {
			return true
		}
		if ok, _ := path.Match(g, p); ok {
			return true
		}
		if strings.Contains(p, strings.TrimSuffix(strings.TrimPrefix(g, "**/"), "/**")) &&
			(strings.HasSuffix(g, "/**") || strings.HasPrefix(g, "**/")) {
			return true
		}
	}
	return false
}

// Redact runs the three layers over text and returns the redacted output.
// excludePaths drops whole lines that reference an excluded path glob.
func Redact(text string, excludePaths []string) Result {
	applied := false

	// Layer 3 (line-level path excludes) first so we don't waste work on
	// lines that are going to be dropped wholesale.
	if len(excludePaths) > 0 {
		lines := strings.Split(text, "\n")
		kept := lines[:0]
		for _, line := range lines {
			if lineReferencesExcluded(line, excludePaths) {
				applied = true
				continue
			}
			kept = append(kept, line)
		}
		text = strings.Join(kept, "\n")
	}

	// Layer 1: provider patterns.
	for _, re := range providerPatterns {
		if re.MatchString(text) {
			applied = true
			text = re.ReplaceAllString(text, placeholder)
		}
	}

	// Connection-string credentials: redact the password span, keep the rest.
	text = urlCredentialPattern.ReplaceAllStringFunc(text, func(m string) string {
		sub := urlCredentialPattern.FindStringSubmatch(m)
		if len(sub) < 4 {
			return m
		}
		applied = true
		return sub[1] + placeholder + sub[3]
	})

	// Assignment values (redact value, keep the key for context).
	text = assignmentPattern.ReplaceAllStringFunc(text, func(m string) string {
		sub := assignmentPattern.FindStringSubmatch(m)
		if len(sub) < 3 {
			return m
		}
		applied = true
		return strings.Replace(m, sub[2], placeholder, 1)
	})

	// Layer 2: high-entropy tokens.
	text = entropyTokenPattern.ReplaceAllStringFunc(text, func(tok string) string {
		if tok == placeholder {
			return tok
		}
		if shannonEntropy(tok) >= entropyThreshold {
			applied = true
			return placeholder
		}
		return tok
	})

	return Result{Text: text, Applied: applied}
}

// lineReferencesExcluded reports whether a line names a path matching an
// excluded glob. It scans whitespace- and quote-delimited tokens.
func lineReferencesExcluded(line string, globs []string) bool {
	fields := strings.FieldsFunc(line, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '"' || r == '\'' || r == '(' || r == ')' || r == ','
	})
	for _, f := range fields {
		if matchesAnyGlob(f, globs) {
			return true
		}
	}
	return false
}
