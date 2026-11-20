package claudecode_test

import (
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
)

func TestSingleQuestion(t *testing.T) {
	c, dir := enginetest.Start(t)
	out := call(t, dir, "question/single.json")
	ps := waiting(t, c, 1)
	p := ps[0]
	if p.Question != "Which colour should the button be?" || p.Harness != "claude-code" || !p.BlocksAgent || p.MoreComing ||
		len(p.Answers) != 2 || p.Answers[0].Label != "Red" || p.Answers[0].LeadsTo != "Red button" || p.AllowSeveral ||
		len(p.Tags) != 1 || p.Tags[0] != (engine.Tag{Text: "Question", Icon: "person"}) {
		t.Fatalf("Ping = %+v", p)
	}
	if p.Session != "dc68d69d-3138-4b80-8497-63951f30e9aa" {
		t.Fatalf("no Session on %+v", p)
	}
	pong(t, c, p.ID, engine.SendPong{Answer: "Blue"})
	golden(t, out(), "question/single.blue.out.json")
}

func TestSeveralQuestionsAnsweredTogether(t *testing.T) {
	c, dir := enginetest.Start(t)
	out := call(t, dir, "question/several.json")
	ps := waiting(t, c, 3)
	want := []struct {
		q          string
		more, many bool
	}{
		{"Which database should be used?", true, false},
		{"Which checks should be run?", true, true},
		{"Which font size?", false, false},
	}
	for i, w := range want {
		if ps[i].Question != w.q || ps[i].MoreComing != w.more || ps[i].AllowSeveral != w.many {
			t.Fatalf("Ping %d = %+v, want %+v", i, ps[i], w)
		}
	}
	// Pongs arrive out of order; Claude Code gets them all at once.
	pong(t, c, ps[2].ID, engine.SendPong{Text: "Medium, about 15px"})
	pong(t, c, ps[0].ID, engine.SendPong{Answer: "SQLite", Text: "keep it in one file"})
	pong(t, c, ps[1].ID, engine.SendPong{Answers: []string{"Lint", "E2E"}})
	golden(t, out(), "question/several.out.json")
}

func TestHandingBackAQuestionReleasesClaudeCode(t *testing.T) {
	c, dir := enginetest.Start(t)
	out := call(t, dir, "question/several.json")
	ps := waiting(t, c, 3)
	pong(t, c, ps[0].ID, engine.SendPong{Answer: "Postgres"})
	if _, err := c.HandBack(ps[1].ID); err != nil {
		t.Fatal(err)
	}
	if got := out(); got != "" {
		t.Fatalf("hand back wrote %q; want nothing, so Claude Code shows its own prompt", got)
	}
	if left, _ := c.Pings(engine.StateWaiting); len(left) != 0 {
		t.Fatalf("Pings still waiting after hand back: %+v", left)
	}
}

// Claude Code's own questions often name their answers ("Postgres or
// SQLite?"); a captured Ping still reaches Pong instead of failing open.
func TestQuestionThatNamesItsAnswersArrives(t *testing.T) {
	c, dir := enginetest.Start(t)
	out := call(t, dir, "question/repeats-labels.json")
	p := waiting(t, c, 1)[0]
	if p.Question != "Postgres or SQLite?" || len(p.Answers) != 2 {
		t.Fatalf("Ping = %+v", p)
	}
	pong(t, c, p.ID, engine.SendPong{Answer: "SQLite"})
	if got := out(); !strings.Contains(got, `"answers":{"Postgres or SQLite?":"SQLite"}`) {
		t.Fatalf("Claude Code would receive %s", got)
	}
}
