package claudecode

import (
	"encoding/json"
	"strings"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// In auto mode Claude Code's classifier settles each tool call from the
// human's typed messages and the calls themselves; it never reads a tool
// result, so a go-ahead the human gave in a Pong does not count and the call
// is refused. The human's approval has to reach Claude Code as a permission
// verdict, which takes three hooks (checked on 2.1.322, testdata/README.md):
//
//  1. PermissionDenied fires with the refused call. It becomes a blocking
//     Ping that approves the action and carries the call. The human's Allow
//     makes the Engine note that call as allowed, for this Session, once
//     (engine.Call): nothing but that Pong can. The hook then answers retry,
//     which tells the agent it may send it again:
//     {"hookSpecificOutput":{"hookEventName":"PermissionDenied","retry":true}}
//  2. The call comes again. PreToolUse asks the Engine whether it is
//     allowed and answers "ask", which sends it to the permission prompt
//     instead of the classifier.
//  3. PermissionRequest takes the Engine's allow and allows the call
//     (permission.go); the allow is used up, so it covers that one call.
//
// Some calls never reach step 3: in the Claude app (2.1.326) auto mode
// refused mcp__ccd_session__start_session and an Edit again after the "ask",
// while Bash and Agent went on to the prompt. A refusal of a call that is
// still noted as allowed is such a second refusal. It is let stand with no
// Ping and no retry, and that call gets neither again in this Session, so
// the human is never asked over and over to allow what cannot be allowed.
func init() {
	on("PermissionDenied", askAfterRefusal)
	on("PreToolUse", otherToolCall)
}

// RetryInstruction is added to the standing instruction at Session start.
// retry is all Claude Code reads from a PermissionDenied hook, and all it
// tells the agent is that it may retry; without this the agent leaves the
// refusal standing or asks its human a second time.
const RetryInstruction = "If auto mode refuses a tool call and you are then told you may retry it, your human has allowed that exact call in Pong: send it again unchanged, and do not ask them again.\n"

type permissionDenied struct {
	HookSpecificOutput struct {
		HookEventName string `json:"hookEventName"`
		Retry         bool   `json:"retry"`
	} `json:"hookSpecificOutput"`
}

func askAfterRefusal(c *client.Client, e Event) (any, error) {
	var d struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(e.Raw, &d); err != nil {
		return nil, err
	}
	if refusedAgain(c, e) {
		return nil, nil
	}
	n := permissionPing(e)
	n.Question = "Auto mode refused this. " + n.Question
	if why := strings.Trim(d.Reason, "[] "); why != "" {
		n.Context = strings.TrimSpace(n.Context + "\n\nClaude Code's reason: " + why + ".")
	}
	n.Answers = []engine.Answer{
		{Label: labelAllow, LeadsTo: "this call only"},
		{Label: labelDeny, LeadsTo: "it stays refused"},
	}
	n.Call = &engine.Call{Tool: e.ToolName, Input: e.ToolInput, Allow: labelAllow}
	pongs, err := ask(c, e, []engine.NewPing{n})
	if err != nil {
		return nil, err
	}
	if pongs[0].Answer != labelAllow {
		return nil, nil
	}
	var out permissionDenied
	out.HookSpecificOutput.HookEventName = "PermissionDenied"
	out.HookSpecificOutput.Retry = true
	return out, nil
}

// otherToolCall handles every tool call that has no handler of its own: a
// call the human allowed goes to the prompt, and text the human wrote in the
// Thread is handed to the agent (text.go). It looks at two files and calls
// the Engine only when text is kept, so it costs any other call nothing.
func otherToolCall(c *client.Client, e Event) (any, error) {
	d := preToolUseDecision{HookEventName: "PreToolUse", AdditionalContext: markedText(c, e)}
	if allowed(c, e, "check") {
		d.PermissionDecision, d.PermissionDecisionReason = "ask", "The human allowed this call in Pong."
	}
	if d.PermissionDecision == "" && d.AdditionalContext == "" {
		return nil, nil
	}
	return preToolUse{HookSpecificOutput: d}, nil
}

// allowed asks the Engine about e's call: "check" whether the human allowed
// it, "take" the allow, or say it was "refused" again (engine.CallQuery).
// With no Engine nothing is allowed.
func allowed(c *client.Client, e Event, do string) bool {
	ok, err := c.AskAllowed(engine.CallQuery{Harness: HarnessName, Session: e.SessionID, Tool: e.ToolName, Input: e.ToolInput, Do: do})
	return err == nil && ok
}

// refusedAgain reports whether auto mode has refused e's call after the
// human allowed it, now or before; the Engine notes a new such refusal.
func refusedAgain(c *client.Client, e Event) bool { return allowed(c, e, "refused") }
