package devices_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
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
	if len(p.Offers) != 1 || len(p.Offers[0].Code) != 6 || p.Offers[0].Device.Name != "Laptop" || !p.Confirm {
		t.Fatalf("pairing %+v", p)
	}
	enginetest.Eventually(t, "the request to show", func() bool {
		s, _ := a.C.Devices()
		return len(s.Waiting) == 1 && s.Waiting[0].Name == "Box" && s.Waiting[0].Platform == "linux" && s.Waiting[0].Code == p.Offers[0].Code
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
		if strings.HasPrefix(s, `{"pair"`) {
			continue // the laptop's commit toward the code, which says nothing of the set
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
	makeCode(t, asker, k)
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

// phoneOf joins a: a Device without an Engine asks on a direct connection
// and is approved. It returns the phone's keys, the connection and a's
// Device.
func phoneOf(t *testing.T, a *enginetest.Computer) (*wire.Keys, *websocket.Conn, wire.DeviceInfo) {
	t.Helper()
	k, _ := wire.NewKeys()
	ws, laptop := greeted(t, a.NetAddr)
	websocket.Message.Send(ws, joinFrame(t, wire.NewJoinRequest(k, "Phone", "ios", false, time.Now().UnixMilli())))
	code := makeCode(t, ws, k)
	enginetest.Eventually(t, "the phone to ask", func() bool {
		s, _ := a.C.Devices()
		return len(s.Waiting) == 1 && s.Waiting[0].Code == code
	})
	if _, err := a.C.ApproveDevice(k.ID()); err != nil {
		t.Fatal(err)
	}
	if _, ok := chainOn(t, ws, k, laptop); !ok {
		t.Fatal("the phone was not sent the chain that admits it")
	}
	return k, ws, laptop
}

// makeCode plays the asking Device's part of the exchange after its join
// request went out on ws: it answers the commit with a nonce, takes the
// reveal, and returns the code.
func makeCode(t *testing.T, ws *websocket.Conn, k *wire.Keys) string {
	t.Helper()
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer ws.SetReadDeadline(time.Time{})
	var ask *wire.PairAsk
	for {
		var s string
		if err := websocket.Message.Receive(ws, &s); err != nil {
			t.Fatalf("no code was made: %v", err)
		}
		var f struct{ Pair *wire.PairStep }
		if json.Unmarshal([]byte(s), &f) != nil || f.Pair == nil {
			continue
		}
		if ask == nil {
			a, nonce, err := wire.NewPairAsk(k, *f.Pair)
			if err != nil {
				t.Fatal(err)
			}
			ask = a
			b, _ := json.Marshal(map[string]any{"pair": nonce})
			websocket.Message.Send(ws, string(b))
			continue
		}
		if !ask.Take(*f.Pair) {
			t.Fatal("the reveal is not what was committed to")
		}
		return ask.Code()
	}
}

// A Device that asks to join gets a code only by giving its own value, and
// no second value changes the code once the computer's is out.
func TestTheCodeIsFixedOnceBothValuesAreOut(t *testing.T) {
	a := enginetest.StartComputer(t, &enginetest.Book{}, "Laptop")
	k, _ := wire.NewKeys()
	ws, laptop := greeted(t, a.NetAddr)
	websocket.Message.Send(ws, joinFrame(t, wire.NewJoinRequest(k, "Box", "linux", true, time.Now().UnixMilli())))
	time.Sleep(200 * time.Millisecond)
	if s, _ := a.C.Devices(); len(s.Waiting) != 0 {
		t.Fatalf("a request with no code yet is shown: %+v", s.Waiting)
	}
	if _, err := a.C.ApproveDevice(k.ID()); err == nil {
		t.Fatal("a request with no code yet was approved")
	}
	code := makeCode(t, ws, k)
	again := make([]byte, 32)
	b, _ := json.Marshal(map[string]any{"pair": wire.PairStep{From: k.ID(), To: laptop.ID, Nonce: again}})
	websocket.Message.Send(ws, string(b))
	time.Sleep(200 * time.Millisecond)
	if s, _ := a.C.Devices(); len(s.Waiting) != 1 || s.Waiting[0].Code != code {
		t.Fatalf("the code changed after a second nonce: %+v, want %s", s.Waiting, code)
	}
}

// A computer that asks to join takes no set before its human said the codes
// match, even when the set approves first; a mismatch ends the request.
func TestAComputerJoinsOnlyAfterItsHumanSaysTheCodesMatch(t *testing.T) {
	book := &enginetest.Book{}
	a := enginetest.StartComputer(t, book, "Laptop")
	b := enginetest.StartComputer(t, book, "Box")
	p, err := b.C.Pair(a.NetAddr)
	if err != nil {
		t.Fatal(err)
	}
	var id string
	enginetest.Eventually(t, "Box to ask", func() bool {
		s, _ := a.C.Devices()
		if len(s.Waiting) == 1 && s.Waiting[0].Code == p.Offers[0].Code {
			id = s.Waiting[0].ID
		}
		return id != ""
	})
	if _, err := a.C.ApproveDevice(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if s, _ := b.C.Devices(); len(s.Devices) != 1 || s.Pairing == nil || s.Pairing.Joined || !s.Pairing.Confirm {
		t.Fatalf("Box took the set before its human confirmed: %+v", s)
	}
	if _, err := b.C.ConfirmPairing(true); err != nil {
		t.Fatal(err)
	}
	enginetest.Eventually(t, "Box to join", func() bool { s, _ := b.C.Devices(); return len(s.Devices) == 2 })

	c := enginetest.StartComputer(t, book, "Desk")
	if _, err := c.C.Pair(a.NetAddr); err != nil {
		t.Fatal(err)
	}
	if s, err := c.C.ConfirmPairing(false); err != nil || s.Pairing != nil {
		t.Fatalf("after a mismatch the request still stands: %+v %v", s.Pairing, err)
	}
}

// greeted connects to the listener at addr and returns the Device it greets as.
func greeted(t *testing.T, addr string) (*websocket.Conn, wire.DeviceInfo) {
	t.Helper()
	ws, err := websocket.Dial("ws://"+addr+"/wire", "", "http://"+addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	var s string
	if err := websocket.Message.Receive(ws, &s); err != nil {
		t.Fatal(err)
	}
	var f struct {
		Device wire.DeviceInfo
		Nonce  []byte
	}
	if json.Unmarshal([]byte(s), &f) != nil || f.Device.ID == "" {
		t.Fatalf("greeting %s", s)
	}
	greetedWith.Store(ws, f.Nonce)
	return ws, f.Device
}

// greetedWith keeps, for each connection greeted returned, the nonce the computer greeted with.
var greetedWith sync.Map

// chainOn waits for the next envelope on ws and returns the set its chain
// leaves, when it carries one from the laptop.
func chainOn(t *testing.T, ws *websocket.Conn, k *wire.Keys, laptop wire.DeviceInfo) (wire.Members, bool) {
	t.Helper()
	ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer ws.SetReadDeadline(time.Time{})
	for {
		var s string
		if websocket.Message.Receive(ws, &s) != nil {
			return nil, false
		}
		var f struct{ Envelope *wire.Envelope }
		if json.Unmarshal([]byte(s), &f) != nil || f.Envelope == nil {
			continue
		}
		m, err := wire.NewOpener(k).Open(f.Envelope, wire.Members{laptop.ID: laptop}, time.Now())
		if err != nil {
			t.Fatalf("an envelope from the laptop did not open: %v", err)
		}
		if cr, ok := m.(*wire.ChainRecords); ok {
			members, err := cr.Records.Verify()
			if err != nil {
				t.Fatal(err)
			}
			return members, true
		}
	}
}

// sayHello greets the laptop as the phone, with a nonce, and says hello binding the connection.
func sayHello(t *testing.T, ws *websocket.Conn, k *wire.Keys, laptop wire.DeviceInfo) {
	t.Helper()
	mine := []byte("the test phone's greeting nonce.")
	p := k.Public()
	g, _ := json.Marshal(map[string]any{"device": wire.DeviceInfo{ID: k.ID(), Name: "Phone", Platform: "ios", SealKey: p.Seal, SignKey: p.Sign}, "nonce": mine})
	websocket.Message.Send(ws, string(g))
	theirs, _ := greetedWith.Load(ws)
	bind := append(append([]byte{}, mine...), theirs.([]byte)...)
	e, err := wire.Seal(k, laptop, &wire.Hello{ChainLen: 2, Bind: bind}, false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"envelope": e})
	websocket.Message.Send(ws, string(b))
}

func closed(ws *websocket.Conn) bool {
	ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var s string
		if err := websocket.Message.Receive(ws, &s); err != nil {
			return !strings.Contains(err.Error(), "timeout")
		}
	}
}

// A phone connected when it is revoked is sent the chain that revokes it,
// so it can forget the set, and is then let go.
func TestARevokedDeviceIsToldItWasRevoked(t *testing.T) {
	a := enginetest.StartComputer(t, &enginetest.Book{}, "Laptop")
	k, ws, laptop := phoneOf(t, a)
	sayHello(t, ws, k, laptop) // makes the connection the phone's
	reachable(t, a, "Phone")
	if _, err := a.C.RevokeDevice(k.ID()); err != nil {
		t.Fatal(err)
	}
	members, ok := chainOn(t, ws, k, laptop)
	if !ok {
		t.Fatal("the revoked phone was sent nothing")
	}
	if _, in := members[k.ID()]; in {
		t.Fatal("the chain the revoked phone was sent still has it in the set")
	}
	if !closed(ws) {
		t.Fatal("the revoked phone's connection stayed open")
	}
}

// A phone away when it was revoked learns it the next time it connects,
// instead of being greeted and then ignored.
func TestARevokedDeviceIsToldWhenItNextConnects(t *testing.T) {
	a := enginetest.StartComputer(t, &enginetest.Book{}, "Laptop")
	k, ws, _ := phoneOf(t, a)
	ws.Close()
	if _, err := a.C.RevokeDevice(k.ID()); err != nil {
		t.Fatal(err)
	}
	back, laptop := greeted(t, a.NetAddr)
	sayHello(t, back, k, laptop)
	members, ok := chainOn(t, back, k, laptop)
	if !ok {
		t.Fatal("the revoked phone was sent nothing when it connected again")
	}
	if _, in := members[k.ID()]; in {
		t.Fatal("the chain the revoked phone was sent still has it in the set")
	}
	if !closed(back) {
		t.Fatal("the revoked phone's connection stayed open")
	}
}

// A stranger whose envelope does not open is sent nothing.
func TestAStrangersEnvelopeIsAnsweredWithNothing(t *testing.T) {
	a := enginetest.StartComputer(t, &enginetest.Book{}, "Laptop")
	k, _ := wire.NewKeys()
	ws, laptop := greeted(t, a.NetAddr)
	sayHello(t, ws, k, laptop)
	ws.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	var s string
	if websocket.Message.Receive(ws, &s) == nil {
		t.Fatalf("a stranger was sent %s", s)
	}
}
