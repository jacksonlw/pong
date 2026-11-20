package claudecode_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
)

func TestPlanApprovalPing(t *testing.T) {
	c, dir := enginetest.Start(t)
	out := call(t, dir, "plan/exit-plan-mode.json")
	p := waiting(t, c, 1)[0]
	if p.Approves != "plan" || !p.BlocksAgent || len(p.Answers) != 1 || p.Answers[0].Label != "Approve" ||
		p.Context != "Plan: add hello.txt" || len(p.Resources) != 1 || p.Resources[0].Kind != "document" ||
		!reflect.DeepEqual(p.Tags, []engine.Tag{{Text: "Approve plan", Icon: "doc.text"}}) {
		t.Fatalf("Ping = %+v", p)
	}
	var in struct {
		ToolInput struct{ Plan string } `json:"tool_input"`
	}
	raw, _ := os.ReadFile("testdata/plan/exit-plan-mode.json")
	json.Unmarshal(raw, &in)
	body, _, err := c.Resource(p.ID, p.Resources[0].ID)
	if err != nil || string(body) != in.ToolInput.Plan {
		t.Fatalf("plan Resource = %q, %v", body, err)
	}
	pong(t, c, p.ID, engine.SendPong{Answer: "Approve"})
	golden(t, out(), "plan/exit-plan-mode.approve.out.json")
}

func TestPlanChangesRequested(t *testing.T) {
	c, dir := enginetest.Start(t)
	out := call(t, dir, "plan/exit-plan-mode.json")
	p := waiting(t, c, 1)[0]
	pong(t, c, p.ID, engine.SendPong{Text: "Also add a CHANGELOG entry."})
	golden(t, out(), "plan/exit-plan-mode.changes.out.json")
}

func TestPlanHandedBack(t *testing.T) {
	c, dir := enginetest.Start(t)
	out := call(t, dir, "plan/exit-plan-mode.json")
	p := waiting(t, c, 1)[0]
	if _, err := c.HandBack(p.ID); err != nil {
		t.Fatal(err)
	}
	if got := out(); strings.TrimSpace(got) != "" {
		t.Fatalf("hand back wrote %q; want nothing", got)
	}
}
