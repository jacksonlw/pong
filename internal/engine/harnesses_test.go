package engine_test

import (
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
)

type twoHarnesses struct{}

func (twoHarnesses) List() []engine.HarnessStatus {
	return []engine.HarnessStatus{{ID: "claude-code", Name: "Claude Code"}, {ID: "zed", Name: "Zed"}}
}
func (twoHarnesses) Repair(string) (engine.HarnessStatus, error) {
	return engine.HarnessStatus{}, engine.ErrNoHarness
}

type claudeRunner struct{}

func (claudeRunner) Harness() string                          { return "claude-code" }
func (claudeRunner) Check([]string, string) error             { return nil }
func (claudeRunner) ResumeCommand(_, _ string) string         { return "" }
func (claudeRunner) Start(_, _, _ string) engine.Launch       { return engine.Launch{} }
func (claudeRunner) Continue(_, _, _, _ string) engine.Launch { return engine.Launch{} }

// A Harness can start Sessions when the Engine has a Runner for it.
func TestHarnessesSayWhichCanStartSessions(t *testing.T) {
	c := enginetest.StartWith(t, engine.Config{Harnesses: twoHarnesses{}, Runners: []engine.Runner{claudeRunner{}}})
	hs, err := c.Harnesses()
	if err != nil || len(hs) != 2 || !hs[0].CanStart || hs[1].CanStart {
		t.Fatalf("harnesses %+v, %v", hs, err)
	}
}
