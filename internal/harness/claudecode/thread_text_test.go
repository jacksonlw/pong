package claudecode_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
	"github.com/jacksonlw/pong/internal/harness/turn"
)

// Text the human writes in a Thread while no Ping waits reaches a Claude Code
// Session through Pong's hooks (stop.go, text.go).

func textEngine(t *testing.T) (*client.Client, string) {
	t.Helper()
	return enginetest.StartWithDir(t, engine.Config{Runners: []engine.Runner{claudecode.Runner{}}})
}

func write(t *testing.T, c *client.Client, session, text string) {
	t.Helper()
	if _, err := c.FollowUp(claudecode.HarnessName, session, text); err != nil {
		t.Fatal(err)
	}
}

const wroteDB = "Your human wrote in Pong: Use the staging database"

func TestTextReachesAWorkingSessionBeforeItsNextToolCall(t *testing.T) {
	c, dir := textEngine(t)
	run(t, dir, "prompt/typed.json")
	toolCall := refusedCall(t, "PreToolUse", func(e, _ map[string]any) { e["session_id"] = recordedSession })
	if got := callFile(t, dir, toolCall)(); got != "" {
		t.Fatalf("with no text kept the hook wrote %q", got)
	}
	write(t, c, recordedSession, "Use the staging database")
	want := `{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"` + wroteDB + `"}}` + "\n"
	if got := callFile(t, dir, toolCall)(); got != want {
		t.Fatalf("the hook wrote %q", got)
	}
	if got := callFile(t, dir, toolCall)(); got != "" {
		t.Fatalf("the text was handed over twice: %q", got)
	}
	// The text is the human's, not a Ping the agent sent: the turn still ends
	// with the nudge to send a finished Ping.
	if _, wake := run(t, dir, "stop/first.json"); wake != turn.Nudge {
		t.Fatalf("the turn ended with %q; want the nudge", wake)
	}
}

// A turn that ends with text still kept (it made no further tool call) is
// woken with it, before any nudge to send a finished Ping.
func TestTextKeptAtTheEndOfATurnWakesTheSession(t *testing.T) {
	c, dir := textEngine(t)
	run(t, dir, "prompt/typed.json")
	write(t, c, recordedSession, "Use the staging database")
	if out, wake := run(t, dir, "stop/first.json"); out != "" || wake != wroteDB {
		t.Fatalf("wrote %q, woke with %q", out, wake)
	}
	if s := state(t, c, recordedSession); s != engine.SessionWorking {
		t.Fatalf("Session is %s, want working", s)
	}
}

// After "Nothing more" a Session the human started keeps listening, so text
// written later still wakes it.
func TestTextWakesAnIdleSessionAfterNothingMore(t *testing.T) {
	c, dir := textEngine(t)
	wait := stopHook(t, dir, "stop/woken.json")
	pong(t, c, waiting(t, c, 1)[0].ID, engine.SendPong{Answer: engine.NothingMore})
	time.Sleep(3 * enginetest.WaitHold)
	write(t, c, woken, "Use the staging database")
	if out, wake := wait(); out != "" || wake != wroteDB {
		t.Fatalf("wrote %q, woke with %q", out, wake)
	}
}

// resumed is the SessionStart payload of a Session opened again: the recorded
// one with its source changed, since a resume was not recorded.
func resumed(t *testing.T, session string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/recorded/ask-and-permissions/SessionStart.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	var e map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(b), &e); err != nil {
		t.Fatal(err)
	}
	e["source"], e["session_id"] = "resume", session
	out, _ := json.Marshal(e)
	return out
}

// listen runs the listener beside a resumed Session and returns a function
// that waits for the text it wakes the Session with ("" when it wakes nothing).
func listen(t *testing.T, dir string, payload []byte) func() string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		var w claudecode.Wake
		errors.As(claudecode.Listen(dir, bytes.NewReader(payload)), &w)
		done <- w.Text
	}()
	return func() string {
		select {
		case s := <-done:
			return s
		case <-time.After(5 * time.Second):
			t.Fatal("the listener did not return")
			return ""
		}
	}
}

// A Session moved into the Claude app is a resumed one there: the hook that
// waited at the end of its turn stayed behind in the terminal, so a listener
// starts with the Session and carries the Pong on its finished Ping.
func TestAResumedSessionHearsThePongOnItsFinishedPing(t *testing.T) {
	c, dir := textEngine(t)
	stopHook(t, dir, "stop/woken.json") // its turn ended in the terminal; that hook is left behind
	p := waiting(t, c, 1)[0]
	if out := callBytes(t, dir, resumed(t, woken)); !strings.Contains(out, "SessionStart") {
		t.Fatalf("the Session's own start hook wrote %q", out)
	}
	if s := state(t, c, woken); s != engine.SessionIdle {
		t.Fatalf("a resumed Session is %s, want idle", s)
	}
	heard := listen(t, dir, resumed(t, woken))
	time.Sleep(50 * time.Millisecond)
	pong(t, c, p.ID, engine.SendPong{Text: "now add a changelog line"})
	if got := heard(); !strings.Contains(got, "finished Ping in Pong: now add a changelog line\nOpen your reply with one line that quotes this Pong") {
		t.Fatalf("woke with %q", got)
	}
	if s := state(t, c, woken); s != engine.SessionWorking {
		t.Fatalf("Session is %s, want working", s)
	}
}

// With no Ping waiting, the listener carries text written in the Thread.
func TestAResumedSessionHearsTextWrittenInItsThread(t *testing.T) {
	c, dir := textEngine(t)
	callBytes(t, dir, resumed(t, recordedSession))
	heard := listen(t, dir, resumed(t, recordedSession))
	time.Sleep(50 * time.Millisecond)
	write(t, c, recordedSession, "Use the staging database")
	if got := heard(); got != wroteDB {
		t.Fatalf("woke with %q", got)
	}
}

// A Session that starts fresh, or is at work again by the time the listener
// looks, has nothing to listen for.
func TestTheListenerLeavesASessionThatIsNotIdleAlone(t *testing.T) {
	c, dir := textEngine(t)
	started, _ := os.ReadFile("testdata/recorded/ask-and-permissions/SessionStart.jsonl")
	if got := listen(t, dir, bytes.TrimSpace(started))(); got != "" {
		t.Fatalf("a fresh Session was woken with %q", got)
	}
	callBytes(t, dir, resumed(t, recordedSession))
	c.RegisterSession(engine.RegisterSession{Harness: claudecode.HarnessName, ID: recordedSession, State: engine.SessionWorking})
	if got := listen(t, dir, resumed(t, recordedSession))(); got != "" {
		t.Fatalf("a working Session was woken with %q", got)
	}
}

// The end of a turn tells the wrapper Pong started the Session under that it
// can now be moved into the Claude app; a turn held by the nudge has not ended.
func TestTheEndOfATurnMarksTheSessionReadyToMove(t *testing.T) {
	c, dir := textEngine(t)
	mark := filepath.Join(t.TempDir(), "mark")
	t.Setenv(claudecode.MoveMarkEnv, mark)
	run(t, dir, "prompt/typed.json")
	if _, wake := run(t, dir, "stop/first.json"); wake != turn.Nudge {
		t.Fatalf("woke with %q; want the nudge", wake)
	}
	if _, err := os.Stat(mark); err == nil {
		t.Fatal("marked ready to move while the nudge holds the turn")
	}
	stopHook(t, dir, "stop/woken.json") // it goes on to wait for the Pong
	waiting(t, c, 1)
	// Its transcript is not on this computer, so it has no title to carry.
	if b, err := os.ReadFile(mark); err != nil || len(b) != 0 {
		t.Fatalf("not marked ready to move, or with a title from nowhere: %q %v", b, err)
	}
}
