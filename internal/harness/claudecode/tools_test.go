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

	"github.com/jacksonlw/pong/internal/client"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
	"github.com/jacksonlw/pong/internal/harness/turn"
)

// The agent's send_ping through the plugin's MCP server is allowed and given
// the Claude Code Session, its folder and the Harness, so the turn's end
// counts it as the agent's own finished Ping.
func TestOwnSendPingCarriesTheSession(t *testing.T) {
	c, dir := enginetest.Start(t)
	const session = "18a161df-6ebe-48e5-bd7a-585961d12947"
	// Its SessionStart ran: the turn began.
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: session, State: engine.SessionWorking}); err != nil {
		t.Fatal(err)
	}
	in, err := os.ReadFile(filepath.Join("testdata", "tools/send-ping.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := claudecode.Hook(dir, bytes.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	var got struct {
		HookSpecificOutput struct {
			HookEventName      string          `json:"hookEventName"`
			PermissionDecision string          `json:"permissionDecision"`
			UpdatedInput       json.RawMessage `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output %q: %v", out.String(), err)
	}
	h := got.HookSpecificOutput
	if h.HookEventName != "PreToolUse" || h.PermissionDecision != "allow" {
		t.Fatalf("output = %s", out.String())
	}
	// What the MCP tool then sends.
	var n engine.NewPing
	json.Unmarshal(h.UpdatedInput, &n)
	if n.Session != session || n.Harness != "claude-code" || !strings.HasSuffix(n.Folder, "/live/proj") ||
		n.Question != "Is the live check working?" || len(n.Answers) != 2 {
		t.Fatalf("updatedInput = %s", h.UpdatedInput)
	}
	n.BlocksAgent = true // the recorded call predates the rule that refuses a question that does not block
	mine, err := c.CreatePing(n)
	if err != nil {
		t.Fatal(err)
	}
	// That Session's Stop (the recorded one, given this Session's id) waits
	// on it rather than nudging, and its Pong wakes the Session.
	stopIn, _ := os.ReadFile(filepath.Join("testdata", "stop/first.json"))
	stopIn = bytes.ReplaceAll(stopIn, []byte(recordedSession), []byte(session))
	woke := make(chan string, 1)
	go func() {
		var w claudecode.Wake
		errors.As(claudecode.Hook(dir, bytes.NewReader(stopIn), &bytes.Buffer{}), &w)
		woke <- w.Text
	}()
	waitIdle(t, c, session)
	pong(t, c, mine.ID, engine.SendPong{Answer: "Yes"})
	select {
	case w := <-woke:
		if w == turn.Nudge || !strings.Contains(w, ": Yes\n") {
			t.Fatalf("woke with %q", w)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the Stop hook did not return")
	}
	if ps, _ := c.Pings(""); len(ps) != 1 {
		t.Fatalf("Pings = %+v; want only the agent's", ps)
	}
}

func waitIdle(t *testing.T, c *client.Client, session string) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if s, err := c.Session("claude-code", session); err == nil && s.State == engine.SessionIdle {
			return
		}
	}
	t.Fatal("the Session never went idle")
}

// Each hook entry of the generated plugin is what its handler needs on
// Claude Code 2.1.322: the end of a turn runs in the background (asyncRewake)
// and may wait a week; prompts held for a Pong may wait a week; the rest are
// quick and synchronous.
func TestPluginHookEntries(t *testing.T) {
	dir := t.TempDir()
	if err := claudecode.WriteMarketplace(dir, "pong"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "plugin", "hooks", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command     string `json:"command"`
				AsyncRewake bool   `json:"asyncRewake"`
				Timeout     int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	type entry struct {
		async   bool
		timeout int
	}
	got := map[string]entry{}
	for event, ms := range f.Hooks {
		for _, m := range ms {
			for _, h := range m.Hooks {
				key := event + "/" + m.Matcher
				if _, dup := got[key]; dup {
					t.Errorf("%s is registered twice", key)
				}
				got[key] = entry{h.AsyncRewake, h.Timeout}
			}
		}
	}
	long := claudecode.WaitTimeout
	want := map[string]entry{
		"SessionStart/":       {},
		"SessionStart/resume": {async: true, timeout: long},
		"SessionEnd/":         {},
		"UserPromptSubmit/":   {},
		"Stop/":               {async: true, timeout: long},
		"PreToolUse/":         {timeout: long},
		"PreToolUse/*":        {async: true, timeout: long}, // listens after a stop (stopturn.go)
		"PermissionDenied/":   {timeout: long},
		"PermissionRequest/":  {timeout: long},
	}
	for k, w := range want {
		if g, ok := got[k]; !ok || g != w {
			t.Errorf("%s = %+v (present %v), want %+v", k, g, ok, w)
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			t.Errorf("unexpected hook entry %s", k)
		}
	}
}

// The agent's send_thought is allowed and given the Session, its folder and
// the Harness, as send_note is. A turn that sent only Thoughts sent no Ping,
// so its end is still nudged for a finished one.
func TestOwnSendThoughtCarriesTheSessionAndIsNoPing(t *testing.T) {
	c, dir := enginetest.Start(t)
	const session = "18a161df-6ebe-48e5-bd7a-585961d12947"
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: session, State: engine.SessionWorking}); err != nil {
		t.Fatal(err)
	}
	// Derived from tools/send-ping.json: the same recorded call, with the
	// tool's name and input changed (testdata/README.md).
	in, err := os.ReadFile(filepath.Join("testdata", "tools/send-thought.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := claudecode.Hook(dir, bytes.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	var got struct {
		HookSpecificOutput struct {
			PermissionDecision string `json:"permissionDecision"`
			UpdatedInput       struct {
				Text, Summary, Session, Harness, Folder string
			} `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("output %q: %v", out.String(), err)
	}
	h := got.HookSpecificOutput
	u := h.UpdatedInput
	if h.PermissionDecision != "allow" || u.Session != session || u.Harness != "claude-code" || !strings.HasSuffix(u.Folder, "/live/proj") ||
		!strings.HasPrefix(u.Text, "The live check") || u.Summary == "" {
		t.Fatalf("output = %s", out.String())
	}
	if _, err := c.CreatePing(engine.NewPing{Thought: true, Session: u.Session, Harness: u.Harness, Folder: u.Folder, Question: u.Text, Summary: u.Summary}); err != nil {
		t.Fatal(err)
	}
	stopIn, _ := os.ReadFile(filepath.Join("testdata", "stop/first.json"))
	stopIn = bytes.ReplaceAll(stopIn, []byte(recordedSession), []byte(session))
	var w claudecode.Wake
	errors.As(claudecode.Hook(dir, bytes.NewReader(stopIn), &bytes.Buffer{}), &w)
	if w.Text != turn.Nudge {
		t.Fatalf("a turn of only Thoughts woke with %q; want the nudge", w.Text)
	}
}
