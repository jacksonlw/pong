package pongcmd

import (
	"fmt"
	"io"
	"os"

	"github.com/jacksonlw/pong/internal/harness/claudecode"
)

// statusLine runs as Claude Code's status line: `pong statusline claude-code`
// reads the status line input on standard input, hands its figures to the
// Engine and prints what the human's own status line prints, exiting with
// its status.
func statusLine(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs, dir := flags("statusline", stderr)
	pos, err := parse(fs, args, 1)
	if err != nil {
		return 2
	}
	if len(pos) == 0 || pos[0] != "claude-code" {
		fmt.Fprintln(stderr, "usage: pong statusline claude-code [-dir DIR] < input.json")
		return 2
	}
	home, _ := os.UserHomeDir()
	return claudecode.StatusLine(*dir, home, stdin, stdout, stderr, claudecode.StatusLineWait)
}
