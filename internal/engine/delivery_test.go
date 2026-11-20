package engine_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// fakeRunner says how a Session of the fake Harness is opened in a terminal;
// launches records what the Engine then opened. Each new Session gets the id
// "s1". For a human who works in the Harness's app the command says "shared".
type fakeRunner struct {
	mu       sync.Mutex
	launches []engine.Launch
	fail     error // opening fails with it
}

func newRunner() *fakeRunner { return &fakeRunner{} }

func (r *fakeRunner) Harness() string                  { return "fake" }
func (r *fakeRunner) Check([]string, string) error     { return nil }
func (r *fakeRunner) ResumeCommand(_, _ string) string { return "" }
func (r *fakeRunner) SharesWithApp()                   {}
func (r *fakeRunner) Start(surface, folder, prompt string) engine.Launch {
	return engine.Launch{Command: fakeTool(surface) + " " + prompt, SessionID: "s1"}
}
func (r *fakeRunner) Continue(surface, folder, id, text string) engine.Launch {
	return engine.Launch{Command: fakeTool(surface) + " resume " + id + " " + text}
}

func fakeTool(surface string) string {
	if surface == engine.SurfaceApp {
		return "fake shared"
	}
	return "fake"
}

// open is the Engine's Launch in these tests.
func (r *fakeRunner) open(l engine.Launch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.launches = append(r.launches, l)
	return nil
}

func (r *fakeRunner) opened() []engine.Launch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]engine.Launch{}, r.launches...)
}

func (r *fakeRunner) runCount() int { return len(r.opened()) }

// withRunner starts an Engine that opens Sessions through r.
func withRunner(t *testing.T, r *fakeRunner) *client.Client {
	t.Helper()
	return enginetest.StartWith(t, engine.Config{Runners: []engine.Runner{r}, Launch: r.open})
}

// worksIn sets where the human works in the fake Harness.
func worksIn(t *testing.T, c *client.Client, surface string) {
	t.Helper()
	if err := c.SetSurface("fake", surface); err != nil {
		t.Fatal(err)
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for " + what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func delivery(t *testing.T, c *client.Client, id, want string) engine.Ping {
	t.Helper()
	var p engine.Ping
	eventually(t, "delivery "+want, func() bool {
		p, _ = c.Ping(id)
		return p.Pong != nil && p.Pong.Delivery == want
	})
	return p
}

// endedSession is a Session of the fake Harness that has ended, with its
// finished Ping still waiting.
func endedSession(t *testing.T, c *client.Client) (engine.Session, engine.Ping) {
	t.Helper()
	s, err := c.RegisterSession(engine.RegisterSession{Harness: "fake", ID: "old", Folder: t.TempDir(), State: engine.SessionEnded})
	if err != nil {
		t.Fatal(err)
	}
	return s, finishedPing(t, c, s.ID, "fake")
}

func finishedPing(t *testing.T, c *client.Client, session, harness string) engine.Ping {
	t.Helper()
	p, err := c.CreatePing(engine.NewPing{Finished: true, Session: session, Harness: harness, Question: "Fixed it. What next?",
		Answers: []engine.Answer{{Label: "Add a test"}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSentPongSurvivesARestartAndIsDeliveredAfterwards(t *testing.T) {
	c, dir := enginetest.Start(t)
	p, _ := c.CreatePing(engine.NewPing{Session: "s", Question: "Which branch?", BlocksAgent: true,
		Answers: []engine.Answer{{Label: "main"}, {Label: "dev"}}})
	got, err := c.Pong(p.ID, engine.SendPong{Answer: "main"})
	if err != nil || got.Pong.Delivery != engine.DeliverySent {
		t.Fatalf("pong: %+v %v", got.Pong, err)
	}
	c = enginetest.Restart(t, dir)
	if got, _ := c.Ping(p.ID); got.Pong.Delivery != engine.DeliverySent {
		t.Fatalf("after restart: %+v", got.Pong)
	}
	got, err = c.Wait(p.ID) // the hook holding the prompt asks again
	if err != nil || got.Pong.Delivery != engine.DeliveryDelivered || got.Pong.DeliveredAt == nil {
		t.Fatalf("wait: %+v %v", got.Pong, err)
	}
	evs, stop, err := c.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	var kinds []string
	for len(kinds) == 0 || kinds[len(kinds)-1] != engine.EventPongDelivered {
		select {
		case e := <-evs:
			if e.Kind == engine.EventPingPonged || e.Kind == engine.EventPongDelivered {
				kinds = append(kinds, e.Kind)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("events: %v", kinds)
		}
	}
	if len(kinds) != 2 || kinds[0] != engine.EventPingPonged {
		t.Errorf("events: %v", kinds)
	}
}

// Pong runs nothing itself: a Pong for an ended Session opens that Session
// again in a terminal, with the Pong as its prompt.
func TestPongOnAnEndedSessionOpensItAgainInATerminal(t *testing.T) {
	r := newRunner()
	c := withRunner(t, r)
	_, p := endedSession(t, c)
	if _, err := c.Pong(p.ID, engine.SendPong{Answer: "Add a test", Text: "cover rounding"}); err != nil {
		t.Fatal(err)
	}
	delivery(t, c, p.ID, engine.DeliveryDelivered)
	if got := r.opened(); len(got) != 1 || got[0].Command != "fake resume old Add a test. cover rounding" {
		t.Fatalf("opened %+v", got)
	}
}

// When the terminal cannot be opened the Pong comes back, and the human can
// send it again: into its own Session, or as a new one.
func TestPongIsUndeliveredWhenTheSessionCannotBeOpened(t *testing.T) {
	r := newRunner()
	c := withRunner(t, r)
	_, p := endedSession(t, c)
	r.mu.Lock()
	r.fail = errors.New("no terminal")
	r.mu.Unlock()
	c.Pong(p.ID, engine.SendPong{Answer: "Add a test"})
	got := delivery(t, c, p.ID, engine.DeliveryUndelivered)
	if got.State != engine.StateAnswered || !strings.Contains(got.Pong.NotDelivered, "no terminal") {
		t.Errorf("state %s, why %q", got.State, got.Pong.NotDelivered)
	}
	r.mu.Lock()
	r.fail = nil
	r.mu.Unlock()
	if got, err := c.SendAgain(p.ID, false); err != nil || got.Pong.Delivery != engine.DeliveryDelivered {
		t.Fatalf("send again: %+v %v", got.Pong, err)
	}
	if got := r.opened(); len(got) != 1 || got[0].Command != "fake resume old Add a test" {
		t.Fatalf("opened %+v", got)
	}
	if _, err := c.Leave(p.ID); !isStatus(err, 409) {
		t.Errorf("leaving a delivered Pong: %v", err)
	}
}

// A Pong for a Session that has not ended is its hooks' to deliver: Pong
// opens nothing for it.
func TestPongOnALiveSessionOpensNothing(t *testing.T) {
	r := newRunner()
	c := withRunner(t, r)
	s, _ := c.RegisterSession(engine.RegisterSession{Harness: "fake", ID: "live", Folder: t.TempDir(), State: engine.SessionIdle})
	p := finishedPing(t, c, s.ID, "fake")
	if got, err := c.Pong(p.ID, engine.SendPong{Answer: "Add a test"}); err != nil || got.Pong.Delivery != engine.DeliverySent {
		t.Fatalf("pong: %+v %v", got.Pong, err)
	}
	time.Sleep(50 * time.Millisecond)
	if r.runCount() != 0 {
		t.Fatalf("opened %+v", r.opened())
	}
}

// Starting a Session opens a terminal that starts on the prompt, wherever
// the human works. Where they work only changes how a Harness that shares
// Sessions with its app is started; with nothing chosen it is the app once
// any Session of theirs in that Harness has run there.
func TestStartOpensATerminalHoweverTheHumanWorks(t *testing.T) {
	r := newRunner()
	c := withRunner(t, r)
	folder := t.TempDir()
	s, err := c.StartSession(engine.StartSession{Harness: "fake", Folder: folder, Prompt: "Fix it"})
	if err != nil || s.ID != "s1" || s.State != engine.SessionWorking || s.StartedBy != engine.StartedByHuman || s.Title != "Fix it" {
		t.Fatalf("started %+v %v", s, err)
	}
	// Their own Session, running in the Harness's app.
	c.RegisterSession(engine.RegisterSession{Harness: "fake", ID: "mine", AppCommand: "open 'fake://continue'", AppName: "Fake"})
	c.StartSession(engine.StartSession{Harness: "fake", Folder: folder, Prompt: "two"})
	// The Session Pong just started is a terminal one; that changes nothing.
	c.StartSession(engine.StartSession{Harness: "fake", Folder: folder, Prompt: "two again"})
	worksIn(t, c, engine.SurfaceTerminal)
	c.StartSession(engine.StartSession{Harness: "fake", Folder: folder, Prompt: "three"})
	got := r.opened()
	if len(got) != 4 || got[0].Command != "fake Fix it" || got[1].Command != "fake shared two" || got[2].Command != "fake shared two again" || got[3].Command != "fake three" {
		t.Fatalf("opened %+v", got)
	}
	if err := c.SetSurface("fake", "browser"); !isStatus(err, 400) {
		t.Errorf("an unknown surface: %v", err)
	}
}

// holdPrompt holds p's prompt as a hook does, until the returned function
// drops the hold as a closed terminal does.
func holdPrompt(t *testing.T, c *client.Client, id string) (drop func()) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := c.Request("GET", "/pings/"+id+"/wait?held=1")
	done := make(chan struct{})
	go func() {
		defer close(done)
		if resp, err := http.DefaultClient.Do(req.WithContext(ctx)); err == nil {
			resp.Body.Close()
		}
	}()
	time.Sleep(20 * time.Millisecond)
	return func() { cancel(); <-done; time.Sleep(20 * time.Millisecond) }
}

func TestPongIsUndeliveredWhenItsHeldPromptHasGone(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{DeliveryGrace: 100 * time.Millisecond})
	p, _ := c.CreatePing(engine.NewPing{Session: "s", Question: "Run the migration?", BlocksAgent: true, Captured: true,
		Answers: []engine.Answer{{Label: "Yes"}, {Label: "No"}}})
	holdPrompt(t, c, p.ID)()
	if got, err := c.Pong(p.ID, engine.SendPong{Answer: "Yes"}); err != nil || got.Pong.Delivery != engine.DeliverySent {
		t.Fatalf("pong: %+v %v", got.Pong, err)
	}
	got := delivery(t, c, p.ID, engine.DeliveryUndelivered)
	if got.Pong.NotDelivered == "" {
		t.Error("no reason given")
	}
}

func TestHeldPromptThatComesBackInTimeIsDelivered(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{DeliveryGrace: 2 * time.Second})
	p, _ := c.CreatePing(engine.NewPing{Session: "s", Question: "Run the migration?", BlocksAgent: true, Captured: true,
		Answers: []engine.Answer{{Label: "Yes"}, {Label: "No"}}})
	holdPrompt(t, c, p.ID)()
	c.Pong(p.ID, engine.SendPong{Answer: "Yes"})
	if got, err := c.Wait(p.ID); err != nil || got.Pong.Delivery != engine.DeliveryDelivered {
		t.Fatalf("wait: %+v %v", got.Pong, err)
	}
}

func TestOnlyAnUndeliveredPongCanBeSentAgain(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{})
	p, _ := c.CreatePing(engine.NewPing{Session: "s", Question: "Which branch?", BlocksAgent: true,
		Answers: []engine.Answer{{Label: "main"}, {Label: "dev"}}})
	c.Pong(p.ID, engine.SendPong{Answer: "main"})
	if _, err := c.SendAgain(p.ID, true); !isStatus(err, 409) {
		t.Errorf("send again: %v", err)
	}
	if _, err := c.Leave(p.ID); !isStatus(err, 409) {
		t.Errorf("leave: %v", err)
	}
}
