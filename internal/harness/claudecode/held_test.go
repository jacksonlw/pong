package claudecode_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
)

// after rewrites a fixture as the PostToolUse Claude Code sends once the
// same call has run.
func after(t *testing.T, fixture, event string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	m["hook_event_name"] = event
	out, _ := json.Marshal(m)
	return out
}

func stateOf(t *testing.T, c interface {
	Ping(string) (engine.Ping, error)
}, id string) string {
	t.Helper()
	p, err := c.Ping(id)
	if err != nil {
		t.Fatal(err)
	}
	return p.State
}

// A permission prompt answered in the terminal leaves Pong as soon as the
// call has run, and the hook that held it lets Claude Code's answer stand.
func TestAPermissionAnsweredInTheTerminalLeavesPongAtOnce(t *testing.T) {
	c, dir := enginetest.Start(t)
	out := call(t, dir, "permission/bash.json")
	p := waiting(t, c, 1)[0]
	if got := callBytes(t, dir, after(t, "permission/bash.json", "PostToolUse")); got != "" {
		t.Fatalf("PostToolUse wrote %q", got)
	}
	if s := stateOf(t, c, p.ID); s != engine.StateAnsweredInHarness {
		t.Fatalf("the Ping is %s; want answered in the Harness", s)
	}
	if got := out(); got != "" {
		t.Fatalf("the holding hook wrote %q; want nothing, so the terminal's answer stands", got)
	}
}

// A call that failed after its prompt was answered settles it too.
func TestAFailedCallSettlesItsPrompt(t *testing.T) {
	c, dir := enginetest.Start(t)
	out := call(t, dir, "permission/write.json")
	p := waiting(t, c, 1)[0]
	callBytes(t, dir, after(t, "permission/write.json", "PostToolUseFailure"))
	if s := stateOf(t, c, p.ID); s != engine.StateAnsweredInHarness {
		t.Fatalf("the Ping is %s", s)
	}
	out()
}

// While the human is at this Mac, Claude Code shows its own question dialog
// at once: Pong only mirrors it, and the mirror leaves when it is answered.
func TestAQuestionAtTheMacIsMirroredNotHeld(t *testing.T) {
	defer claudecode.SetAway(false)()
	c, dir := enginetest.Start(t)
	done := make(chan string, 1)
	go func() { done <- call(t, dir, "question/single.json")() }()
	select {
	case got := <-done:
		if got != "" {
			t.Fatalf("PreToolUse wrote %q; want nothing, so Claude Code asks", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("PreToolUse held the question while the human is at the Mac")
	}
	p := waiting(t, c, 1)[0]
	if p.Question != "Which colour should the button be?" || !p.BlocksAgent {
		t.Fatalf("mirror = %+v", p)
	}
	callBytes(t, dir, after(t, "question/single.json", "PostToolUse"))
	if s := stateOf(t, c, p.ID); s != engine.StateAnsweredInHarness {
		t.Fatalf("the mirror is %s after the dialog was answered", s)
	}
}
