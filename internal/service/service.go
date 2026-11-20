// Package service installs, starts and removes the Engine as a per-user
// background service: a
// launch agent on the Mac, a systemd user service with lingering on Linux.
package service

import (
	_ "embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed launchagent.plist
var launchAgent string

// Label names the service on both platforms.
const Label = "fly.pong.engine"

// Run runs a command and returns its combined output. Tests replace it.
type Run func(name string, args ...string) ([]byte, error)

// Exec runs the real command.
func Exec(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput()
}

// Service is the Engine's service for one human.
type Service struct {
	GOOS   string // "darwin" or "linux"
	Home   string
	Engine string // path of pong-engine
	Dir    string // the Engine's data directory
	User   string // login name, for lingering
	UID    int
	Run    Run
}

// ForCurrentUser fills in the human running this process.
func ForCurrentUser(goos, home, engine, dir string) Service {
	s := Service{GOOS: goos, Home: home, Engine: engine, Dir: dir, UID: os.Getuid(), Run: Exec}
	if u, err := user.Current(); err == nil {
		s.User = u.Username
	}
	return s
}

// Path is the service's definition file.
func (s Service) Path() string {
	if s.GOOS == "darwin" {
		return filepath.Join(s.Home, "Library", "LaunchAgents", Label+".plist")
	}
	return filepath.Join(s.Home, ".config", "systemd", "user", "pong-engine.service")
}

func (s Service) definition() string {
	if s.GOOS == "darwin" {
		return fmt.Sprintf(launchAgent, Label, xmlEscape(s.Engine), xmlEscape(s.Dir), xmlEscape(filepath.Join(s.Dir, "engine.log")))
	}
	return fmt.Sprintf(`[Unit]
Description=Pong Engine

[Service]
ExecStart=%s -dir %s
Restart=always
RestartSec=1

[Install]
WantedBy=default.target
`, systemdQuote(s.Engine), systemdQuote(s.Dir))
}

// Install writes the service and starts it. It returns steps the human must
// do themselves, such as enabling lingering when that needs their password.
func (s Service) Install() (todo []string, err error) {
	if s.GOOS != "darwin" && s.GOOS != "linux" {
		return nil, fmt.Errorf("the Engine runs as a service on Mac and Linux only, not %s", s.GOOS)
	}
	if err := os.MkdirAll(filepath.Dir(s.Path()), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.Path(), []byte(s.definition()), 0o644); err != nil {
		return nil, err
	}
	if s.GOOS == "darwin" {
		// Load the new definition: take out any older one first. bootout
		// returns before the job is gone, so bootstrap is retried briefly.
		s.Run("launchctl", "bootout", s.domain()+"/"+Label)
		var out []byte
		var err error
		for i := 0; i < 20; i++ {
			if out, err = s.Run("launchctl", "bootstrap", s.domain(), s.Path()); err == nil {
				return nil, nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return nil, fmt.Errorf("launchctl bootstrap (the Engine's launch agent needs a login session on this Mac): %s", firstLine(out, err))
	}
	// Lingering first: on a box reached only over SSH it also starts the
	// user's service manager that systemctl --user talks to.
	if out, err := s.Run("loginctl", "enable-linger", s.User); err != nil {
		todo = append(todo, fmt.Sprintf("let the Engine start at boot without you logging in: sudo loginctl enable-linger %s (%s), then run pong setup again", s.User, firstLine(out, err)))
	}
	for _, args := range [][]string{{"--user", "daemon-reload"}, {"--user", "enable", "pong-engine.service"}, {"--user", "restart", "pong-engine.service"}} {
		if out, err := s.Run("systemctl", args...); err != nil {
			return todo, fmt.Errorf("systemctl %s: %s", strings.Join(args, " "), firstLine(out, err))
		}
	}
	return todo, nil
}

// Start starts the installed service if it is not running.
func (s Service) Start() error {
	if _, err := os.Stat(s.Path()); err != nil {
		return err
	}
	var out []byte
	var err error
	if s.GOOS == "darwin" {
		if _, err = s.Run("launchctl", "print", s.domain()+"/"+Label); err != nil {
			if out, err := s.Run("launchctl", "bootstrap", s.domain(), s.Path()); err != nil {
				return fmt.Errorf("loading the Engine's launch agent: %s", firstLine(out, err))
			}
		}
		out, err = s.Run("launchctl", "kickstart", s.domain()+"/"+Label)
	} else {
		out, err = s.Run("systemctl", "--user", "start", "pong-engine.service")
	}
	if err != nil {
		return fmt.Errorf("starting the Engine: %s", firstLine(out, err))
	}
	return nil
}

// Remove stops the service and deletes its definition. Lingering is left as
// it was, since other services may need it.
func (s Service) Remove() error {
	if _, err := os.Stat(s.Path()); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if s.GOOS == "darwin" {
		s.Run("launchctl", "bootout", s.domain()+"/"+Label)
	} else {
		s.Run("systemctl", "--user", "disable", "--now", "pong-engine.service")
	}
	if err := os.Remove(s.Path()); err != nil {
		return err
	}
	if s.GOOS == "linux" {
		s.Run("systemctl", "--user", "daemon-reload")
	}
	return nil
}

// Installed reports whether the service's definition is present.
func (s Service) Installed() bool {
	_, err := os.Stat(s.Path())
	return err == nil
}

func (s Service) domain() string { return "gui/" + strconv.Itoa(s.UID) }

func firstLine(out []byte, err error) string {
	if l, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n"); l != "" {
		return l
	}
	return err.Error()
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// systemdQuote quotes a path for ExecStart.
func systemdQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(s) + `"`
}
