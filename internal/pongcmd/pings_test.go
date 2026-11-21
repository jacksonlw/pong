package pongcmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/client"
	"github.com/jacksonlw/pong/internal/engine"
)

// waitingPing waits until the Engine holds one waiting Ping and returns it.
func waitingPing(t *testing.T, c *client.Client) engine.Ping {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if ps, _ := c.Pings(engine.StateWaiting); len(ps) == 1 {
			return ps[0]
		}
	}
	t.Fatal("no waiting Ping")
	return engine.Ping{}
}

func TestPingBlocksUntilPongThenPrintsIt(t *testing.T) {
	c, dir := enginetest.Start(t)
	var out, errOut bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"ping", "-dir", dir, "-session", "s1", "-question", "Release before the review finishes?", "-blocks",
			"-answer", "Deploy now", "-answer", "After review"}, &out, &errOut)
	}()
	p := waitingPing(t, c)

	var inboxOut bytes.Buffer
	if code := run([]string{"inbox", "-dir", dir}, &inboxOut, &inboxOut); code != 0 {
		t.Fatalf("inbox exit %d: %s", code, inboxOut.String())
	}
	for _, want := range []string{p.ID, "Release before the review finishes?", "- After review"} {
		if !strings.Contains(inboxOut.String(), want) {
			t.Fatalf("inbox = %q, missing %q", inboxOut.String(), want)
		}
	}
	select {
	case <-done:
		t.Fatal("ping returned before the Pong")
	case <-time.After(2 * enginetest.WaitHold):
	}

	if _, err := c.Pong(p.ID, client.SendPong{Answer: "After review", Text: "ask Sam"}); err != nil { // the human, in Pong's app
		t.Fatal(err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("ping exit %d: %s", code, errOut.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ping did not return after the Pong")
	}
	if out.String() != "answer: After review\ntext: ask Sam\n" {
		t.Fatalf("ping printed %q", out.String())
	}
	inboxOut.Reset()
	run([]string{"inbox", "-dir", dir}, &inboxOut, &inboxOut)
	if strings.TrimSpace(inboxOut.String()) != "No waiting Pings." {
		t.Fatalf("inbox after Pong = %q", inboxOut.String())
	}
}

// No command answers a Ping: that is the human's, in Pong's apps.
func TestNoCommandAnswersAPing(t *testing.T) {
	c, dir := enginetest.Start(t)
	p, err := c.CreatePing(client.NewPing{Finished: true, Session: "s1", Question: "Go?", Answers: []engine.Answer{{Label: "Yes"}}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if code := run([]string{"pong", p.ID, "-dir", dir, "-answer", "Yes"}, &out, &out); code == 0 {
		t.Fatalf("pong exit 0: %s", out.String())
	}
	if got, _ := c.Ping(p.ID); got.State != engine.StateWaiting {
		t.Fatalf("the Ping is %s", got.State)
	}
}

func TestNoteFromAScriptReturnsAtOnce(t *testing.T) {
	c, dir := enginetest.Start(t)
	var out bytes.Buffer
	if code := run([]string{"ping", "-dir", dir, "-session", "s1", "-note", "-question", "Nightly build passed."}, &out, &out); code != 0 {
		t.Fatalf("exit %d: %s", code, out.String())
	}
	p := waitingPing(t, c)
	if !p.IsNote || !strings.Contains(out.String(), p.ID) {
		t.Fatalf("note %+v, output %q", p, out.String())
	}
}

func TestPingHandedBackExitsThree(t *testing.T) {
	c, dir := enginetest.Start(t)
	var out bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"ping", "-dir", dir, "-session", "s1", "-question", "Which checks?", "-blocks", "-several",
			"-answer", "Lint", "-answer", "Tests"}, &out, &out)
	}()
	p := waitingPing(t, c)
	c.HandBack(p.ID)
	select {
	case code := <-done:
		if code != 3 || out.String() != "state: handed_back\n" {
			t.Fatalf("exit %d: %q", code, out.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ping did not return")
	}
}
