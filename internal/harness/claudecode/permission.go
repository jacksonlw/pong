package claudecode

import (
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness"
)

// A permission prompt (PermissionRequest) becomes a blocking Ping that
// approves an action, with the command or change as a Resource. Its choices
// are the Ping's ordinary answers. Only an Allow or Always allow Pong allows
// the call; anything else that comes back as a Pong denies it, and no Pong
// (handed back, no Engine) writes nothing so Claude Code shows its own prompt.
//
// Claude Code 2.1.322 took these answers from a PermissionRequest hook, for
// Bash, Write and an MCP tool alike (testdata/recorded/permission-decisions):
//
//	{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}
//	... "decision":{"behavior":"allow","updatedPermissions":[<permission_suggestions>]}
//	... "decision":{"behavior":"deny","message":"<reason>"}
//
// Note: when Claude Code runs with --permission-prompt-tool, that tool's
// answer wins over the hook's.
func init() {
	on("PermissionRequest", askPermission)
}

const (
	labelAllow       = "Allow"
	labelAlwaysAllow = "Always allow"
	labelDeny        = "Deny"
)

type permissionEvent struct {
	Suggestions []json.RawMessage `json:"permission_suggestions"`
}

type permissionAnswer struct {
	HookSpecificOutput struct {
		HookEventName string             `json:"hookEventName"`
		Decision      permissionDecision `json:"decision"`
	} `json:"hookSpecificOutput"`
}

type permissionDecision struct {
	Behavior           string            `json:"behavior"`
	UpdatedPermissions []json.RawMessage `json:"updatedPermissions,omitempty"`
	Message            string            `json:"message,omitempty"`
}

func askPermission(c *client.Client, e Event) (any, error) {
	// A call auto mode refused and the human then allowed in Pong (denied.go).
	if allowed(c, e, "take") {
		var out permissionAnswer
		out.HookSpecificOutput.HookEventName = "PermissionRequest"
		out.HookSpecificOutput.Decision.Behavior = "allow"
		return out, nil
	}
	// In auto mode Claude Code settles the call itself and never asks the
	// human, so neither does Pong.
	if e.PermissionMode == "auto" {
		return nil, nil
	}
	var pe permissionEvent
	if err := json.Unmarshal(e.Raw, &pe); err != nil {
		return nil, err
	}
	n := permissionPing(e)
	n.Answers = []engine.Answer{{Label: labelAllow, LeadsTo: "this call only"}}
	if len(pe.Suggestions) > 0 {
		n.Answers = append(n.Answers, engine.Answer{Label: labelAlwaysAllow, LeadsTo: suggestionText(pe.Suggestions)})
	}
	n.Answers = append(n.Answers, engine.Answer{Label: labelDeny, LeadsTo: "type a reason to tell Claude why"})
	pongs, err := ask(c, e, []engine.NewPing{n})
	if err != nil {
		return nil, err
	}
	p := pongs[0]
	var d permissionDecision
	switch p.Answer {
	case labelAllow:
		d.Behavior = "allow"
	case labelAlwaysAllow:
		if len(pe.Suggestions) == 0 {
			d.Behavior = "deny"
			break
		}
		d.Behavior, d.UpdatedPermissions = "allow", pe.Suggestions
	default:
		d.Behavior = "deny"
	}
	if d.Behavior == "deny" {
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

// CommandInstruction is added to the standing instruction at Session start,
// so the description a shell command's approval Ping asks in says why.
const CommandInstruction = "Give every shell command a description that says what it does and why, in one plain line: when the command needs approval, Pong asks your human that line.\n"

// fileEdits are Claude Code's tools other than Write that change a file.
var fileEdits = map[string]bool{"Edit": true, "MultiEdit": true, "NotebookEdit": true}

// permissionPing is the question, Resource and kind Tag for one tool call.
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
	case "Write":
		path := str("file_path")
		n.Question = "Write " + filepath.Base(path) + "?"
		n.Context = path
		n.Resources = []engine.NewResource{{Title: "New file content", Kind: engine.KindChange, Content: newFileDiff(path, str("content"))}}
		n.Tags = []engine.Tag{harness.TagApproveEdit}
	default:
		n.Question = "Allow " + e.ToolName + "?"
		b, _ := json.MarshalIndent(in, "", "  ")
		n.Resources = []engine.NewResource{{Title: "Tool call input", Kind: engine.KindText, Content: string(b)}}
		n.Tags = []engine.Tag{harness.TagApproveTool}
		if fileEdits[e.ToolName] {
			n.Tags = []engine.Tag{harness.TagApproveEdit}
		}
	}
	return n
}

// newFileDiff shows a whole new file as a unified diff.
func newFileDiff(path, content string) string {
	var b strings.Builder
	b.WriteString("--- /dev/null\n+++ " + path + "\n")
	for _, l := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

// suggestionText says what Always allow adds, from Claude Code's suggestions.
func suggestionText(ss []json.RawMessage) string {
	var parts []string
	for _, raw := range ss {
		var s struct {
			Type        string   `json:"type"`
			Mode        string   `json:"mode"`
			Directories []string `json:"directories"`
			Rules       []struct {
				ToolName    string `json:"toolName"`
				RuleContent string `json:"ruleContent"`
			} `json:"rules"`
			Destination string `json:"destination"`
		}
		json.Unmarshal(raw, &s)
		switch s.Type {
		case "addRules":
			for _, r := range s.Rules {
				rule := r.ToolName
				if r.RuleContent != "" {
					rule += "(" + r.RuleContent + ")"
				}
				parts = append(parts, "allows "+rule+" from now on ("+s.Destination+")")
			}
		case "setMode":
			parts = append(parts, "switches to "+s.Mode+" mode ("+s.Destination+")")
		case "addDirectories":
			parts = append(parts, "allows work in "+strings.Join(s.Directories, ", ")+" ("+s.Destination+")")
		default:
			parts = append(parts, s.Type+" ("+s.Destination+")")
		}
	}
	return strings.Join(parts, "; ")
}
