package claudecode_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
	"github.com/jacksonlw/pong/internal/harness/claudecode"
)

// Sessions in the recorded payloads.
const (
	recordedSession = "dc68d69d-3138-4b80-8497-63951f30e9aa" // stop/first.json, prompt/typed.json
	woken           = "a92d08b1-87b5-464a-a646-94ec7ea932d8" // stop/woken.json, prompt/woken.json
	reported        = "fd3ebd8d-997c-4d92-a73e-21d79046553d" // prompt/subagent-report.json
)

// typeIn plays the human typing a prompt in session's terminal: the recorded
// typed prompt, with its Session changed.
func typeIn(t *testing.T, dir, session string) {
	t.Helper()
	in, err := os.ReadFile(filepath.Join("testdata", "prompt/typed.json"))
	if err != nil {
		t.Fatal(err)
	}
	in = bytes.ReplaceAll(in, []byte(recordedSession), []byte(session))
	var out bytes.Buffer
	if err := claudecode.Hook(dir, bytes.NewReader(in), &out); err != nil || out.Len() != 0 {
		t.Fatalf("prompt hook: %v, wrote %q", err, out.String())
	}
}

// hook runs the hook on a recorded payload in the background and returns a
// function that waits for its result: what it wrote, and the text it would
// wake the Session with ("" when it exits 0).
func stopHook(t *testing.T, dir, fixture string) func() (out, wake string) {
	t.Helper()
	in, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	return stopHookOn(t, dir, in)
}

// stopHookOn is stopHook on the payload in.
func stopHookOn(t *testing.T, dir string, in []byte) func() (out, wake string) {
	t.Helper()
	type result struct{ out, wake string }
	done := make(chan result, 1)
	go func() {
		var out bytes.Buffer
		err := claudecode.Hook(dir, bytes.NewReader(in), &out)
		var w claudecode.Wake
		errors.As(err, &w)
		done <- result{out.String(), w.Text}
	}()
	return func() (string, string) {
		select {
		case r := <-done:
			return r.out, r.wake
		case <-time.After(5 * time.Second):
			t.Fatal("the hook did not return")
			return "", ""
		}
	}
}

func run(t *testing.T, dir, fixture string) (out, wake string) {
	t.Helper()
	return stopHook(t, dir, fixture)()
}

// agentPing plays the agent sending a finished Ping through Pong's MCP tool,
// which names the Session but no Harness.
func agentPing(t *testing.T, c *client.Client, session string) engine.Ping {
	t.Helper()
	p, err := c.CreatePing(engine.NewPing{Finished: true, Session: session, Folder: "/tmp", Question: "Added the total; tests not run. What next?",
		Answers: []engine.Answer{{Label: "Run the tests"}, {Label: engine.NothingMore}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func state(t *testing.T, c *client.Client, session string) string {
	t.Helper()
	s, err := c.Session("claude-code", session)
	if err != nil {
		t.Fatal(err)
	}
	return s.State
}

// A turn with no Ping of the agent's own is never pushed for one: Pong builds
// it from the turn, here from an offer at the end of the closing message.
func TestTurnWithNoPingGetsThePingBuiltFromIt(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/typed.json")
	wait := stopHook(t, dir, "stop/first.json")
	p := waiting(t, c, 1)[0]
	if !p.BlocksAgent || p.Question != "If you want me to try steps 3 to 5 again, tell me and I'll run them." ||
		len(p.Answers) != 2 || p.Answers[0].Label != "Yes" || !strings.Contains(p.Context, "**Button colour:** You chose Red.") {
		t.Fatalf("built Ping = %+v", p)
	}
	if s := state(t, c, recordedSession); s != engine.SessionIdle {
		t.Fatalf("Session is %s, want idle", s)
	}
	typeIn(t, dir, recordedSession) // answered in the terminal instead
	if out, wake := wait(); out != "" || wake != "" {
		t.Fatalf("wrote %q and woke with %q", out, wake)
	}
}

func TestAClosingMessageThatAsksNothingGetsAFinishedPing(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/woken.json")
	wait := stopHook(t, dir, "stop/woken.json")
	p := waiting(t, c, 1)[0]
	if p.Question != "BANANA" || p.Session != woken || p.Harness != "claude-code" || p.BlocksAgent || !p.Finished {
		t.Fatalf("built Ping = %+v", p)
	}
	if s := state(t, c, woken); s != engine.SessionIdle {
		t.Fatalf("Session is %s, want idle", s)
	}
	// "Nothing more" clears it and wakes nothing.
	pong(t, c, p.ID, engine.SendPong{Answer: engine.NothingMore})
	if out, wake := wait(); out != "" || wake != "" {
		t.Fatalf("Nothing more wrote %q and woke with %q", out, wake)
	}
	if s := state(t, c, woken); s != engine.SessionIdle {
		t.Fatalf("Session is %s, want idle", s)
	}
}

// Claude Code shows a Session running while work it started in the
// background still runs, and lists that work in the Stop hook's
// background_tasks. Such a turn leaves the Session working, awaiting its own
// work, whatever the agent sent; the report of that work begins the next
// turn. A Note alone, with nothing in the background, leaves it idle.
func TestATurnEndingWithBackgroundWorkLeavesTheSessionWorking(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/woken.json")
	if _, err := c.CreatePing(engine.NewPing{IsNote: true, Session: woken, Folder: "/tmp",
		Question: "The research runs in the background; I report when it ends."}); err != nil {
		t.Fatal(err)
	}
	in, err := os.ReadFile(filepath.Join("testdata", "stop/woken.json"))
	if err != nil {
		t.Fatal(err)
	}
	in = bytes.Replace(in, []byte(`"background_tasks":[]`), []byte(`"background_tasks":[{"id":"ac3563b1b7329362c","type":"local_agent","status":"running","description":"Research macOS cursors","agent_type":"general-purpose"}]`), 1)
	wait := stopHookOn(t, dir, in)
	time.Sleep(50 * time.Millisecond)
	if s, _ := c.Session("claude-code", woken); s.State != engine.SessionWorking || !s.AwaitsOwnWork {
		t.Fatalf("Session is %s (awaits own work: %v); want working and awaiting", s.State, s.AwaitsOwnWork)
	}
	report(t, dir, woken) // the background work reports
	if s, _ := c.Session("claude-code", woken); s.State != engine.SessionWorking || s.AwaitsOwnWork {
		t.Fatalf("after the report the Session is %s (awaits own work: %v); want working, no longer awaiting", s.State, s.AwaitsOwnWork)
	}
	typeIn(t, dir, woken) // ends the hook's listening
	if out, wake := wait(); out != "" || wake != "" {
		t.Fatalf("wrote %q and woke with %q", out, wake)
	}
}

// A Thought is not a Ping: after one, the turn still gets its built Ping.
func TestATurnOfThoughtsStillGetsItsBuiltPing(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/woken.json")
	if _, err := c.CreatePing(engine.NewPing{IsNote: true, Thought: true, Session: woken, Folder: "/tmp",
		Question: "The total was off by one."}); err != nil {
		t.Fatal(err)
	}
	stopHook(t, dir, "stop/woken.json")
	if p := waiting(t, c, 1)[0]; p.Question != "BANANA" {
		t.Fatalf("built Ping = %+v", p)
	}
}

// A Ping that registers its Session (no SessionStart or prompt seen yet)
// counts as sent in the Session's turn: no nudge, no second plain Ping.
func TestPingThatRegistersTheSessionCountsInItsTurn(t *testing.T) {
	c, dir := enginetest.Start(t)
	mine, err := c.CreatePing(engine.NewPing{Finished: true, Session: recordedSession, Harness: "claude-code", Folder: "/tmp",
		Question: "Added the total; tests not run. What next?", Answers: []engine.Answer{{Label: "Run the tests"}}})
	if err != nil {
		t.Fatal(err)
	}
	wait := stopHook(t, dir, "stop/first.json")
	time.Sleep(20 * time.Millisecond)
	if ps, _ := c.Pings(""); len(ps) != 1 {
		t.Fatalf("Pings = %+v; want only the agent's", ps)
	}
	pong(t, c, mine.ID, engine.SendPong{Answer: engine.NothingMore})
	if out, wake := wait(); out != "" || wake != "" {
		t.Fatalf("wrote %q, woke with %q", out, wake)
	}
}

func TestAgentsOwnFinishedPingIsNotDoubledAndItsPongWakes(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/typed.json")
	mine := agentPing(t, c, recordedSession)
	wait := stopHook(t, dir, "stop/first.json")
	time.Sleep(20 * time.Millisecond)
	if ps, _ := c.Pings(""); len(ps) != 1 {
		t.Fatalf("Pings = %+v; want only the agent's", ps)
	}
	pong(t, c, mine.ID, engine.SendPong{Answer: "Run the tests", Text: "and fix any failure"})
	_, wake := wait()
	if !strings.Contains(wake+"\n", ": Run the tests. and fix any failure\n") {
		t.Fatalf("woke with %q", wake)
	}
}

func TestTypedPongWakesThePlainPingsSession(t *testing.T) {
	c, dir := enginetest.Start(t)
	wait := stopHook(t, dir, "stop/woken.json")
	p := waiting(t, c, 1)[0]
	pong(t, c, p.ID, engine.SendPong{Text: "now add a changelog line"})
	if _, wake := wait(); !strings.Contains(wake+"\n", ": now add a changelog line\n") {
		t.Fatalf("woke with %q", wake)
	}
}

// An earlier turn's Ping, its Pong delivered, does not count in this turn.
func TestPingFromAnEarlierTurnDoesNotCount(t *testing.T) {
	c, dir := enginetest.Start(t)
	p := agentPing(t, c, recordedSession)
	pong(t, c, p.ID, engine.SendPong{Answer: "Run the tests"})
	if _, err := c.Wait(p.ID); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "prompt/typed.json")
	stopHook(t, dir, "stop/first.json")
	if ps := waiting(t, c, 1); ps[0].ID == p.ID || !ps[0].BlocksAgent {
		t.Fatalf("waiting = %+v; want the Ping built from this turn", ps)
	}
}

func TestTypingInTheTerminalAnswersTheFinishedPingInTheHarness(t *testing.T) {
	c, dir := enginetest.Start(t)
	wait := stopHook(t, dir, "stop/woken.json")
	p := waiting(t, c, 1)[0]
	typeIn(t, dir, woken)
	if out, wake := wait(); out != "" || wake != "" {
		t.Fatalf("stop hook wrote %q, woke with %q", out, wake)
	}
	got, _ := c.Ping(p.ID)
	if got.State != engine.StateAnsweredInHarness {
		t.Fatalf("Ping is %s", got.State)
	}
	if s := state(t, c, woken); s != engine.SessionWorking {
		t.Fatalf("Session is %s, want working", s)
	}
}

// round plays the agent ending its turn with several questions, each sent
// as a blocking Ping through Pong's MCP tool, without waiting on them.
func round(t *testing.T, c *client.Client, session string, questions ...string) []engine.Ping {
	t.Helper()
	var ps []engine.Ping
	for i, q := range questions {
		p, err := c.CreatePing(engine.NewPing{Session: session, Folder: "/tmp", Question: q, BlocksAgent: true,
			MoreComing: i < len(questions)-1, Answers: []engine.Answer{{Label: "Yes"}, {Label: "No"}}})
		if err != nil {
			t.Fatal(err)
		}
		ps = append(ps, p)
	}
	return ps
}

func TestRoundEndsTheTurnAndWakesWithEveryPong(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/typed.json")
	qs := round(t, c, recordedSession, "Cut the first line?", "Shrink the nudge?")
	wait := stopHook(t, dir, "stop/first.json")
	time.Sleep(20 * time.Millisecond)
	if ps, _ := c.Pings(""); len(ps) != 2 {
		t.Fatalf("Pings = %+v; want only the round", ps)
	}
	for _, p := range qs {
		got, _ := c.Ping(p.ID)
		for _, a := range got.Answers {
			if a.Label == engine.NothingMore {
				t.Fatalf("a question got %q: %+v", engine.NothingMore, got)
			}
		}
	}
	if s := state(t, c, recordedSession); s != engine.SessionIdle {
		t.Fatalf("Session is %s, want idle", s)
	}
	pong(t, c, qs[1].ID, engine.SendPong{Answer: "No", Text: "keep it"})
	pong(t, c, qs[0].ID, engine.SendPong{Answer: "Yes"})
	_, wake := wait()
	want := "The human answered your Pings in Pong:\n- Cut the first line?: Yes\n- Shrink the nudge?: No. keep it"
	if wake != want {
		t.Fatalf("woke with %q; want %q", wake, want)
	}
}

// The human may answer a question in the seconds between the agent sending
// it and its turn ending. That Pong still wakes the Session, which never goes
// idle in between: it was never done, only asking.
func TestPongSentBeforeTheTurnEndsWakesTheSession(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/typed.json")
	q := round(t, c, recordedSession, "Which right curve?")[0]
	pong(t, c, q.ID, engine.SendPong{Answer: "Yes"})
	_, wake := run(t, dir, "stop/first.json")
	if !strings.Contains(wake+"\n", "\n- Which right curve?: Yes\n") {
		t.Fatalf("woke with %q; want the Pong", wake)
	}
	if got, _ := c.Ping(q.ID); got.Pong == nil || got.Pong.Delivery != engine.DeliveryDelivering {
		t.Fatalf("the Pong is %+v; want it handed to the wake (delivering)", got.Pong)
	}
	if s := state(t, c, recordedSession); s != engine.SessionWorking {
		t.Fatalf("Session is %s, want working", s)
	}
	events, stop, err := c.Events(0)
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	for {
		select {
		case ev := <-events:
			if ev.Kind == engine.EventSessionChanged && strings.Contains(string(ev.Data), `"state":"idle"`) {
				t.Fatal("the Session went idle while its Pong was on its way")
			}
		case <-time.After(200 * time.Millisecond):
			return
		}
	}
}

// The human may answer the finished Ping with "Nothing more" before the turn
// ends. Nothing wakes the Session then, so it is idle as the turn ends, not
// left running until the Engine sees its transcript fall quiet.
func TestNothingMoreSentBeforeTheTurnEndsLeavesTheSessionIdle(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/typed.json")
	mine := agentPing(t, c, recordedSession)
	pong(t, c, mine.ID, engine.SendPong{Answer: engine.NothingMore})
	wait := stopHook(t, dir, "stop/first.json")
	time.Sleep(50 * time.Millisecond)
	if s := state(t, c, recordedSession); s != engine.SessionIdle {
		t.Fatalf("Session is %s, want idle", s)
	}
	typeIn(t, dir, recordedSession) // ends the hook's listening
	if out, wake := wait(); out != "" || wake != "" {
		t.Fatalf("wrote %q and woke with %q", out, wake)
	}
}

// report plays a background subagent's report reaching session: the
// recorded framed prompt, with its Session changed.
func report(t *testing.T, dir, session string) {
	t.Helper()
	in, err := os.ReadFile(filepath.Join("testdata", "prompt/subagent-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	in = bytes.ReplaceAll(in, []byte(reported), []byte(session))
	var out bytes.Buffer
	if err := claudecode.Hook(dir, bytes.NewReader(in), &out); err != nil || out.Len() != 0 {
		t.Fatalf("prompt hook: %v, wrote %q", err, out.String())
	}
}

// Claude Code folds a background subagent's report into a running turn and
// sends UserPromptSubmit for it. The turn goes on: the questions it sent
// before the report are still its own, and a Pong to one that is on its way
// wakes the Session when the turn ends, with no nudge. Recorded on
// 2026-11-20.
func TestASubagentsReportMidTurnKeepsTheTurnsPongs(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/typed.json")
	qs := round(t, c, recordedSession, "Thread only?", "Who writes them?")
	pong(t, c, qs[0].ID, engine.SendPong{Answer: "Yes"})
	time.Sleep(5 * time.Millisecond)
	report(t, dir, recordedSession)
	wait := stopHook(t, dir, "stop/first.json")
	time.Sleep(20 * time.Millisecond)
	if got, _ := c.Ping(qs[0].ID); got.Pong == nil || got.Pong.Delivery != engine.DeliveryDelivering {
		t.Fatalf("the Pong is %+v; want it handed to the wake (delivering) as the turn ends", got.Pong)
	}
	pong(t, c, qs[1].ID, engine.SendPong{Answer: "No"})
	_, wake := wait()
	if !strings.Contains(wake+"\n", "\n- Thread only?: Yes\n") || !strings.Contains(wake+"\n", "\n- Who writes them?: No\n") {
		t.Fatalf("woke with %q; want both Pongs", wake)
	}
}

// A Pong still on its way from an earlier turn, which no hook waited on, is
// delivered when the next turn ends rather than left sent.
func TestAnEarlierTurnsPongOnItsWayWakesTheNextTurn(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/typed.json")
	q := round(t, c, recordedSession, "Thread only?")[0]
	pong(t, c, q.ID, engine.SendPong{Answer: "Yes"})
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: recordedSession, State: engine.SessionIdle}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	report(t, dir, recordedSession)
	_, wake := run(t, dir, "stop/first.json")
	if !strings.Contains(wake+"\n", "\n- Thread only?: Yes\n") {
		t.Fatalf("woke with %q; want the earlier turn's Pong", wake)
	}
	if got, _ := c.Ping(q.ID); got.Pong == nil || got.Pong.Delivery != engine.DeliveryDelivering {
		t.Fatalf("the Pong is %+v; want it handed to the wake (delivering)", got.Pong)
	}
}

// A question an earlier turn ended on still waits for the human when a turn
// nobody typed, such as a background task's report, ends with no Ping of its
// own. That turn is not nudged for a finished Ping or a Note: the question is
// already in the human's Queue, so the hook waits for its Pong instead.
func TestAQuestionStillWaitingFromAnEarlierTurnIsNotNudged(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/typed.json")
	q := round(t, c, recordedSession, "Lead with these three?")[0]
	if _, err := c.RegisterSession(engine.RegisterSession{Harness: "claude-code", ID: recordedSession, State: engine.SessionIdle}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	report(t, dir, recordedSession) // a turn nobody typed
	wait := stopHook(t, dir, "stop/first.json")
	time.Sleep(20 * time.Millisecond)
	if ps, _ := c.Pings(""); len(ps) != 1 {
		t.Fatalf("Pings = %+v; want only the waiting question", ps)
	}
	pong(t, c, q.ID, engine.SendPong{Answer: "Yes"})
	if _, wake := wait(); !strings.Contains(wake+"\n", "\n- Lead with these three?: Yes\n") {
		t.Fatalf("woke with %q; want the question's Pong, not the nudge", wake)
	}
}

func TestTypingInTheTerminalAnswersARoundInTheHarness(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/woken.json")
	qs := round(t, c, woken, "Cut the first line?", "Shrink the nudge?")
	wait := stopHook(t, dir, "stop/woken.json")
	time.Sleep(20 * time.Millisecond)
	typeIn(t, dir, woken)
	if out, wake := wait(); out != "" || wake != "" {
		t.Fatalf("stop hook wrote %q, woke with %q", out, wake)
	}
	for _, p := range qs {
		if got, _ := c.Ping(p.ID); got.State != engine.StateAnsweredInHarness {
			t.Fatalf("Ping %q is %s", got.Question, got.State)
		}
	}
}

// Claude Code sends UserPromptSubmit for turns nobody typed: a background
// subagent's report, and the turn a Stop hook woke. Neither answers a
// waiting Ping; only a prompt the human typed does.
func TestATurnNobodyTypedLeavesWaitingPingsWaiting(t *testing.T) {
	for session, fixture := range map[string]string{reported: "prompt/subagent-report.json", woken: "prompt/woken.json"} {
		c, dir := enginetest.Start(t)
		q := round(t, c, session, "Lead with these three?")[0]
		if out, wake := run(t, dir, fixture); out != "" || wake != "" {
			t.Fatalf("%s: wrote %q, woke with %q", fixture, out, wake)
		}
		if got, _ := c.Ping(q.ID); got.State != engine.StateWaiting {
			t.Fatalf("%s: the Ping is %s; want it still waiting", fixture, got.State)
		}
		if s := state(t, c, session); s != engine.SessionWorking {
			t.Fatalf("%s: Session is %s, want working", fixture, s)
		}
		typeIn(t, dir, session)
		if got, _ := c.Ping(q.ID); got.State != engine.StateAnsweredInHarness {
			t.Fatalf("%s: after the human typed, the Ping is %s", fixture, got.State)
		}
	}
}

// A Stop hook outlives its turn (Claude Code keeps it running through a turn
// a subagent's report starts), so when its Pongs wake the Session, questions
// a later turn ended on may be waiting. They stay waiting.
func TestAPongsWakeLeavesALaterTurnsPingWaiting(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/typed.json")
	first := round(t, c, recordedSession, "Cut the first line?")[0]
	wait := stopHook(t, dir, "stop/first.json")
	time.Sleep(20 * time.Millisecond)
	later := round(t, c, recordedSession, "Shrink the nudge?")[0]
	pong(t, c, first.ID, engine.SendPong{Answer: "Yes"})
	if _, wake := wait(); !strings.Contains(wake+"\n", "\n- Cut the first line?: Yes\n") {
		t.Fatalf("woke with %q", wake)
	}
	if got, _ := c.Ping(later.ID); got.State != engine.StateWaiting {
		t.Fatalf("the later Ping is %s; want it still waiting", got.State)
	}
	if s := state(t, c, recordedSession); s != engine.SessionWorking {
		t.Fatalf("Session is %s, want working", s)
	}
}

// A closing message that asks nothing is broken into a Finished Ping: its
// first sentence is the question and the rest is the context, nothing lost.
func TestTheBuiltPingCarriesTheWholeMessage(t *testing.T) {
	c, dir := enginetest.Start(t)
	long := "I added the total to the invoice view and wired it to the tax table.\n\n" +
		"Tests: the unit tests pass; I did not run the browser tests.\n- next: rounding, " + strings.Repeat("and more detail ", 30)
	long = long[:600]
	in, _ := os.ReadFile(filepath.Join("testdata", "stop/woken.json"))
	msg, _ := json.Marshal("\n  " + long + "\n\n")
	in = bytes.Replace(in, []byte(`"BANANA"`), msg, 1)
	go claudecode.Hook(dir, bytes.NewReader(in), &bytes.Buffer{})
	p := waiting(t, c, 1)[0]
	if p.Question != "I added the total to the invoice view and wired it to the tax table." || !p.Finished ||
		!strings.HasPrefix(p.Context, "Tests: the unit tests pass; I did not run the browser tests.\n\n- next: rounding, and more detail") {
		t.Fatalf("built Ping = %+v", p)
	}
}

// A permission answered in the terminal during the turn is not the agent's
// Ping for the turn's end: the turn still gets the Ping built from it.
func TestASettledApprovalStillLeavesTheBuiltPing(t *testing.T) {
	c, dir := enginetest.Start(t)
	run(t, dir, "prompt/woken.json")
	out := call(t, dir, "permission/bash.json")
	p := waiting(t, c, 1)[0]
	if _, err := c.AnsweredInHarness(p.ID); err != nil {
		t.Fatal(err)
	}
	out()
	stopHook(t, dir, "stop/woken.json")
	if b := waiting(t, c, 1)[0]; b.ID == p.ID || b.Question != "BANANA" || !b.Finished {
		t.Fatalf("built Ping = %+v", b)
	}
}

// A closing message whose Resources come out short, such as a table after
// "Done." and a bare link, still gives the turn its Ping: the Engine takes
// every title the builder makes.
func TestShortResourceTitlesStillGiveTheTurnItsPing(t *testing.T) {
	c, dir := enginetest.Start(t)
	in, _ := os.ReadFile(filepath.Join("testdata", "stop/woken.json"))
	msg, _ := json.Marshal("Done.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\nSee https://github.com for more.")
	in = bytes.Replace(in, []byte(`"BANANA"`), msg, 1)
	stopHookOn(t, dir, in)
	p := waiting(t, c, 1)[0]
	if !p.Finished || len(p.Resources) != 2 {
		t.Fatalf("built Ping = %+v", p)
	}
}
