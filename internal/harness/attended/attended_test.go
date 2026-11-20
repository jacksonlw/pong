package attended

import (
	"strings"
	"testing"
)

func TestHeadlessRunsAreUnattended(t *testing.T) {
	for _, c := range []struct {
		args string
		want bool
		why  string
	}{
		{"claude", false, "a terminal Session"},
		{"claude --resume abc", false, "a resumed terminal Session"},
		{"claude --session-id abc -- Fix it", false, "a Session Pong opened in a terminal"},
		{"/Users/me/Library/Application Support/Claude/claude --output-format stream-json --input-format stream-json --permission-prompt-tool=stdio", false, "the desktop app"},
		{"claude -p --model haiku", true, "a script's model call"},
		{"node /opt/claude/cli.js --print", true, "the long flag"},
		{"codex exec -", true, "Codex's headless run"},
		{"node /opt/codex/bin/codex.js exec -", true, "the same through node"},
		{"codex", false, "Codex in a terminal"},
		{"codex app-server", false, "Codex as its app runs it"},
		{"", false, "the Harness process could not be read"},
	} {
		if got := unattended(strings.Fields(c.args)); got != c.want {
			t.Errorf("%s (%q): unattended = %v", c.why, c.args, got)
		}
	}
}

func TestHarnessSkipsShells(t *testing.T) {
	// go test runs this binary: the nearest ancestor that is not a shell.
	if args := Harness(); len(args) == 0 {
		t.Fatal("no Harness process found")
	}
}
