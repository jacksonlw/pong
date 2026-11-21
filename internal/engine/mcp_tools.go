package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jacksonlw/pong/internal/tagicons"
)

// The tool names.
const (
	ToolSendPing     = "send_ping"
	ToolWaitForPong  = "wait_for_pong"
	ToolSendNote     = "send_note"
	ToolSendThought  = "send_thought"
	ToolWithdrawPing = "withdraw_ping"
)

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func boolean(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

var whereProps = map[string]any{
	"session": str("the Session's identifier; leave out to use the one Pong already knows"),
	"folder":  str("the Session's working folder; leave out to use the one Pong already knows"),
}

func props(extra map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range whereProps {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// forbiddenSchemeList names forbiddenSchemes in order, as "a:, b: or c:".
func forbiddenSchemeList() string {
	var names []string
	for _, k := range slices.Sorted(maps.Keys(forbiddenSchemes)) {
		names = append(names, k+":")
	}
	return strings.Join(names[:len(names)-1], ", ") + " or " + names[len(names)-1]
}

var tagsSchema = map[string]any{
	"type": "array",
	"description": "Tags, the first saying what kind of Ping this is, such as Design question or Approve command, " +
		"then any saying where the change lands or what it concerns, such as an environment, a branch or a ticket. " +
		"A finished Ping takes no kind Tag, nor any Tag saying it is finished or done: Pong marks it Finished itself",
	"items": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": str(fmt.Sprintf("at most %d characters", MaxTagText)),
			"icon": str("one of: " + strings.Join(tagicons.Icons, ", ") +
				"; any other name is drawn as a plain tag icon"),
			"colour": str("optional, and used sparingly, for what is worth spotting at a glance such as prod; one of: " +
				strings.Join(tagicons.Colours, ", ") + "; leave out for a neutral Tag"),
		},
		"required": []string{"text"},
	},
}

var resourcesSchema = map[string]any{
	"type":        "array",
	"description": "Resources to attach instead of pasting them into the question or context; give each a file location or inline content",
	"items": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title": str("two to four words"),
			"kind": map[string]any{"type": "string", "enum": resourceKinds, "description": "image; document, which a plan to approve " +
				"always is; sheet, from a .csv, .tsv or .xlsx file or comma-separated content; slides, from a .pdf or .pptx file; " +
				"link, one URL in content, such as a source, a prototype or a preview, and never " + forbiddenSchemeList() + "; every link goes here, since Tags take none; change; error; output; file; text"},
			"file":    str("a file's location, absolute or relative to the Session's folder; Pong copies it now"),
			"content": str("inline text, instead of file"),
		},
		"required": []string{"title", "kind"},
	},
}

// summarySchema asks for the Session's summary on every Ping and Note, so the
// one the human sees on its Thread stays current.
var summarySchema = str("one or two lines on where this Session stands now: what it is doing and what is left. " +
	"Give it on every Ping and Note; the latest is what your human sees on the Session's Thread")

var pingIDSchema = map[string]any{
	"type":       "object",
	"properties": map[string]any{"ping_id": str("the ping_id send_ping returned")},
	"required":   []string{"ping_id"},
}

var mcpTools = []mcpTool{
	{
		Name: ToolSendPing,
		Description: "Ask your human one question, ask them to approve a plan or action, or hand over finished work, " +
			"and get their Pong. Set blocks_agent on a question or an approval, and finished on finished work. Returns the Pong if it arrives within a short wait. Otherwise returns a ping_id: " +
			"call wait_for_pong with it. Send several questions as several Pings, one at a time.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": props(map[string]any{
				"question": str("one or two lines carrying their own context; never restate the answers"),
				"answers": map[string]any{
					"type": "array", "maxItems": 4,
					"description": "0-4 answers the human can pick, the recommended one first",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"label":        str("at most 5 words"),
							"leads_to":     str("what choosing this answer leads to"),
							"hard_to_undo": boolean("this answer is hard to undo"),
						},
						"required": []string{"label"},
					},
				},
				"recommended":   str("label of the answer you recommend, exactly as in answers"),
				"allow_several": boolean("the human may tick several answers"),
				"approves": map[string]any{"type": "string", "enum": []string{ApprovesPlan, ApprovesAction},
					"description": "set when asking to approve a plan or an action; attach a plan as a document Resource"},
				"context": str("a short paragraph of background, in the Ping itself: what you found and why it matters, " +
					"or on finished work what changed and why it works. Your human reads this, never your Session's closing text. " +
					"Say it rather than pointing to a Thought, a file or your Session; put anything longer in a document Resource"),
				"not_verified": str("on finished work, always: what you did not check, such as tests not run, a fix not tried " +
					"in the app or a case you assumed; \"nothing\" only if you checked it all. Never say you checked what you did not"),
				"why":          str("why you are asking now"),
				"summary":      summarySchema,
				"more_coming":  boolean("you will send another Ping after this one"),
				"blocks_agent": boolean("you cannot continue until the Pong arrives; set it on every question and approval"),
				"finished": boolean("this Ping hands over finished work: Pong draws the Finished mark on it and adds " +
					NothingMore + " as the last answer, so give it no Tag saying it is finished; never set it with blocks_agent. " +
					"Say in context what changed and why, set not_verified, and attach the test output when you ran tests; " +
					"Pong attaches the uncommitted diff itself"),
				"resources": resourcesSchema,
				"tags":      tagsSchema,
			}),
			"required": []string{"question"},
		},
	},
	{
		Name: ToolWaitForPong,
		Description: "Wait a short while for the Pong to a Ping you sent. Returns the Pong, or says it has not " +
			"arrived yet: then call wait_for_pong again.",
		InputSchema: pingIDSchema,
	},
	{
		Name: ToolSendNote,
		Description: "Tell your human something that needs no Pong, with Resources such as a link or a screenshot when they help. " +
			"Returns at once and never holds you up.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": props(map[string]any{
				"text":        str("the news, in one or two lines"),
				"context":     str("a short paragraph of background"),
				"summary":     summarySchema,
				"more_coming": boolean("you will send another Ping after this one"),
				"resources":   resourcesSchema,
				"tags":        tagsSchema,
			}),
			"required": []string{"text"},
		},
	},
	{
		Name: ToolSendThought,
		Description: "As you work, tell your human in one sentence something you found, decided or changed, in words they " +
			"understand without the code open: never what you are running, never output. It shows only in the Session's Thread, " +
			"never asks anything, and returns at once.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": props(map[string]any{
				"text":    str(fmt.Sprintf("one plain sentence, at most %d characters", MaxThought)),
				"summary": summarySchema,
			}),
			"required": []string{"text"},
		},
	},
	{
		Name:        ToolWithdrawPing,
		Description: "Withdraw a Ping or Note you sent that no longer needs the human, for example because you found the answer.",
		InputSchema: pingIDSchema,
	},
}

type toolResult struct {
	Content           []map[string]string `json:"content"`
	StructuredContent any                 `json:"structuredContent,omitempty"`
	IsError           bool                `json:"isError,omitempty"`
}

func textResult(isError bool, structured any, format string, a ...any) toolResult {
	return toolResult{
		Content:           []map[string]string{{"type": "text", "text": fmt.Sprintf(format, a...)}},
		StructuredContent: structured,
		IsError:           isError,
	}
}

const askUsually = "Pong's Engine is not running, so nothing was sent. Ask your human in your usual way instead."

// Replies to send_thought. A Thought asks nothing, so when Pong cannot take
// it the agent just carries on.
const (
	thoughtKept    = "Thought kept. Carry on."
	thoughtDropped = "Pong is not taking Thoughts here, so this one was dropped. Carry on."
)

// call runs the named tool; ok is false when there is no such tool.
func (m MCP) call(name string, args json.RawMessage) (res toolResult, ok bool) {
	if len(args) == 0 {
		args = json.RawMessage("{}")
	}
	fail := func(err error) toolResult {
		if errors.Is(err, ErrEngineDown) {
			return textResult(false, map[string]string{"state": "engine_not_running"}, askUsually)
		}
		return textResult(true, nil, "%s", err.Error())
	}
	switch name {
	case ToolSendPing, ToolSendNote:
		var in struct {
			NewPing
			Text string `json:"text"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return textResult(true, nil, "arguments do not fit %s: %v", name, err), true
		}
		n := in.NewPing
		n.Captured = false // only Harness adapters capture Pings
		if name == ToolSendNote {
			n = NewPing{Session: n.Session, Harness: n.Harness, Folder: n.Folder, Question: in.Text, Context: n.Context, Summary: n.Summary, MoreComing: n.MoreComing, IsNote: true, Resources: n.Resources, Tags: n.Tags}
		} else {
			n.IsNote = false
		}
		if n.Session == "" {
			n.Session = m.Session
		}
		if n.Harness == "" {
			n.Harness = m.Harness
		}
		if n.Folder == "" {
			n.Folder = m.Folder
		}
		p, err := m.Backend.CreatePing(n)
		if err != nil {
			return fail(err), true
		}
		if n.IsNote {
			return textResult(false, map[string]string{"ping_id": p.ID, "state": p.State},
				"Note sent (ping_id %s). Carry on.", p.ID), true
		}
		if p, err = m.Backend.WaitOnce(p.ID); err != nil {
			return fail(err), true
		}
		return pingResult(p), true
	case ToolSendThought:
		var in struct {
			Text    string `json:"text"`
			Summary string `json:"summary"`
			Session string `json:"session"`
			Harness string `json:"harness"`
			Folder  string `json:"folder"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return textResult(true, nil, "arguments do not fit %s: %v", name, err), true
		}
		if m.Quiet {
			return textResult(false, map[string]string{"state": "dropped"}, thoughtDropped), true
		}
		n := NewPing{Thought: true, Session: in.Session, Harness: in.Harness, Folder: in.Folder, Question: in.Text, Summary: in.Summary}
		if n.Session == "" {
			n.Session = m.Session
		}
		if n.Harness == "" {
			n.Harness = m.Harness
		}
		if n.Folder == "" {
			n.Folder = m.Folder
		}
		p, err := m.Backend.CreatePing(n)
		if errors.Is(err, ErrEngineDown) {
			return textResult(false, map[string]string{"state": "engine_not_running"}, thoughtDropped), true
		}
		if err != nil {
			return fail(err), true
		}
		return textResult(false, map[string]string{"ping_id": p.ID, "state": p.State}, thoughtKept), true
	case ToolWaitForPong, ToolWithdrawPing:
		var in struct {
			PingID string `json:"ping_id"`
		}
		if json.Unmarshal(args, &in); in.PingID == "" {
			return textResult(true, nil, "ping_id is required: pass the ping_id send_ping returned"), true
		}
		if name == ToolWithdrawPing {
			p, err := m.Backend.Withdraw(in.PingID)
			if err != nil {
				return fail(err), true
			}
			return textResult(false, map[string]string{"ping_id": p.ID, "state": p.State}, "Ping %s withdrawn.", p.ID), true
		}
		p, err := m.Backend.WaitOnce(in.PingID)
		if err != nil {
			return fail(err), true
		}
		return pingResult(p), true
	}
	return toolResult{}, false
}

// pingResult tells the agent where a Ping stands after a wait.
func pingResult(p Ping) toolResult {
	out := map[string]any{"ping_id": p.ID, "state": p.State}
	switch p.State {
	case StateAnswered:
		out["pong"] = p.Pong
		var parts []string
		if p.Pong.Answer != "" {
			parts = append(parts, fmt.Sprintf("answer: %s", p.Pong.Answer))
		}
		if len(p.Pong.Answers) > 0 {
			parts = append(parts, fmt.Sprintf("ticked: %v", p.Pong.Answers))
		}
		if p.Pong.Text != "" {
			parts = append(parts, fmt.Sprintf("text: %s", p.Pong.Text))
		}
		if p.Pong.NewSession {
			parts = append(parts, "the human wants this carried on in a new Session")
		}
		return textResult(false, out, "Pong from your human: %s", strings.Join(parts, "; "))
	case StateWaiting:
		return textResult(false, out, "No Pong yet. Call wait_for_pong with ping_id %s.", p.ID)
	case StateHandedBack:
		return textResult(false, out, "The human handed this Ping back. Ask in your usual way in the Harness.")
	case StateAnsweredInHarness:
		return textResult(false, out, "The human answered this Ping in the Harness. Follow what they said there.")
	default:
		return textResult(false, out, "This Ping is %s and will get no Pong.", p.State)
	}
}
