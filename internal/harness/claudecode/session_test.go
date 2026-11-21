package claudecode_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/integrations"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/attended"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
)

// recorded returns the first payload of a recorded event.
func recorded(t *testing.T, run, event string) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "recorded", run, event+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(nil, 1<<20)
	if !s.Scan() {
		t.Fatalf("no %s payload", event)
	}
	return append([]byte{}, s.Bytes()...)
}

func hook(t *testing.T, dir string, in []byte) string {
	t.Helper()
	var out bytes.Buffer
	if err := claudecode.Hook(dir, bytes.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestSessionStartRegistersAndGivesTheStandingInstruction(t *testing.T) {
	c, dir := enginetest.Start(t)
	was := attended.HarnessPID
	attended.HarnessPID = func() int { return 4242 }
	t.Cleanup(func() { attended.HarnessPID = was })
	out := hook(t, dir, recorded(t, "ask-and-permissions", "SessionStart"))

	var got struct {
		HookSpecificOutput struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output %q: %v", out, err)
	}
	if got.HookSpecificOutput.HookEventName != "SessionStart" || got.HookSpecificOutput.AdditionalContext != integrations.StandingInstruction+claudecode.RetryInstruction+claudecode.CommandInstruction {
		t.Fatalf("output = %s", out)
	}
	s, err := c.Session("claude-code", "dc68d69d-3138-4b80-8497-63951f30e9aa")
	if err != nil {
		t.Fatal(err)
	}
	if s.Folder != "/private/tmp/claude-501/-Users-me-pong/820f22ca-c4af-470d-80d9-fdc6020a3b61/scratchpad/rec/proj" ||
		s.StartedBy != engine.StartedByHuman || s.State != engine.SessionWorking ||
		s.ResumeCommand != "cd "+s.Folder+" && claude --resume dc68d69d-3138-4b80-8497-63951f30e9aa" || s.PID != 4242 {
		t.Fatalf("Session = %+v", s)
	}
}

func TestSessionEndIsRecorded(t *testing.T) {
	c, dir := enginetest.Start(t)
	hook(t, dir, recorded(t, "plan-mode", "SessionStart"))
	if out := hook(t, dir, recorded(t, "plan-mode", "SessionEnd")); out != "" {
		t.Fatalf("SessionEnd wrote %q", out)
	}
	s, err := c.Session("claude-code", "db478e84-d472-4920-a67e-e18303060599")
	if err != nil {
		t.Fatal(err)
	}
	if s.State != engine.SessionEnded {
		t.Fatalf("Session = %+v", s)
	}
}

func TestSessionEventsFailOpenWithNoEngine(t *testing.T) {
	for _, ev := range []string{"SessionStart", "SessionEnd"} {
		var out bytes.Buffer
		claudecode.Hook(t.TempDir(), bytes.NewReader(recorded(t, "plan-mode", ev)), &out)
		if out.Len() != 0 {
			t.Errorf("%s wrote %q with no Engine", ev, out.String())
		}
	}
}

// A Session moved into the Claude app is open there before its first copy
// ends: that copy's SessionEnd, arriving after the app's copy took the
// Session, leaves it open. Its own end still ends it.
func TestTheEndOfACopyThatNoLongerHoldsTheSessionLeavesItOpen(t *testing.T) {
	c, dir := enginetest.Start(t)
	id := "db478e84-d472-4920-a67e-e18303060599" // plan-mode's
	was := attended.HarnessPID
	t.Cleanup(func() { attended.HarnessPID = was })
	attended.HarnessPID = func() int { return 4358 } // the first copy
	hook(t, dir, recorded(t, "plan-mode", "SessionStart"))
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: id, PID: 4816, State: engine.SessionIdle}); err != nil {
		t.Fatal(err) // the app's copy
	}
	hook(t, dir, recorded(t, "plan-mode", "SessionEnd"))
	if s, _ := c.Session("claude-code", id); s.State != engine.SessionIdle {
		t.Fatalf("the first copy's end left it %s", s.State)
	}
	attended.HarnessPID = func() int { return 4816 }
	hook(t, dir, recorded(t, "plan-mode", "SessionEnd"))
	if s, _ := c.Session("claude-code", id); s.State != engine.SessionEnded {
		t.Fatalf("the app's copy's end left it %s", s.State)
	}
}
