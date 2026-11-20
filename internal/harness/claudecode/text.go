package claudecode

import (
	"os"
	"strings"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// Text the human writes in a Thread while no Ping waits reaches the Session
// through two hooks (the Engine's thread_text.go keeps it meanwhile):
//
//   - PreToolUse hands it to a working agent as additional context before its
//     next tool call (denied.go). Claude Code 2.1.328 gave the agent a
//     PreToolUse hook's additionalContext in the same turn.
//   - Stop wakes the Session with it: at the end of a turn that still has
//     some, and for as long as a Session the human started sits idle (stop.go).
//
// Claude Code shows the agent a Stop hook's text as hook feedback, so the
// words say whose they are.

// TakesText marks Claude Code as a Harness whose hooks take text written in
// a Thread (engine.TextRunner).
func (Runner) TakesText() {}

var _ engine.TextRunner = Runner{}

// wrote starts every text handed to the agent.
const wrote = "Your human wrote in Pong: "

func handOver(texts []string) string {
	if len(texts) == 0 {
		return ""
	}
	return wrote + strings.Join(texts, "\n\n")
}

// markedText takes the text kept for e's Session when the Engine has marked
// that there is some; with no mark it costs one look at a file.
func markedText(c *client.Client, e Event) string {
	if _, err := os.Stat(engine.HeldTextMark(e.Dir, e.SessionID)); err != nil {
		return ""
	}
	texts, _ := c.TakeText(HarnessName, e.SessionID)
	return handOver(texts)
}
