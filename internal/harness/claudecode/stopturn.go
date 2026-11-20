package claudecode

import (
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// A stop the human asked for in Pong ends the turn at the agent's next tool
// call, whatever the tool, Pong's own included. Recorded on Claude
// Code 2.1.329 (testdata/README.md, "A hook that stops a turn"):
//
//   - continue false ends the turn after the hook, but alone it lets the tool
//     call run first, so the answer also denies the call.
//   - The terminal shows the reason. The agent writes nothing more, and the
//     Session stays open.
//   - The Stop hook does not fire for a turn stopped this way, so nothing at
//     the end of the turn listens for text. A second PreToolUse hook,
//     registered with asyncRewake, runs in the background beside every tool
//     call for that: ListenAfterStop.

// StopsTurns marks Claude Code as a Harness whose hooks can stop a turn
// (engine.StopRunner).
func (Runner) StopsTurns() {}

var _ engine.StopRunner = Runner{}

// StoppedReason is what Claude Code shows in the terminal when a stop takes.
const StoppedReason = "Your human stopped this turn in Pong."

// stopTurn is the PreToolUse answer that denies the tool call and ends the turn.
type stopTurn struct {
	Continue           bool               `json:"continue"`
	StopReason         string             `json:"stopReason"`
	HookSpecificOutput preToolUseDecision `json:"hookSpecificOutput"`
}

// takeStop answers a PreToolUse event with the stop when one was asked for
// e's Session; with no mark it costs one look at a file and returns nil.
func takeStop(c *client.Client, e Event) any {
	if _, err := os.Stat(engine.StopMark(e.Dir, e.SessionID)); err != nil {
		return nil
	}
	if stopped, _ := c.TakeStop(HarnessName, e.SessionID); !stopped {
		return nil
	}
	return stopTurn{StopReason: StoppedReason, HookSpecificOutput: preToolUseDecision{
		HookEventName: "PreToolUse", PermissionDecision: "deny", PermissionDecisionReason: StoppedReason}}
}

// stopSettles is how long ListenAfterStop waits for the PreToolUse hook that
// runs beside it to take the stop.
var stopSettles = 10 * time.Second

// ListenAfterStop is the background PreToolUse hook. It returns at once
// unless a stop is taking at this tool call; then it sits beside the stopped
// Session for as long as it is idle and wakes it with text written in its
// Thread. It returns a Wake as its error, as the Stop hook does.
func ListenAfterStop(dir string, in io.Reader) error {
	raw, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	var e Event
	if err := json.Unmarshal(raw, &e); err != nil {
		return err
	}
	sent, stopped := engine.StopMark(dir, e.SessionID), engine.StoppedMark(dir, e.SessionID)
	// The Engine writes stopped before it removes sent, so looking in this
	// order never misses a stop that is taking.
	for until := time.Now().Add(stopSettles); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(sent); err != nil {
			break
		}
		if time.Now().After(until) {
			return nil
		}
	}
	if _, err := os.Stat(stopped); err != nil {
		return nil
	}
	c, err := client.Load(dir)
	if err != nil {
		return nil
	}
	texts, err := c.ListenText(HarnessName, e.SessionID)
	if err != nil || len(texts) == 0 {
		return nil
	}
	_, err = wakeWith(c, e, handOver(texts))
	return err
}
