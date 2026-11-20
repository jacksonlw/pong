package engine_test

import (
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
)

// A Session's turn starts when it is registered and each time it becomes working.
func TestTurnStartsWhenASessionBecomesWorking(t *testing.T) {
	c, _ := enginetest.Start(t)
	s, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1"})
	if err != nil || s.TurnStartedAt == nil {
		t.Fatalf("new Session %+v, %v: no turn start", s, err)
	}
	first := *s.TurnStartedAt
	s, _ = c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", State: engine.SessionIdle})
	if !s.TurnStartedAt.Equal(first) {
		t.Fatalf("going idle moved the turn start: %v", s.TurnStartedAt)
	}
	s, _ = c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", State: engine.SessionWorking})
	if !s.TurnStartedAt.After(first) {
		t.Fatalf("working again did not start a turn: %v after %v", s.TurnStartedAt, first)
	}
}

// Registered working while it is working already, as when its Harness folds
// a subagent's report into the running turn, the Session's turn goes on.
// Only new_turn begins one then, as when a Pong wakes it.
func TestWorkingAgainMidTurnKeepsTheTurnStart(t *testing.T) {
	c, _ := enginetest.Start(t)
	s, _ := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", State: engine.SessionWorking})
	first := *s.TurnStartedAt
	s, _ = c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", State: engine.SessionWorking})
	if !s.TurnStartedAt.Equal(first) {
		t.Fatalf("working while working moved the turn start: %v after %v", s.TurnStartedAt, first)
	}
	s, _ = c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", State: engine.SessionWorking, NewTurn: true})
	if !s.TurnStartedAt.After(first) {
		t.Fatalf("new_turn did not start a turn: %v after %v", s.TurnStartedAt, first)
	}
}
