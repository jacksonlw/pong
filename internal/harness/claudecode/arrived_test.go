package claudecode_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/engine"
)

// wokenWith is the prompt Claude Code submits for a turn a hook woke with
// wake, as its UserPromptSubmit carries it.
func wokenWith(t *testing.T, wake string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "prompt/woken.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	m["prompt"] = "<task-notification>\n<summary>Stop hook feedback</summary>\n</task-notification>\n<system-reminder>\nStop hook blocking error from command \"Stop\": " + wake + "\n</system-reminder>"
	out, _ := json.Marshal(m)
	return out
}

func deliveryOf(t *testing.T, c interface {
	Ping(string) (engine.Ping, error)
}, id string) string {
	t.Helper()
	p, err := c.Ping(id)
	if err != nil || p.Pong == nil {
		t.Fatalf("Ping %s: %+v, %v", id, p, err)
	}
	return p.Pong.Delivery
}

// A Pong a hook hands to a wake is delivered only when the woken turn shows
// it: until then it is delivering.
func TestAPongIsDeliveredWhenTheWokenTurnShowsIt(t *testing.T) {
	c, dir := enginetest.Start(t)
	wait := stopHook(t, dir, "stop/woken.json")
	p := waiting(t, c, 1)[0]
	pong(t, c, p.ID, engine.SendPong{Text: "now add a changelog line"})
	_, wake := wait()
	if d := deliveryOf(t, c, p.ID); d != engine.DeliveryDelivering {
		t.Fatalf("after the hook woke the Session the Pong is %q; want delivering", d)
	}
	callBytes(t, dir, wokenWith(t, wake))
	if d := deliveryOf(t, c, p.ID); d != engine.DeliveryDelivered {
		t.Fatalf("after the woken turn began the Pong is %q; want delivered", d)
	}
}

// A woken turn that carries other Pongs does not confirm this one: on 23
// November two Pongs were handed to a wake Claude Code dropped, and the next
// wake, carrying later Pongs, must not count them as arrived.
func TestAWakeTheHarnessDroppedBringsThePongBack(t *testing.T) {
	c, dir := enginetest.StartWithDir(t, engine.Config{DeliveryGrace: 300 * time.Millisecond})
	wait := stopHook(t, dir, "stop/woken.json")
	p := waiting(t, c, 1)[0]
	pong(t, c, p.ID, engine.SendPong{Text: "now add a changelog line"})
	wait() // the wake Claude Code then drops
	callBytes(t, dir, wokenWith(t, "The human answered your Pings in Pong:\n- Another question?: Yes"))
	deadline := time.Now().Add(5 * time.Second)
	for deliveryOf(t, c, p.ID) == engine.DeliveryDelivering && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if d := deliveryOf(t, c, p.ID); d != engine.DeliveryUndelivered {
		t.Fatalf("the dropped Pong is %q; want undelivered, back with the human", d)
	}
}

// "Nothing more" wakes nothing, so it is delivered as it is taken.
func TestNothingMoreIsDeliveredAsItIsTaken(t *testing.T) {
	c, dir := enginetest.Start(t)
	wait := stopHook(t, dir, "stop/woken.json")
	p := waiting(t, c, 1)[0]
	pong(t, c, p.ID, engine.SendPong{Answer: engine.NothingMore})
	wait()
	if d := deliveryOf(t, c, p.ID); d != engine.DeliveryDelivered {
		t.Fatalf("Nothing more is %q; want delivered", d)
	}
}

// A Pong's time to arrive starts when its hook wakes the Session, not when it
// was taken: a hook waiting on a later Pong first does not send it back.
func TestAPongWaitingBesideAnotherIsNotSentBack(t *testing.T) {
	c, dir := enginetest.StartWithDir(t, engine.Config{DeliveryGrace: 200 * time.Millisecond})
	run(t, dir, "prompt/woken.json")
	q, err := c.CreatePing(engine.NewPing{Session: woken, Harness: "claude-code", Folder: "/tmp", Question: "Which one?", BlocksAgent: true, Answers: []engine.Answer{{Label: "This"}, {Label: "That"}}})
	if err != nil {
		t.Fatal(err)
	}
	f, err := c.CreatePing(engine.NewPing{Finished: true, Session: woken, Harness: "claude-code", Folder: "/tmp", Question: "Done the first part."})
	if err != nil {
		t.Fatal(err)
	}
	wait := stopHook(t, dir, "stop/woken.json")
	pong(t, c, q.ID, engine.SendPong{Answer: "This"})
	time.Sleep(600 * time.Millisecond) // well past the grace, while the hook waits on the other
	if d := deliveryOf(t, c, q.ID); d != engine.DeliveryDelivering {
		t.Fatalf("while its hook waits on another Pong, the first is %q", d)
	}
	pong(t, c, f.ID, engine.SendPong{Text: "go on"})
	_, wake := wait()
	callBytes(t, dir, wokenWith(t, wake))
	for _, id := range []string{q.ID, f.ID} {
		if d := deliveryOf(t, c, id); d != engine.DeliveryDelivered {
			t.Fatalf("Pong %s is %q after the woken turn", id, d)
		}
	}
}
