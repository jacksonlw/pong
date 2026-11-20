package claudecode_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
)

// askWithTranscript sends the recorded single-question payload with its
// transcript_path pointed at transcript, and returns the Session as it is
// when the Ping arrives.
func askWithTranscript(t *testing.T, transcript string) engine.Session {
	t.Helper()
	c, dir := enginetest.Start(t)
	raw, err := os.ReadFile(filepath.Join("testdata", "question", "single.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ev map[string]any
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	ev["transcript_path"] = transcript
	in, _ := json.Marshal(ev)
	done := make(chan error, 1)
	go func() { done <- claudecode.Hook(dir, bytes.NewReader(in), &bytes.Buffer{}) }()
	p := waiting(t, c, 1)[0]
	s, err := c.Session(p.Harness, p.Session)
	if err != nil {
		t.Fatal(err)
	}
	pong(t, c, p.ID, engine.SendPong{Answer: "Blue"})
	if err := <-done; err != nil {
		t.Fatalf("hook: %v", err)
	}
	return s
}

func TestPingCarriesFiguresFromTheTranscript(t *testing.T) {
	abs, _ := filepath.Abs(filepath.Join("testdata", "context", "transcript.jsonl"))
	got := askWithTranscript(t, abs).Context
	// The last assistant entry: 2 input + 132 cache-creation + 40844 cache-read tokens.
	if got == nil || got.Tokens == nil || *got.Tokens != 40978 || got.Model != "claude-haiku-5-5" {
		t.Fatalf("context %+v", got)
	}
	// Claude Code does not state the window, the fraction or the cost at this point.
	if got.Window != nil || got.UsedFraction != nil || got.CostUSD != nil {
		t.Fatalf("figures Claude Code did not state: %+v", got)
	}
}

// The Session is titled as Claude Code shows it: the last name given, and a
// name the human or the desktop app gave before one made up in a terminal.
func TestPingCarriesTheTitleFromTheTranscript(t *testing.T) {
	named := filepath.Join(t.TempDir(), "named.jsonl")
	os.WriteFile(named, []byte(`{"type":"custom-title","customTitle":"Fix the total","sessionId":"s"}
{"type":"ai-title","aiTitle":"Checkout rounding","sessionId":"s"}
{"type":"custom-title","customTitle":"Fix the checkout total","sessionId":"s"}
`), 0o600)
	if got := askWithTranscript(t, named).Title; got != "Fix the checkout total" {
		t.Fatalf("title %q", got)
	}
	terminal := filepath.Join(t.TempDir(), "terminal.jsonl")
	os.WriteFile(terminal, []byte(`{"type":"ai-title","aiTitle":"Checkout rounding","sessionId":"s"}`+"\n"), 0o600)
	if got := askWithTranscript(t, terminal).Title; got != "Checkout rounding" {
		t.Fatalf("title %q", got)
	}
}

// A Session in the Claude desktop app opens there by the app's own id for it;
// one in a terminal gets no such command here.
func TestSessionInTheDesktopAppOpensThere(t *testing.T) {
	t.Setenv("CLAUDE_CODE_HOST_SESSION_ID", "local_33296244-0685-465b-81e6-b2d4a34daf1f")
	s := askWithTranscript(t, "")
	if got, want := s.AppCommand, "open 'claude://code/continue?session=local_33296244-0685-465b-81e6-b2d4a34daf1f'"; got != want {
		t.Fatalf("app command %q, want %q", got, want)
	}
	if s.AppName != claudecode.DesktopAppName {
		t.Fatalf("app app name %q", s.AppName)
	}
	for _, env := range [][2]string{{"CLAUDE_CODE_HOST_SESSION_ID", ""}, {"CLAUDE_CODE_HOST_SESSION_ID", "x'; rm -rf ~"}} {
		t.Run(env[0]+"="+env[1], func(t *testing.T) {
			t.Setenv(env[0], env[1])
			if s := askWithTranscript(t, ""); s.AppCommand != "" || s.AppName != "" {
				t.Fatalf("app command %q, app name %q", s.AppCommand, s.AppName)
			}
		})
	}
}

func TestUnrecognisedTranscriptYieldsNoFigures(t *testing.T) {
	odd := filepath.Join(t.TempDir(), "odd.jsonl")
	os.WriteFile(odd, []byte("not json\n{\"type\":\"assistant\",\"message\":{\"tokens\":5}}\n"), 0o600)
	for _, path := range []string{odd, filepath.Join(t.TempDir(), "missing.jsonl"), ""} {
		if s := askWithTranscript(t, path); s.Context != nil {
			t.Fatalf("%q: context %+v", path, s.Context)
		}
	}
}
