package engine_test

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// transcriptRunner reads turns from what each test says of a transcript.
type transcriptRunner struct {
	mu    sync.Mutex
	turns map[string]turnSeen
}

type turnSeen struct {
	ended   bool
	written time.Time
}

func (r *transcriptRunner) Harness() string                    { return "claude-code" }
func (r *transcriptRunner) Check([]string, string) error       { return nil }
func (r *transcriptRunner) ResumeCommand(_, _ string) string   { return "" }
func (r *transcriptRunner) Start(_, _, _ string) engine.Launch { return engine.Launch{} }
func (r *transcriptRunner) Continue(_, _, _, _ string) engine.Launch {
	return engine.Launch{}
}

func (r *transcriptRunner) TurnOver(transcript string) (bool, time.Time, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.turns[transcript]
	return t.ended, t.written, ok
}

// A turn that ends where the Engine does not hear of it (stopped in the
// Harness, whose Stop hook then never runs, or ended while the Engine
// restarted) leaves its Session working while its process lives on. The
// transcript says the turn is over, so the Session becomes idle; a turn still
// writing, one waiting on the human, and one quiet only briefly stay working.
func TestATurnThatEndedUnseenLeavesItsSessionIdle(t *testing.T) {
	long := time.Now().Add(-3 * time.Hour)
	r := &transcriptRunner{turns: map[string]turnSeen{
		"/t/ended":   {ended: true, written: time.Now().Add(-5 * time.Minute)},
		"/t/quiet":   {written: long},
		"/t/asking":  {written: long},
		"/t/writing": {written: time.Now()},
		"/t/settles": {ended: true, written: time.Now()},
	}}
	c := enginetest.StartWith(t, engine.Config{Runners: []engine.Runner{r},
		SessionCheckEvery: 20 * time.Millisecond, SessionQuietFor: time.Hour})
	folder := repo(t, "shop")
	// The question comes first, so no check runs while "asking" has a transcript and no question.
	if _, err := c.CreatePing(client.NewPing{Harness: "claude-code", Session: "asking", Folder: folder,
		Question: "Run this shell command?", Answers: []engine.Answer{{Label: "Allow"}}, BlocksAgent: true}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ended", "quiet", "asking", "writing", "settles"} {
		if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: id, Folder: folder,
			PID: os.Getpid(), Transcript: "/t/" + id, State: engine.SessionWorking}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"ended", "quiet"} {
		eventually(t, id+" to become idle", func() bool {
			s, err := c.Session("claude-code", id)
			return err == nil && s.State == engine.SessionIdle
		})
	}
	for _, id := range []string{"asking", "writing", "settles"} {
		if s, _ := c.Session("claude-code", id); s.State != engine.SessionWorking {
			t.Errorf("%s became %s", id, s.State)
		}
	}
	// Its next hook makes it working again.
	if s, _ := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "ended", State: engine.SessionWorking}); s.State != engine.SessionWorking {
		t.Errorf("a hook after the lost turn left it %s", s.State)
	}
}

// A turn that ended while work the agent started in the background still
// runs, such as a subagent, leaves its Session working, as its Harness
// shows it, though the transcript shows the turn ended. It becomes idle only
// once the transcript has been quiet for the quiet time.
func TestASessionAwaitingItsOwnWorkStaysWorking(t *testing.T) {
	r := &transcriptRunner{turns: map[string]turnSeen{
		"/t/awaits": {ended: true, written: time.Now().Add(-5 * time.Minute)},
		"/t/gone":   {ended: true, written: time.Now().Add(-3 * time.Hour)},
	}}
	c := enginetest.StartWith(t, engine.Config{Runners: []engine.Runner{r},
		SessionCheckEvery: 20 * time.Millisecond, SessionQuietFor: time.Hour})
	folder := repo(t, "shop")
	for _, id := range []string{"awaits", "gone"} {
		if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: id, Folder: folder,
			PID: os.Getpid(), Transcript: "/t/" + id, State: engine.SessionWorking, AwaitsOwnWork: true}); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "gone to become idle", func() bool {
		s, err := c.Session("claude-code", "gone")
		return err == nil && s.State == engine.SessionIdle && !s.AwaitsOwnWork
	})
	time.Sleep(100 * time.Millisecond)
	if s, _ := c.Session("claude-code", "awaits"); s.State != engine.SessionWorking || !s.AwaitsOwnWork {
		t.Fatalf("awaits is %s (awaits own work: %v); want working", s.State, s.AwaitsOwnWork)
	}
	// The report of that work starts a turn like any other: it no longer waits.
	if s, _ := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "awaits", State: engine.SessionWorking}); s.AwaitsOwnWork {
		t.Fatal("a hook with a state left the Session awaiting its own work")
	}
}

// The prompt a Session began on is kept for its Thread: the first one given,
// whoever gives it later.
func TestASessionKeepsTheFirstPromptItWasGiven(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := repo(t, "shop")
	c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", Folder: folder, FirstPrompt: "  Fix the checkout totals\nThey round wrong.  "})
	s, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", FirstPrompt: "And the tax line"})
	if err != nil {
		t.Fatal(err)
	}
	if s.FirstPrompt != "Fix the checkout totals\nThey round wrong." {
		t.Errorf("first prompt = %q", s.FirstPrompt)
	}
	th, err := c.Thread("claude-code", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if th.Session.FirstPrompt != s.FirstPrompt {
		t.Errorf("the Thread's Session has first prompt %q", th.Session.FirstPrompt)
	}
}
