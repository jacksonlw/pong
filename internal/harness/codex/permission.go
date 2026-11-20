package codex

import (
	"encoding/json"
	"strings"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness"
)

// A permission prompt (PermissionRequest) becomes a blocking Ping that
// approves an action, with the command, patch or tool input as a Resource.
// Only an Allow Pong allows the call; any other Pong denies it, and no Pong
// writes nothing so Codex shows its own prompt. Codex documents these
// answers (unverified against the real Codex):
//
//	{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}
//	{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"<reason>"}}}
//
// Codex documents no rule suggestions on this event, so there is no Always
// allow.

const (
	labelAllow = "Allow"
	labelDeny  = "Deny"
)

type permissionAnswer struct {
	HookSpecificOutput struct {
		HookEventName string             `json:"hookEventName"`
		Decision      permissionDecision `json:"decision"`
	} `json:"hookSpecificOutput"`
}

type permissionDecision struct {
	Behavior string `json:"behavior"`
	Message  string `json:"message,omitempty"`
}

func askPermission(c *client.Client, e Event) (any, error) {
	n := permissionPing(e)
	n.Answers = []engine.Answer{{Label: labelAllow, LeadsTo: "this call only"},
		{Label: labelDeny, LeadsTo: "type a reason to tell Codex why"}}
	p, err := ask(c, e, n)
	if err != nil {
		return nil, err
	}
	d := permissionDecision{Behavior: "allow"}
	if p.Answer != labelAllow {
		d.Behavior = "deny"
		d.Message = strings.TrimSpace(p.Text)
		if d.Message == "" {
			d.Message = "The human denied this in Pong."
		}
	}
	var out permissionAnswer
	out.HookSpecificOutput.HookEventName = "PermissionRequest"
	out.HookSpecificOutput.Decision = d
	return out, nil
}

// permissionPing is the question, Resource and kind Tag for one tool call. Codex
// documents tool_input.command for Bash and apply_patch, and
// tool_input.description as its own reason for asking, which a shell command
// is asked in.
func permissionPing(e Event) engine.NewPing {
	n := engine.NewPing{Approves: engine.ApprovesAction}
	var in map[string]any
	json.Unmarshal(e.ToolInput, &in)
	str := func(k string) string { s, _ := in[k].(string); return s }
	switch e.ToolName {
	case "Bash":
		n.Question = harness.CommandQuestion(str("description"))
		n.Resources = []engine.NewResource{{Title: "Shell command", Kind: engine.KindText, Content: str("command")}}
		n.Tags = []engine.Tag{harness.TagApproveCommand}
	case "apply_patch":
		n.Question = "Apply this patch?"
		n.Context = str("description")
		n.Resources = []engine.NewResource{{Title: "Patch to apply", Kind: engine.KindText, Content: str("command")}}
		n.Tags = []engine.Tag{harness.TagApproveEdit}
	default:
		n.Question = "Allow " + e.ToolName + "?"
		n.Context = str("description")
		delete(in, "description")
		b, _ := json.MarshalIndent(in, "", "  ")
		n.Resources = []engine.NewResource{{Title: "Tool call input", Kind: engine.KindText, Content: string(b)}}
		n.Tags = []engine.Tag{harness.TagApproveTool}
	}
	return n
}
