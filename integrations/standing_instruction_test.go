package integrations

import (
	"strings"
	"testing"
)

// The Harness's own transcript must show what was asked and answered, so the
// agent is told to write both in its messages, not only in Pong's tools.
func TestStandingInstructionKeepsAskingAndAnsweringInTheTranscript(t *testing.T) {
	for _, want := range []string{
		"first write it in your message with the answers you offer, then send it",
		`open your next message with one line that quotes it, starting "You answered in Pong:"`,
	} {
		if !strings.Contains(StandingInstruction, want) {
			t.Errorf("the standing instruction lacks %q", want)
		}
	}
}

// Thoughts are taught in the standing instruction, with the rule that keeps
// them rare and concrete.
func TestStandingInstructionTeachesThoughts(t *testing.T) {
	want := "As you work, send a Thought with send_thought when you find, decide or change something, in one sentence your human understands without the code open: never what you are running, never output. Most turns need one to three."
	if !strings.Contains(StandingInstruction, want) {
		t.Errorf("the standing instruction lacks %q", want)
	}
}

// An agent offers work for a new Session on its finished Ping, and names the
// choice the human sends it with as the Mac and iPhone apps name it.
func TestStandingInstructionOffersNewSessionsOnTheFinishedPing(t *testing.T) {
	want := "offer that work as an answer on your finished Ping, saying what the new Session should do: they send it with New Session with this Pong, and Pong starts that Session"
	if !strings.Contains(StandingInstruction, want) {
		t.Errorf("the standing instruction lacks %q", want)
	}
}
