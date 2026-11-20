package claudecode_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/attended"
)

// harnessIs makes the hook see args as the command line of its Claude Code.
func harnessIs(t *testing.T, args ...string) {
	t.Helper()
	was := attended.Harness
	attended.Harness = func() []string { return args }
	t.Cleanup(func() { attended.Harness = was })
}

// refusedCall is the recorded refusal's call as another event sees it; set
// changes the copy.
func refusedCall(t *testing.T, event string, set func(e, input map[string]any)) string {
	t.Helper()
	in, err := os.ReadFile("testdata/denied/bash.json")
	if err != nil {
		t.Fatal(err)
	}
	var e map[string]any
	json.Unmarshal(in, &e)
	e["hook_event_name"] = event
	delete(e, "reason")
	if set != nil {
		set(e, e["tool_input"].(map[string]any))
	}
	b, _ := json.Marshal(e)
	f := t.TempDir() + "/event.json"
	os.WriteFile(f, b, 0o600)
	return f
}

const (
	retryOut = `{"hookSpecificOutput":{"hookEventName":"PermissionDenied","retry":true}}` + "\n"
	askOut   = `{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"The human allowed this call in Pong."}}` + "\n"
	allowOut = `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}` + "\n"
)

// A call auto mode refused becomes an approval Ping. Allow reaches Claude Code
// as its own permission verdict: the agent is told it may retry, the retried
// call goes to the permission prompt, and the prompt is allowed, once.
func TestAllowAfterAutoModeRefusal(t *testing.T) {
	harnessIs(t, "claude")
	c, dir := enginetest.Start(t)
	out := call(t, dir, "denied/bash.json")
	p := waiting(t, c, 1)[0]
	if p.Question != "Auto mode refused this. Force push main to origin as specified in TASK.md?" || p.Approves != engine.ApprovesAction ||
		!p.BlocksAgent || !same(labels(p), []string{"Allow", "Deny"}) || len(p.Resources) != 1 {
		t.Fatalf("Ping = %+v", p)
	}
	if b, _, err := c.Resource(p.ID, p.Resources[0].ID); err != nil || string(b) != "git push --force origin main" {
		t.Fatalf("Resource = %q, %v", b, err)
	}
	pong(t, c, p.ID, engine.SendPong{Answer: "Allow"})
	if got := out(); got != retryOut {
		t.Fatalf("PermissionDenied answer = %q", got)
	}

	// Another command is still auto mode's to settle.
	other := refusedCall(t, "PreToolUse", func(_, in map[string]any) { in["command"] = "git push --force origin release" })
	if got := callFile(t, dir, other)(); got != "" {
		t.Fatalf("another command got %q; want nothing", got)
	}
	// The agent sends the command again, describing it in new words.
	again := func(event string) string {
		return refusedCall(t, event, func(_, in map[string]any) { in["description"] = "Retry the force push" })
	}
	if got := callFile(t, dir, again("PreToolUse"))(); got != askOut {
		t.Fatalf("PreToolUse answer = %q", got)
	}
	if got := callFile(t, dir, again("PermissionRequest"))(); got != allowOut {
		t.Fatalf("PermissionRequest answer = %q", got)
	}
	// The Allow covered one call.
	if got := callFile(t, dir, again("PreToolUse"))(); got != "" {
		t.Fatalf("a second retry got %q; want nothing", got)
	}
	if ps, err := c.Pings(engine.StateWaiting); err != nil || len(ps) != 0 {
		t.Fatalf("waiting Pings = %v, %v; want none", ps, err)
	}
}

// Without an Allow the refusal stands: nothing is written and the same call
// sent again is still auto mode's to settle.
func TestRefusalStandsWithoutAllow(t *testing.T) {
	harnessIs(t, "claude")
	c, dir := enginetest.Start(t)
	for _, answer := range []engine.SendPong{{Answer: "Deny"}, {Text: "not on main"}} {
		out := call(t, dir, "denied/bash.json")
		pong(t, c, waiting(t, c, 1)[0].ID, answer)
		if got := out(); got != "" {
			t.Fatalf("%+v wrote %q; want nothing", answer, got)
		}
		if got := callFile(t, dir, refusedCall(t, "PreToolUse", nil))(); got != "" {
			t.Fatalf("after %+v the retried call got %q; want nothing", answer, got)
		}
	}
}

// In the Claude app auto mode refused some calls again after the human's
// Allow had sent them to the prompt (testdata/README.md, "Auto mode
// refusals"). Each refusal used to bring a new Allow Ping and another retry.
// A call refused again after one Allow gets no Ping and no retry, then or
// later in the Session, while a different call is still asked about.
func TestNoSecondRetryAfterRefusedAgain(t *testing.T) {
	for _, c := range []struct {
		name  string
		tool  string
		input map[string]any
	}{
		{"recorded Bash", "", nil},
		{"the Claude app's start_session", "mcp__ccd_session__start_session",
			map[string]any{"initiation": "user_asked", "title": "Add the export button", "context": "fresh", "prompt": "Add the export button."}},
		{"an Edit", "Edit",
			map[string]any{"file_path": "/tmp/repo/.claude/settings.json", "old_string": "a", "new_string": "b"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			harnessIs(t, "claude")
			cl, dir := enginetest.Start(t)
			payload := func(event string, change func(in map[string]any)) string {
				return refusedCall(t, event, func(e, in map[string]any) {
					if c.tool != "" {
						e["tool_name"], e["tool_input"] = c.tool, c.input
						in = c.input
					}
					if event == "PermissionDenied" {
						e["reason"] = "[Undermining Oversight]"
					}
					if change != nil {
						change(in)
					}
				})
			}
			refused := payload("PermissionDenied", nil)

			out := callFile(t, dir, refused)
			pong(t, cl, waiting(t, cl, 1)[0].ID, engine.SendPong{Answer: "Allow"})
			if got := out(); got != retryOut {
				t.Fatalf("first refusal answered %q; want a retry", got)
			}
			if got := callFile(t, dir, payload("PreToolUse", nil))(); got != askOut {
				t.Fatalf("retried call answered %q; want ask", got)
			}
			// Auto mode refuses the retried call anyway, and again later.
			for i := range 2 {
				if got := callFile(t, dir, refused)(); got != "" {
					t.Fatalf("refusal %d after the Allow answered %q; want nothing", i+2, got)
				}
				if ps, err := cl.Pings(engine.StateWaiting); err != nil || len(ps) != 0 {
					t.Fatalf("refusal %d after the Allow made Pings %v, %v; want none", i+2, ps, err)
				}
				if got := callFile(t, dir, payload("PreToolUse", nil))(); got != "" {
					t.Fatalf("call sent again after refusal %d answered %q; want nothing", i+2, got)
				}
			}
			// Another call is still the human's to allow.
			other := payload("PermissionDenied", func(in map[string]any) { in["other"] = "call" })
			out = callFile(t, dir, other)
			pong(t, cl, waiting(t, cl, 1)[0].ID, engine.SendPong{Answer: "Allow"})
			if got := out(); got != retryOut {
				t.Fatalf("another call answered %q; want a retry", got)
			}
		})
	}
}
