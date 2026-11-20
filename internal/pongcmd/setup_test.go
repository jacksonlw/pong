package pongcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/integrations"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/service"
)

// Tests never open the real app.
func init() {
	startApp = func(string) error { return errors.New("tests do not open Pong.app") }
	real := serviceFor
	serviceFor = func(f setupFlags) service.Service {
		s := real(f)
		s.Run = func(string, ...string) ([]byte, error) { return nil, nil }
		return s
	}
}

// opensEngine makes opening the app start an Engine on dir instead.
func opensEngine(t *testing.T, dir string) {
	t.Helper()
	old := startApp
	t.Cleanup(func() { startApp = old })
	startApp = func(string) error {
		e, err := engine.Start(engine.Config{Dir: dir, WaitHold: enginetest.WaitHold})
		if err != nil {
			return err
		}
		t.Cleanup(func() { e.Close() })
		return nil
	}
}

// answerTestPing plays the human: it answers the first waiting Ping.
func answerTestPing(t *testing.T, dir string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if c, err := client.Load(dir); err == nil {
			if ps, _ := c.Pings(engine.StateWaiting); len(ps) > 0 {
				if _, err := c.Pong(ps[0].ID, client.SendPong{Answer: ps[0].Answers[0].Label}); err != nil {
					t.Error(err)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("no test Ping arrived")
}

// snapshot reads every file under root.
func snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			b, _ := os.ReadFile(p)
			m[p] = string(b)
		}
		return nil
	})
	return m
}

func seed(t *testing.T, home string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		p = filepath.Join(home, p)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Setup installs Pong in the Harness found, opens Pong, and finishes once the
// human answers the test Ping.
func TestSetupFinishesWhenTheTestPingIsAnswered(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(home, ".gemini"), 0o755)
	opensEngine(t, dir)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); answerTestPing(t, dir) }()
	var out bytes.Buffer
	code := run([]string{"setup", "-home", home, "-dir", dir, "-pong", "/bin/pong", "-wait", "10s"}, &out, &out)
	wg.Wait()
	s := out.String()
	if code != 0 || !strings.Contains(s, "Gemini CLI: installed") || !strings.Contains(s, "The test Ping was answered") || strings.Contains(s, "Left to do") {
		t.Fatalf("exit %d:\n%s", code, s)
	}
	if strings.Contains(s, "Amp") || strings.Contains(s, "Codex") {
		t.Fatalf("named a Harness that is not installed:\n%s", s)
	}
}

// With no Pong within the wait, setup fails and says so.
func TestSetupFailsWithoutAPong(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".gemini"), 0o755)
	c, dir := enginetest.Start(t)
	var out bytes.Buffer
	code := run([]string{"setup", "-home", home, "-dir", dir, "-pong", "/bin/pong", "-wait", "50ms"}, &out, &out)
	s := out.String()
	if code != 1 || !strings.Contains(s, "no Pong came back") || !strings.Contains(s, "Setup is not finished") {
		t.Fatalf("exit %d:\n%s", code, s)
	}
	if ps, _ := c.Pings(engine.StateWaiting); len(ps) != 0 {
		t.Fatalf("test Ping left waiting: %+v", ps)
	}
	// The test Ping's Session is not left working.
	ss, _ := c.Sessions()
	for _, x := range ss {
		if x.State != engine.SessionEnded {
			t.Fatalf("Session left %s: %+v", x.State, x)
		}
	}
}

// Every step setup could not do is listed together at the end: here Pong
// could not be opened, and Codex's hooks still need trusting.
func TestSetupListsWhatIsLeftAtTheEnd(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	var out bytes.Buffer
	code := run([]string{"setup", "-home", home, "-dir", t.TempDir(), "-pong", "/bin/pong", "-wait", "50ms"}, &out, &out)
	s := out.String()
	_, tail, ok := strings.Cut(s, "\nLeft to do:\n")
	if code != 1 || !ok || !strings.Contains(tail, "- open Pong") || !strings.Contains(tail, "- Codex: ") || !strings.Contains(tail, "/hooks") {
		t.Fatalf("exit %d:\n%s", code, s)
	}
	run([]string{"remove", "-home", home, "-dir", t.TempDir()}, &out, &out)
}

func TestStatusPerHarness(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".gemini"), 0o755)
	_, dir := enginetest.Start(t)
	var out bytes.Buffer
	if code := run([]string{"setup", "-home", home, "-dir", dir, "-pong", "/bin/pong", "gemini"}, &out, &out); code != 0 {
		t.Fatalf("setup gemini: %s", out.String())
	}
	out.Reset()
	if code := run([]string{"status", "-home", home, "-dir", dir}, &out, &out); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	s := out.String()
	for _, want := range []string{"Engine: running", "Gemini CLI: connected", "Codex: not installed", "Claude Code: not installed", "Amp: not installed"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q:\n%s", want, s)
		}
	}

	out.Reset()
	run([]string{"status", "-json", "-home", home, "-dir", dir}, &out, &out)
	var got statusJSON
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v:\n%s", err, out.String())
	}
	if !got.Engine.Running || got.Engine.Version != engine.Version {
		t.Fatalf("engine: %+v", got.Engine)
	}
	for _, h := range got.Harnesses {
		if want := h.ID == "gemini"; h.Installed != want || h.Connected != want {
			t.Fatalf("harness: %+v", h)
		}
	}
}

// Remove leaves every Harness's configuration exactly as it was before setup.
func TestRemoveRestoresTheHomeFolder(t *testing.T) {
	home := t.TempDir()
	seed(t, home, map[string]string{
		".gemini/settings.json":     "{\n  \"theme\": \"dark\",\n  \"mcpServers\": {\"other\": {\"command\": \"x\"}}\n}\n",
		".gemini/GEMINI.md":         "# Mine\n\nBe terse.\n",
		".factory/mcp.json":         "{}\n",
		".config/amp/settings.json": "{\"amp.theme\": \"light\"}\n",
	})
	before := snapshot(t, home)
	var out bytes.Buffer
	run([]string{"setup", "-home", home, "-dir", t.TempDir(), "-pong", "/bin/pong", "-wait", "1ms"}, &out, &out)
	if maps.Equal(before, snapshot(t, home)) {
		t.Fatalf("setup changed nothing:\n%s", out.String())
	}
	if code := run([]string{"remove", "-home", home, "-dir", t.TempDir()}, &out, &out); code != 0 {
		t.Fatalf("remove exit %d:\n%s", code, out.String())
	}
	if after := snapshot(t, home); !maps.Equal(before, after) {
		t.Fatalf("home after remove differs:\nbefore %v\nafter  %v", before, after)
	}
}

func TestSetupUnknownHarnessPrintsWhatToAdd(t *testing.T) {
	home := t.TempDir()
	var out, errb bytes.Buffer
	if code := run([]string{"setup", "-home", home, "-pong", "/bin/pong", "zed"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	s := out.String()
	if !strings.Contains(s, `"pong": {"command": "/bin/pong", "args": ["mcp"]}`) || !strings.Contains(s, integrations.StandingInstruction) {
		t.Fatalf("output:\n%s", s)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatal("wrote into home")
	}
}

// Installing in Codex alone tells the human about the one-time trust step.
func TestSetupCodexSaysToTrustHooks(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	dir := t.TempDir()
	var out, errb bytes.Buffer
	if code := run([]string{"setup", "-home", home, "-dir", dir, "-pong", "/bin/pong", "codex"}, &out, &errb); code != 0 {
		t.Fatalf("install exit %d: %s", code, errb.String())
	}
	if s := out.String(); !strings.HasPrefix(s, "Codex: installed") || !strings.Contains(s, "/hooks") {
		t.Fatalf("install said:\n%s", s)
	}
	if code := run([]string{"remove", "-home", home, "-dir", dir, "codex"}, &out, &errb); code != 0 {
		t.Fatalf("remove exit %d: %s", code, errb.String())
	}
	// Status runs codex app-server, which keeps its own state in .codex; only
	// Pong's files must be gone.
	for _, f := range []string{"config.toml", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(home, ".codex", f)); err == nil {
			t.Fatalf("left %s in .codex", f)
		}
	}
}

// With the real claude command: setup installs Pong's plugin into a temporary
// home, lists reloading running Sessions as left to do, and remove puts the
// settings back as they were (the adapter's own tests compare the whole tree,
// less Claude Code's own state files).
func TestSetupAndRemoveClaudeCode(t *testing.T) {
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude is not installed")
	}
	home := t.TempDir()
	seed(t, home, map[string]string{".claude/settings.json": "{\n  \"theme\": \"dark\"\n}\n"})
	_, dir := enginetest.Start(t)
	var out bytes.Buffer
	run([]string{"setup", "-home", home, "-dir", dir, "-pong", "/bin/pong", "-wait", "50ms"}, &out, &out)
	s := out.String()
	// Claude Code updates itself, so the version on this machine may be one
	// Pong was not tested with; setup then says so first on the same line.
	if !strings.Contains(s, "Claude Code: installed") || !regexp.MustCompile(`(?m)^- Claude Code: (Pong was tested with Claude Code [0-9., ]+ only\. )?Sessions already running need /reload-plugins`).MatchString(s) {
		t.Fatalf("setup:\n%s", s)
	}
	if code := run([]string{"remove", "-home", home, "-dir", t.TempDir()}, &out, &out); code != 0 {
		t.Fatalf("remove:\n%s", out.String())
	}
	if b, _ := os.ReadFile(filepath.Join(home, ".claude/settings.json")); string(b) != "{\n  \"theme\": \"dark\"\n}\n" {
		t.Fatalf("settings after remove:\n%s", b)
	}
}
