package codex_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/codex"
)

// withCodex starts an Engine whose captured PATH has a codex that answers
// `codex login status`, signed in or not, and records what the Engine opens.
func withCodex(t *testing.T, signedIn bool) (*client.Client, *[]engine.Launch, string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\nexit 0\n"
	if !signedIn {
		script = "#!/bin/sh\necho 'Not logged in'\nexit 1\n"
	}
	if err := os.WriteFile(filepath.Join(bin, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	var opened []engine.Launch
	c := enginetest.StartWith(t, engine.Config{Env: []string{"PATH=" + bin + ":/usr/bin:/bin"}, Runners: []engine.Runner{codex.Runner{}},
		Launch: func(l engine.Launch) error { opened = append(opened, l); return nil }})
	folder, _ := filepath.EvalSymlinks(t.TempDir())
	return c, &opened, folder
}

// Codex starts on the prompt in a terminal; its hooks name the Session later.
func TestStartOpensCodexInATerminal(t *testing.T) {
	c, opened, folder := withCodex(t, true)
	s, err := c.StartSession(engine.StartSession{Harness: codex.HarnessName, Folder: folder, Prompt: "Fix the total"})
	if err != nil || s.ID != "" {
		t.Fatalf("started %+v %v", s, err)
	}
	if got := *opened; len(got) != 1 || got[0].Command != "cd "+folder+" && codex -- 'Fix the total'" {
		t.Fatalf("opened %+v", got)
	}
	// Where the human works makes no difference to Codex, so there is no choice to set.
	if err := c.SetSurface(codex.HarnessName, engine.SurfaceApp); err == nil {
		t.Error("Codex took a surface")
	}
}

func TestStartingFailsWhenNotSignedIn(t *testing.T) {
	c, opened, folder := withCodex(t, false)
	_, err := c.StartSession(engine.StartSession{Harness: codex.HarnessName, Folder: folder, Prompt: "hi"})
	var ce *client.Error
	if !asError(err, &ce) || ce.Status != 422 || !strings.Contains(ce.Message, "not signed in") || len(*opened) != 0 {
		t.Fatalf("got %v, opened %+v", err, *opened)
	}
}

func TestTextForAnEndedSessionResumesItInATerminal(t *testing.T) {
	c, opened, folder := withCodex(t, true)
	c.RegisterSession(engine.RegisterSession{Harness: codex.HarnessName, ID: "gone", Folder: folder, State: engine.SessionEnded})
	if _, err := c.FollowUp(codex.HarnessName, "gone", "Pick this up again"); err != nil {
		t.Fatal(err)
	}
	if got := *opened; len(got) != 1 || got[0].Command != "cd "+folder+" && codex resume gone -- 'Pick this up again'" {
		t.Fatalf("opened %+v", got)
	}
}

func asError(err error, target **client.Error) bool { return errors.As(err, target) }
