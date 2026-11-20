package claudecode

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/jacksonlw/pong/internal/engine"
)

// Runner says how a Claude Code Session is opened where the human sees it
//. Pong runs no `claude -p`: a Session opens in a terminal, as the
// human's own `claude` with their settings, login and plugins.
//
// The prompt is an argument, so Claude Code starts on it at once, and
// --session-id lets Pong name the Session before it exists. The Claude
// desktop app is not used to start one: its link (claude://code/new) only
// fills the prompt in and has nothing that sends it (checked in the app's
// own link rules, 2026-11-19). For a human who works in that app the
// Session runs its first turn with no window and then moves into the app
// (move.go).
type Runner struct{}

var _ engine.Runner = Runner{}

func (Runner) Harness() string { return HarnessName }

// Check reports whether `claude` is on the human's PATH and signed in.
func (Runner) Check(env []string, folder string) error {
	path, err := engine.LookPath(env, "claude")
	if err != nil {
		return engine.CannotStart("Claude Code is not installed: there is no claude command on your PATH. Install Claude Code, then try again.")
	}
	cmd := exec.Command(path, "auth", "status", "--json")
	cmd.Env, cmd.Dir = env, folder
	out, _ := cmd.Output() // exits 1 when signed out
	var st struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return engine.CannotStart("Claude Code did not say whether it is signed in (claude auth status): " + strings.TrimSpace(string(out)))
	}
	if !st.LoggedIn {
		return engine.CannotStart("Claude Code is not signed in: run claude in a terminal and sign in, then try again.")
	}
	return nil
}

// Start opens a new Session started on prompt: in a terminal, or hidden
// until it moves into the app.
func (Runner) Start(surface, folder, prompt string) engine.Launch {
	id := newUUID()
	if surface == engine.SurfaceApp {
		return engine.Launch{SessionID: id, Command: "cd " + shellQuote(folder) + " && " + moved(MoveNew, id, prompt), Hidden: true}
	}
	return engine.Launch{SessionID: id, Command: "cd " + shellQuote(folder) + " && claude --session-id " + id + " -- " + engine.ShellQuote(prompt)}
}

// Continue opens an ended Session again on text, as Start does.
func (Runner) Continue(surface, folder, id, text string) engine.Launch {
	if surface == engine.SurfaceApp {
		return engine.Launch{Command: "cd " + shellQuote(folder) + " && " + moved(MoveResumed, id, text), Hidden: true}
	}
	return engine.Launch{Command: "cd " + shellQuote(folder) + " && claude --resume " + shellQuote(id) + " -- " + engine.ShellQuote(text)}
}

// SharesWithApp marks Claude Code as a Harness whose terminal Sessions can
// be moved into its app (engine.AppSharer).
func (Runner) SharesWithApp() {}

var _ engine.AppSharer = Runner{}

// ResumeCommand resumes the Session in a terminal.
func (Runner) ResumeCommand(folder, id string) string {
	return "cd " + shellQuote(folder) + " && claude --resume " + shellQuote(id)
}

// DesktopAppName is what the Claude desktop app is called.
const DesktopAppName = "Claude"

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
