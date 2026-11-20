package codex_test

// These tests run the hook on payloads written from Codex's documentation
// (testdata/documented/), not recordings: they exercise the code, but its
// behaviour against the real Codex is unverified.

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/attended"
	"github.com/jacksonlw/pong/internal/harness/codex"
	"github.com/jacksonlw/pong/internal/harness/turn"
)

const session = "thr_doc1"

// call runs the hook on a fixture in the background; the returned function
// waits for what it wrote.
func call(t *testing.T, dir, fixture string) func() string {
	t.Helper()
	in, err := os.ReadFile(filepath.Join("testdata", "documented", fixture))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		var out bytes.Buffer
		codex.Hook(dir, bytes.NewReader(in), &out)
		done <- out.String()
	}()
	return func() string {
		select {
		case s := <-done:
			return s
		case <-time.After(5 * time.Second):
			t.Fatal("the hook did not return")
			return ""
		}
	}
}

func waiting(t *testing.T, c *client.Client) engine.Ping {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if ps, err := c.Pings(engine.StateWaiting); err == nil && len(ps) > 0 {
			return ps[0]
		} else if err != nil {
			t.Fatal(err)
		}
	}
	t.Fatal("no Ping arrived")
	return engine.Ping{}
}

func pong(t *testing.T, c *client.Client, id string, p engine.SendPong) {
	t.Helper()
	if _, err := c.Pong(id, p); err != nil {
		t.Fatal(err)
	}
}

// The Pings are sent as captured: "Allow mcp__host__echo?" names its own
// answer, which the strict rules for Pings an agent writes refuse.
func TestPermissionPromptsBecomeCapturedPingsThatApproveAnAction(t *testing.T) {
	for _, tc := range []struct {
		fixture, question, content string
		pong                       engine.SendPong
		want                       string
		tag                        engine.Tag
	}{
		{"permission-bash.json", "Write the date to stamp.txt?", "date > stamp.txt", engine.SendPong{Answer: "Allow"},
			`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`,
			engine.Tag{Text: "Approve command", Icon: "terminal"}},
		{"permission-apply-patch.json", "Apply this patch?", "*** Begin Patch\n*** Add File: notes.txt\n+hello\n*** End Patch\n",
			engine.SendPong{Answer: "Deny", Text: "use notes.md instead"},
			`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"use notes.md instead"}}}`,
			engine.Tag{Text: "Approve edit", Icon: "pencil"}},
		{"permission-mcp-tool.json", "Allow mcp__host__echo?", "{\n  \"text\": \"hi\"\n}", engine.SendPong{Text: "not now"},
			`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"deny","message":"not now"}}}`,
			engine.Tag{Text: "Approve tool", Icon: "wrench.and.screwdriver"}},
	} {
		t.Run(tc.fixture, func(t *testing.T) {
			c, dir := enginetest.Start(t)
			out := call(t, dir, tc.fixture)
			p := waiting(t, c)
			if p.Question != tc.question || p.Approves != engine.ApprovesAction || !p.BlocksAgent ||
				p.Harness != "codex" || p.Session != session || len(p.Answers) != 2 || len(p.Resources) != 1 ||
				len(p.Tags) != 1 || p.Tags[0] != tc.tag {
				t.Fatalf("Ping = %+v", p)
			}
			if b, _, err := c.Resource(p.ID, p.Resources[0].ID); err != nil || string(b) != tc.content {
				t.Fatalf("Resource = %q, %v", b, err)
			}
			pong(t, c, p.ID, tc.pong)
			if got := out(); got != tc.want+"\n" {
				t.Fatalf("Codex would receive %s", got)
			}
		})
	}
}

func TestHandedBackPermissionWritesNothing(t *testing.T) {
	c, dir := enginetest.Start(t)
	out := call(t, dir, "permission-bash.json")
	p := waiting(t, c)
	if _, err := c.HandBack(p.ID); err != nil {
		t.Fatal(err)
	}
	if got := out(); got != "" {
		t.Fatalf("hand back wrote %q; want nothing, so Codex shows its own prompt", got)
	}
}

func TestTurnWithNoPingIsContinuedOnceWithTheNudge(t *testing.T) {
	c, dir := enginetest.Start(t)
	call(t, dir, "prompt-typed.json")()
	if got := out(t, dir, "stop-first.json"); got != `{"decision":"block","reason":`+quote(turn.Nudge)+"}\n" {
		t.Fatalf("Codex would receive %s", got)
	}
	if ps, _ := c.Pings(""); len(ps) != 0 {
		t.Fatalf("the nudge made Pings: %+v", ps)
	}
}

func TestPongOnTheFinishedPingContinuesTheSession(t *testing.T) {
	c, dir := enginetest.Start(t)
	wait := call(t, dir, "stop-continued.json")
	p := waiting(t, c)
	if p.Question != "Done." || p.BlocksAgent || len(p.Answers) != 1 || p.Answers[0].Label != engine.NothingMore {
		t.Fatalf("plain Ping = %+v", p)
	}
	if s, _ := c.Session("codex", session); s.State != engine.SessionIdle {
		t.Fatalf("Session is %s, want idle", s.State)
	}
	pong(t, c, p.ID, engine.SendPong{Text: "now add a changelog line"})
	if got := wait(); got != `{"decision":"block","reason":`+quote("The human answered your finished Ping in Pong: now add a changelog line\nOpen your reply with one line that quotes this Pong, starting \"You answered in Pong:\".")+"}\n" {
		t.Fatalf("Codex would receive %s", got)
	}
}

func TestRoundEndsTheTurnAndContinuesWithEveryPong(t *testing.T) {
	c, dir := enginetest.Start(t)
	call(t, dir, "prompt-typed.json")()
	var ids []string
	for i, q := range []string{"Cut the first line?", "Shrink the nudge?"} {
		p, err := c.CreatePing(engine.NewPing{Session: session, Folder: "/tmp", Question: q, BlocksAgent: true,
			MoreComing: i == 0, Answers: []engine.Answer{{Label: "Yes"}, {Label: "No"}}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	wait := call(t, dir, "stop-first.json")
	time.Sleep(20 * time.Millisecond)
	if ps, _ := c.Pings(""); len(ps) != 2 {
		t.Fatalf("Pings = %+v; want only the round", ps)
	}
	pong(t, c, ids[0], engine.SendPong{Answer: "Yes"})
	pong(t, c, ids[1], engine.SendPong{Answer: "No"})
	want := `{"decision":"block","reason":` + quote("The human answered your Pings in Pong:\n- Cut the first line?: Yes\n- Shrink the nudge?: No\nOpen your reply by quoting each Pong on a line of its own, starting \"You answered in Pong:\".") + "}\n"
	if got := wait(); got != want {
		t.Fatalf("Codex would receive %s", got)
	}
}

func TestNothingMoreEndsTheTurn(t *testing.T) {
	c, dir := enginetest.Start(t)
	wait := call(t, dir, "stop-continued.json")
	pong(t, c, waiting(t, c).ID, engine.SendPong{Answer: engine.NothingMore})
	if got := wait(); got != "" {
		t.Fatalf("Nothing more wrote %q", got)
	}
}

func TestTypedPromptAnswersTheFinishedPingInTheHarness(t *testing.T) {
	c, dir := enginetest.Start(t)
	wait := call(t, dir, "stop-continued.json")
	p := waiting(t, c)
	call(t, dir, "prompt-typed.json")()
	if got := wait(); got != "" {
		t.Fatalf("stop hook wrote %q", got)
	}
	if got, _ := c.Ping(p.ID); got.State != engine.StateAnsweredInHarness {
		t.Fatalf("Ping is %s", got.State)
	}
}

func TestModelResumeCommandAndProcessAreRecordedOnTheSession(t *testing.T) {
	c, dir := enginetest.Start(t)
	was := attended.HarnessPID
	attended.HarnessPID = func() int { return 4242 }
	t.Cleanup(func() { attended.HarnessPID = was })
	call(t, dir, "prompt-typed.json")()
	s, err := c.Session("codex", session)
	if err != nil || s.Context == nil || s.Context.Model != "gpt-5.5-codex" || s.Folder == "" ||
		s.ResumeCommand != "cd "+s.Folder+" && codex resume "+session || s.PID != 4242 {
		t.Fatalf("Session = %+v, %v", s, err)
	}
}

// With no Engine listening every hook writes nothing at once, which Codex
// documents as success with no decision.
func TestFailOpenWithNoEngine(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	stale := t.TempDir()
	os.WriteFile(filepath.Join(stale, "engine.json"), []byte(`{"address":"`+addr+`","secret":"x"}`), 0o600)
	fs, _ := filepath.Glob("testdata/documented/*.json")
	for _, dir := range []string{t.TempDir(), stale} {
		for _, f := range fs {
			start := time.Now()
			if got := out(t, dir, filepath.Base(f)); got != "" {
				t.Errorf("%s: wrote %q with no Engine", f, got)
			}
			if d := time.Since(start); d > 200*time.Millisecond {
				t.Errorf("%s: took %v with no Engine", f, d)
			}
		}
	}
}

func out(t *testing.T, dir, fixture string) string { t.Helper(); return call(t, dir, fixture)() }

func quote(s string) string {
	return `"` + strings.NewReplacer(`"`, `\"`, "\n", `\n`).Replace(s) + `"`
}
