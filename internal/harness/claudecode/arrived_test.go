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
