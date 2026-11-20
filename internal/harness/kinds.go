package harness

import (
	"strings"

	"github.com/jacksonlw/pong/internal/engine"
)

// CommandQuestion asks to run a shell command in the words the agent gave
// with it, which say what the command does and often why: "Write the date to
// stamp.txt?" says more than "Run this shell command?", which is left for a
// command that came with none.
func CommandQuestion(description string) string {
	d := strings.TrimRight(strings.TrimSpace(description), ".? ")
	if d == "" {
		return "Run this shell command?"
	}
	return d + "?"
}

// The kind Tags an adapter puts first on each Ping it captures from its
// Harness's own prompts, so they read like the Pings agents tag themselves.
var (
	TagApprovePlan    = engine.Tag{Text: "Approve plan", Icon: "doc.text"}
	TagApproveCommand = engine.Tag{Text: "Approve command", Icon: "terminal"}
	TagApproveEdit    = engine.Tag{Text: "Approve edit", Icon: "pencil"}
	TagApproveTool    = engine.Tag{Text: "Approve tool", Icon: "wrench.and.screwdriver"}
	TagQuestion       = engine.Tag{Text: "Question", Icon: "person"}
)
