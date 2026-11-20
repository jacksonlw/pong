package harnesses_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/harnesses"
)

func start(t *testing.T, home string) *client.Client {
	t.Helper()
	dir := t.TempDir()
	e, err := engine.Start(engine.Config{Dir: dir, Harnesses: harnesses.ForEngine{Home: home, Pong: "/bin/pong", EngineDir: dir}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	c, err := client.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func find(t *testing.T, list []engine.HarnessStatus, id string) engine.HarnessStatus {
	t.Helper()
	for _, s := range list {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("%s not listed in %+v", id, list)
	return engine.HarnessStatus{}
}

// The Settings screen lists every Harness and repairs one through the API.
func TestListAndRepairThroughTheEngine(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".gemini"), 0o755)
	c := start(t, home)

	list, err := c.Harnesses()
	if err != nil {
		t.Fatal(err)
	}
	if s := find(t, list, "gemini"); !s.Installed || s.Connected || s.Name != "Gemini CLI" {
		t.Fatalf("gemini before repair: %+v", s)
	}
	if s := find(t, list, "codex"); s.Installed || s.Connected {
		t.Fatalf("codex: %+v", s)
	}
	s, err := c.RepairHarness("gemini")
	if err != nil || !s.Connected {
		t.Fatalf("repair: %+v %v", s, err)
	}
	list, _ = c.Harnesses()
	if s := find(t, list, "gemini"); !s.Connected {
		t.Fatalf("gemini after repair: %+v", s)
	}
	var ce *client.Error
	if _, err := c.RepairHarness("zed"); !errors.As(err, &ce) || ce.Status != 404 {
		t.Fatalf("repair of unknown Harness: %v", err)
	}
}

// Repair mends Pong's entry when the human changed it by hand.
func TestRepairMendsAHandEditedEntry(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".gemini"), 0o755)
	c := start(t, home)
	if _, err := c.RepairHarness("gemini"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(home, ".gemini", "GEMINI.md"), []byte("mine\n"), 0o644)
	if list, _ := c.Harnesses(); find(t, list, "gemini").Connected {
		t.Fatal("still connected after the instruction was replaced")
	}
	if s, err := c.RepairHarness("gemini"); err != nil || !s.Connected {
		t.Fatalf("repair: %+v %v", s, err)
	}
}

// A repair of Claude Code with no claude to install with changes nothing,
// though Remove alone would take Pong out.
func TestRepairWithoutClaudeChangesNothing(t *testing.T) {
	home := t.TempDir()
	record := filepath.Join(home, "Library", "Application Support", "Pong", "harnesses", "claude-code.json")
	settings := filepath.Join(home, ".claude", "settings.json")
	for p, body := range map[string]string{
		record:   `{"installed_at": "2026-11-01T00:00:00Z", "files": {}}`,
		settings: `{"enabledPlugins": {"pong@pong": true}}`,
	} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(body), 0o644)
	}
	f := harnesses.ForEngine{Home: home, Pong: "/bin/pong", EngineDir: t.TempDir(), Env: []string{"PATH=" + t.TempDir()}}
	if _, err := f.Repair("claude-code"); err == nil {
		t.Fatal("repair succeeded with no claude")
	}
	if b, _ := os.ReadFile(settings); string(b) != `{"enabledPlugins": {"pong@pong": true}}` {
		t.Fatalf("settings.json changed: %s", b)
	}
	if _, err := os.Stat(record); err != nil {
		t.Fatal("the record was removed")
	}
}
