package engine_test

import (
	"encoding/json"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// An allow comes only from the human's Allow to the Ping that showed the call. It covers that
// call once, for that Session; a withdrawn Allow covers nothing.
func TestOnlyTheHumansAllowAllowsACallOnce(t *testing.T) {
	c, _ := enginetest.Start(t)
	input := json.RawMessage(`{"command":"git push --force","description":"Push the branch"}`)
	ask := func(session, do string, in json.RawMessage) bool {
		t.Helper()
		ok, err := c.AskAllowed(engine.CallQuery{Harness: "claude-code", Session: session, Tool: "Bash", Input: in, Do: do})
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	newPing := func() engine.Ping {
		t.Helper()
		p, err := c.CreatePing(client.NewPing{Harness: "claude-code", Session: "s1", Question: "Push the branch by force?", BlocksAgent: true,
			Answers: []engine.Answer{{Label: "Allow"}, {Label: "Deny"}},
			Call:    &engine.Call{Tool: "Bash", Input: input, Allow: "Allow"}})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	p := newPing()
	if ask("s1", "check", input) || ask("s1", "take", input) {
		t.Fatal("allowed before the human answered")
	}
	if _, err := c.Pong(p.ID, client.SendPong{Answer: "Allow"}); err != nil {
		t.Fatal(err)
	}
	if ask("s2", "check", input) {
		t.Fatal("allowed for another Session")
	}
	if ask("s1", "check", json.RawMessage(`{"command":"rm -rf /","description":"Push the branch"}`)) {
		t.Fatal("allowed another command")
	}
	if !ask("s1", "check", json.RawMessage(`{"command":"git push --force","description":"Push it again"}`)) {
		t.Fatal("a retry that words its description again is not the same call")
	}
	if !ask("s1", "take", input) || ask("s1", "take", input) {
		t.Fatal("an allow is not used exactly once")
	}

	// A Deny allows nothing, and an Allow withdrawn before it was used is gone.
	denied := newPing()
	c.Pong(denied.ID, client.SendPong{Answer: "Deny"})
	if ask("s1", "check", input) {
		t.Fatal("a Deny allowed the call")
	}
	withdrawn := newPing()
	c.Pong(withdrawn.ID, client.SendPong{Answer: "Allow"})
	if _, err := c.WithdrawPong(withdrawn.ID); err != nil {
		t.Fatal(err)
	}
	if ask("s1", "check", input) {
		t.Fatal("a withdrawn Allow still allows the call")
	}
}

// A call refused again after it was allowed stands: it is not asked about again.
func TestACallRefusedAfterItsAllowStands(t *testing.T) {
	c, _ := enginetest.Start(t)
	input := json.RawMessage(`{"file_path":"/x"}`)
	q := engine.CallQuery{Harness: "claude-code", Session: "s1", Tool: "Edit", Input: input}
	refused := func() bool { q.Do = "refused"; ok, _ := c.AskAllowed(q); return ok }
	if refused() {
		t.Fatal("a first refusal, with no Allow, stands")
	}
	p, _ := c.CreatePing(client.NewPing{Harness: "claude-code", Session: "s1", Question: "Edit the file?", BlocksAgent: true,
		Answers: []engine.Answer{{Label: "Allow"}}, Call: &engine.Call{Tool: "Edit", Input: input, Allow: "Allow"}})
	c.Pong(p.ID, client.SendPong{Answer: "Allow"})
	if !refused() || !refused() {
		t.Fatal("a refusal after the Allow does not stand")
	}
	q.Do = "take"
	if ok, _ := c.AskAllowed(q); ok {
		t.Fatal("a call that stands refused is still allowed")
	}
}
