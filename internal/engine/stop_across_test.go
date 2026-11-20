package engine_test

import (
	"os"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// stoppableDesk is a desktop whose fake Harness can stop a turn, with a
// working Session that has sent one Ping, and a laptop paired with it that
// shows the Ping.
func stoppableDesk(t *testing.T) (book *enginetest.Book, desk, laptop *enginetest.Computer, s engine.Session) {
	t.Helper()
	book = &enginetest.Book{}
	desk = enginetest.StartComputerWith(t, book, "Desktop", engine.Config{Runners: []engine.Runner{stopRunner{textRunner{newRunner()}}}})
	laptop = enginetest.StartComputer(t, book, "Laptop")
	enginetest.Pair(t, desk, laptop)
	s = humanSession(t, desk.C, engine.SessionWorking)
	p, err := desk.C.CreatePing(client.NewPing{Finished: true, Session: s.ID, Harness: s.Harness, Folder: s.Folder,
		Question: "Added the total. What next?", Answers: []engine.Answer{{Label: "Run the tests"}}})
	if err != nil {
		t.Fatal(err)
	}
	s = setState(t, desk.C, s, engine.SessionWorking)
	shows(t, laptop, "the Ping", func(engine.Ping) bool { return true }, p.ID)
	return book, desk, laptop, s
}

func TestAStopAskedOnOneComputerTakesOnTheOneRunningTheSession(t *testing.T) {
	_, desk, laptop, s := stoppableDesk(t)
	stop, err := laptop.C.Stop(s.Harness, s.ID)
	if err != nil || stop.State != engine.StopSent || stop.Device != "Laptop" {
		t.Fatalf("the stop as the laptop was answered: %+v %v", stop, err)
	}
	// Only the desktop keeps it, and the laptop sees it through the Thread
	// it already follows.
	if got, _ := desk.C.Session(s.Harness, s.ID); got.Stop == nil || *got.Stop != stop {
		t.Fatalf("the desktop's Session: %+v", got.Stop)
	}
	if !marked(desk.Dir, s) || marked(laptop.Dir, s) {
		t.Error("the mark is not on the desktop alone")
	}
	if th, err := laptop.C.Thread(s.Harness, s.ID); err != nil || !th.StopsTurns || th.Session.Stop == nil || th.Session.Stop.State != engine.StopSent {
		t.Fatalf("the laptop's view of the Session: %+v %v", th.Session.Stop, err)
	}
	// The desktop's hook takes it at the agent's next tool call.
	if stopped, err := desk.C.TakeStop(s.Harness, s.ID); err != nil || !stopped {
		t.Fatalf("the stop did not take: %v", err)
	}
	th, err := laptop.C.Thread(s.Harness, s.ID)
	if err != nil || th.Session.Stop == nil || th.Session.Stop.State != engine.StopStopped || th.Session.State != engine.SessionIdle {
		t.Fatalf("the laptop's view after the stop took: %+v %v", th.Session, err)
	}
	if line := th.Pings[len(th.Pings)-1]; !line.Line || line.Question != "You stopped this turn from Laptop." {
		t.Errorf("the Thread's line: %+v", line)
	}
	// The same refusals as the local API's: the Session is idle now.
	_, local := desk.C.Stop(s.Harness, s.ID)
	_, far := laptop.C.Stop(s.Harness, s.ID)
	if !isStatus(far, 409) || !isStatus(local, 409) || !strings.Contains(far.Error(), "idle") {
		t.Errorf("a stop for an idle Session: %v, and locally %v", far, local)
	}
}

func TestAStopForAnUnreachableComputerFailsAtOnceAndIsNotKept(t *testing.T) {
	book, desk, laptop, s := stoppableDesk(t)
	enginetest.Stop(desk)
	enginetest.Eventually(t, "the laptop to see the desktop gone", func() bool {
		ps, _ := laptop.C.Pings(engine.StateWaiting)
		return len(ps) == 1 && ps[0].Computer != nil && !ps[0].Computer.Reachable
	})
	_, err := laptop.C.Stop(s.Harness, s.ID)
	if !isStatus(err, 503) || !strings.Contains(err.Error(), "Can't reach Desktop.") {
		t.Fatalf("got %v", err)
	}
	// Nothing waits for the desktop: back again, its turn runs on unstopped.
	enginetest.RestartComputer(t, book, laptop)
	enginetest.RestartComputer(t, book, desk)
	enginetest.Eventually(t, "the laptop to see the desktop again", func() bool {
		ps, _ := laptop.C.Pings(engine.StateWaiting)
		return len(ps) == 1 && ps[0].Computer != nil && ps[0].Computer.Reachable
	})
	if got, err := desk.C.Session(s.Harness, s.ID); err != nil || got.Stop != nil || marked(desk.Dir, s) {
		t.Fatalf("a stop reached the desktop later: %+v %v", got.Stop, err)
	}
	if entries, _ := os.ReadDir(laptop.Dir + "/stops"); len(entries) != 0 {
		t.Errorf("the laptop kept a stop: %v", entries)
	}
}

