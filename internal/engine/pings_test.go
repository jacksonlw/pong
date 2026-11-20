package engine_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

func newPing(t *testing.T, c *client.Client) engine.Ping {
	t.Helper()
	p, err := c.CreatePing(client.NewPing{
		Session: "s1", Folder: "/tmp/project", Question: "Ship the fix to staging first?", Finished: true,
		Answers: []engine.Answer{{Label: "Staging first"}, {Label: "Straight to production"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || p.State != engine.StateWaiting || len(p.Answers) != 3 { // with Nothing more
		t.Fatalf("created %+v", p)
	}
	return p
}

func TestWaitReturnsPongWhenItArrives(t *testing.T) {
	c, _ := enginetest.Start(t)
	p := newPing(t, c)
	got := make(chan engine.Ping, 1)
	go func() {
		w, err := c.Wait(p.ID)
		if err != nil {
			t.Error(err)
		}
		got <- w
	}()
	time.Sleep(2 * enginetest.WaitHold) // let at least one wait run out and repeat
	if _, err := c.Pong(p.ID, client.SendPong{Answer: "Staging first"}); err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-got:
		if w.State != engine.StateAnswered || w.Pong == nil || w.Pong.Answer != "Staging first" {
			t.Fatalf("wait returned %+v", w)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("wait did not return after the Pong")
	}
}

func TestWaitRunsOutWithPingStillWaiting(t *testing.T) {
	c, _ := enginetest.Start(t)
	p := newPing(t, c)
	w, err := c.WaitOnce(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.State != engine.StateWaiting || w.Pong != nil {
		t.Fatalf("wait returned %+v", w)
	}
}

func TestWaitingAgainAfterPongStillReturnsIt(t *testing.T) {
	c, _ := enginetest.Start(t)
	p := newPing(t, c)
	if _, err := c.Pong(p.ID, client.SendPong{Answer: "Straight to production", Text: "tests passed"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		w, err := c.WaitOnce(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		if w.Pong == nil || w.Pong.Answer != "Straight to production" || w.Pong.Text != "tests passed" {
			t.Fatalf("wait %d returned %+v", i, w)
		}
	}
}

func TestPongShapes(t *testing.T) {
	cases := []struct {
		name string
		pong client.SendPong
		want int // 0 for success, else HTTP status
	}{
		{"chosen answer", client.SendPong{Answer: "Staging first"}, 0},
		{"typed text", client.SendPong{Text: "hold off until Monday"}, 0},
		{"both", client.SendPong{Answer: "Staging first", Text: "and tell QA"}, 0},
		{"empty", client.SendPong{}, http.StatusBadRequest},
		{"unknown answer", client.SendPong{Answer: "Maybe"}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := enginetest.Start(t)
			p := newPing(t, c)
			got, err := c.Pong(p.ID, tc.pong)
			if tc.want != 0 {
				var e *client.Error
				if !errors.As(err, &e) || e.Status != tc.want || e.Message == "" {
					t.Fatalf("err = %v, want status %d with a message", err, tc.want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Pong.Answer != tc.pong.Answer || got.Pong.Text != tc.pong.Text {
				t.Fatalf("pong = %+v", got.Pong)
			}
		})
	}
}

func TestSecondPongIsRefused(t *testing.T) {
	c, _ := enginetest.Start(t)
	p := newPing(t, c)
	c.Pong(p.ID, client.SendPong{Text: "first"})
	_, err := c.Pong(p.ID, client.SendPong{Text: "second"})
	var e *client.Error
	if !errors.As(err, &e) || e.Status != http.StatusConflict {
		t.Fatalf("err = %v, want 409", err)
	}
}

func TestCreateNeedsQuestionAndSession(t *testing.T) {
	c, _ := enginetest.Start(t)
	for _, np := range []client.NewPing{{Session: "s1"}, {Question: "Ready?"}} {
		_, err := c.CreatePing(np)
		var e *client.Error
		if !errors.As(err, &e) || e.Status != http.StatusBadRequest {
			t.Fatalf("create %+v: err = %v, want 400", np, err)
		}
	}
}

func TestUnknownPing(t *testing.T) {
	c, _ := enginetest.Start(t)
	_, err := c.WaitOnce("nope")
	var e *client.Error
	if !errors.As(err, &e) || e.Status != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
}

func TestListShowsOnlyWaitingPings(t *testing.T) {
	c, _ := enginetest.Start(t)
	a, b := newPing(t, c), newPing(t, c)
	c.Pong(a.ID, client.SendPong{Text: "done"})
	ps, err := c.Pings(engine.StateWaiting)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].ID != b.ID {
		t.Fatalf("waiting = %+v", ps)
	}
}

func TestPingAndPongSurviveRestart(t *testing.T) {
	c, dir := enginetest.Start(t)
	waiting, answered := newPing(t, c), newPing(t, c)
	c.Pong(answered.ID, client.SendPong{Answer: "Staging first"})

	c = enginetest.Restart(t, dir)
	ps, err := c.Pings(engine.StateWaiting)
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].ID != waiting.ID || ps[0].Question != waiting.Question {
		t.Fatalf("waiting after restart = %+v", ps)
	}
	w, err := c.WaitOnce(answered.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.Pong == nil || w.Pong.Answer != "Staging first" {
		t.Fatalf("answered after restart = %+v", w)
	}
}
