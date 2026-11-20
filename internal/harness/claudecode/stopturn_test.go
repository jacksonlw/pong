package claudecode_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
)

// working registers the Session of a recorded payload as working and
// returns its id.
func working(t *testing.T, c *client.Client, fixture string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	var e claudecode.Event
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: claudecode.HarnessName, ID: e.SessionID, Folder: t.TempDir(), State: engine.SessionWorking}); err != nil {
		t.Fatal(err)
	}
	return e.SessionID
}

func askStop(t *testing.T, c *client.Client, session string) engine.Stop {
	t.Helper()
	stop, err := c.Stop(claudecode.HarnessName, session)
	if err != nil {
		t.Fatal(err)
	}
	return stop
}

// The payloads are the two PreToolUse events that were answered with a stop
// on the real Claude Code (testdata/README.md): a shell command, and Pong's
// own send_ping.
func TestAStopEndsTheTurnAtTheNextToolCall(t *testing.T) {
	want, err := os.ReadFile("testdata/stopped/stop.out.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ fixture, asToday string }{
		{"stopped/bash.json", ""},
		{"stopped/send-ping.json", `"permissionDecision":"allow"`},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			c, dir := textEngine(t)
			id := working(t, c, tc.fixture)
			left := agentPing(t, c, id) // a Ping the turn sent before it was stopped
			today := func(when string) {
				t.Helper()
				out, _ := run(t, dir, tc.fixture)
				if (tc.asToday == "") != (out == "") || !strings.Contains(out, tc.asToday) || strings.Contains(out, "continue") {
					t.Fatalf("%s the hook wrote %q", when, out)
				}
			}
			today("with no stop asked")
			stop := askStop(t, c, id)
			if out, _ := run(t, dir, tc.fixture); out != string(want) {
				t.Fatalf("the hook wrote %q\nwant %q", out, want)
			}
			sess, err := c.Session(claudecode.HarnessName, id)
			if err != nil || sess.Stop == nil || sess.Stop.State != engine.StopStopped || sess.State != engine.SessionIdle {
				t.Fatalf("Session: %+v %+v %v", sess, sess.Stop, err)
			}
			if _, err := os.Stat(engine.StopMark(dir, id)); err == nil {
				t.Error("the mark stayed after the stop took")
			}
			th, err := c.Thread(claudecode.HarnessName, id)
			if err != nil || len(th.Pings) == 0 {
				t.Fatalf("Thread: %+v %v", th.Pings, err)
			}
			if line := th.Pings[len(th.Pings)-1]; !line.Line || line.Question != "You stopped this turn from "+stop.Device+"." || len(line.Answers) != 0 || line.State == engine.StateWaiting {
				t.Errorf("the Thread's line: %+v", line)
			}
			// The stopped turn's own Ping still waits, and nothing was added
			// to the Queue: no Ping about to be sent, none from Pong.
			if ps := waiting(t, c, 1); len(ps) != 1 || ps[0].ID != left.ID {
				t.Errorf("waiting Pings: %+v", ps)
			}
			// The stop is used up: the next turn's tool calls go ahead.
			if _, err := c.RegisterSession(engine.RegisterSession{Harness: claudecode.HarnessName, ID: id, State: engine.SessionWorking}); err != nil {
				t.Fatal(err)
			}
			today("in the next turn")
		})
	}
}

// Claude Code 2.1.329 does not call the Stop hook for a turn a hook stopped.
// If a version does, the hook sends no nudge and writes no Ping from the
// last message: it only listens for text.
func TestTheStopHookOfAStoppedTurnOnlyListens(t *testing.T) {
	c, dir := textEngine(t)
	run(t, dir, "prompt/typed.json")
	askStop(t, c, recordedSession)
	toolCall := refusedCall(t, "PreToolUse", func(e, _ map[string]any) { e["session_id"] = recordedSession })
	if out := callFile(t, dir, toolCall)(); !strings.Contains(out, `"continue":false`) {
		t.Fatalf("the stop did not take: %q", out)
	}
	hook := stopHook(t, dir, "stop/first.json")
	time.Sleep(100 * time.Millisecond) // long enough for a nudge or a Ping to show
	if ps, _ := c.Pings(engine.StateWaiting); len(ps) != 0 {
		t.Fatalf("Pong wrote a Ping for a stopped turn: %+v", ps)
	}
	write(t, c, recordedSession, "Use the staging database")
	if _, wake := hook(); wake != wroteDB {
		t.Fatalf("woke with %q", wake)
	}
}

// afterStop runs the background PreToolUse hook and returns a function that
// waits for the text it wakes the Session with ("" when it wakes nothing).
func afterStop(t *testing.T, dir, fixture string) func() string {
	t.Helper()
	in, err := os.ReadFile("testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		var w claudecode.Wake
		errors.As(claudecode.ListenAfterStop(dir, bytes.NewReader(in)), &w)
		done <- w.Text
	}()
	return func() string {
		select {
		case s := <-done:
			return s
		case <-time.After(5 * time.Second):
			t.Fatal("the background hook did not return")
			return ""
		}
	}
}

func TestTextWakesAStoppedSession(t *testing.T) {
	const fixture = "stopped/bash.json"
	c, dir := textEngine(t)
	id := working(t, c, fixture)
	if got := afterStop(t, dir, fixture)(); got != "" {
		t.Fatalf("with no stop asked the background hook woke the Session with %q", got)
	}
	askStop(t, c, id)
	// Both PreToolUse hooks run beside the same tool call, in either order.
	listening := afterStop(t, dir, fixture)
	run(t, dir, fixture)
	late := afterStop(t, dir, fixture)
	write(t, c, id, "Use the staging database")
	a, b := listening(), late()
	if (a == wroteDB) == (b == wroteDB) || a+b != wroteDB {
		t.Fatalf("woke with %q and %q; want the text once", a, b)
	}
	if got := state(t, c, id); got != engine.SessionWorking {
		t.Errorf("the woken Session is %s", got)
	}
}

func TestNothingListensAfterAStopThatWasMissed(t *testing.T) {
	const fixture = "stopped/bash.json"
	c, dir := textEngine(t)
	id := working(t, c, fixture)
	askStop(t, c, id)
	listening := afterStop(t, dir, fixture)
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: claudecode.HarnessName, ID: id, State: engine.SessionIdle}); err != nil {
		t.Fatal(err)
	}
	if got := listening(); got != "" {
		t.Fatalf("woke with %q", got)
	}
}
