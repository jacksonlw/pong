package claudecode

import (
	"encoding/json"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/turn"
)

// The end of a turn (Stop) is registered as an asyncRewake hook: Claude Code
// 2.1.322 runs it in the background, so the terminal stays usable, and when
// it exits 2 its standard error wakes the Session, even when idle, as a new
// turn. That is how the nudge, a Pong and text the human wrote in the Thread
// (text.go) reach the agent. The woken
// turn's Stop carries stop_hook_active true, which is how the nudge is kept
// to once. Claude Code sends SIGTERM to a hook still running when the Session
// ends, which leaves the finished Ping waiting.
func init() { on("Stop", stop) }

// Wake is returned by a handler to wake the Session with Text: the hook
// writes Text on standard error and exits 2.
type Wake struct{ Text string }

func (w Wake) Error() string { return w.Text }

type stopEvent struct {
	StopHookActive       bool   `json:"stop_hook_active"`
	LastAssistantMessage string `json:"last_assistant_message"`
}

func stop(c *client.Client, e Event) (any, error) {
	var s stopEvent
	if err := json.Unmarshal(e.Raw, &s); err != nil {
		return nil, err
	}
	// Text written while the turn ran, which no tool call took, comes first:
	// the turn it wakes ends with the finished Ping.
	if texts, _ := c.TakeText(HarnessName, e.SessionID); len(texts) > 0 {
		return wakeWith(c, e, handOver(texts))
	}
	// A turn the human stopped gets no nudge and no Ping from its last
	// message (stopturn.go). Claude Code 2.1.329 does not call this hook for
	// one; if a later version does, it only listens.
	if sess, err := c.Session(HarnessName, e.SessionID); err == nil && sess.Stop != nil && sess.Stop.State == engine.StopStopped {
		return listenIdle(c, e)
	}
	nudge, waits, err := turn.End(c, turn.Turn{Harness: HarnessName, Session: e.SessionID, Folder: e.Cwd,
		LastMessage: s.LastAssistantMessage, Nudged: s.StopHookActive})
	if err != nil {
		return nil, err
	}
	if nudge {
		return nil, Wake{turn.Nudge}
	}
	markTurnEnded(e) // move.go
	if len(waits) > 0 {
		as, err := turn.WaitAll(c, waits)
		if err != nil {
			return nil, err
		}
		if len(as) > 0 {
			return wakeWith(c, e, turn.WakeText(as, answerText))
		}
	}
	// Nothing waits for a Pong any more. A Session the human started stays
	// open in their terminal, so the hook listens for text written in its
	// Thread for as long as it sits idle; the Engine ends the listening when
	// the human types in the terminal, or at once for a Session it started.
	return listenIdle(c, e)
}

func listenIdle(c *client.Client, e Event) (any, error) {
	texts, err := c.ListenText(HarnessName, e.SessionID)
	if err != nil || len(texts) == 0 {
		return nil, err
	}
	return wakeWith(c, e, handOver(texts))
}

// wakeWith wakes the Session into a new turn, which starts now. The hook
// outlives its turn, so Pings a later turn sent may be waiting by now; they
// stay waiting.
func wakeWith(c *client.Client, e Event, text string) (any, error) {
	if err := turn.Began(c, HarnessName, e.SessionID, e.Cwd); err != nil {
		return nil, err
	}
	return nil, Wake{text}
}
