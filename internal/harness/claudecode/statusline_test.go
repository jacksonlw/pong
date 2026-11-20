package claudecode_test

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
)

const wait = 100 * time.Millisecond

func statusInput(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "statusline", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestStatusLineFiguresReachTheSession(t *testing.T) {
	c, dir := enginetest.Start(t)
	var out bytes.Buffer
	if code := claudecode.StatusLine(dir, t.TempDir(), bytes.NewReader(statusInput(t, "input.json")), &out, &out, time.Second); code != 0 || out.Len() != 0 {
		t.Fatalf("exit %d, printed %q with no status line of the human's", code, out.String())
	}
	s, err := c.Session(claudecode.HarnessName, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(s.Context)
	// 8500 input + 5000 cache-creation + 2000 cache-read tokens; 8 % used.
	if string(raw) != `{"used_fraction":0.08,"tokens":15500,"window":200000,"cost_usd":0.01234,"model":"claude-opus-5-5"}` {
		t.Fatalf("context %s", raw)
	}

	// Early in a Session the percentage and usage are null: they stay as they
	// were, and the figures stated replace theirs.
	claudecode.StatusLine(dir, t.TempDir(), bytes.NewReader(statusInput(t, "early.json")), &out, &out, time.Second)
	s, _ = c.Session(claudecode.HarnessName, "abc123")
	raw, _ = json.Marshal(s.Context)
	if string(raw) != `{"used_fraction":0.08,"tokens":15500,"window":200000,"cost_usd":0,"model":"claude-opus-5-5"}` {
		t.Fatalf("context %s", raw)
	}
}

func TestStatusLineInputWithoutFiguresRecordsNothing(t *testing.T) {
	c, dir := enginetest.Start(t)
	for _, in := range []string{"not json", `{"session_id":"s2","context":{"tokens":5}}`, `{"context_window":{"context_window_size":1}}`} {
		var out bytes.Buffer
		if code := claudecode.StatusLine(dir, t.TempDir(), strings.NewReader(in), &out, &out, wait); code != 0 || out.Len() != 0 {
			t.Fatalf("%s: exit %d, printed %q", in, code, out.String())
		}
	}
	if ss, _ := c.Sessions(); len(ss) != 0 {
		t.Fatalf("sessions %+v", ss)
	}
}

// humanScript is a status line of the human's: odd bytes, ANSI colours, no
// trailing newline, its input copied to a file, and a non-zero exit.
func humanScript(t *testing.T) (path, copied string) {
	t.Helper()
	d := t.TempDir()
	path, copied = filepath.Join(d, "my status.sh"), filepath.Join(d, "input")
	os.WriteFile(path, []byte("#!/bin/sh\ncat > '"+copied+"'\nprintf '\\033[32mmain\\033[0m \\377\\001 \\342\\226\\210\\n line two'\nexit 3\n"), 0o755)
	return path, copied
}

func settingsOf(t *testing.T, home string) map[string]json.RawMessage {
	t.Helper()
	m := map[string]json.RawMessage{}
	b, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json"))
	json.Unmarshal(b, &m)
	return m
}

func TestInstallWrapsTheHumansStatusLineAndRemoveRestoresIt(t *testing.T) {
	needClaude(t)
	script, copied := humanScript(t)
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	cmd, _ := json.Marshal("'" + script + "'")
	settings := "{\n  \"statusLine\": {\"type\": \"command\", \"command\": " + string(cmd) + ", \"padding\": 2},\n    \"model\": \"opus\"\n}\n"
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(settings), 0o600)
	before := tree(t, home)
	a := claudecode.Adapter{Engine: t.TempDir()}
	if err := a.Install(home, "/Applications/Pong Tools/pong"); err != nil {
		t.Fatal(err)
	}
	var sl map[string]any
	json.Unmarshal(settingsOf(t, home)["statusLine"], &sl)
	if sl["command"] != "'/Applications/Pong Tools/pong' statusline claude-code" || sl["padding"] != 2.0 || sl["type"] != "command" {
		t.Fatalf("statusLine after install: %v", sl)
	}
	once := tree(t, home)
	if err := a.Install(home, "/Applications/Pong Tools/pong"); err != nil {
		t.Fatal(err)
	}
	if tree(t, home) != once {
		t.Fatal("second install changed files")
	}

	// Pong's status line prints exactly what the human's prints, from the same input.
	c, dir := enginetest.Start(t)
	in := statusInput(t, "input.json")
	var out, errs bytes.Buffer
	code := claudecode.StatusLine(dir, home, bytes.NewReader(in), &out, &errs, time.Second)
	want := "\033[32mmain\033[0m \377\001 \342\226\210\n line two"
	if out.String() != want || code != 3 {
		t.Fatalf("printed %q exit %d, want %q exit 3 (stderr %q)", out.String(), code, want, errs.String())
	}
	if got, _ := os.ReadFile(copied); !bytes.Equal(got, in) {
		t.Fatalf("the human's status line got %q", got)
	}
	if s, err := c.Session(claudecode.HarnessName, "abc123"); err != nil || s.Context == nil || s.Context.Window == nil {
		t.Fatalf("figures did not reach the Session: %+v %v", s, err)
	}

	if err := a.Remove(home); err != nil {
		t.Fatal(err)
	}
	if after := tree(t, home); after != before {
		t.Fatalf("after remove:\n%s\nbefore:\n%s", after, before)
	}
}

// A human who uninstalls Pong's plugin with claude and runs setup again keeps
// their status line, and removal still restores their settings exactly.
func TestReinstallAfterPluginUninstalledKeepsTheHumansStatusLine(t *testing.T) {
	needClaude(t)
	script, _ := humanScript(t)
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	cmd, _ := json.Marshal("'" + script + "'")
	settings := "{\n  \"statusLine\": {\"type\": \"command\", \"command\": " + string(cmd) + "},\n    \"model\": \"opus\"\n}\n"
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(settings), 0o600)
	a := claudecode.Adapter{Engine: t.TempDir()}
	pong := "/Applications/Pong Tools/pong"
	if err := a.Install(home, pong); err != nil {
		t.Fatal(err)
	}
	claudeIn(t, home, "plugin", "uninstall", claudecode.Plugin, "--scope", "user")
	if err := a.Install(home, pong); err != nil {
		t.Fatal(err)
	}

	var out, errs bytes.Buffer
	claudecode.StatusLine(t.TempDir(), home, bytes.NewReader(statusInput(t, "input.json")), &out, &errs, 10*time.Millisecond)
	if !strings.Contains(out.String(), "main") {
		t.Fatalf("the human's status line did not print: %q (stderr %q)", out.String(), errs.String())
	}
	if err := a.Remove(home); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(home, ".claude", "settings.json")); string(got) != settings {
		t.Fatalf("settings.json after remove:\n%s\nwant\n%s", got, settings)
	}
}

func TestInstallWithNoStatusLineAddsPongsAndRemoveTakesItAway(t *testing.T) {
	needClaude(t)
	home := seededHome(t)
	before := tree(t, home)
	a := claudecode.Adapter{Engine: t.TempDir()}
	if err := a.Install(home, "/bin/pong"); err != nil {
		t.Fatal(err)
	}
	var sl map[string]any
	json.Unmarshal(settingsOf(t, home)["statusLine"], &sl)
	if sl["command"] != "/bin/pong statusline claude-code" || len(sl) != 2 {
		t.Fatalf("statusLine after install: %v", sl)
	}
	var out bytes.Buffer
	if code := claudecode.StatusLine(t.TempDir(), home, bytes.NewReader(statusInput(t, "input.json")), &out, &out, wait); code != 0 || out.Len() != 0 {
		t.Fatalf("exit %d, printed %q", code, out.String())
	}
	if err := a.Remove(home); err != nil {
		t.Fatal(err)
	}
	if after := tree(t, home); after != before {
		t.Fatalf("after remove:\n%s\nbefore:\n%s", after, before)
	}
}

// An Engine that accepts connections but never answers must not hold up the
// human's status line.
func TestStatusLineRendersPromptlyWhenTheEngineHangs(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()
	hung := t.TempDir()
	os.WriteFile(filepath.Join(hung, "engine.json"), []byte(`{"address":"`+l.Addr().String()+`","secret":"x"}`), 0o600)

	needClaude(t)
	script, _ := humanScript(t)
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	os.WriteFile(filepath.Join(home, ".claude", "settings.json"), []byte(`{"statusLine":{"type":"command","command":"'`+script+`'"}}`), 0o600)
	a := claudecode.Adapter{Engine: t.TempDir()}
	if err := a.Install(home, "/bin/pong"); err != nil {
		t.Fatal(err)
	}
	defer a.Remove(home)

	for _, dir := range []string{hung, t.TempDir()} {
		var out bytes.Buffer
		start := time.Now()
		claudecode.StatusLine(dir, home, bytes.NewReader(statusInput(t, "input.json")), &out, &out, wait)
		if d := time.Since(start); d > wait+300*time.Millisecond {
			t.Errorf("took %v", d)
		}
		if !strings.HasPrefix(out.String(), "\033[32mmain") {
			t.Errorf("printed %q", out.String())
		}
	}
}
