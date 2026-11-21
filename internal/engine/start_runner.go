package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Pong runs no Session itself: a Session is always the human's
// own, in their Harness. Starting one from Pong, or continuing an ended one,
// opens it in a terminal, where the Harness's tool starts on the prompt at
// once. A Harness's own app cannot be handed a prompt that it then sends, so
// Pong does not start Sessions there; for a human who works in the app, the
// first turn runs with no window and the Session then moves into the app.

// Where a human works in a Harness. It does not change where Pong opens a
// Session, only how: a Harness that can share a terminal Session with its
// app does so for a human who works in the app (AppSharer).
const (
	SurfaceApp      = "app"      // the Harness's own desktop app
	SurfaceTerminal = "terminal" // the Harness's command-line tool, in a terminal
)

// Surfaces lists the surfaces, the first being the one assumed when the human
// has chosen none and none of their Sessions ran in the Harness's app.
var Surfaces = []string{SurfaceTerminal, SurfaceApp}

// Launch is what the Engine opens to put a Session in front of the human: a
// command run in a new terminal window, or with no window at all when Hidden.
type Launch struct {
	Command string // run in the human's login shell
	// Hidden runs Command with no terminal window. Only for a Session that
	// moves itself into its Harness's app once its first turn ends (ADR
	// 0013): until then the human follows it through Pong.
	Hidden bool
	// SessionID is the Session's id when the Harness lets it be chosen up
	// front; "" when it is known only once the Harness reports the Session.
	SessionID string
}

// Runner knows how one Harness opens a Session in a terminal. Each full
// adapter provides one; nothing above it knows which Harness it serves. It
// runs nothing: the Engine opens what it returns (Config.Launch).
type Runner interface {
	// Harness is the name Pings from this Harness carry.
	Harness() string
	// Check returns why a Session cannot be started now, as a CannotStart:
	// the tool is missing, or not signed in. env is the human's environment.
	Check(env []string, folder string) error
	// Start is how a new Session is opened in folder and started on prompt.
	// surface is where the human works in the Harness.
	Start(surface, folder, prompt string) Launch
	// Continue is how the ended Session id is opened again with text as its
	// next prompt.
	Continue(surface, folder, id, text string) Launch
	// ResumeCommand is the shell command that resumes the Session in a terminal.
	ResumeCommand(folder, id string) string
}

// AppSharer is a Runner whose Harness can share a Session started in a
// terminal with its own app. Only for such a Harness does it matter where
// the human works, so only it gets that choice in Settings.
type AppSharer interface {
	SharesWithApp()
}

// OpenLaunch opens l on this computer through a .command file, which the
// system opens in the human's terminal with no Automation prompt. It is the
// Engine's default Launch.
func OpenLaunch(l Launch) error {
	if runtime.GOOS != "darwin" {
		return CannotStart("Pong can open a Session only on a Mac for now: start it in the Harness on this computer")
	}
	if l.Hidden {
		return openHidden(l)
	}
	dir := filepath.Join(os.TempDir(), "pong-open")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "*.command")
	if err != nil {
		return err
	}
	// Run in the login shell so the Harness's tool is on the PATH, then stay
	// in an interactive shell once it exits.
	script := "#!/bin/sh\nrm -f \"$0\"\nexec \"${SHELL:-/bin/zsh}\" -l -c " + ShellQuote(l.Command+"; exec \"${SHELL:-/bin/zsh}\" -l") + "\n"
	if _, err := f.WriteString(script); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(f.Name(), 0o755); err != nil {
		return err
	}
	if out, err := exec.Command("/usr/bin/open", openArgs(f.Name())...).CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// openArgs opens the .command file without bringing the terminal app forward:
// the Session's window appears behind the app the human is using, so it never
// takes the keyboard or a click meant for that app.
func openArgs(command string) []string { return []string{"-g", command} }

// openHidden runs l's command in the human's login shell in its own process
// session, with no terminal and no input or output, so it keeps running if
// the Engine stops.
func openHidden(l Launch) error {
	shell := loginShell(os.Getenv("SHELL"), runtime.GOOS, "/etc/passwd", os.Getuid())
	cmd := exec.Command(shell, "-l", "-c", l.Command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

// ShellQuote quotes s as one word for a POSIX shell.
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// CannotStart says why a Session could not be started, in words an
// interface shows the human.
type CannotStart string

func (e CannotStart) Error() string { return string(e) }

// LookPath finds an executable named file in the PATH of env.
func LookPath(env []string, file string) (string, error) {
	path := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			path = v
		}
	}
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, file)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// LoginEnv is the human's shell environment as a terminal sees it: the
// output of env in their login, interactive shell. It falls back to the
// Engine's own environment when the shell cannot be run.
func LoginEnv() []string {
	shell := loginShell(os.Getenv("SHELL"), runtime.GOOS, "/etc/passwd", os.Getuid())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-l", "-i", "-c", "env -0")
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil {
		return os.Environ()
	}
	var env []string
	for _, kv := range bytes.Split(out, []byte{0}) {
		if s := string(kv); strings.Contains(s, "=") {
			env = append(env, s)
		}
	}
	if len(env) == 0 {
		return os.Environ()
	}
	return env
}

// loginShell is the human's shell. A service started by launchd or systemd
// may have no $SHELL, so on Linux it is read from the passwd file; the Mac's
// default is zsh.
func loginShell(env, goos, passwd string, uid int) string {
	if env != "" {
		return env
	}
	if goos == "darwin" {
		if u, err := user.LookupId(strconv.Itoa(uid)); err == nil {
			out, err := exec.Command("/usr/bin/dscl", ".", "-read", "/Users/"+u.Username, "UserShell").Output()
			if _, sh, ok := strings.Cut(strings.TrimSpace(string(out)), "UserShell: "); err == nil && ok && sh != "" {
				return sh
			}
		}
		return "/bin/zsh"
	}
	if b, err := os.ReadFile(passwd); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Split(line, ":")
			if len(f) >= 7 && f[2] == strconv.Itoa(uid) && f[6] != "" {
				return f[6]
			}
		}
	}
	return "/bin/sh"
}
