package service_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/internal/service"
)

type calls []string

func fake(c *calls, fail map[string]bool) service.Run {
	return func(name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		*c = append(*c, cmd)
		for k := range fail {
			if strings.HasPrefix(cmd, k) {
				return []byte("Interactive authentication required."), errors.New("exit status 1")
			}
		}
		return nil, nil
	}
}

func svc(t *testing.T, goos string, c *calls, fail map[string]bool) service.Service {
	home := t.TempDir()
	return service.Service{GOOS: goos, Home: home, Engine: "/opt/Pong.app/Contents/Helpers/pong-engine", Dir: filepath.Join(home, "data"), User: "ada", UID: 501, Run: fake(c, fail)}
}

func TestMacInstallsALaunchAgentThatOutlivesTheApp(t *testing.T) {
	var c calls
	s := svc(t, "darwin", &c, nil)
	todo, err := s.Install()
	if err != nil || len(todo) > 0 {
		t.Fatal(err, todo)
	}
	b, err := os.ReadFile(filepath.Join(s.Home, "Library/LaunchAgents/fly.pong.engine.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<string>/opt/Pong.app/Contents/Helpers/pong-engine</string>", "<key>RunAtLoad</key><true/>", "<key>KeepAlive</key><true/>", s.Dir} {
		if !strings.Contains(string(b), want) {
			t.Errorf("plist lacks %s", want)
		}
	}
	if strings.Contains(string(b), "exit-with-stdin") {
		t.Error("the launch agent's Engine would exit with the app")
	}
	if !strings.Contains(strings.Join(c, "\n"), "launchctl bootstrap gui/501 "+s.Path()) {
		t.Errorf("not loaded: %v", c)
	}
}

func TestLinuxInstallsAUserServiceWithLingering(t *testing.T) {
	var c calls
	s := svc(t, "linux", &c, nil)
	todo, err := s.Install()
	if err != nil || len(todo) > 0 {
		t.Fatal(err, todo)
	}
	b, _ := os.ReadFile(filepath.Join(s.Home, ".config/systemd/user/pong-engine.service"))
	if !strings.Contains(string(b), `ExecStart="/opt/Pong.app/Contents/Helpers/pong-engine" -dir`) || !strings.Contains(string(b), "WantedBy=default.target") {
		t.Errorf("unit:\n%s", b)
	}
	all := strings.Join(c, "\n")
	for _, want := range []string{"systemctl --user enable pong-engine.service", "systemctl --user restart pong-engine.service", "loginctl enable-linger ada"} {
		if !strings.Contains(all, want) {
			t.Errorf("did not run %s: %v", want, c)
		}
	}
}

func TestLingeringThatNeedsAPasswordIsLeftToTheHuman(t *testing.T) {
	var c calls
	s := svc(t, "linux", &c, map[string]bool{"loginctl": true})
	todo, err := s.Install()
	if err != nil {
		t.Fatal(err)
	}
	if len(todo) != 1 || !strings.Contains(todo[0], "sudo loginctl enable-linger ada") {
		t.Fatalf("todo %v", todo)
	}
}

func TestRemoveTakesTheServiceOut(t *testing.T) {
	for _, goos := range []string{"darwin", "linux"} {
		var c calls
		s := svc(t, goos, &c, nil)
		if _, err := s.Install(); err != nil {
			t.Fatal(err)
		}
		if err := s.Remove(); err != nil {
			t.Fatal(err)
		}
		if s.Installed() {
			t.Errorf("%s: definition left behind", goos)
		}
		if err := s.Remove(); err != nil {
			t.Errorf("%s: removing twice: %v", goos, err)
		}
	}
}

func TestStartLoadsAndKicksTheLaunchAgent(t *testing.T) {
	var c calls
	s := svc(t, "darwin", &c, nil)
	s.Install()
	c = nil
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(c, "\n"), "launchctl kickstart gui/501/fly.pong.engine") {
		t.Errorf("not started: %v", c)
	}
}
