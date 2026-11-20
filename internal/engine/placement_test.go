package engine_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// harnessesOn reports one Harness, "fake", as this computer has it.
type harnessesOn struct {
	installed, connected bool
	warning              string
}

func (h harnessesOn) List() []engine.HarnessStatus {
	return []engine.HarnessStatus{{ID: "fake", Name: "Fake", Installed: h.installed, Connected: h.connected, Warning: h.warning}}
}
func (harnessesOn) Repair(string) (engine.HarnessStatus, error) {
	return engine.HarnessStatus{}, engine.ErrNoHarness
}

// countingRunner opens Sessions in a terminal, each with its own id.
type countingRunner struct {
	mu      sync.Mutex
	prompts []string
}

func (r *countingRunner) Harness() string                  { return "fake" }
func (r *countingRunner) Check([]string, string) error     { return nil }
func (r *countingRunner) ResumeCommand(_, _ string) string { return "" }
func (r *countingRunner) Start(_, _, prompt string) engine.Launch {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prompts = append(r.prompts, prompt)
	return engine.Launch{Command: "fake " + prompt, SessionID: fmt.Sprintf("run-%d", len(r.prompts))}
}
func (r *countingRunner) Continue(_, _, _, _ string) engine.Launch { return engine.Launch{} }

func (r *countingRunner) started() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.prompts...)
}

func TestStartingSessionsOnAnotherComputer(t *testing.T) {
	book := &enginetest.Book{}
	deskRun, boxRun := &countingRunner{}, &countingRunner{}
	desk := enginetest.StartComputerWith(t, book, "Desktop", engine.Config{Harnesses: harnessesOn{true, true, ""}, Runners: []engine.Runner{deskRun}})
	box := enginetest.StartComputerWith(t, book, "Box", engine.Config{Harnesses: harnessesOn{true, true, "version 9 is outside the tested range"}, Runners: []engine.Runner{boxRun}})
	studio := enginetest.StartComputerWith(t, book, "Studio", engine.Config{Harnesses: harnessesOn{true, false, ""}, Runners: []engine.Runner{&countingRunner{}}})
	laptop := enginetest.StartComputerWith(t, book, "Laptop", engine.Config{Harnesses: harnessesOn{true, true, ""}, Runners: []engine.Runner{&countingRunner{}}})
	enginetest.Pair(t, desk, box)
	enginetest.Pair(t, desk, studio)
	enginetest.Pair(t, desk, laptop)

	const remote = "git@github.com:me/shop.git"
	for _, c := range []*enginetest.Computer{desk, box, studio} {
		p, err := c.C.ProjectFor(gitRepo(t, remote))
		if err != nil {
			t.Fatal(err)
		}
		if c != desk || true {
			c.C.ChangeProject(p.ID, engine.ChangeProject{Enabled: ptr(true)})
		}
	}
	// The laptop does not have the Project: it knows it from the others.
	var shop engine.Project
	enginetest.Eventually(t, "the laptop to know the Project and every computer's state", func() bool {
		ps, _ := laptop.C.Projects()
		for _, p := range ps {
			if p.Key == "git:github.com/me/shop" {
				shop = p
			}
		}
		cs, _ := laptop.C.Computers()
		enabled := 0
		for _, c := range cs {
			if len(c.Projects) == 1 && c.Reachable {
				enabled++
			}
		}
		return shop.ID != "" && enabled == 3
	})

	pl, err := laptop.C.Placement(shop.ID, "fake")
	if err != nil {
		t.Fatal(err)
	}
	why := map[string]engine.Candidate{}
	for _, c := range pl.Candidates {
		why[c.Name] = c
	}
	if why["Laptop"].Eligible || why["Studio"].Eligible || !why["Desktop"].Eligible || !why["Box"].Eligible {
		t.Fatalf("eligibility: %+v", pl.Candidates)
	}
	if !strings.Contains(strings.Join(why["Studio"].Reasons, " "), "not connected") || why["Box"].Warning == "" {
		t.Fatalf("reasons and warnings: %+v", pl.Candidates)
	}

	// Pong chooses; then the one with fewer running Sessions; the human can choose instead.
	if _, err := laptop.C.StartSession(engine.StartSession{Harness: "fake", Project: shop.ID, Prompt: "first"}); err != nil {
		t.Fatal(err)
	}
	first := "Desktop"
	if len(boxRun.started()) == 1 {
		first = "Box"
	}
	other := map[string]string{"Desktop": "Box", "Box": "Desktop"}[first]
	enginetest.Eventually(t, "the laptop to see one Session running on "+first, func() bool {
		cs, _ := laptop.C.Computers()
		for _, c := range cs {
			if c.Name == first {
				return c.Running == 1
			}
		}
		return false
	})
	laptop.C.StartSession(engine.StartSession{Harness: "fake", Project: shop.ID, Prompt: "second"})
	if len(deskRun.started())+len(boxRun.started()) != 2 || len(deskRun.started()) != 1 {
		t.Fatalf("not the computer with fewer Sessions: desk %v box %v (first went to %s, second should go to %s)", deskRun.started(), boxRun.started(), first, other)
	}
	if _, err := laptop.C.StartSession(engine.StartSession{Harness: "fake", Project: shop.ID, Prompt: "chosen", Computer: why["Desktop"].ID}); err != nil {
		t.Fatal(err)
	}
	if got := deskRun.started(); got[len(got)-1] != "chosen" {
		t.Fatalf("the chosen computer did not start it: %v", got)
	}

	// No computer can take it: each says why, and nothing starts.
	_, err = laptop.C.StartSession(engine.StartSession{Harness: "fake", Project: shop.ID, Prompt: "nowhere", Computer: why["Studio"].ID})
	var ce *client.Error
	if !errors.As(err, &ce) || ce.Status != http.StatusConflict || !strings.Contains(ce.Message, "Studio") {
		t.Fatalf("a refused choice: %v", err)
	}
	box.C.ChangeProject(projectOn(t, box), engine.ChangeProject{Enabled: ptr(false)})
	desk.C.ChangeProject(projectOn(t, desk), engine.ChangeProject{Enabled: ptr(false)})
	enginetest.Eventually(t, "no computer to take it", func() bool {
		_, err := laptop.C.StartSession(engine.StartSession{Harness: "fake", Project: shop.ID, Prompt: "nowhere"})
		return errors.As(err, &ce) && ce.Status == http.StatusConflict && strings.Contains(ce.Message, "not enabled")
	})
}

func projectOn(t *testing.T, c *enginetest.Computer) string {
	t.Helper()
	ps, _ := c.C.Projects()
	for _, p := range ps {
		if !p.Remote {
			return p.ID
		}
	}
	t.Fatal("no Project")
	return ""
}
