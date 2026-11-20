package claudecode

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

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
//     Ping that approves the action. On Allow the hook notes the call as
//     allowed and answers retry, which tells the agent it may send it again:
//     {"hookSpecificOutput":{"hookEventName":"PermissionDenied","retry":true}}
//  2. The call comes again. PreToolUse finds it noted and answers "ask",
//     which sends it to the permission prompt instead of the classifier.
//  3. PermissionRequest finds it noted and allows it (permission.go); the
//     note is used up, so the Allow covers that one call.
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

// allowedFor is how long an Allow waits for the agent to send the call again.
const allowedFor = 10 * time.Minute

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
	if refusedAgain(e) {
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
	pongs, err := ask(c, e, []engine.NewPing{n})
	if err != nil {
		return nil, err
	}
	if pongs[0].Answer != labelAllow {
		return nil, nil
	}
	p := allowedCall(e)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		return nil, err
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
	if isAllowed(e) {
		d.PermissionDecision, d.PermissionDecisionReason = "ask", "The human allowed this call in Pong."
	}
	if d.PermissionDecision == "" && d.AdditionalContext == "" {
		return nil, nil
	}
	return preToolUse{HookSpecificOutput: d}, nil
}

// allowedCall is where an Allow for e's tool call is noted: a file named for
// the Session, the tool and its input. The agent rewrites a shell command's
// description when it sends the command again, so that part is left out.
func allowedCall(e Event) string { return callFile(e, "allowed-calls") }

// stuckCall is where a call auto mode refused again after an Allow is noted.
func stuckCall(e Event) string { return callFile(e, "refused-after-allow") }

func callFile(e Event, folder string) string {
	var in map[string]any
	json.Unmarshal(e.ToolInput, &in)
	if e.ToolName == "Bash" {
		delete(in, "description")
	}
	b, _ := json.Marshal(in)
	sum := sha256.Sum256([]byte(e.SessionID + "\x00" + e.ToolName + "\x00" + string(b)))
	return filepath.Join(e.Dir, folder, hex.EncodeToString(sum[:]))
}

// refusedAgain reports whether auto mode has refused e's call after the
// human allowed it, now or before; a new such refusal is noted.
func refusedAgain(e Event) bool {
	if _, err := os.Stat(stuckCall(e)); err == nil {
		return true
	}
	if !useAllowed(e) {
		return false
	}
	p := stuckCall(e)
	if os.MkdirAll(filepath.Dir(p), 0o700) == nil {
		os.WriteFile(p, nil, 0o600)
	}
	return true
}

func isAllowed(e Event) bool {
	fi, err := os.Stat(allowedCall(e))
	return err == nil && time.Since(fi.ModTime()) < allowedFor
}

// useAllowed reports whether e's call was allowed, and uses the Allow up.
func useAllowed(e Event) bool {
	return isAllowed(e) && os.Remove(allowedCall(e)) == nil
}
