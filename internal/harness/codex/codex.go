// Package codex is the Codex adapter. Its hook entry point reads one hook
// event from Codex, turns its permission prompts and finished turns into
// Pings, waits for the human's Pongs and writes the answer in Codex's shape.
//
// No Codex payload has been recorded yet (Codex was not signed in on the
// computer this was built on). The hook payloads and answers here follow
// Codex's documentation (https://learn.chatgpt.com/docs/hooks, read
// 2026-11-18); the fixtures under testdata/documented/ were written from it
// and are not recordings, so hook behaviour against the real Codex is
// unverified. The configuration Install writes was checked against
// codex-cli 0.170.0, which reads it (install_test.go).
//
// Fail open: whenever the Engine cannot be reached, or a Ping ends
// without a Pong, the hook writes nothing and exits 0, which Codex documents
// as success with no decision, so it carries on as if Pong were absent.
package codex

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/attended"
	"github.com/jacksonlw/pong/internal/harness/turn"
)

// HarnessName is how Pings from Codex name their Harness.
const HarnessName = "codex"

// TestedVersions are the Codex versions the adapter was tested against: none
// yet, since no payload has been recorded from the real Codex.
var TestedVersions = []string{}

// Event is one hook event as Codex documents it. Raw holds the whole payload.
type Event struct {
	SessionID            string          `json:"session_id"`
	Cwd                  string          `json:"cwd"`
	HookEventName        string          `json:"hook_event_name"`
	Model                string          `json:"model"`
	ToolName             string          `json:"tool_name"`
	ToolInput            json.RawMessage `json:"tool_input"`
	StopHookActive       bool            `json:"stop_hook_active"`
	LastAssistantMessage string          `json:"last_assistant_message"`
}

// Hook handles one hook call: the event is read from in, the Engine is found
// through the address file in dir, and Codex's answer, if any, is written to
// out. The error says why nothing was written; the caller still exits 0.
func Hook(dir string, in io.Reader, out io.Writer) error {
	raw, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	var e Event
	if err := json.Unmarshal(raw, &e); err != nil {
		return fmt.Errorf("reading hook event: %w", err)
	}
	var h func(*client.Client, Event) (any, error)
	switch e.HookEventName {
	case "PermissionRequest":
		h = askPermission
	case "Stop":
		h = stop
	case "UserPromptSubmit":
		h = func(c *client.Client, e Event) (any, error) {
			return nil, turn.Prompted(c, HarnessName, e.SessionID, e.Cwd)
		}
	default:
		return nil
	}
	c, err := client.Load(dir)
	if err != nil {
		return errors.New("Engine is not running")
	}
	registerSession(c, e)
	res, err := h(c, e)
	if err != nil || res == nil {
		return err
	}
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	_, err = out.Write(append(b, '\n'))
	return err
}

// registerSession registers the Session every documented hook payload names, with
// its folder, the command that resumes it (the Runner's) and its model.
// Codex documents no context-window size, used fraction or cost for hooks,
// and its token_usage (PostModelRequest only) counts one model request rather
// than the context in use, so only the model is recorded of its figures.
func registerSession(c *client.Client, e Event) {
	if e.SessionID == "" {
		return
	}
	// The Codex process it runs in, so the Engine ends it if that dies unseen.
	n := engine.RegisterSession{Harness: HarnessName, ID: e.SessionID, Folder: e.Cwd, PID: attended.HarnessPID()}
	if e.Cwd != "" {
		n.ResumeCommand = Runner{}.ResumeCommand(e.Cwd, e.SessionID)
	}
	if e.Model != "" {
		n.Context = &engine.Context{Model: e.Model}
	}
	c.RegisterSession(n)
}

// errNoPong means a Ping ended without a Pong: Codex shows its own prompt.
var errNoPong = errors.New("a Ping ended without a Pong; Codex shows its own prompt")

// ask sends n as a blocking, captured Ping from e's Session and waits for its
// Pong.
func ask(c *client.Client, e Event, n engine.NewPing) (*engine.Pong, error) {
	n.Session, n.Harness, n.Folder, n.BlocksAgent, n.Captured = e.SessionID, HarnessName, e.Cwd, true, true
	p, err := c.CreatePing(n)
	if err != nil {
		return nil, err
	}
	remember(c, p.ID)
	defer forget(p.ID)
	p, err = c.Wait(p.ID)
	if err == nil && p.Pong == nil {
		err = errNoPong
	}
	if err != nil {
		c.Withdraw(p.ID)
		return nil, err
	}
	return p.Pong, nil
}

// waitingOn holds the Pings this hook process waits on, so they can be
// withdrawn if Codex stops the hook.
var waitingOn = struct {
	sync.Mutex
	m map[string]*client.Client
}{m: map[string]*client.Client{}}

func remember(c *client.Client, id string) {
	waitingOn.Lock()
	waitingOn.m[id] = c
	waitingOn.Unlock()
}

func forget(id string) {
	waitingOn.Lock()
	delete(waitingOn.m, id)
	waitingOn.Unlock()
}

// WithdrawWaiting withdraws every Ping this process still waits on. The hook
// command calls it when the hook is stopped by a signal.
func WithdrawWaiting() {
	waitingOn.Lock()
	defer waitingOn.Unlock()
	for id, c := range waitingOn.m {
		c.Withdraw(id)
		delete(waitingOn.m, id)
	}
}

// answerText is a Pong as text for the agent.
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
