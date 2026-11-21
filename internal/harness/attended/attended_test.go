package attended

import (
	"os"
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

// A prompt is one argument however many words it has, so words in it that
// look like flags are no flags.
func TestAPromptIsNeverReadAsFlags(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
		why  string
	}{
		{[]string{"claude", "run pytest -p no:cacheprovider"}, false, "a prompt the human typed, with -p in it"},
		{[]string{"claude", "--session-id", "abc", "--", "-p"}, false, "a prompt of just -p, after --"},
		{[]string{"claude", "--resume", "abc", "--", "add a --print flag"}, false, "a follow-up Pong sent"},
		{[]string{"codex", "--", "exec the plan"}, false, "a Codex Session Pong started"},
		{[]string{"/Users/me/Library/Application Support/Claude/claude", "--output-format", "stream-json"}, false, "the desktop app, its path with a space"},
		{[]string{"claude", "--print=json"}, true, "the long flag with a value"},
		{[]string{"claude", "-p", "Fix it"}, true, "a headless run with its prompt"},
	} {
		if got := unattended(c.args); got != c.want {
			t.Errorf("%s (%q): unattended = %v", c.why, c.args, got)
		}
	}
}

// This test's own process is read as the system holds it.
func TestArgumentsAreReadExactly(t *testing.T) {
	args := argsOf(os.Getpid())
	if len(args) == 0 || args[0] != os.Args[0] || len(args) != len(os.Args) {
		t.Fatalf("read %q, the process has %q", args, os.Args)
	}
	if p := parentOf(0); p != os.Getppid() {
		t.Fatalf("parent %d, want %d", p, os.Getppid())
	}
	if p := parentOf(os.Getpid()); p != os.Getppid() {
		t.Fatalf("parent of this process %d, want %d", p, os.Getppid())
	}
}

func TestHarnessSkipsShells(t *testing.T) {
	// go test runs this binary: the nearest ancestor that is not a shell.
	if args := Harness(); len(args) == 0 {
		t.Fatal("no Harness process found")
	}
}
