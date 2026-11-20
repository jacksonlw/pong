package engine_test

import (
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// A finished Ping always offers Nothing more last, once,
// beyond the four answers and never recommended; the human can answer with it.
func TestFinishedPingAlwaysOffersNothingMoreLast(t *testing.T) {
	c, _ := enginetest.Start(t)
	four := []engine.Answer{{Label: "Add a test"}, {Label: "Run it"}, {Label: "Ship it"}, {Label: "Write docs"}}
	for name, in := range map[string]client.NewPing{
		"none":        {Finished: true, Session: "s1", Folder: "/tmp/p", Question: "Migration written."},
		"four":        {Finished: true, Session: "s1", Folder: "/tmp/p", Question: "Migration written.", Answers: four, Recommended: "Run it"},
		"given first": {Finished: true, Session: "s1", Folder: "/tmp/p", Question: "Migration written.", Answers: append([]engine.Answer{{Label: engine.NothingMore}}, four...)},
		"recommended": {Finished: true, Session: "s1", Folder: "/tmp/p", Question: "Migration written.", Answers: []engine.Answer{{Label: "Run it"}, {Label: engine.NothingMore}}, Recommended: engine.NothingMore},
	} {
		p, err := c.CreatePing(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		n := 0
		for _, a := range p.Answers {
			if a.Label == engine.NothingMore {
				n++
			}
		}
		if n != 1 || p.Answers[len(p.Answers)-1].Label != engine.NothingMore || (len(p.Answers) > 1 && p.Answers[0].Label == engine.NothingMore) {
			t.Fatalf("%s: answers %+v", name, p.Answers)
		}
		if name == "four" && (len(p.Answers) != 5 || p.Answers[0].Label != "Run it") {
			t.Fatalf("four: answers %+v", p.Answers)
		}
		if _, err := c.Pong(p.ID, client.SendPong{Answer: engine.NothingMore}); err != nil {
			t.Fatalf("%s: Nothing more refused: %v", name, err)
		}
	}
	for name, in := range map[string]client.NewPing{
		"blocking": {Session: "s1", Folder: "/tmp/p", Question: "Which one?", Answers: []engine.Answer{{Label: "A"}, {Label: "B"}}, BlocksAgent: true},
		"note":     {Session: "s1", Folder: "/tmp/p", Question: "Build done.", IsNote: true},
	} {
		p, err := c.CreatePing(in)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range p.Answers {
			if a.Label == engine.NothingMore {
				t.Fatalf("%s offers Nothing more: %+v", name, p.Answers)
			}
		}
	}
}

// A Pong asking for a new Session on a Ping that blocks the agent is refused:
// the agent waits for that Pong in its own Session.
func TestNewSessionPongOnBlockingPingIsRefused(t *testing.T) {
	c, _ := enginetest.Start(t)
	p, err := c.CreatePing(client.NewPing{Session: "s1", Folder: "/tmp/p", Question: "Which one?",
		Answers: []engine.Answer{{Label: "A"}, {Label: "B"}}, BlocksAgent: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Pong(p.ID, client.SendPong{Answer: "A", NewSession: true})
	wantStatus(t, err, 400, "its own Session")
	if got, _ := c.Ping(p.ID); got.State != engine.StateWaiting {
		t.Fatalf("Ping is %s", got.State)
	}
}
