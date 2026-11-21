package engine_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

func wantStatus(t *testing.T, err error, status int, mentions string) {
	t.Helper()
	var e *client.Error
	if !errors.As(err, &e) || e.Status != status || !strings.Contains(e.Message, mentions) {
		t.Fatalf("err = %v, want %d mentioning %q", err, status, mentions)
	}
}

func TestEveryPartOfAPingIsReturnedWithRecommendedFirst(t *testing.T) {
	c, _ := enginetest.Start(t)
	p, err := c.CreatePing(client.NewPing{
		Session: "s1", Folder: "/tmp/p", Question: "Which tables should the migration touch?",
		Answers: []engine.Answer{
			{Label: "Users only", LeadsTo: "smaller change"},
			{Label: "Users and orders", LeadsTo: "one migration", HardToUndo: true},
		},
		Recommended: "Users and orders", AllowSeveral: true, Approves: engine.ApprovesPlan,
		Context: "The schema drifted.", Why: "Migration is next.", MoreComing: true, BlocksAgent: true,
		Resources: []engine.NewResource{{Title: "Migration plan", Kind: engine.KindDocument, Content: "Touch users and orders."}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.Ping(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Answers[0] != (engine.Answer{Label: "Users and orders", LeadsTo: "one migration", HardToUndo: true}) ||
		got.Answers[1].Label != "Users only" || !got.AllowSeveral || got.Approves != "plan" ||
		got.Context != "The schema drifted." || got.Why != "Migration is next." || !got.MoreComing || !got.BlocksAgent || got.IsNote {
		t.Fatalf("got %+v", got)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "recommended") {
		t.Fatalf("recommended is marked: %s", raw)
	}
}

// A Ping a Harness adapter captured from the Harness's own prompt is accepted
// as it comes: each rule on its answers is skipped, the same Ping sent without
// captured is refused, and only what the store cannot hold still is.
func TestCapturedPingIsAcceptedAsItComes(t *testing.T) {
	c, _ := enginetest.Start(t)
	ans := func(ls ...string) []engine.Answer {
		var out []engine.Answer
		for _, l := range ls {
			out = append(out, engine.Answer{Label: l})
		}
		return out
	}
	for _, tc := range []struct {
		name, mentions string
		p              client.NewPing
	}{
		{"five answers", "at most 4 answers", client.NewPing{Finished: true, Question: "Pick one?", Answers: ans("a", "b", "c", "d", "e")}},
		{"long label", "at most 12", client.NewPing{Finished: true, Question: "Pick one?", Answers: ans("one two three four five six seven eight nine ten eleven twelve thirteen")}},
		{"question repeats label", "word for word", client.NewPing{Finished: true, Question: "Postgres or SQLite?", Answers: ans("Postgres", "SQLite")}},
		{"repeated label", "appears twice", client.NewPing{Finished: true, Question: "Pick?", Answers: ans("a", "a")}},
		{"recommended matches nothing", "not one of the answers", client.NewPing{Finished: true, Question: "Pick?", Answers: ans("a", "b"), Recommended: "c"}},
		{"several of one answer", "at least 2 answers", client.NewPing{Finished: true, Question: "Pick?", Answers: ans("a"), AllowSeveral: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.p.Session = "s1"
			_, err := c.CreatePing(tc.p)
			wantStatus(t, err, http.StatusBadRequest, tc.mentions)
			tc.p.Captured = true
			if p, err := c.CreatePing(tc.p); err != nil || p.State != engine.StateWaiting {
				t.Fatalf("captured Ping = %+v, %v", p, err)
			}
		})
	}
	for _, tc := range []struct {
		name, mentions string
		p              client.NewPing
	}{
		{"no session", "session", client.NewPing{Finished: true, Question: "Pick?"}},
	} {
		tc.p.Captured = true
		_, err := c.CreatePing(tc.p)
		wantStatus(t, err, http.StatusBadRequest, tc.mentions)
	}

	// The human can pick a label the captured prompt offers twice.
	p, err := c.CreatePing(client.NewPing{Session: "s1", Question: "Pick?", Answers: ans("a", "a", "b"), Captured: true, BlocksAgent: true})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := c.Pong(p.ID, client.SendPong{Answer: "a"}); err != nil || got.Pong == nil || got.Pong.Answer != "a" {
		t.Fatalf("Pong on a repeated label = %+v, %v", got, err)
	}
}

func TestMalformedPingsAreRefusedNamingTheFault(t *testing.T) {
	c, _ := enginetest.Start(t)
	ans := func(ls ...string) []engine.Answer {
		var out []engine.Answer
		for _, l := range ls {
			out = append(out, engine.Answer{Label: l})
		}
		return out
	}
	cases := []struct {
		name     string
		p        client.NewPing
		mentions string
	}{
		{"five answers", client.NewPing{Finished: true, Question: "Pick one?", Answers: ans("a", "b", "c", "d", "e")}, "at most 4 answers"},
		{"long label", client.NewPing{Finished: true, Question: "Pick one?", Answers: ans("one two three four five six seven eight nine ten eleven twelve thirteen")}, "at most 12"},
		{"question repeats label", client.NewPing{Finished: true, Question: "Deploy now or wait?", Answers: ans("Deploy now", "Wait a day")}, `"Deploy now" word for word`},
		{"unknown recommended", client.NewPing{Finished: true, Question: "Pick one?", Answers: ans("a", "b"), Recommended: "c"}, "recommended"},
		{"bad approves", client.NewPing{Finished: true, Question: "Go?", Approves: "merge"}, "approves"},
		{"note with answers", client.NewPing{Question: "FYI", IsNote: true, Answers: ans("ok")}, "Note"},
		{"blocking note", client.NewPing{Question: "FYI", IsNote: true, BlocksAgent: true}, "never blocks"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.p.Session = "s1"
			_, err := c.CreatePing(tc.p)
			wantStatus(t, err, http.StatusBadRequest, tc.mentions)
		})
	}
}

func TestNoteNeverBlocksAndCanBeDismissed(t *testing.T) {
	c, _ := enginetest.Start(t)
	n, err := c.CreatePing(client.NewPing{Session: "s1", Question: "Tests are green on main.", IsNote: true})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if w, err := c.Wait(n.ID); err != nil || w.State != engine.StateWaiting || time.Since(start) > enginetest.WaitHold {
		t.Fatalf("wait on Note = %+v, %v after %v", w, err, time.Since(start))
	}
	_, err = c.Pong(n.ID, client.SendPong{Text: "thanks"})
	wantStatus(t, err, http.StatusConflict, "dismiss")
	d, err := c.Dismiss(n.ID)
	if err != nil || d.State != engine.StateDismissed {
		t.Fatalf("dismiss = %+v, %v", d, err)
	}
	p := newPing(t, c)
	_, err = c.Dismiss(p.ID)
	wantStatus(t, err, http.StatusConflict, "only a Note")
}

func TestHandBackAndWithdrawEndTheWait(t *testing.T) {
	for _, tc := range []struct {
		state string
		end   func(*client.Client, string) (engine.Ping, error)
	}{
		{engine.StateHandedBack, (*client.Client).HandBack},
		{engine.StateWithdrawn, (*client.Client).Withdraw},
		{engine.StateAnsweredInHarness, (*client.Client).AnsweredInHarness},
	} {
		t.Run(tc.state, func(t *testing.T) {
			c, _ := enginetest.Start(t)
			p := newPing(t, c)
			got := make(chan engine.Ping, 1)
			go func() { w, _ := c.Wait(p.ID); got <- w }()
			time.Sleep(enginetest.WaitHold / 2)
			if _, err := tc.end(c, p.ID); err != nil {
				t.Fatal(err)
			}
			select {
			case w := <-got:
				if w.State != tc.state || w.Pong != nil {
					t.Fatalf("wait returned %+v", w)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("wait did not end")
			}
			_, err := c.Pong(p.ID, client.SendPong{Text: "late"})
			wantStatus(t, err, http.StatusConflict, tc.state)
		})
	}
}

func TestWaitingPingsBlockingFirstThenOldest(t *testing.T) {
	c, _ := enginetest.Start(t)
	mk := func(q string, blocks bool) string {
		p, err := c.CreatePing(client.NewPing{Session: "s1", Question: q, BlocksAgent: blocks, Finished: !blocks})
		if err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	a, b, cc, d := mk("a?", false), mk("b?", true), mk("c?", false), mk("d?", true)
	ps, err := c.Pings(engine.StateWaiting)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	if strings.Join(ids, ",") != strings.Join([]string{b, d, a, cc}, ",") {
		t.Fatalf("order = %v", ids)
	}
}

func TestPongWithTickedAnswersAndText(t *testing.T) {
	c, _ := enginetest.Start(t)
	p, err := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Question: "Which checks should run?", AllowSeveral: true,
		Answers: []engine.Answer{{Label: "Lint"}, {Label: "Unit tests"}, {Label: "E2E"}}})
	if err != nil {
		t.Fatal(err)
	}
	// No Harness can start a new Session for this Ping, so that Pong is refused and the Ping still waits.
	_, err = c.Pong(p.ID, client.SendPong{Answers: []string{"Lint", "E2E"}, Text: "skip flaky ones", NewSession: true})
	wantStatus(t, err, http.StatusUnprocessableEntity, "cannot start Sessions")
	got, err := c.Pong(p.ID, client.SendPong{Answers: []string{"Lint", "E2E"}, Text: "skip flaky ones"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Pong.Answers, ",") != "Lint,E2E" || got.Pong.Text != "skip flaky ones" || got.Pong.NewSession {
		t.Fatalf("pong = %+v", got.Pong)
	}
	single := newPing(t, c)
	_, err = c.Pong(single.ID, client.SendPong{Answers: []string{"Staging first"}})
	wantStatus(t, err, http.StatusBadRequest, "does not allow several")
}

func next(t *testing.T, ch <-chan engine.Event) engine.Event {
	t.Helper()
	for {
		select {
		case e := <-ch:
			if strings.HasPrefix(e.Kind, "ping.") { // Project and Session events are not under test here
				return e
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no event")
			return engine.Event{}
		}
	}
}

func eventPing(t *testing.T, e engine.Event) engine.Ping {
	t.Helper()
	var p engine.Ping
	if err := json.Unmarshal(e.Data, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestEventStreamReportsChangesInOrderAndCatchesUp(t *testing.T) {
	c, _ := enginetest.Start(t)
	ch, stop, err := c.Events(-1)
	if err != nil {
		t.Fatal(err)
	}
	p := newPing(t, c)
	n, _ := c.CreatePing(client.NewPing{Session: "s1", Question: "Build finished.", IsNote: true})
	c.Pong(p.ID, client.SendPong{Answer: "Staging first"})
	c.Dismiss(n.ID)
	want := []struct {
		kind, id, state string
	}{
		{engine.EventPingCreated, p.ID, engine.StateWaiting},
		{engine.EventPingCreated, n.ID, engine.StateWaiting},
		{engine.EventPingPonged, p.ID, engine.StateAnswered},
		{engine.EventPingState, n.ID, engine.StateDismissed},
	}
	var seqs []int64
	for i, w := range want {
		e := next(t, ch)
		ep := eventPing(t, e)
		if e.Kind != w.kind || ep.ID != w.id || ep.State != w.state || (i > 0 && e.Seq <= seqs[i-1]) {
			t.Fatalf("event %d = %+v (%+v), want %+v", i, e, ep, w)
		}
		seqs = append(seqs, e.Seq)
	}
	stop()

	// A client reconnecting after the second event gets the rest, then new ones.
	ch, stop, err = c.Events(seqs[1])
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for i := 2; i < 4; i++ {
		if e := next(t, ch); e.Seq != seqs[i] {
			t.Fatalf("caught up %d, want seq %d", e.Seq, seqs[i])
		}
	}
	q := newPing(t, c)
	if e := next(t, ch); eventPing(t, e).ID != q.ID {
		t.Fatalf("live event after catch-up = %+v", e)
	}
}

func TestEventStreamHonoursLastEventID(t *testing.T) {
	c, _ := enginetest.Start(t)
	a, b := newPing(t, c), newPing(t, c)
	all, stop, err := c.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	aSeq := next(t, all).Seq // a's ping.created
	stop()
	req, _ := c.Request("GET", "/events")
	req.Header.Set("Last-Event-ID", strconv.FormatInt(aSeq, 10))
	hc := *c.HTTP
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	var got string
	for deadline := time.Now().Add(2 * time.Second); !strings.Contains(got, "\n\n") && time.Now().Before(deadline); {
		n, _ := resp.Body.Read(buf)
		got += string(buf[:n])
	}
	if strings.Contains(got, a.ID) || !strings.Contains(got, b.ID) || !strings.HasPrefix(got, fmt.Sprintf("id: %d\nevent: ping.created\ndata: ", aSeq+1)) {
		t.Fatalf("stream = %q", got)
	}
}
