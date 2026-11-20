package claudecode

import (
	"encoding/json"
	"strings"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness"
)

// A structured question (the AskUserQuestion tool) is held at PreToolUse: one
// Ping per question, in order, each but the last marked more_coming. The
// Pongs go back together as "allow" with the tool's input plus an answers
// object keyed by question text, which is how Claude Code 2.1.322 takes a
// hook's answer.
func init() {
	on("PreToolUse/AskUserQuestion", askQuestions)
	// Claude Code 2.1.322 also sends PermissionRequest for this tool when
	// PreToolUse gave no decision (handed back, or no Engine): leave it alone
	// so Claude Code shows its own prompt.
	on("PermissionRequest/AskUserQuestion", noDecision)
}

type askInput struct {
	Questions []struct {
		Question string `json:"question"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
		MultiSelect bool `json:"multiSelect"`
	} `json:"questions"`
}

func askQuestions(c *client.Client, e Event) (any, error) {
	var in askInput
	if err := json.Unmarshal(e.ToolInput, &in); err != nil {
		return nil, err
	}
	var ns []engine.NewPing
	for i, q := range in.Questions {
		n := engine.NewPing{Question: q.Question, AllowSeveral: q.MultiSelect, MoreComing: i < len(in.Questions)-1,
			Tags: []engine.Tag{harness.TagQuestion}}
		for _, o := range q.Options {
			n.Answers = append(n.Answers, engine.Answer{Label: o.Label, LeadsTo: o.Description})
		}
		ns = append(ns, n)
	}
	pongs, err := ask(c, e, ns)
	if err != nil {
		return nil, err
	}
	answers := map[string]string{}
	for i, q := range in.Questions {
		answers[q.Question] = answerText(pongs[i])
	}
	var updated map[string]json.RawMessage
	if err := json.Unmarshal(e.ToolInput, &updated); err != nil {
		return nil, err
	}
	updated["answers"], _ = json.Marshal(answers)
	return preToolUse{preToolUseDecision{HookEventName: "PreToolUse", PermissionDecision: "allow", UpdatedInput: updated}}, nil
}

// answerText is a Pong as Claude Code reads one answer: the chosen or ticked
// labels joined with ", ", then any typed text.
func answerText(p *engine.Pong) string {
	labels := p.Answers
	if p.Answer != "" {
		labels = append([]string{p.Answer}, labels...)
	}
	s := strings.Join(labels, ", ")
	if t := strings.TrimSpace(p.Text); t != "" {
		if s != "" {
			s += ". "
		}
		s += t
	}
	return s
}
