package codex

import (
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/harness/turn"
)

// The end of a turn (Stop). Codex documents that a Stop hook answering
// {"decision":"block","reason":R} does not reject the turn: R becomes the
// prompt of a continuation. That is how a Pong reaches the agent; a turn
// without a Ping gets the one Pong builds from its last message.
// Codex documents only synchronous
// Stop hooks as able to continue a turn, so the hook waits for the Pong with
// a long timeout (WaitTimeout). Unverified against the real Codex.
func stop(c *client.Client, e Event) (any, error) {
	waits, err := turn.End(c, turn.Turn{Harness: HarnessName, Session: e.SessionID, Folder: e.Cwd,
		LastMessage: e.LastAssistantMessage})
	if err != nil {
		return nil, err
	}
	for _, p := range waits {
		remember(c, p.ID)
		defer forget(p.ID)
	}
	as, err := turn.WaitAll(c, waits)
	if err != nil || len(as) == 0 {
		return nil, err
	}
	// The Pong continues the Session in a new turn, which starts now.
	if err := turn.Began(c, HarnessName, e.SessionID, e.Cwd); err != nil {
		return nil, err
	}
	return block{"block", turn.WakeText(as, answerText)}, nil
}

type block struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}
