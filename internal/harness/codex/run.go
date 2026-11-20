package codex

import (
	"os/exec"

	"github.com/jacksonlw/pong/internal/engine"
)

// Runner says how a Codex Session is opened where the human sees it (ADR
// 0013). Pong runs no `codex exec` and no app server of its own: a Session
// opens in a terminal, as the human's own `codex`.
//
// The prompt is an argument, so Codex starts on it at once; its id is known
// only once Codex's hooks report the Session. `codex resume ID PROMPT`
// (codex-cli 0.170.0) opens an ended one again. Neither was tried against a
// signed-in Codex.
type Runner struct{}

var _ engine.Runner = Runner{}

func (Runner) Harness() string { return HarnessName }

// Check reports whether codex is on the human's PATH and signed in, as
// `codex login status` says.
func (Runner) Check(env []string, folder string) error {
	path, err := engine.LookPath(env, "codex")
	if err != nil {
		return engine.CannotStart("Codex is not installed: there is no codex command on your PATH. Install Codex, then try again.")
	}
	cmd := exec.Command(path, "login", "status")
	cmd.Env, cmd.Dir = env, folder
	if err := cmd.Run(); err != nil {
		return engine.CannotStart("Codex is not signed in: run codex in a terminal and sign in, then try again.")
	}
	return nil
}

// Start opens a new Session in a terminal, started on prompt.
func (Runner) Start(_, folder, prompt string) engine.Launch {
	return engine.Launch{Command: "cd " + shellQuote(folder) + " && codex -- " + engine.ShellQuote(prompt)}
}

// Continue opens an ended Session again in a terminal, on text.
func (Runner) Continue(_, folder, id, text string) engine.Launch {
	return engine.Launch{Command: "cd " + shellQuote(folder) + " && codex resume " + shellQuote(id) + " -- " + engine.ShellQuote(text)}
}

// ResumeCommand resumes the Session in a terminal.
func (Runner) ResumeCommand(folder, id string) string {
	return "cd " + shellQuote(folder) + " && codex resume " + shellQuote(id)
}
