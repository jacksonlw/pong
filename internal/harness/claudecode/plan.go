package claudecode

import (
	"encoding/json"
	"strings"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness"
)

// A plan approval (the ExitPlanMode tool) is held at PreToolUse as one Ping
// that approves a plan, with the plan as a document Resource. Approving
// returns "allow" with the tool's input unchanged (Claude Code needs
// updatedInput for this tool); typed text alone returns "deny" with the text,
// which Claude reads and revises the plan. Text sent with Approve reaches
// Claude as additional context.
func init() {
	on("PreToolUse/ExitPlanMode", approvePlan)
	// Claude Code 2.1.322 also sends PermissionRequest for this tool when
	// PreToolUse gave no decision (handed back, or no Engine): leave it alone
	// so Claude Code shows its own prompt.
	on("PermissionRequest/ExitPlanMode", noDecision)
}

const approveLabel = "Approve"

func approvePlan(c *client.Client, e Event) (any, error) {
	var in struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(e.ToolInput, &in); err != nil {
		return nil, err
	}
	n := engine.NewPing{
		Question:    "Ready to go ahead with this plan?",
		Answers:     []engine.Answer{{Label: approveLabel}},
		Recommended: approveLabel,
		Approves:    engine.ApprovesPlan,
		Context:     planTitle(in.Plan),
		Resources:   []engine.NewResource{{Title: "Proposed plan", Kind: "document", Content: in.Plan}},
		Tags:        []engine.Tag{harness.TagApprovePlan},
	}
	if !away() {
		return nil, mirror(c, e, []engine.NewPing{n}) // presence.go: Claude Code asks in the terminal
	}
	pongs, err := ask(c, e, []engine.NewPing{n})
	if err != nil {
		return nil, err
	}
	p := pongs[0]
	text := strings.TrimSpace(p.Text)
	if p.Answer == approveLabel {
		d := preToolUseDecision{HookEventName: "PreToolUse", PermissionDecision: "allow", UpdatedInput: e.ToolInput}
		if text != "" {
			d.AdditionalContext = "The human approved the plan and added: " + text
		}
		return preToolUse{d}, nil
	}
	return preToolUse{preToolUseDecision{HookEventName: "PreToolUse", PermissionDecision: "deny",
		PermissionDecisionReason: "The human did not approve the plan and asked for changes: " + text}}, nil
}

// planTitle is the plan's first line without its heading marks.
func planTitle(plan string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(plan), "\n")
	return strings.TrimSpace(strings.TrimLeft(first, "#"))
}
