package engine_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

const (
	neitherMsg  = "this Ping neither blocks you, nor is finished, nor is a Note: set blocks_agent for a question, finished for finished work, or use send_note for news"
	planDocMsg  = "a plan to approve must be attached as a document Resource: put the plan in resources with kind document, and keep context to a sentence or two"
	planContent = "1. Add the column\n2. Backfill it"
)

// refused fails unless err is a 400 whose message contains says.
func refused(t *testing.T, err error, says string) {
	t.Helper()
	var e *client.Error
	if !errors.As(err, &e) || e.Status != http.StatusBadRequest || !strings.Contains(e.Message, says) {
		t.Fatalf("err = %v, want 400 saying %q", err, says)
	}
}

// A Ping an agent wrote must say its shape: it blocks, it is finished, or it
// is a Note. Finished work never blocks. A plan to approve comes as a document.
func TestPingsSayTheirShape(t *testing.T) {
	c, _ := enginetest.Start(t)
	plan := []engine.NewResource{{Title: "Migration plan", Kind: engine.KindDocument, Content: planContent}}
	base := client.NewPing{Session: "s1", Folder: "/tmp/p", Question: "Migration written."}
	with := func(f func(*client.NewPing)) client.NewPing { n := base; f(&n); return n }
	for name, tc := range map[string]struct {
		in   client.NewPing
		says string // "" means accepted
	}{
		"neither":               {base, neitherMsg},
		"neither with answers":  {with(func(n *client.NewPing) { n.Answers = []engine.Answer{{Label: "Run it"}} }), neitherMsg},
		"finished and blocking": {with(func(n *client.NewPing) { n.Finished, n.BlocksAgent = true, true }), "finished work does not hold you"},
		"finished Note":         {with(func(n *client.NewPing) { n.Finished, n.IsNote = true, true }), "a Note is never finished"},
		"plan without document": {with(func(n *client.NewPing) { n.BlocksAgent, n.Approves, n.Context = true, engine.ApprovesPlan, planContent }), planDocMsg},
		"plan with only text": {with(func(n *client.NewPing) {
			n.BlocksAgent, n.Approves, n.Resources = true, engine.ApprovesPlan, []engine.NewResource{{Title: "Migration plan", Kind: engine.KindText, Content: planContent}}
		}), planDocMsg},
		"finished":                {with(func(n *client.NewPing) { n.Finished = true }), ""},
		"blocking":                {with(func(n *client.NewPing) { n.BlocksAgent = true }), ""},
		"note":                    {with(func(n *client.NewPing) { n.IsNote = true }), ""},
		"plan with document":      {with(func(n *client.NewPing) { n.BlocksAgent, n.Approves, n.Resources = true, engine.ApprovesPlan, plan }), ""},
		"action without document": {with(func(n *client.NewPing) { n.BlocksAgent, n.Approves = true, engine.ApprovesAction }), ""},
		// Captured Pings are taken as they come.
		"captured neither":           {with(func(n *client.NewPing) { n.Captured = true }), ""},
		"captured plan, no document": {with(func(n *client.NewPing) { n.Captured, n.BlocksAgent, n.Approves = true, true, engine.ApprovesPlan }), ""},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := c.CreatePing(tc.in)
			if tc.says != "" {
				refused(t, err, tc.says)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if p.Finished != tc.in.Finished {
				t.Fatalf("finished = %v, sent %v", p.Finished, tc.in.Finished)
			}
		})
	}
}

// Nothing more is offered on a finished Ping and on a captured Ping that does
// not block, never because a Ping simply does not block.
func TestNothingMoreFollowsFinished(t *testing.T) {
	c, _ := enginetest.Start(t)
	offers := func(p engine.Ping) bool {
		return len(p.Answers) > 0 && p.Answers[len(p.Answers)-1].Label == engine.NothingMore
	}
	for name, tc := range map[string]struct {
		in   client.NewPing
		want bool
	}{
		"finished":               {client.NewPing{Session: "s1", Question: "Migration written.", Finished: true, Answers: []engine.Answer{{Label: "Run it"}}}, true},
		"blocking":               {client.NewPing{Session: "s1", Question: "Which table?", BlocksAgent: true, Answers: []engine.Answer{{Label: "Users"}}}, false},
		"note":                   {client.NewPing{Session: "s1", Question: "Build passed.", IsNote: true}, false},
		"captured, not blocking": {client.NewPing{Session: "s1", Question: "All done here.", Captured: true}, true},
		"captured, blocking":     {client.NewPing{Session: "s1", Question: "Run this shell command?", Captured: true, BlocksAgent: true}, false},
	} {
		p, err := c.CreatePing(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if offers(p) != tc.want {
			t.Fatalf("%s: answers %+v", name, p.Answers)
		}
	}
}

// finished travels with the Ping: the API, the store and the event stream.
func TestFinishedIsKept(t *testing.T) {
	c, dir := enginetest.Start(t)
	p, err := c.CreatePing(client.NewPing{Session: "s1", Folder: "/tmp/p", Question: "Migration written.", Finished: true})
	if err != nil || !p.Finished {
		t.Fatalf("created %+v, %v", p, err)
	}
	evs, stop, err := c.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	timeout := time.After(2 * time.Second)
	for found := false; !found; {
		select {
		case e := <-evs:
			var got engine.Ping
			if e.Kind == engine.EventPingCreated && json.Unmarshal(e.Data, &got) == nil && got.ID == p.ID {
				if !got.Finished {
					t.Fatalf("event Ping = %+v", got)
				}
				found = true
			}
		case <-timeout:
			t.Fatal("no ping.created event")
		}
	}
	c = enginetest.Restart(t, dir)
	if got, err := c.Ping(p.ID); err != nil || !got.Finished {
		t.Fatalf("after restart %+v, %v", got, err)
	}
}
