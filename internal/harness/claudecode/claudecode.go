// Package claudecode is the Claude Code adapter. Its hook entry point reads one
// hook event from Claude Code, turns what it captures into Pings, waits for the
// human's Pongs and writes the answer in Claude Code's own shape.
//
// Each kind of event is handled in its own file, which registers itself with
// on() from an init function; Hook dispatches on the event name (and, for tool
// events, the tool name). Fixtures recorded from the real Claude Code are under
// testdata/<event kind>/ and testdata/recorded/.
//
// Fail open: whenever the Engine cannot be reached, or a Ping ends
// without a Pong, the hook writes nothing and exits 0, which lets Claude Code
// carry on exactly as if Pong were absent.
package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// HarnessName is how Pings from Claude Code name their Harness.
const HarnessName = "claude-code"

// TestedVersions are the Claude Code versions Pong was tested with: the one
// the fixtures were recorded from, and each later one whose hook events were
// recorded again and found the same (testdata/README.md).
var TestedVersions = []string{"2.1.322", "2.1.328"}

// WaitTimeout is the hook timeout, in seconds, for hooks that hold a prompt
// while a Ping waits: a week, far above Claude Code's 600-second default, so a
// Ping can sit for hours. Claude Code 2.1.322 honoured a hook running past 600
// seconds with this value.
const WaitTimeout = 7 * 24 * 60 * 60

// Event is one hook event as Claude Code sends it on standard input. Raw holds
// the whole payload for handlers that need more.
type Event struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	PermissionMode string          `json:"permission_mode"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolUseID      string          `json:"tool_use_id"`
	Raw            json.RawMessage `json:"-"`
	Dir            string          `json:"-"` // the Engine's data folder
}

// handler answers one event. It returns what to write on standard output, or
// nil to write nothing and let Claude Code proceed on its own.
type handler func(c *client.Client, e Event) (any, error)

var handlers = map[string]handler{}

// on registers h for an event name, or for "Event/ToolName" to handle one
// tool's events only. A tool-specific handler wins over an event-wide one.
func on(key string, h handler) {
	if handlers[key] != nil {
		panic("claudecode: two handlers for " + key)
	}
	handlers[key] = h
}

// Hook handles one hook call: the event is read from in, the Engine is found
// through the address file in dir, and Claude Code's answer, if any, is
// written to out. The error says why nothing was written; the caller still
// exits 0 so Claude Code proceeds.
func Hook(dir string, in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	var e Event
	if err := json.Unmarshal(raw, &e); err != nil {
		return fmt.Errorf("reading hook event: %w", err)
	}
	e.Raw, e.Dir = raw, dir
	h := handlers[e.HookEventName+"/"+e.ToolName]
	if h == nil {
		h = handlers[e.HookEventName]
	}
	if h == nil {
		return nil
	}
	c, err := client.Load(dir)
	if err != nil {
		return errors.New("Engine is not running")
	}
	var res any
	if e.HookEventName == "PreToolUse" {
		res = takeStop(c, e) // stopturn.go
	}
	if res == nil {
		if res, err = h(c, e); err != nil || res == nil {
			return err
		}
	}
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	_, err = out.Write(append(b, '\n'))
	return err
}

// errNoPong means a Ping ended without a Pong (handed back, answered in the
// Harness, withdrawn): Claude Code should show its own prompt.
var errNoPong = errors.New("a Ping ended without a Pong; Claude Code shows its own prompt")

// ask sends the Pings in order, as blocking Pings from e's Session, and
// waits for every Pong. If any Ping ends without one, the rest are withdrawn
// and errNoPong is returned.
func ask(c *client.Client, e Event, ns []engine.NewPing) ([]*engine.Pong, error) {
	refreshContext(c, e) // figures as of this Ping (context.go)
	ids := make([]string, 0, len(ns))
	withdrawRest := func() {
		for _, id := range ids {
			c.Withdraw(id)
		}
	}
	defer forget(ids)
	for _, n := range ns {
		n.Session, n.Harness, n.Folder, n.BlocksAgent, n.Captured = e.SessionID, HarnessName, e.Cwd, true, true
		p, err := c.CreatePing(n)
		if err != nil {
			withdrawRest()
			return nil, err
		}
		ids = append(ids, p.ID)
		remember(c, p.ID)
	}
	hold(e, ids) // held.go: answered in the terminal, they leave Pong at once
	defer release(e)
	pongs := make([]*engine.Pong, len(ids))
	for i, id := range ids {
		p, err := c.Wait(id)
		if err == nil && p.Pong == nil {
			err = errNoPong
		}
		if err != nil {
			withdrawRest()
			return nil, err
		}
		pongs[i] = p.Pong
	}
	return pongs, nil
}

// mirror sends ns as Pings for a prompt Claude Code shows in the terminal
// itself, without holding it: the human is at this Mac and answers there. The
// call's PostToolUse marks them answered in the Harness (held.go); a Pong sent
// first reaches the Session when its turn ends, as any question's does.
func mirror(c *client.Client, e Event, ns []engine.NewPing) error {
	refreshContext(c, e)
	var ids []string
	for _, n := range ns {
		n.Session, n.Harness, n.Folder, n.BlocksAgent, n.Captured = e.SessionID, HarnessName, e.Cwd, true, true
		p, err := c.CreatePing(n)
		if err != nil {
			return err
		}
		ids = append(ids, p.ID)
	}
	hold(e, ids)
	return nil
}

// waitingOn holds the Pings this hook process is waiting on, so they can be
// withdrawn if Claude Code kills the hook.
var waitingOn = struct {
	sync.Mutex
	m map[string]*client.Client
}{m: map[string]*client.Client{}}

func remember(c *client.Client, id string) {
	waitingOn.Lock()
	waitingOn.m[id] = c
	waitingOn.Unlock()
}

func forget(ids []string) {
	waitingOn.Lock()
	for _, id := range ids {
		delete(waitingOn.m, id)
	}
	waitingOn.Unlock()
}

// WithdrawWaiting withdraws every Ping this process is still waiting on. The
// hook command calls it when Claude Code stops the hook (the human pressed
// Esc, or the timeout ran out), so those Pings do not stay waiting.
func WithdrawWaiting() {
	waitingOn.Lock()
	defer waitingOn.Unlock()
	for id, c := range waitingOn.m {
		c.Withdraw(id)
		delete(waitingOn.m, id)
	}
}

// preToolUse is the PreToolUse answer: {"hookSpecificOutput": {...}}.
type preToolUse struct {
	HookSpecificOutput preToolUseDecision `json:"hookSpecificOutput"`
}

type preToolUseDecision struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             any    `json:"updatedInput,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
}

// noDecision lets Claude Code proceed on its own.
func noDecision(*client.Client, Event) (any, error) { return nil, nil }
