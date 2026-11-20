package engine_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

func isStatus(err error, status int) bool {
	var e *client.Error
	return errors.As(err, &e) && e.Status == status && e.Message != ""
}

func jsonInto(b []byte, v any) bool { return json.Unmarshal(b, v) == nil }

func TestSessionRecordsWhatItsHarnessTells(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := repo(t, "shop")
	s, err := c.RegisterSession(engine.RegisterSession{Harness: "codex", ID: "abc", Folder: folder,
		FirstPrompt: "  Fix the checkout total\nIt rounds wrong.", StartedBy: engine.StartedByEngine})
	if err != nil {
		t.Fatal(err)
	}
	if s.Harness != "codex" || s.ID != "abc" || s.Folder != folder || s.Title != "Fix the checkout total" ||
		s.StartedBy != engine.StartedByEngine || s.State != engine.SessionWorking || s.Project == "" {
		t.Fatalf("session %+v", s)
	}
	for _, st := range []string{engine.SessionWaiting, engine.SessionIdle, engine.SessionEnded} {
		if s, err = c.RegisterSession(engine.RegisterSession{Harness: "codex", ID: "abc", State: st}); err != nil || s.State != st {
			t.Fatalf("state %s: %+v %v", st, s, err)
		}
	}
	// The Harness's own name wins over the first prompt.
	s, _ = c.RegisterSession(engine.RegisterSession{Harness: "codex", ID: "abc", Name: "Checkout rounding"})
	if s.Title != "Checkout rounding" || s.Folder != folder {
		t.Fatalf("after naming: %+v", s)
	}
	// A later first prompt does not replace the title.
	s, _ = c.RegisterSession(engine.RegisterSession{Harness: "codex", ID: "abc", FirstPrompt: "something else"})
	if s.Title != "Checkout rounding" {
		t.Fatalf("title became %q", s.Title)
	}
	if got, err := c.Session("codex", "abc"); err != nil || got.Title != s.Title {
		t.Fatalf("read %+v %v", got, err)
	}
}

func TestRegisterSessionRefusesBadParts(t *testing.T) {
	c, _ := enginetest.Start(t)
	for _, r := range []engine.RegisterSession{
		{Harness: "codex"},
		{Harness: "codex", ID: "a", State: "busy"},
		{Harness: "codex", ID: "a", StartedBy: "robot"},
		{Harness: "codex", ID: "a", ContinuedFrom: &engine.SessionRef{Harness: "codex"}},
	} {
		if _, err := c.RegisterSession(r); !isStatus(err, 400) {
			t.Errorf("%+v: %v", r, err)
		}
	}
	if _, err := c.Session("codex", "nope"); !isStatus(err, 404) {
		t.Fatalf("unknown Session: %v", err)
	}
}

func TestPingFromUnknownSessionRegistersIt(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := repo(t, "api")
	p := pingFrom(t, c, "fresh", folder)
	s, err := c.Session("claude-code", "fresh")
	if err != nil {
		t.Fatal(err)
	}
	if s.Folder != folder || s.Project != p.Project || s.State != engine.SessionWorking || s.StartedBy != engine.StartedByHuman {
		t.Fatalf("session %+v", s)
	}
	// A later Ping without a folder still lands in the Session's Project.
	q, err := c.CreatePing(client.NewPing{Finished: true, Session: "fresh", Harness: "claude-code", Question: "And now?"})
	if err != nil || q.Project != p.Project {
		t.Fatalf("folderless Ping in %q, %v", q.Project, err)
	}
}

func TestTheLatestSummaryOnAPingOrNoteIsTheSessions(t *testing.T) {
	c, _ := enginetest.Start(t)
	pingFrom(t, c, "s", repo(t, "api"))
	if s, _ := c.Session("claude-code", "s"); s.Summary != "" {
		t.Fatalf("a Ping without a summary gave the Session %q", s.Summary)
	}
	if _, err := c.CreatePing(client.NewPing{BlocksAgent: true, Session: "s", Harness: "claude-code", Question: "Which?",
		Summary: "  Moving the login form to the new token API; tests next.  "}); err != nil {
		t.Fatal(err)
	}
	if s, _ := c.Session("claude-code", "s"); s.Summary != "Moving the login form to the new token API; tests next." {
		t.Fatalf("summary %q", s.Summary)
	}
	// A Note's summary replaces it; a later Ping with none leaves it.
	c.CreatePing(client.NewPing{IsNote: true, Session: "s", Harness: "claude-code", Question: "Tests pass.", Summary: "Login form moved; writing the PR."})
	c.CreatePing(client.NewPing{Finished: true, Session: "s", Harness: "claude-code", Question: "Done"})
	if s, _ := c.Session("claude-code", "s"); s.Summary != "Login form moved; writing the PR." {
		t.Fatalf("summary %q", s.Summary)
	}
}

func TestThreadReturnsSessionsPingsAndPongsInOrder(t *testing.T) {
	c, _ := enginetest.Start(t)
	folder := t.TempDir()
	first := pingFrom(t, c, "s1", folder)
	pingFrom(t, c, "other", folder)
	if _, err := c.Pong(first.ID, client.SendPong{Text: "yes"}); err != nil {
		t.Fatal(err)
	}
	note, err := c.CreatePing(client.NewPing{Session: "s1", Harness: "claude-code", Question: "Done.", IsNote: true})
	if err != nil {
		t.Fatal(err)
	}
	// Same Harness id in another Harness is another Session.
	if _, err := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Harness: "codex", Question: "Elsewhere?"}); err != nil {
		t.Fatal(err)
	}
	th, err := c.Thread("claude-code", "s1")
	if err != nil {
		t.Fatal(err)
	}
	if th.Session.ID != "s1" || len(th.Pings) != 2 || th.Pings[0].ID != first.ID || th.Pings[1].ID != note.ID {
		t.Fatalf("thread %+v", th)
	}
	if th.Pings[0].Pong == nil || th.Pings[0].Pong.Text != "yes" {
		t.Fatalf("first Ping lacks its Pong: %+v", th.Pings[0])
	}
	if _, err := c.Thread("claude-code", "nope"); !isStatus(err, 404) {
		t.Fatalf("unknown Thread: %v", err)
	}
}

func TestSessionRecordsWhatItContinues(t *testing.T) {
	c, _ := enginetest.Start(t)
	pingFrom(t, c, "old", t.TempDir())
	s, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "new", StartedBy: engine.StartedByEngine,
		ContinuedFrom: &engine.SessionRef{Harness: "claude-code", ID: "old"}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := c.Session("claude-code", "new")
	if s.ContinuedFrom == nil || got.ContinuedFrom == nil || *got.ContinuedFrom != (engine.SessionRef{Harness: "claude-code", ID: "old"}) {
		t.Fatalf("session %+v", got)
	}
}

func TestSessionChangesAreEvents(t *testing.T) {
	c, _ := enginetest.Start(t)
	ev, stop, err := c.Events(-1)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	pingFrom(t, c, "s1", t.TempDir())
	var kinds []string
	for len(kinds) < 4 {
		kinds = append(kinds, (<-ev).Kind)
	}
	// The second project.changed records the Session's Harness as the last used.
	want := []string{engine.EventProjectChanged, engine.EventSessionChanged, engine.EventProjectChanged, engine.EventPingCreated}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("events %v, want %v", kinds, want)
		}
	}
}

func TestSessionCarriesItsHarnesssContextFigures(t *testing.T) {
	c, _ := enginetest.Start(t)
	pingFrom(t, c, "s1", t.TempDir())
	tokens := int64(41000)
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1",
		Context: &engine.Context{Tokens: &tokens, Model: "claude-haiku-5-5"}}); err != nil {
		t.Fatal(err)
	}
	// A change without figures keeps them.
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", State: engine.SessionIdle}); err != nil {
		t.Fatal(err)
	}
	th, err := c.Thread("claude-code", "s1")
	if err != nil {
		t.Fatal(err)
	}
	got := th.Session.Context
	if got == nil || got.Tokens == nil || *got.Tokens != tokens || got.Model != "claude-haiku-5-5" {
		t.Fatalf("context %+v", got)
	}
	// Parts the Harness did not state are absent, not zero.
	raw, err := json.Marshal(got)
	if err != nil || string(raw) != `{"tokens":41000,"model":"claude-haiku-5-5"}` {
		t.Fatalf("context JSON %s %v", raw, err)
	}
	bad := 1.5
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1",
		Context: &engine.Context{UsedFraction: &bad}}); !isStatus(err, 400) {
		t.Fatalf("fraction above 1: %v", err)
	}
	// A Session without figures has no context at all.
	other, _ := c.RegisterSession(engine.RegisterSession{Harness: "codex", ID: "x"})
	if other.Context != nil {
		t.Fatalf("figures on a Session with none: %+v", other.Context)
	}
}

func TestContextFiguresMergePartByPart(t *testing.T) {
	c, _ := enginetest.Start(t)
	pingFrom(t, c, "s1", t.TempDir())
	window, frac, tokens := int64(200000), 0.08, int64(16000)
	reg := func(ctx engine.Context) {
		t.Helper()
		if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: "s1", Context: &ctx}); err != nil {
			t.Fatal(err)
		}
	}
	reg(engine.Context{Window: &window, UsedFraction: &frac, Model: "claude-opus-5-5"})
	reg(engine.Context{Tokens: &tokens, Model: "claude-haiku-5-5"})
	s, err := c.Session("claude-code", "s1")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(s.Context)
	if string(raw) != `{"used_fraction":0.08,"tokens":16000,"window":200000,"model":"claude-haiku-5-5"}` {
		t.Fatalf("context %s", raw)
	}
}
