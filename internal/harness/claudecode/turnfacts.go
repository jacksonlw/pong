package claudecode

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jacksonlw/pong/internal/harness/turn"
)

// factsTail is how much of the end of a transcript turnFacts reads.
const factsTail = 4 << 20

// testCommand matches a shell command that runs tests.
var testCommand = regexp.MustCompile(`\b(go test|pytest|python3? -m (unittest|pytest)|npm (run )?test|pnpm (run )?test|yarn test|jest|vitest|swift test|xcodebuild\b.*\btest\b|cargo test|make (test|check)|rspec|mix test|gradle\w* test|mvn test)\b`)

// turnFacts reads what the turn that just ended left beside its closing
// message: the output of its last command that ran tests, and the Markdown
// or text files it wrote that still exist. The turn began at the last prompt
// in the main thread: one the human typed or one Claude Code submitted, such
// as a woken turn's.
func turnFacts(path string) turn.Facts {
	var f turn.Facts
	b := tail(path, factsTail)
	if b == nil {
		return f
	}
	type entry struct {
		Type        string `json:"type"`
		IsSidechain bool   `json:"isSidechain"`
		IsMeta      bool   `json:"isMeta"`
		Message     struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	type part struct {
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
	}
	var entries []entry
	start := 0
	for _, line := range bytes.Split(b, []byte("\n")) {
		var en entry
		if json.Unmarshal(line, &en) != nil || en.IsSidechain {
			continue
		}
		entries = append(entries, en)
		var s string
		if en.Type == "user" && !en.IsMeta && json.Unmarshal(en.Message.Content, &s) == nil {
			start = len(entries) - 1
		}
	}
	tests := map[string]bool{}
	var written []string
	for _, en := range entries[start:] {
		var parts []part
		if json.Unmarshal(en.Message.Content, &parts) != nil {
			continue
		}
		for _, p := range parts {
			switch {
			case en.Type == "assistant" && p.Type == "tool_use" && p.Name == "Bash":
				var in struct {
					Command string `json:"command"`
				}
				if json.Unmarshal(p.Input, &in) == nil && testCommand.MatchString(in.Command) {
					tests[p.ID] = true
				}
			case en.Type == "assistant" && p.Type == "tool_use" && p.Name == "Write":
				var in struct {
					FilePath string `json:"file_path"`
				}
				if json.Unmarshal(p.Input, &in) == nil && in.FilePath != "" {
					if ext := strings.ToLower(filepath.Ext(in.FilePath)); ext == ".md" || ext == ".txt" {
						written = append(written, in.FilePath)
					}
				}
			case en.Type == "user" && p.Type == "tool_result" && tests[p.ToolUseID]:
				f.TestOutput = resultText(p.Content)
			}
		}
	}
	seen := map[string]bool{}
	for _, w := range written {
		if fi, err := os.Stat(w); err == nil && !fi.IsDir() && !seen[w] {
			seen[w] = true
			f.Documents = append(f.Documents, w)
		}
	}
	return f
}

// resultText is a tool result's text, whether Claude Code stored it as a
// string or as text blocks.
func resultText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	json.Unmarshal(raw, &blocks)
	var out []string
	for _, b := range blocks {
		if b.Type == "text" {
			out = append(out, b.Text)
		}
	}
	return strings.Join(out, "\n")
}

// tail reads the last n bytes of the file at path, or nil.
func tail(path string, n int64) []byte {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	from := fi.Size() - n
	if from < 0 {
		from = 0
	}
	b := make([]byte, fi.Size()-from)
	if _, err := f.ReadAt(b, from); err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	return b
}
