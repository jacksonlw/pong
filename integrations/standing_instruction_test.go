package integrations

import (
	"strings"
	"testing"
)

// A Session with Pong reads as one without it: the agent is told to write as
// it would without Pong, and is never told to write a question twice, quote a
// Pong back, or end every turn on a Ping.
func TestStandingInstructionKeepsPongOutOfTheAgentsWords(t *testing.T) {
	want := "Write every message as you would without Pong: never say you sent something to Pong, never write a question twice, and never offer a step, such as committing, because of Pong."
	if !strings.Contains(StandingInstruction, want) {
		t.Errorf("the standing instruction lacks %q", want)
	}
	for _, gone := range []string{"You answered in Pong", "first write it in your message", "finished: true", "blocks_agent"} {
		if strings.Contains(StandingInstruction, gone) {
			t.Errorf("the standing instruction still has %q", gone)
		}
	}
}

// Thoughts are taught with the rule that keeps them rare and concrete.
func TestStandingInstructionTeachesThoughts(t *testing.T) {
	for _, want := range []string{"send a Thought with send_thought for each real finding or decision", "One to three a turn.", "A Thought goes beside what you write in the Session, never instead of it"} {
		if !strings.Contains(StandingInstruction, want) {
			t.Errorf("the standing instruction lacks %q", want)
		}
	}
}

// An agent offers work for a new Session as a choice, named as the apps name
// it, and never starts that Session itself.
func TestStandingInstructionOffersNewSessionsAndNeverStartsThem(t *testing.T) {
	for _, want := range []string{"they send it with New Session with this Pong, and Pong starts that Session", "Never start that Session yourself."} {
		if !strings.Contains(StandingInstruction, want) {
			t.Errorf("the standing instruction lacks %q", want)
		}
	}
}

// Where Pong cannot read the Harness, the agent's own Pings are the only way
// questions and finished work reach the human.
func TestAskingInstructionSendsQuestionsAndFinishedWork(t *testing.T) {
	for _, want := range []string{"one question per Ping, with blocks_agent set", "send a Ping with finished: true"} {
		if !strings.Contains(AskingInstruction, want) {
			t.Errorf("the asking instruction lacks %q", want)
		}
	}
}
