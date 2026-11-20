package engine_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// queue starts an Engine whose clock moves a second per Ping, so oldest
// first is unambiguous.
func queue(t *testing.T) *client.Client {
	t.Helper()
	at := time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)
	return enginetest.StartWith(t, engine.Config{Now: func() time.Time { at = at.Add(time.Second); return at }})
}

func ask(t *testing.T, c *client.Client, session, folder string, blocks bool) engine.Ping {
	t.Helper()
	p, err := c.CreatePing(engine.NewPing{Session: session, Folder: folder, Question: "Next?", BlocksAgent: blocks, Finished: !blocks,
		Answers: []engine.Answer{{Label: "Go"}, {Label: "Stop"}}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func order(t *testing.T, c *client.Client) []string {
	t.Helper()
	ps, err := c.Pings(engine.StateWaiting)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func sameOrder(t *testing.T, got []string, want ...engine.Ping) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d Pings, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i] != w.ID {
			t.Fatalf("place %d: got %s, want %s (%q)", i+1, got[i], w.ID, w.Session)
		}
	}
}

func TestBeforeAnyPongTheQueueIsBlockingThenOldest(t *testing.T) {
	c := queue(t)
	a, b := repo(t, "a"), repo(t, "b")
	old := ask(t, c, "s1", a, false)
	mid := ask(t, c, "s2", b, false)
	blocking := ask(t, c, "s3", b, true)
	sameOrder(t, order(t, c), blocking, old, mid)
}

func TestSameSessionPingPassesAnOlderOneFromAnotherProject(t *testing.T) {
	c := queue(t)
	a, b := repo(t, "a"), repo(t, "b")
	answered := ask(t, c, "s1", a, false)
	other := ask(t, c, "s2", b, false)
	if _, err := c.Pong(answered.ID, engine.SendPong{Answer: "Go"}); err != nil {
		t.Fatal(err)
	}
	next := ask(t, c, "s1", a, false)
	sameOrder(t, order(t, c), next, other)
}

func TestSameProjectPingPassesAnOlderOneFromAnotherProject(t *testing.T) {
	c := queue(t)
	a, b := repo(t, "a"), repo(t, "b")
	answered := ask(t, c, "s1", a, false)
	other := ask(t, c, "s2", b, false)
	if _, err := c.Pong(answered.ID, engine.SendPong{Answer: "Go"}); err != nil {
		t.Fatal(err)
	}
	sibling := ask(t, c, "s3", a, false)
	sameOrder(t, order(t, c), sibling, other)
}

func TestWorktreeOfTheRepositoryCountsAsTheSameProject(t *testing.T) {
	c := queue(t)
	a, b := repo(t, "a"), repo(t, "b")
	wt := filepath.Join(t.TempDir(), "wt")
	git(t, a, "worktree", "add", "-q", wt)
	answered := ask(t, c, "s1", a, false)
	other := ask(t, c, "s2", b, false)
	if _, err := c.Pong(answered.ID, engine.SendPong{Answer: "Go"}); err != nil {
		t.Fatal(err)
	}
	inWorktree := ask(t, c, "s3", wt, false)
	sameOrder(t, order(t, c), inWorktree, other)
}

func TestBlockingPingFromAnotherProjectComesBeforeBoth(t *testing.T) {
	c := queue(t)
	a, b := repo(t, "a"), repo(t, "b")
	answered := ask(t, c, "s1", a, false)
	other := ask(t, c, "s2", b, false)
	if _, err := c.Pong(answered.ID, engine.SendPong{Answer: "Go"}); err != nil {
		t.Fatal(err)
	}
	sibling := ask(t, c, "s3", a, false)
	same := ask(t, c, "s1", a, false)
	blocking := ask(t, c, "s4", b, true)
	sameOrder(t, order(t, c), blocking, same, sibling, other)
}
