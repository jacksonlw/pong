package engine_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

const found = "The duplicate Pongs come from Inbox dropping the Session id on retry."

func thought(t *testing.T, c *client.Client, text, summary string) engine.Ping {
	t.Helper()
	p, err := c.CreatePing(engine.NewPing{Thought: true, Session: "s1", Harness: "claude-code", Folder: "/tmp/p", Question: text, Summary: summary})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// A Thought is kept in its Session's Thread, dismissed from the start, so it
// never enters the Queue.
func TestThoughtIsKeptInTheThreadOutsideTheQueue(t *testing.T) {
	c, _ := enginetest.Start(t)
	p := thought(t, c, "  "+found+"\n", "")
	if !p.Thought || !p.IsNote || p.Line || p.Question != found || len(p.Answers) != 0 || p.State != engine.StateDismissed || p.First {
		t.Fatalf("kept %+v", p)
	}
	if q, _ := c.Pings(engine.StateWaiting); len(q) != 0 {
		t.Fatalf("in the Queue: %+v", q)
	}
	th, err := c.Thread("claude-code", "s1")
	if err != nil || len(th.Pings) != 1 || th.Pings[0].ID != p.ID || !th.Pings[0].Thought {
		t.Fatalf("Thread %+v %v", th.Pings, err)
	}
}

// A Thought is one sentence of at most 140 characters, and nothing else.
func TestThoughtTakesItsSentenceAlone(t *testing.T) {
	c, _ := enginetest.Start(t)
	base := engine.NewPing{Thought: true, Session: "s1", Folder: "/tmp/p", Question: found}
	for name, tc := range map[string]struct {
		change   func(*engine.NewPing)
		mentions string
	}{
		"past 140":  {func(n *engine.NewPing) { n.Question = strings.Repeat("a", 141) }, "at most 140"},
		"empty":     {func(n *engine.NewPing) { n.Question = "  \n" }, "needs its sentence"},
		"answers":   {func(n *engine.NewPing) { n.Answers = []engine.Answer{{Label: "Yes"}} }, "no answers"},
		"resources": {func(n *engine.NewPing) { n.Resources = []engine.NewResource{{Kind: engine.KindDocument, Content: "x"}} }, "alone"},
		"tags":      {func(n *engine.NewPing) { n.Tags = []engine.Tag{{Text: "bug"}} }, "alone"},
		"context":   {func(n *engine.NewPing) { n.Context = "more" }, "alone"},
		"why":       {func(n *engine.NewPing) { n.Why = "because" }, "alone"},
		"blocks":    {func(n *engine.NewPing) { n.BlocksAgent = true }, "never blocks"},
	} {
		t.Run(name, func(t *testing.T) {
			n := base
			tc.change(&n)
			_, err := c.CreatePing(n)
			wantStatus(t, err, http.StatusUnprocessableEntity, tc.mentions)
		})
	}
	// 140 characters after trimming, counted as characters, is kept.
	n := base
	n.Question = "  " + strings.Repeat("é", 140) + "  "
	if _, err := c.CreatePing(n); err != nil {
		t.Fatalf("140 characters refused: %v", err)
	}
}

// A Thought's summary becomes its Session's, as a Ping's does.
func TestThoughtSummaryUpdatesTheSession(t *testing.T) {
	c, _ := enginetest.Start(t)
	thought(t, c, found, "Fixing the duplicate Pongs; the retry is next.")
	s, err := c.Session("claude-code", "s1")
	if err != nil || s.Summary != "Fixing the duplicate Pongs; the retry is next." {
		t.Fatalf("Session %+v %v", s, err)
	}
	thought(t, c, "Inbox now keeps the Session id when it retries.", "Retry fixed; running the tests.")
	if s, _ = c.Session("claude-code", "s1"); s.Summary != "Retry fixed; running the tests." {
		t.Fatalf("summary %q", s.Summary)
	}
}

// Nothing ends or answers a Thought.
func TestThoughtCannotBeAnsweredOrEnded(t *testing.T) {
	c, _ := enginetest.Start(t)
	p := thought(t, c, found, "")
	_, err := c.Pong(p.ID, engine.SendPong{Text: "thanks"})
	wantStatus(t, err, http.StatusConflict, "Thought")
	for name, end := range map[string]func(string) (engine.Ping, error){
		"dismiss": c.Dismiss, "withdraw": c.Withdraw, "hand back": c.HandBack, "answered in harness": c.AnsweredInHarness,
	} {
		_, err := end(p.ID)
		if err == nil {
			t.Fatalf("%s took a Thought", name)
		}
		wantStatus(t, err, http.StatusConflict, "Thought")
	}
}

// Thoughts do not count as a Session's Pings: the first Ping after them is
// still its Session's first.
func TestThoughtsDoNotTakeTheFirstPing(t *testing.T) {
	c, _ := enginetest.Start(t)
	thought(t, c, found, "")
	thought(t, c, "Inbox now keeps the Session id when it retries.", "")
	p, err := c.CreatePing(engine.NewPing{Session: "s1", Harness: "claude-code", Folder: "/tmp/p", Question: "Ship the fix now?", BlocksAgent: true,
		Answers: []engine.Answer{{Label: "Commit"}, {Label: "Wait"}}})
	if err != nil || !p.First {
		t.Fatalf("Ping after Thoughts %+v %v; want first", p, err)
	}
}
