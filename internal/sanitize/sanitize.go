// Package sanitize cleans raw Claude Code transcript text BEFORE it is redacted,
// hashed, or stored (CAPQ-4 / FR-Q3). Real transcripts leak terminal control
// codes, Claude Code command wrappers, and literal escape sequences into the
// captured text; if those reach the marketplace they produce garbage titles and
// unreadable bodies, and — worse — they can hide secrets from the redactor.
//
// The contractual pipeline order is: sanitize → redact → hash/store. Sanitizing
// first guarantees the redactor sees plain text (so a secret buried inside an
// ANSI escape or a <command-*> wrapper is still caught) and that the canonical
// content hash is stable across re-runs.
//
// Sanitize applies, in this exact internal order:
//
//	(a) strip ANSI/C0 control sequences (CSI/SGR escapes, OSC, lone ESC, and the
//	    remaining C0 control bytes \x00–\x1f except \n and \t; bare \r dropped);
//	(b) unwrap Claude Code wrappers (<command-name>, <command-message>,
//	    <command-args>, <local-command-stdout>, <local-command-stderr>,
//	    <local-command-caveat>, <task-notification>) — keep inner text, drop tags;
//	(c) unescape literal escape sequences that leaked as text: \n → newline,
//	    \t → tab.
package sanitize

import "regexp"

var (
	// ansiCSI matches CSI/SGR escapes such as ESC[1m / ESC[22m / ESC[0K.
	ansiCSI = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	// ansiOSC matches OSC sequences (ESC] ... BEL or ST), best-effort.
	ansiOSC = regexp.MustCompile(`\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)?`)
	// ansiTwoChar matches the remaining two-byte ESC sequences.
	ansiTwoChar = regexp.MustCompile(`\x1b[@-Z\\-_]`)
	// loneESC removes any ESC byte left after the structured passes.
	loneESC = regexp.MustCompile(`\x1b`)
	// c0Controls matches C0 control bytes except \t (\x09) and \n (\x0a). This
	// also drops bare \r (\x0d).
	c0Controls = regexp.MustCompile(`[\x00-\x08\x0b-\x1f]`)

	// ccWrapper matches the opening, closing, and self-closing forms of every
	// Claude Code text wrapper, case-insensitively. Replacing with "" keeps the
	// inner text and removes only the tag.
	ccWrapper = regexp.MustCompile(`(?i)</?(?:command-name|command-message|command-args|local-command-stdout|local-command-stderr|local-command-caveat|task-notification)(?:\s[^>]*)?/?>`)

	// literalNewline / literalTab match the two-character escape sequences that
	// leak into stored text as literal backslash-n / backslash-t.
	literalNewline = regexp.MustCompile(`\\n`)
	literalTab     = regexp.MustCompile(`\\t`)
)

// Sanitize cleans a single string per the package contract. It is pure and
// idempotent: sanitizing already-sanitized text is a no-op.
func Sanitize(s string) string {
	if s == "" {
		return s
	}

	// (a) ANSI / C0 control sequences. Structured escapes first (they consume
	// the ESC byte), then any lone ESC, then the remaining C0 controls — so the
	// C0 pass never decapitates a sequence the regexes were meant to remove.
	s = ansiCSI.ReplaceAllString(s, "")
	s = ansiOSC.ReplaceAllString(s, "")
	s = ansiTwoChar.ReplaceAllString(s, "")
	s = loneESC.ReplaceAllString(s, "")
	s = c0Controls.ReplaceAllString(s, "")

	// (b) Unwrap Claude Code wrappers, keeping inner text.
	s = ccWrapper.ReplaceAllString(s, "")

	// (c) Unescape literal escape sequences that leaked as text. Mandated by the
	// contract (step c). Tradeoff: this is lossy for legitimate content that
	// contains a real backslash-n / backslash-t — e.g. Windows paths (C:\name)
	// or regex/code snippets — which will be corrupted into a newline/tab.
	s = literalNewline.ReplaceAllString(s, "\n")
	s = literalTab.ReplaceAllString(s, "\t")

	return s
}

// Strings sanitizes each element of in, returning a new slice. nil in, nil out.
func Strings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = Sanitize(s)
	}
	return out
}
