package claudecode_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
)

// bin holds the fake claude, built once for the package: it answers
// `claude auth status` and fails `claude plugin list`.
var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "pong-fakeclaude")
	if err != nil {
		panic(err)
	}
	if out, err := exec.Command("go", "build", "-o", filepath.Join(dir, "claude"), "./testdata/fakeclaude").CombinedOutput(); err != nil {
		panic(string(out))
	}
	bin = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// opened records what the Engine opened in place of a terminal or the app.
type opened struct {
	mu sync.Mutex
	l  []engine.Launch
}

func (o *opened) open(l engine.Launch) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.l = append(o.l, l)
	return nil
}

func (o *opened) all() []engine.Launch {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]engine.Launch{}, o.l...)
}

// withClaude starts an Engine whose captured PATH has the fake claude, with
// the human working on surface; extra environment entries are added as given.
func withClaude(t *testing.T, surface string, extra ...string) (*client.Client, *opened, string) {
	t.Helper()
	testdata, _ := filepath.Abs("testdata")
	folder, _ := filepath.EvalSymlinks(t.TempDir())
	env := append([]string{"PATH=" + bin + ":/usr/bin:/bin", "HOME=" + t.TempDir(), "FAKE_CLAUDE_FIXTURES=" + testdata}, extra...)
	o := &opened{}
	c := enginetest.StartWith(t, engine.Config{Env: env, Runners: []engine.Runner{claudecode.Runner{}}, Launch: o.open})
	if err := c.SetSurface(claudecode.HarnessName, surface); err != nil {
		t.Fatal(err)
	}
	return c, o, folder
}

func status(err error) (int, string) {
	var ce *client.Error
	if errors.As(err, &ce) {
		return ce.Status, ce.Message
	}
	return 0, ""
}

// In a terminal Claude Code starts on the prompt at once, under an id Pong
// chose, so the Session is in Pong before its first hook runs.
func TestStartOpensATerminalOnThePrompt(t *testing.T) {
	c, o, folder := withClaude(t, engine.SurfaceTerminal)
	s, err := c.StartSession(engine.StartSession{Harness: claudecode.HarnessName, Folder: folder, Prompt: "Fix the checkout total\nand don't round"})
	if err != nil {
		t.Fatal(err)
	}
	if s.ID == "" || s.StartedBy != engine.StartedByHuman || s.State != engine.SessionWorking || s.Title != "Fix the checkout total" || s.Project == "" {
		t.Fatalf("started Session: %+v", s)
	}
	if want := "cd " + folder + " && claude --resume " + s.ID; s.ResumeCommand != want {
		t.Errorf("resume command %q, want %q", s.ResumeCommand, want)
	}
	got := o.all()
	want := "cd " + folder + " && claude --session-id " + s.ID + ` -- 'Fix the checkout total` + "\n" + `and don'\''t round'`
	if len(got) != 1 || got[0].Command != want {
		t.Fatalf("opened %+v\nwant command %s", got, want)
	}
}

// The Claude desktop app cannot be handed a prompt that it sends, so for a
// human who works in it the Session starts with no terminal window, under a
// wrapper that moves it into the app once its first turn has ended.
func TestStartForSomeoneWhoWorksInTheAppMovesTheSessionThere(t *testing.T) {
	c, o, folder := withClaude(t, engine.SurfaceApp)
	s, err := c.StartSession(engine.StartSession{Harness: claudecode.HarnessName, Folder: folder, Prompt: "Fix it, don't break it"})
	if err != nil || s.ID == "" {
		t.Fatalf("started %+v %v", s, err)
	}
	got := o.all()
	want := "cd " + folder + ` && PONG_MOVE_MARK="$(mktemp -t pong-move)" PONG_MOVE_SESSION=` + s.ID + ` PONG_MOVE_PROMPT='Fix it, don'\''t break it' expect -c '` + claudecode.MoveNew + `'`
	if len(got) != 1 || got[0].Command != want || !got[0].Hidden {
		t.Fatalf("opened %+v\nwant hidden command %s", got, want)
	}
	for _, script := range []string{claudecode.MoveNew, claudecode.MoveResumed} {
		if strings.Contains(script, "'") || strings.Contains(script, "interact") || !strings.Contains(script, `send "/desktop\r"`) ||
			!strings.Contains(script, `send "/rename $title\r"`) {
			t.Errorf("wrapper script: %s", script)
		}
	}
}

func TestStartingFailsWhenClaudeIsMissing(t *testing.T) {
	o := &opened{}
	c := enginetest.StartWith(t, engine.Config{Env: []string{"PATH=/usr/bin:/bin"}, Runners: []engine.Runner{claudecode.Runner{}}, Launch: o.open})
	_, err := c.StartSession(engine.StartSession{Harness: claudecode.HarnessName, Folder: t.TempDir(), Prompt: "hi"})
	if code, msg := status(err); code != 422 || !strings.Contains(msg, "not installed") || len(o.all()) != 0 {
		t.Fatalf("got %v, opened %+v", err, o.all())
	}
}

func TestStartingFailsWhenNotSignedIn(t *testing.T) {
	c, o, folder := withClaude(t, engine.SurfaceTerminal, "FAKE_CLAUDE_AUTH=signed-out")
	_, err := c.StartSession(engine.StartSession{Harness: claudecode.HarnessName, Folder: folder, Prompt: "hi"})
	if code, msg := status(err); code != 422 || !strings.Contains(msg, "not signed in") || len(o.all()) != 0 {
		t.Fatalf("got %v, opened %+v", err, o.all())
	}
}

// A Pong sent as a new Session opens one on the Pong and the Ping it answers.
func TestPongAsNewSessionCarriesThePing(t *testing.T) {
	c, o, folder := withClaude(t, engine.SurfaceTerminal)
	old, _ := c.RegisterSession(engine.RegisterSession{Harness: claudecode.HarnessName, ID: "old-session", Folder: folder})
	p, err := c.CreatePing(engine.NewPing{Finished: true, Session: old.ID, Harness: claudecode.HarnessName, Folder: folder,
		Question: "Migration written. What next?", Context: "Only the users table changed.",
		Answers:   []engine.Answer{{Label: "Run it", LeadsTo: "applies to the dev database"}, {Label: "Nothing more"}},
		Resources: []engine.NewResource{{Title: "Failing test output", Kind: "output", Content: "FAIL TestTotal"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := pong(t, c, p.ID, engine.SendPong{Answer: "Run it", Text: "then seed it", NewSession: true}); got.Pong.Delivery != engine.DeliveryDelivered {
		t.Fatalf("the Pong: %+v", got.Pong)
	}
	var ns engine.Session
	all, _ := c.Sessions()
	for _, s := range all {
		if s.ContinuedFrom != nil {
			ns = s
		}
	}
	if ns.Title != "Run it. then seed it" || ns.Project != old.Project || *ns.ContinuedFrom != (engine.SessionRef{Harness: claudecode.HarnessName, ID: old.ID}) {
		t.Errorf("new Session: %+v", ns)
	}
	got := o.all()
	if len(got) != 1 {
		t.Fatalf("opened %+v", got)
	}
	for _, want := range []string{"Run it. then seed it", "Migration written. What next?", "Only the users table changed.", "FAIL TestTotal"} {
		if !strings.Contains(got[0].Command, want) {
			t.Errorf("the first prompt lacks %q:\n%s", want, got[0].Command)
		}
	}
}

// Text for an ended Session opens it again in a terminal on that text.
func TestTextForAnEndedSessionOpensItInATerminal(t *testing.T) {
	c, o, folder := withClaude(t, engine.SurfaceTerminal)
	c.RegisterSession(engine.RegisterSession{Harness: claudecode.HarnessName, ID: "gone", Folder: folder, State: engine.SessionEnded})
	if _, err := c.FollowUp(claudecode.HarnessName, "gone", "Pick this up again"); err != nil {
		t.Fatal(err)
	}
	c.SetSurface(claudecode.HarnessName, engine.SurfaceApp)
	c.FollowUp(claudecode.HarnessName, "gone", "And once more")
	got := o.all()
	if len(got) != 2 || got[0].Command != "cd "+folder+" && claude --resume gone -- 'Pick this up again'" ||
		got[1].Command != "cd "+folder+` && PONG_MOVE_MARK="$(mktemp -t pong-move)" PONG_MOVE_SESSION=gone PONG_MOVE_PROMPT='And once more' expect -c '`+claudecode.MoveResumed+`'` ||
		got[0].Hidden || !got[1].Hidden {
		t.Fatalf("opened %+v", got)
	}
	th, _ := c.Thread(claudecode.HarnessName, "gone")
	if len(th.Pings) != 2 || th.Pings[0].Pong.Delivery != engine.DeliveryDelivered || th.Pings[1].Pong.Delivery != engine.DeliveryDelivered {
		t.Fatalf("Thread %+v", th.Pings)
	}
}
