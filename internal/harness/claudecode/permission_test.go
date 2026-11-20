package claudecode_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
)

func labels(p engine.Ping) []string {
	var ls []string
	for _, a := range p.Answers {
		ls = append(ls, a.Label)
	}
	return ls
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Each recorded permission prompt arrives as a Ping that approves an action,
// its choices as ordinary answers and the call as a Resource; each choice
// goes back in the decision shape Claude Code 2.1.322 took.
func TestPermissionPrompts(t *testing.T) {
	for _, tc := range []struct {
		fixture, question, kind, content string
		pong                             engine.SendPong
		golden                           string
		tag                              engine.Tag
	}{
		{"permission/bash.json", "Write current date into stamp.txt?", engine.KindText, "date > stamp.txt",
			engine.SendPong{Answer: "Allow"}, "permission/bash.allow.out.json", engine.Tag{Text: "Approve command", Icon: "terminal"}},
		{"permission/write.json", "Write notes.txt?", engine.KindChange, "",
			engine.SendPong{Answer: "Deny", Text: "use notes.md instead"}, "permission/write.deny.out.json", engine.Tag{Text: "Approve edit", Icon: "pencil"}},
		{"permission/mcp-tool.json", "Allow mcp__host__echo?", engine.KindText, "",
			engine.SendPong{Answer: "Always allow"}, "permission/mcp-tool.always.out.json", engine.Tag{Text: "Approve tool", Icon: "wrench.and.screwdriver"}},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			c, dir := enginetest.Start(t)
			out := call(t, dir, tc.fixture)
			p := waiting(t, c, 1)[0]
			if p.Question != tc.question || p.Approves != engine.ApprovesAction || !p.BlocksAgent ||
				!same(labels(p), []string{"Allow", "Always allow", "Deny"}) || len(p.Resources) != 1 || p.Resources[0].Kind != tc.kind ||
				len(p.Tags) != 1 || p.Tags[0] != tc.tag {
				t.Fatalf("Ping = %+v", p)
			}
			if tc.content != "" {
				b, _, err := c.Resource(p.ID, p.Resources[0].ID)
				if err != nil || string(b) != tc.content {
					t.Fatalf("Resource = %q, %v", b, err)
				}
			}
			pong(t, c, p.ID, tc.pong)
			golden(t, out(), tc.golden)
		})
	}
}

// In auto mode Claude Code decides without the human, so the hook sends no
// Ping and writes nothing.
func TestNoPermissionPingInAutoMode(t *testing.T) {
	c, dir := enginetest.Start(t)
	in, _ := os.ReadFile("testdata/permission/bash.json")
	var e map[string]any
	json.Unmarshal(in, &e)
	e["permission_mode"] = "auto"
	b, _ := json.Marshal(e)
	f := t.TempDir() + "/bash.json"
	os.WriteFile(f, b, 0o600)
	if got := callFile(t, dir, f)(); got != "" {
		t.Fatalf("the hook wrote %q; want nothing", got)
	}
	if ps, err := c.Pings(engine.StateWaiting); err != nil || len(ps) != 0 {
		t.Fatalf("waiting Pings = %v, %v; want none", ps, err)
	}
}

// Always allow is offered only when Claude Code supplies a rule to add.
func TestNoAlwaysAllowWithoutASuggestion(t *testing.T) {
	c, dir := enginetest.Start(t)
	in, _ := os.ReadFile("testdata/permission/bash.json")
	var e map[string]any
	json.Unmarshal(in, &e)
	delete(e, "permission_suggestions")
	b, _ := json.Marshal(e)
	f := t.TempDir() + "/bash.json"
	os.WriteFile(f, b, 0o600)
	out := callFile(t, dir, f)
	p := waiting(t, c, 1)[0]
	if !same(labels(p), []string{"Allow", "Deny"}) {
		t.Fatalf("answers = %v", labels(p))
	}
	pong(t, c, p.ID, engine.SendPong{Answer: "Deny"})
	if got := out(); !strings.Contains(got, `"behavior":"deny"`) {
		t.Fatalf("Claude Code would receive %s", got)
	}
}

// Nothing is allowed without an Allow or Always allow Pong: typed text alone
// denies, with the text as the reason; a hand back writes nothing.
func TestPermissionIsNeverAllowedWithoutAPong(t *testing.T) {
	c, dir := enginetest.Start(t)
	out := call(t, dir, "permission/bash.json")
	p := waiting(t, c, 1)[0]
	pong(t, c, p.ID, engine.SendPong{Text: "not now"})
	if got := out(); got != `{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"not now"}}}`+"\n" {
		t.Fatalf("Claude Code would receive %s", got)
	}

	out = call(t, dir, "permission/mcp-tool.json")
	p = waiting(t, c, 1)[0]
	if _, err := c.HandBack(p.ID); err != nil {
		t.Fatal(err)
	}
	if got := out(); got != "" {
		t.Fatalf("hand back wrote %q; want nothing, so Claude Code shows its own prompt", got)
	}
}
