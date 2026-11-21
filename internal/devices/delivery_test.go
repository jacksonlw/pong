package devices_test

import (
	"encoding/json"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/devices"
	"github.com/jacksonlw/pong/wire"
)

// phoneOfNode joins a Node as phoneOf joins a computer: it asks, makes the code, is approved,
// and greets and says hello binding the connection.
func phoneOfNode(t *testing.T, n *devices.Node) (*wire.Keys, *websocket.Conn, wire.DeviceInfo) {
	t.Helper()
	k, _ := wire.NewKeys()
	ws, laptop := greeted(t, n.NetAddr)
	websocket.Message.Send(ws, joinFrame(t, wire.NewJoinRequest(k, "Phone", "ios", false, time.Now().UnixMilli())))
	makeCode(t, ws, k)
	if err := n.Approve(k.ID()); err != nil {
		t.Fatal(err)
	}
	if _, ok := chainOn(t, ws, k, laptop); !ok {
		t.Fatal("the phone was not sent the chain that admits it")
	}
	sayHello(t, ws, k, laptop)
	enginetest.Eventually(t, "the phone's connection to be bound", func() bool {
		for _, d := range n.Set().Devices {
			if d.ID == k.ID() {
				return d.Reachable
			}
		}
		return false
	})
	return k, ws, laptop
}

// nextOf waits for the next envelope of kind on ws and returns it, opened.
func nextOf(t *testing.T, ws *websocket.Conn, k *wire.Keys, from wire.DeviceInfo, kind string) (*wire.Envelope, wire.Message) {
	t.Helper()
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer ws.SetReadDeadline(time.Time{})
	for {
		var s string
		if err := websocket.Message.Receive(ws, &s); err != nil {
			t.Fatalf("no %s came: %v", kind, err)
		}
		var f struct{ Envelope *wire.Envelope }
		if json.Unmarshal([]byte(s), &f) != nil || f.Envelope == nil {
			continue
		}
		m, err := wire.NewOpener(k).Open(f.Envelope, wire.Members{from.ID: from}, time.Now())
		if err == nil && m.Kind() == kind {
			return f.Envelope, m
		}
	}
}

// A queued message stays until the Device acknowledges it, and once sent it cannot be taken back.
func TestAQueuedMessageStaysUntilAcknowledged(t *testing.T) {
	n, err := devices.Start(devices.Config{Dir: t.TempDir(), Name: "Laptop", Platform: wire.PlatformMac, NetAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	k, ws, laptop := phoneOfNode(t, n)
	went, err := n.SendOrQueue(k.ID(), "pong-1", &wire.Ping{Computer: laptop.ID, Event: "ping.created", Ping: json.RawMessage(`{"id":"p1"}`)})
	if err != nil || !went {
		t.Fatalf("went %v, %v", went, err)
	}
	e, _ := nextOf(t, ws, k, laptop, "ping")
	if len(n.Outbox()) != 1 {
		t.Fatal("a sent message left the outbox before it was acknowledged")
	}
	if n.Unqueue("pong-1") {
		t.Fatal("a message already sent was taken back")
	}
	ack, _ := wire.Seal(k, laptop, &wire.Ack{IDs: []string{e.ID}}, false, time.Now())
	b, _ := json.Marshal(map[string]any{"envelope": ack})
	websocket.Message.Send(ws, string(b))
	enginetest.Eventually(t, "the acknowledged message to leave the outbox", func() bool { return len(n.Outbox()) == 0 })
}

// A hello that does not bind the connection it came on makes that connection no one's.
func TestAHelloFromAnotherConnectionBindsNothing(t *testing.T) {
	n, err := devices.Start(devices.Config{Dir: t.TempDir(), Name: "Laptop", Platform: wire.PlatformMac, NetAddr: "127.0.0.1:0"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { n.Close() })
	k, ws, laptop := phoneOfNode(t, n)
	ws.Close()
	enginetest.Eventually(t, "the phone to drop", func() bool {
		for _, d := range n.Set().Devices {
			if d.ID == k.ID() {
				return !d.Reachable
			}
		}
		return false
	})
	// A second connection carries a hello whose bind was made for another connection.
	other, _ := greeted(t, n.NetAddr)
	e, _ := wire.Seal(k, laptop, &wire.Hello{ChainLen: 2, Bind: []byte("a bind for another connection")}, false, time.Now())
	b, _ := json.Marshal(map[string]any{"envelope": e})
	websocket.Message.Send(other, string(b))
	time.Sleep(300 * time.Millisecond)
	for _, d := range n.Set().Devices {
		if d.ID == k.ID() && d.Reachable {
			t.Fatal("a hello that binds another connection made this one the phone's")
		}
	}
}
