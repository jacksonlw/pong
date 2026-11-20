package engine_test

import (
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// promised sends a Ping that says more are coming from Session s1, and the
// human's Pong to it: the interfaces now wait for the Session's next Ping.
func promised(t *testing.T, c *client.Client) engine.Ping {
	t.Helper()
	p, err := c.CreatePing(engine.NewPing{Harness: "claude-code", Session: "s1", Question: "Which branch?", MoreComing: true, BlocksAgent: true,
		Answers: []engine.Answer{{Label: "main"}, {Label: "dev"}}})
	if err != nil || !p.MoreComing {
		t.Fatalf("create: %+v %v", p, err)
	}
	if _, err := c.Pong(p.ID, engine.SendPong{Answer: "main"}); err != nil {
		t.Fatal(err)
	}
	return p
}

// A Ping that said more are coming stops saying so once its Session's turn
// ends or the Session ends: the Ping it promised will not come, and no
// interface may go on waiting for it.
func TestMoreComingIsTakenBackWhenTheTurnOrTheSessionEnds(t *testing.T) {
	for _, state := range []string{engine.SessionIdle, engine.SessionEnded} {
		t.Run(state, func(t *testing.T) {
			c, _ := enginetest.Start(t)
			p := promised(t, c)
			c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", State: engine.SessionWorking})
			if got, _ := c.Ping(p.ID); !got.MoreComing {
				t.Fatal("more_coming was taken back while the Session still works")
			}
			if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", State: state}); err != nil {
				t.Fatal(err)
			}
			if got, _ := c.Ping(p.ID); got.MoreComing {
				t.Fatalf("the Session is %s and the Ping still says more are coming", state)
			}
			// Every interface is told.
			evs, stop, err := c.Events(0)
			if err != nil {
				t.Fatal(err)
			}
			defer stop()
			for {
				select {
				case e := <-evs:
					if e.Kind == engine.EventPingState {
						return
					}
				case <-time.After(5 * time.Second):
					t.Fatal("no ping.state event")
				}
			}
		})
	}
}

// The Engine cannot know what a Session tried to send while it was not
// running, so when it starts no Ping still says more are coming.
func TestMoreComingIsTakenBackWhenTheEngineRestarts(t *testing.T) {
	c, dir := enginetest.Start(t)
	p := promised(t, c)
	c = enginetest.Restart(t, dir)
	if got, _ := c.Ping(p.ID); got.MoreComing {
		t.Fatal("after a restart the Ping still says more are coming")
	}
}
