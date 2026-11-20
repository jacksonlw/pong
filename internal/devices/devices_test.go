package devices_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"

	"github.com/jacksonlw/pong/enginetest"
	"github.com/jacksonlw/pong/internal/devices"
	"github.com/jacksonlw/pong/wire"
)

func names(s devices.Set) map[string]devices.Device {
	m := map[string]devices.Device{}
	for _, d := range s.Devices {
		m[d.Name] = d
	}
	return m
}

func reachable(t *testing.T, c *enginetest.Computer, want ...string) {
	t.Helper()
	enginetest.Eventually(t, c.Name+" to reach "+strings.Join(want, ", "), func() bool {
		s, err := c.C.Devices()
		if err != nil {
			return false
		}
		m := names(s)
		for _, n := range want {
			if !m[n].Reachable {
				return false
			}
		}
		return true
	})
}

func TestTheFirstDeviceStartsASetOfOneOnLocal(t *testing.T) {
	a := enginetest.StartComputer(t, &enginetest.Book{}, "Laptop")
	s, err := a.C.Devices()
	if err != nil {
		t.Fatal(err)
	}
	if s.Mode != "local" || len(s.Devices) != 1 || !s.Devices[0].This || s.Devices[0].Name != "Laptop" {
		t.Fatalf("set %+v", s)
	}
}

func TestPairingThreeComputersReachesEveryDevice(t *testing.T) {
	book := &enginetest.Book{}
	a := enginetest.StartComputer(t, book, "Laptop")
	b := enginetest.StartComputer(t, book, "Desktop")
	c := enginetest.StartComputer(t, book, "Box")
	enginetest.Pair(t, a, b)
	enginetest.Pair(t, b, c) // approved on the second computer
	for _, m := range []*enginetest.Computer{a, b, c} {
		reachable(t, m, "Laptop", "Desktop", "Box")
	}
}

func TestTheApprovalShowsTheAskingDeviceAndTheSameCode(t *testing.T) {
	book := &enginetest.Book{}
	a := enginetest.StartComputer(t, book, "Laptop")
	b := enginetest.StartComputer(t, book, "Box")
	p, err := b.C.Pair(a.NetAddr)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Code) != 6 || p.Target.Name != "Laptop" {
		t.Fatalf("pairing %+v", p)
	}
	enginetest.Eventually(t, "the request to show", func() bool {
		s, _ := a.C.Devices()
		return len(s.Waiting) == 1 && s.Waiting[0].Name == "Box" && s.Waiting[0].Platform == "linux" && s.Waiting[0].Code == p.Code
	})
}

func TestADeviceThatAsksWithoutApprovalReceivesNothing(t *testing.T) {
	book := &enginetest.Book{}
	a := enginetest.StartComputer(t, book, "Laptop")
	b := enginetest.StartComputer(t, book, "Desktop")
	enginetest.Pair(t, a, b)

	// A stranger connects, sends a join request, and listens.
	k, _ := wire.NewKeys()
	ws, err := websocket.Dial("ws://"+a.NetAddr+"/wire", "", "http://"+a.NetAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	j := wire.NewJoinRequest(k, "Stranger", "mac", true, time.Now().UnixMilli())
	b2, _ := json.Marshal(map[string]any{"join": j})
	websocket.Message.Send(ws, string(b2))
	got := 0
	ws.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	for {
		var s string
		if websocket.Message.Receive(ws, &s) != nil {
			break
		}
		if !strings.HasPrefix(s, `{"device"`) {
			t.Fatalf("an unapproved Device received %s", s)
		}
		got++
	}
	if got != 1 {
		t.Fatalf("got %d greetings", got)
	}
	s, _ := a.C.Devices()
	if len(s.Devices) != 2 {
		t.Fatal("the stranger joined without approval")
	}
}

func TestTheNetworkListenerRefusesTheLocalAPI(t *testing.T) {
	a := enginetest.StartComputer(t, &enginetest.Book{}, "Laptop")
	for _, path := range []string{"/health", "/pings", "/devices", "/sessions", "/settings", "/mcp", "/events", "/"} {
		for _, method := range []string{"GET", "POST"} {
			req, _ := http.NewRequest(method, "http://"+a.NetAddr+path, strings.NewReader("{}"))
			req.Header.Set("X-Pong-Secret", "anything")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode < 400 || strings.Contains(string(body), "version") {
				t.Errorf("%s %s answered %d: %s", method, path, resp.StatusCode, body)
			}
		}
	}
	// A plain request to /wire that is not a WebSocket is refused too.
	resp, err := http.Get("http://" + a.NetAddr + "/wire")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Errorf("/wire without a WebSocket answered %d", resp.StatusCode)
	}
}

func TestTheNetworkListenerClosesOnAnythingButEnvelopesAndJoins(t *testing.T) {
	a := enginetest.StartComputer(t, &enginetest.Book{}, "Laptop")
	for _, junk := range []string{`hello`, `{"ping":{}}`, `{"join":{},"envelope":{}}`, `{"device":{"id":"x"}}`} {
		ws, err := websocket.Dial("ws://"+a.NetAddr+"/wire", "", "http://"+a.NetAddr)
		if err != nil {
			t.Fatal(err)
		}
		var greet string
		websocket.Message.Receive(ws, &greet)
		websocket.Message.Send(ws, junk)
		ws.SetReadDeadline(time.Now().Add(2 * time.Second))
		var s string
		if err := websocket.Message.Receive(ws, &s); err == nil {
			t.Errorf("after %s the listener sent %s", junk, s)
		} else if strings.Contains(err.Error(), "timeout") {
			t.Errorf("after %s the connection stayed open", junk)
		}
		ws.Close()
	}
}

func TestComputersReconnectWhenOneReturns(t *testing.T) {
	book := &enginetest.Book{}
	a := enginetest.StartComputer(t, book, "Laptop")
	b := enginetest.StartComputer(t, book, "Box")
	enginetest.Pair(t, a, b)
	reachable(t, a, "Box")
	enginetest.Stop(b)
	enginetest.Eventually(t, "Box to be unreachable", func() bool {
		s, _ := a.C.Devices()
		return !names(s)["Box"].Reachable
	})
	enginetest.RestartComputer(t, book, b)
	reachable(t, a, "Box")
	reachable(t, b, "Laptop")
}

func TestAComputerAlreadyInASetCannotPairElsewhere(t *testing.T) {
	book := &enginetest.Book{}
	a := enginetest.StartComputer(t, book, "Laptop")
	b := enginetest.StartComputer(t, book, "Box")
	other := enginetest.StartComputer(t, &enginetest.Book{}, "Other")
	enginetest.Pair(t, a, b)
	if _, err := b.C.Pair(other.NetAddr); err == nil {
		t.Fatal("a Device in a set of two paired with another set")
	}
}

// A stranger on the network that greets as a member is never taken for it.
func TestAStrangerGreetingAsAMemberIsNotTakenForIt(t *testing.T) {
	book := &enginetest.Book{}
	a := enginetest.StartComputer(t, book, "Laptop")
	b := enginetest.StartComputer(t, book, "Box")
	enginetest.Pair(t, a, b)
	enginetest.Stop(b)
	enginetest.Eventually(t, "Box to be unreachable", func() bool {
		s, _ := a.C.Devices()
		return !names(s)["Box"].Reachable
	})
	var boxInfo devices.Device
	s, _ := a.C.Devices()
	boxInfo = names(s)["Box"]

	// The stranger connects and greets with Box's identifier.
	ws, err := websocket.Dial("ws://"+a.NetAddr+"/wire", "", "http://"+a.NetAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var greet string
	websocket.Message.Receive(ws, &greet)
	b2, _ := json.Marshal(map[string]any{"device": map[string]any{"id": boxInfo.ID, "name": "Box", "platform": "linux", "engine": true}})
	websocket.Message.Send(ws, string(b2))
	time.Sleep(300 * time.Millisecond)
	s, _ = a.C.Devices()
	if names(s)["Box"].Reachable {
		t.Fatal("a greeting alone made Box reachable")
	}
}

func joinFrame(t *testing.T, j wire.JoinRequest) string {
	b, _ := json.Marshal(map[string]any{"join": j})
	return string(b)
}

func dialWire(t *testing.T, addr, origin string) *websocket.Conn {
	t.Helper()
	ws, err := websocket.Dial("ws://"+addr+"/wire", "", origin)
	if err != nil {
		t.Fatal(err)
	}
	var greet string
	websocket.Message.Receive(ws, &greet)
	return ws
}

func TestAStaleJoinRequestIsIgnored(t *testing.T) {
	a := enginetest.StartComputer(t, &enginetest.Book{}, "Laptop")
	k, _ := wire.NewKeys()
	ws := dialWire(t, a.NetAddr, "http://"+a.NetAddr)
	defer ws.Close()
	websocket.Message.Send(ws, joinFrame(t, wire.NewJoinRequest(k, "Old", "mac", true, time.Now().Add(-time.Hour).UnixMilli())))
	time.Sleep(200 * time.Millisecond)
	if s, _ := a.C.Devices(); len(s.Waiting) != 0 {
		t.Fatal("a join request an hour old is waiting")
	}
}

func TestAReplayedJoinRequestDoesNotTakeOverTheWaitingOne(t *testing.T) {
	a := enginetest.StartComputer(t, &enginetest.Book{}, "Laptop")
	k, _ := wire.NewKeys()
	frame := joinFrame(t, wire.NewJoinRequest(k, "Box", "linux", true, time.Now().UnixMilli()))
	asker := dialWire(t, a.NetAddr, "http://"+a.NetAddr)
	defer asker.Close()
	websocket.Message.Send(asker, frame)
	enginetest.Eventually(t, "Box to ask", func() bool { s, _ := a.C.Devices(); return len(s.Waiting) == 1 })
	// An eavesdropper replays the same request from its own connection.
	thief := dialWire(t, a.NetAddr, "http://"+a.NetAddr)
	defer thief.Close()
	websocket.Message.Send(thief, frame)
	time.Sleep(100 * time.Millisecond)
	if _, err := a.C.ApproveDevice(k.ID()); err != nil {
		t.Fatal(err)
	}
	thief.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	var got string
	if websocket.Message.Receive(thief, &got) == nil {
		t.Fatalf("the replaying connection received %s", got)
	}
	asker.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := websocket.Message.Receive(asker, &got); err != nil || !strings.Contains(got, `"envelope"`) {
		t.Fatalf("the asking connection did not receive the chain: %v %s", err, got)
	}
}

func TestBrowserPagesCannotOpenTheListener(t *testing.T) {
	a := enginetest.StartComputer(t, &enginetest.Book{}, "Laptop")
	if _, err := websocket.Dial("ws://"+a.NetAddr+"/wire", "", "https://evil.example"); err == nil {
		t.Fatal("a WebSocket from a web page's origin was accepted")
	}
}

func TestTooManyAskingDevicesAreRefused(t *testing.T) {
	a := enginetest.StartComputer(t, &enginetest.Book{}, "Laptop")
	ws := dialWire(t, a.NetAddr, "http://"+a.NetAddr)
	defer ws.Close()
	for i := 0; i < 20; i++ {
		k, _ := wire.NewKeys()
		websocket.Message.Send(ws, joinFrame(t, wire.NewJoinRequest(k, "Flood", "mac", true, time.Now().UnixMilli())))
	}
	time.Sleep(300 * time.Millisecond)
	if s, _ := a.C.Devices(); len(s.Waiting) > 8 {
		t.Fatalf("%d Devices waiting", len(s.Waiting))
	}
}
