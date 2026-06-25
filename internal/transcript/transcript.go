// Package transcript reads Claude Code session transcript JSONL files and
// extracts the structural metadata and text body the capture uploader needs.
//
// The transcript format is one JSON object per line. We tolerate unknown
// shapes: each line is parsed leniently and we pull what we recognize
// (role/type, text content, tool name + tool input paths/commands). Lines we
// cannot parse are skipped, never fatal — a hook must never fail the session.
package transcript

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
)

// Parsed is the distilled structural view of a transcript.
type Parsed struct {
	// Text is the concatenated human-readable conversation (user + assistant
	// text), suitable for redaction + distillation.
	Text string
	// FilesTouched are unique file paths referenced by Edit/Write/Read tools.
	FilesTouched []string
	// Commands are unique shell commands referenced by Bash tools.
	Commands []string
	// MessageCount is the number of transcript lines parsed.
	MessageCount int
}

// line is the lenient shape we extract from each JSONL record.
type line struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Message json.RawMessage `json:"message"`
	Content json.RawMessage `json:"content"`
}

// Read parses a transcript file at path. A missing/unreadable file returns an
// empty Parsed and the error, so callers can decide whether to spool.
func Read(path string) (*Parsed, error) {
	f, err := os.Open(path)
	if err != nil {
		return &Parsed{}, err
	}
	defer f.Close()

	p := &Parsed{}
	files := map[string]bool{}
	cmds := map[string]bool{}
	var sb strings.Builder

	scanner := bufio.NewScanner(f)
	// Transcript lines can be large (inlined file bodies); raise the limit.
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		var l line
		if err := json.Unmarshal(raw, &l); err != nil {
			continue
		}
		p.MessageCount++
		extractBlocks(l, &sb, files, cmds)
	}

	p.Text = sb.String()
	p.FilesTouched = sortedKeys(files)
	p.Commands = sortedKeys(cmds)
	return p, scanner.Err()
}

// extractBlocks pulls text, file paths, and commands out of one transcript line.
func extractBlocks(l line, sb *strings.Builder, files, cmds map[string]bool) {
	// The content blocks may live under message.content or content.
	var blocks []contentBlock
	for _, raw := range [][]byte{messageContent(l.Message), l.Content} {
		if len(raw) == 0 {
			continue
		}
		blocks = append(blocks, parseBlocks(raw)...)
	}
	for _, b := range blocks {
		if b.Text != "" {
			sb.WriteString(b.Text)
			sb.WriteByte('\n')
		}
		if b.Name == "" {
			continue
		}
		switch strings.ToLower(b.Name) {
		case "bash":
			if c := stringField(b.Input, "command"); c != "" {
				cmds[c] = true
			}
		case "edit", "write", "read", "notebookedit":
			if fp := stringField(b.Input, "file_path"); fp != "" {
				files[fp] = true
			}
		}
	}
}

type contentBlock struct {
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// messageContent returns the raw content array nested under a message object.
func messageContent(msg json.RawMessage) []byte {
	if len(msg) == 0 {
		return nil
	}
	var m struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(msg, &m); err != nil {
		return nil
	}
	return m.Content
}

// parseBlocks handles both an array of blocks and a bare string content value.
func parseBlocks(raw []byte) []contentBlock {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil
	}
	if trimmed[0] == '[' {
		var blocks []contentBlock
		if err := json.Unmarshal(raw, &blocks); err != nil {
			return nil
		}
		return blocks
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil
		}
		return []contentBlock{{Type: "text", Text: s}}
	}
	return nil
}

func stringField(raw json.RawMessage, key string) string {
	if len(raw) == 0 {
		return ""
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
