package engine_test

import (
	"os"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// stopRunner is a fake Runner whose Harness's hooks can stop a turn and take
// text.
type stopRunner struct{ textRunner }

func (stopRunner) StopsTurns() {}

func stoppable(t *testing.T) (*client.Client, string) {
	t.Helper()
	return enginetest.StartWithDir(t, engine.Config{Runners: []engine.Runner{stopRunner{textRunner{newRunner()}}}})
}

func setState(t *testing.T, c *client.Client, s engine.Session, state string) engine.Session {
	t.Helper()
	s, err := c.RegisterSession(engine.RegisterSession{Harness: s.Harness, ID: s.ID, State: state})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func marked(dir string, s engine.Session) bool {
	_, err := os.Stat(engine.StopMark(dir, s.ID))
	return err == nil
}

func TestAStopForAWorkingSessionIsSent(t *testing.T) {
	c, dir := stoppable(t)
	s := humanSession(t, c, engine.SessionWorking)
	events, cancel, err := c.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	stop, err := c.Stop(s.Harness, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stop.State != engine.StopSent || stop.Device == "" || stop.AskedAt.IsZero() {
		t.Fatalf("stop: %+v", stop)
	}
	got, _ := c.Session(s.Harness, s.ID)
	if got.Stop == nil || *got.Stop != stop || got.State != engine.SessionWorking {
		t.Errorf("Session: %+v %+v", got, got.Stop)
	}
	if !marked(dir, s) {
		t.Error("no mark for the hooks")
	}
	if th, _ := c.Thread(s.Harness, s.ID); !th.StopsTurns {
		t.Error("the Thread does not say its Harness can stop a turn")
	}
	enginetest.Eventually(t, "session.changed with the stop", func() bool {
		for {
			select {
			case e := <-events:
				if e.Kind == engine.EventSessionChanged && strings.Contains(string(e.Data), `"stop":{"state":"sent"`) {
					return true
				}
			default:
				return false
			}
		}
	})
	// A second stop for the same turn is the first one.
	again, err := c.Stop(s.Harness, s.ID)
	if err != nil || again != stop {
		t.Errorf("second stop: %+v %v", again, err)
	}
}

func TestAStopIsRefusedForASessionThatIsNotWorking(t *testing.T) {
	for state, reason := range map[string]string{
		engine.SessionIdle:    "idle",
		engine.SessionEnded:   "ended",
		engine.SessionWaiting: "waiting on a Ping",
	} {
		c, dir := stoppable(t)
		s := humanSession(t, c, state)
		_, err := c.Stop(s.Harness, s.ID)
		if !isStatus(err, 409) || !strings.Contains(err.Error(), reason) {
			t.Errorf("%s: got %v", state, err)
		}
		if got, _ := c.Session(s.Harness, s.ID); got.Stop != nil || marked(dir, s) {
			t.Errorf("%s: a stop was kept: %+v", state, got.Stop)
		}
	}
}

func TestAStopIsRefusedWhereTheHarnessStopsNoTurn(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{Runners: []engine.Runner{textRunner{newRunner()}}})
	s := humanSession(t, c, engine.SessionWorking)
	if _, err := c.Stop(s.Harness, s.ID); !isStatus(err, 409) {
		t.Fatalf("got %v", err)
	}
	if th, _ := c.Thread(s.Harness, s.ID); th.StopsTurns {
		t.Error("the Thread offers a stop its Harness cannot do")
	}
	if _, err := c.Stop("fake", "nobody"); !isStatus(err, 404) {
		t.Errorf("unknown Session: got %v", err)
	}
}

func TestATurnThatEndsByItselfMissesItsStop(t *testing.T) {
	for _, state := range []string{engine.SessionIdle, engine.SessionEnded} {
		c, dir := stoppable(t)
		s := humanSession(t, c, engine.SessionWorking)
		if _, err := c.Stop(s.Harness, s.ID); err != nil {
			t.Fatal(err)
		}
		got := setState(t, c, s, state)
		if got.Stop == nil || got.Stop.State != engine.StopMissed {
			t.Errorf("%s: stop %+v", state, got.Stop)
		}
		if marked(dir, s) {
			t.Errorf("%s: the mark stayed", state)
		}
	}
}

func TestTheNextTurnClearsAStop(t *testing.T) {
	c, dir := stoppable(t)
	s := humanSession(t, c, engine.SessionWorking)
	if _, err := c.Stop(s.Harness, s.ID); err != nil {
		t.Fatal(err)
	}
	setState(t, c, s, engine.SessionIdle)
	if got := setState(t, c, s, engine.SessionWorking); got.Stop != nil {
		t.Errorf("stop after the next turn began: %+v", got.Stop)
	}
	// The new turn can be stopped in its own right.
	if stop, err := c.Stop(s.Harness, s.ID); err != nil || stop.State != engine.StopSent || !marked(dir, s) {
		t.Errorf("stop of the next turn: %+v %v", stop, err)
	}
}
