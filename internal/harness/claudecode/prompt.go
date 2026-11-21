package claudecode

import (
	"encoding/json"
	"regexp"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/attended"
	"github.com/jacksonlw/pong/internal/harness/turn"
)

// UserPromptSubmit starts a turn, and Claude Code sends it for more than
// the prompts the human types. Recorded on 2.1.326 and 2.1.329
// (testdata/README.md): a background subagent's report and the turn a Stop
// hook woke both arrive as one, and the payload has the same fields as a
// typed prompt. Only its prompt tells them apart: a turn the human did not
// type carries a frame Claude Code wrote, which opens with a tag
// (<task-notification>, <agent-message from=...>).
//
// Only a typed prompt answers the Session's waiting Pings in the Harness,
// and begins a new turn whatever the Session's state. Any other prompt leaves
// them waiting, since nobody answered them, and begins a turn only for a
// Session not working already: a report folded into a running turn is part
// of that turn.
//
// The event states the Session's permission mode, which SessionStart does
// not; it is recorded with the Session.
func init() { on("UserPromptSubmit", prompted) }

// framed matches a prompt that opens with a tag. A prompt the human typed
// that opens with one is taken for a frame too, and leaves the Pings waiting.
var framed = regexp.MustCompile(`^\s*<[a-z][a-z-]*[\s>]`)

func prompted(c *client.Client, e Event) (any, error) {
	var p struct {
		Prompt string `json:"prompt"`
	}
	if err := json.Unmarshal(e.Raw, &p); err != nil {
		return nil, err
	}
	// The prompt the Session began on, which its Thread opens with: the
	// transcript's first, else this one when the human typed it, as in a
	// Session's first turn, which the transcript does not hold yet. The
	// Engine keeps the first it is given.
	first := readFirstPrompt(e.TranscriptPath)
	if first == "" {
		first = typed(p.Prompt)
	}
	// The Claude Code process too, so a Session that started before Pong
	// recorded it is still ended if that process dies unseen.
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: HarnessName, ID: e.SessionID, Folder: e.Cwd,
		PermissionMode: e.PermissionMode, PID: attended.HarnessPID(), FirstPrompt: first, Transcript: e.TranscriptPath}); err != nil {
		return nil, err
	}
	if framed.MatchString(p.Prompt) {
		return nil, turn.HarnessPrompted(c, HarnessName, e.SessionID, e.Cwd)
	}
	return nil, turn.Prompted(c, HarnessName, e.SessionID, e.Cwd)
}
