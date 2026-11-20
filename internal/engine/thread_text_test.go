package engine_test

import (
	"os"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// textRunner is a fake Runner whose Harness's hooks take text the human
// writes in a Thread.
type textRunner struct{ *fakeRunner }

func (textRunner) TakesText() {}

// humanSession registers a Session the human started in the fake Harness.
func humanSession(t *testing.T, c *client.Client, state string) engine.Session {
	t.Helper()
	s, err := c.RegisterSession(engine.RegisterSession{Harness: "fake", ID: "mine", Folder: t.TempDir(), State: state})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// unasked is the one record of text written in the Session's Thread.
func unasked(t *testing.T, c *client.Client, s engine.Session) engine.Ping {
	t.Helper()
	th, err := c.Thread(s.Harness, s.ID)
	if err != nil || len(th.Pings) != 1 || !th.Pings[0].Unasked || th.Pings[0].Pong == nil {
		t.Fatalf("Thread: %+v %v", th.Pings, err)
	}
	return th.Pings[0]
}

// An undelivered Pong sent again into its own Session that has not ended is
// kept for the Session's hooks, as text written in its Thread is, with the
// question it answers; it is delivered when they take it.
func TestPongSentAgainIntoAnIdleSessionIsKeptForItsHooks(t *testing.T) {
	r := newRunner()
	c, dir := enginetest.StartWithDir(t, engine.Config{Runners: []engine.Runner{textRunner{r}}, DeliveryGrace: 50 * time.Millisecond})
	s := humanSession(t, c, engine.SessionIdle)
	p, _ := c.CreatePing(engine.NewPing{Session: s.ID, Harness: s.Harness, Question: "Which right curve?", BlocksAgent: true,
		Answers: []engine.Answer{{Label: "B: higher"}, {Label: "C: highest"}}})
	holdPrompt(t, c, p.ID)()
	c.Pong(p.ID, engine.SendPong{Answer: "B: higher", Text: "and rebuild"})
	delivery(t, c, p.ID, engine.DeliveryUndelivered)
	got, err := c.SendAgain(p.ID, false)
	if err != nil || got.Pong.Delivery != engine.DeliverySent || got.Pong.NotDelivered != "" {
		t.Fatalf("send again: %+v %v", got.Pong, err)
	}
	if _, err := os.Stat(engine.HeldTextMark(dir, s.ID)); err != nil {
		t.Errorf("no mark for the hooks: %v", err)
	}
	time.Sleep(100 * time.Millisecond) // past the grace: nothing holds it, and it stays sent
	texts, err := c.TakeText(s.Harness, s.ID)
	if err != nil || len(texts) != 1 || texts[0] != `Their Pong to your Ping "Which right curve?": B: higher. and rebuild` {
		t.Fatalf("took %q %v", texts, err)
	}
	delivery(t, c, p.ID, engine.DeliveryDelivered)
	if th, _ := c.Thread(s.Harness, s.ID); len(th.Pings) != 1 {
		t.Errorf("sending again added to the Thread: %+v", th.Pings)
	}
	if r.runCount() != 0 {
		t.Errorf("opened %+v", r.opened())
	}
}

// A blocking question's Pong sent again into its ended Session opens it again
// with the question beside the Pong.
func TestBlockingPongSentAgainIntoAnEndedSessionOpensIt(t *testing.T) {
	r := newRunner()
	c := enginetest.StartWith(t, engine.Config{Runners: []engine.Runner{r}, Launch: r.open, DeliveryGrace: 50 * time.Millisecond})
	s, _ := c.RegisterSession(engine.RegisterSession{Harness: "fake", ID: "old", Folder: t.TempDir(), State: engine.SessionIdle})
	p, _ := c.CreatePing(engine.NewPing{Session: s.ID, Harness: s.Harness, Question: "Which branch?", BlocksAgent: true,
		Answers: []engine.Answer{{Label: "main"}, {Label: "dev"}}})
	holdPrompt(t, c, p.ID)()
	c.Pong(p.ID, engine.SendPong{Answer: "main"})
	delivery(t, c, p.ID, engine.DeliveryUndelivered)
	c.RegisterSession(engine.RegisterSession{Harness: "fake", ID: "old", State: engine.SessionEnded})
	if got, err := c.SendAgain(p.ID, false); err != nil || got.Pong.Delivery != engine.DeliveryDelivered {
		t.Fatalf("send again: %+v %v", got.Pong, err)
	}
	if got := r.opened(); len(got) != 1 || got[0].Command != `fake resume old Their Pong to your Ping "Which branch?": main` {
		t.Fatalf("opened %+v", got)
	}
}

func TestTextForAWorkingSessionIsKeptUntilItsHooksTakeIt(t *testing.T) {
	r := newRunner()
	c, dir := enginetest.StartWithDir(t, engine.Config{Runners: []engine.Runner{textRunner{r}}})
	s := humanSession(t, c, engine.SessionWorking)
	if th, _ := c.Thread(s.Harness, s.ID); !th.TakesText {
		t.Error("the Thread does not say it takes text")
	}
	if _, err := c.FollowUp(s.Harness, s.ID, "Use the staging database"); err != nil {
		t.Fatal(err)
	}
	p := unasked(t, c, s)
	if p.State != engine.StateAnswered || p.Pong.Text != "Use the staging database" || p.Pong.Delivery != engine.DeliverySent {
		t.Fatalf("record: %+v %+v", p, p.Pong)
	}
	if ps, _ := c.Pings(engine.StateWaiting); len(ps) != 0 {
		t.Errorf("the text waits in the Queue: %+v", ps)
	}
	if _, err := os.Stat(engine.HeldTextMark(dir, s.ID)); err != nil {
		t.Errorf("no mark for the hooks: %v", err)
	}
	texts, err := c.TakeText(s.Harness, s.ID)
	if err != nil || len(texts) != 1 || texts[0] != "Use the staging database" {
		t.Fatalf("took %q %v", texts, err)
	}
	delivery(t, c, p.ID, engine.DeliveryDelivered)
	if _, err := os.Stat(engine.HeldTextMark(dir, s.ID)); err == nil {
		t.Error("the mark stayed after the text was taken")
	}
	if texts, _ := c.TakeText(s.Harness, s.ID); len(texts) != 0 {
		t.Errorf("taken twice: %q", texts)
	}
	if r.runCount() != 0 {
		t.Error("the Engine ran a Session the human has open")
	}
}

func TestTextReachesAnIdleSessionThatListens(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{Runners: []engine.Runner{textRunner{newRunner()}}})
	s := humanSession(t, c, engine.SessionIdle)
	got := make(chan []string, 1)
	go func() {
		texts, _ := c.ListenText(s.Harness, s.ID)
		got <- texts
	}()
	time.Sleep(3 * enginetest.WaitHold) // the listener outlasts one hold
	if _, err := c.FollowUp(s.Harness, s.ID, "Now add a changelog line"); err != nil {
		t.Fatal(err)
	}
	select {
	case texts := <-got:
		if len(texts) != 1 || texts[0] != "Now add a changelog line" {
			t.Fatalf("heard %q", texts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the listener heard nothing")
	}
}

// The human typing in the Harness starts a turn: the listener of the turn
// before stops, so two never wait on one Session.
func TestListeningStopsWhenTheSessionWorksAgain(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{Runners: []engine.Runner{textRunner{newRunner()}}})
	s := humanSession(t, c, engine.SessionIdle)
	got := make(chan []string, 1)
	go func() {
		texts, _ := c.ListenText(s.Harness, s.ID)
		got <- texts
	}()
	time.Sleep(20 * time.Millisecond)
	c.RegisterSession(engine.RegisterSession{Harness: s.Harness, ID: s.ID, State: engine.SessionWorking})
	select {
	case texts := <-got:
		if len(texts) != 0 {
			t.Fatalf("heard %q", texts)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the listener kept waiting")
	}
}

func TestTextForAnEndedSessionOpensItAgain(t *testing.T) {
	r := newRunner()
	c := enginetest.StartWith(t, engine.Config{Runners: []engine.Runner{textRunner{r}}, Launch: r.open})
	s := humanSession(t, c, engine.SessionEnded)
	if _, err := c.FollowUp(s.Harness, s.ID, "Pick this up again"); err != nil {
		t.Fatal(err)
	}
	delivery(t, c, unasked(t, c, s).ID, engine.DeliveryDelivered)
	if got := r.opened(); len(got) != 1 || got[0].Command != "fake resume mine Pick this up again" {
		t.Fatalf("opened %+v", got)
	}
}

// A Harness whose hooks take no text keeps the rule it had.
func TestTextIsRefusedWhereTheHarnessTakesNone(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{Runners: []engine.Runner{newRunner()}})
	s := humanSession(t, c, engine.SessionWorking)
	if _, err := c.FollowUp(s.Harness, s.ID, "hello"); !isStatus(err, 409) {
		t.Fatalf("got %v", err)
	}
	if th, _ := c.Thread(s.Harness, s.ID); th.TakesText || len(th.Pings) != 0 {
		t.Errorf("Thread: %+v", th)
	}
}
