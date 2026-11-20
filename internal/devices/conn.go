package devices

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/websocket"

	"github.com/jacksonlw/pong/wire"
)

// maxFrame bounds one WebSocket frame: a 192 KiB Resource part or a 1 MB logo, sealed and
// base64-encoded, fits with room to spare.
const maxFrame = 3 << 20

// frame is one WebSocket text frame on the network listener: exactly one
// field is set.
type frame struct {
	Device   *wire.DeviceInfo  `json:"device,omitempty"`
	Join     *wire.JoinRequest `json:"join,omitempty"`
	Envelope *wire.Envelope    `json:"envelope,omitempty"`
}

func decodeFrame(b []byte) (frame, error) {
	var f frame
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&f); err != nil {
		return f, err
	}
	set := 0
	for _, p := range []bool{f.Device != nil, f.Join != nil, f.Envelope != nil} {
		if p {
			set++
		}
	}
	if set != 1 {
		return f, errors.New("a frame carries exactly one of device, join or envelope")
	}
	return f, nil
}

type conn struct {
	ws     *websocket.Conn
	dialed bool
	addr   string
	wmu    sync.Mutex
	mu     sync.Mutex
	remote *wire.DeviceInfo // from its greeting; a hint until an envelope opens
	since  time.Time        // when it became a peer's connection
	once   sync.Once
}

func (c *conn) send(f frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return websocket.Message.Send(c.ws, string(b))
}

func (c *conn) close() { c.once.Do(func() { c.ws.Close() }) }

func (c *conn) greeting() *wire.DeviceInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.remote
}

// listener serves only /wire, as a WebSocket. Every other route, including
// all of the local API, is not found here.
func (n *Node) listener() http.Handler {
	mux := http.NewServeMux()
	ws := websocket.Server{
		// Browsers send their page's Origin: refuse any but our own peers,
		// which name this listener. The phone's app sends none.
		Handshake: func(cfg *websocket.Config, r *http.Request) error {
			if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
				return errors.New("origin refused")
			}
			return nil
		},
		Handler: func(ws *websocket.Conn) {
			ws.MaxPayloadBytes = maxFrame
			n.serve(&conn{ws: ws})
		},
	}
	mux.Handle("GET /wire", ws)
	return mux
}

func (n *Node) self() wire.DeviceInfo {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.members[n.ID()]
}

// serve reads frames from c until it closes.
func (n *Node) serve(c *conn) {
	n.mu.Lock()
	closing := n.isClosing
	if !closing {
		n.conns[c] = true
		n.wg.Add(1) // Close waits for every connection's handlers to finish
	}
	n.mu.Unlock()
	if closing {
		c.close()
		return
	}
	defer n.wg.Done()
	defer n.dropConn(c)
	if err := c.send(frame{Device: ptr(n.self())}); err != nil {
		return
	}
	for {
		var s string
		if err := websocket.Message.Receive(c.ws, &s); err != nil {
			return
		}
		f, err := decodeFrame([]byte(s))
		if err != nil {
			return
		}
		switch {
		case f.Device != nil:
			if !n.onGreeting(c, *f.Device) {
				return
			}
		case f.Join != nil:
			if c.dialed {
				return
			}
			n.onJoin(c, *f.Join)
		case f.Envelope != nil:
			n.onEnvelope(c, f.Envelope)
		}
	}
}

func ptr[T any](v T) *T { return &v }

// onGreeting records who c says it is. A connection this Engine dialled to
// a computer outside the set is closed, unless it is the one being paired
// with; one to a member is taken as that member's and greeted with hello.
func (n *Node) onGreeting(c *conn, d wire.DeviceInfo) bool {
	if wire.DeviceID(d.SignKey) != d.ID {
		return false
	}
	c.mu.Lock()
	first := c.remote == nil
	if first {
		c.remote = &d
	}
	c.mu.Unlock()
	if !first || !c.dialed {
		return true
	}
	if d.ID == n.ID() {
		n.mu.Lock()
		n.own[c.addr] = true // found itself on the network: never dial it again
		n.mu.Unlock()
		return false
	}
	n.mu.Lock()
	m, member := n.members[d.ID]
	pairingHere := n.pairing != nil && n.pairing.c == c
	n.mu.Unlock()
	if pairingHere {
		return true
	}
	if !member || !bytes.Equal(m.SignKey, d.SignKey) {
		n.backOff(c.addr)
		return false
	}
	// The greeting is only a hint: the connection becomes this member's
	// once an envelope from it opens. hello is sealed to the member, so a
	// stranger repeating its greeting learns nothing.
	n.sendHello(c, m)
	return true
}

func (n *Node) sendHello(c *conn, to wire.DeviceInfo) {
	n.mu.Lock()
	l := len(n.chain)
	n.mu.Unlock()
	n.sendOn(c, to, &wire.Hello{ChainLen: l})
}

// addPeer takes c as the connection to id. Only called once an envelope
// from id opened on c. When two computers dialled each other, both keep the
// connection dialled by the one with the smaller identifier, so they never
// each close the other's; otherwise a newer connection replaces an older
// one, which may be half-dead after sleep.
func (n *Node) addPeer(id string, c *conn) bool {
	n.mu.Lock()
	old, had := n.peers[id]
	// The tie-break applies only while both are fresh, as when the two
	// dialled each other at once; later a newer connection always wins.
	fresh := had && n.cfg.Now().Sub(old.since) < 5*time.Second
	if had && old != c && fresh && preferred(n.ID(), id, old) && !preferred(n.ID(), id, c) {
		n.mu.Unlock()
		return false
	}
	c.since = n.cfg.Now()
	n.peers[id] = c
	n.mu.Unlock()
	if had && old != c {
		old.close()
	}
	if !had || old != c {
		// A new connection, or one replacing another that may have taken
		// what was sent on it down with it: the Device is sent again what a
		// connecting Device gets. Snapshots are safe to repeat.
		n.peerChanged(id, true)
		go n.flush(id)
	}
	n.notify()
	return true
}

// preferred reports whether c was dialled by the smaller of self and peer.
func preferred(self, peer string, c *conn) bool {
	if c.dialed {
		return self < peer
	}
	return peer < self
}

func (n *Node) dropConn(c *conn) {
	c.close()
	n.mu.Lock()
	delete(n.conns, c)
	changed := false
	var gone []string
	for id, p := range n.peers {
		if p == c {
			delete(n.peers, id)
			changed = true
			gone = append(gone, id)
		}
	}
	for id, a := range n.waiting {
		if a.c == c {
			delete(n.waiting, id)
			changed = true
		}
	}
	if n.pairing != nil && n.pairing.c == c && !n.pairing.joined {
		n.pairing = nil
	}
	if c.addr != "" {
		delete(n.dialing, c.addr)
	}
	n.mu.Unlock()
	for _, id := range gone {
		n.peerChanged(id, false)
	}
	if changed {
		n.notify()
	}
}

// onJoin keeps a join request until a human approves it. Nothing is sent
// back meanwhile.
func (n *Node) onJoin(c *conn, j wire.JoinRequest) {
	d, err := j.Device()
	if err != nil {
		return
	}
	now := n.cfg.Now()
	if age := now.Sub(time.UnixMilli(j.Time)); age > joinFresh || age < -joinFresh {
		return
	}
	n.mu.Lock()
	_, member := n.members[d.ID]
	_, already := n.waiting[d.ID] // a replay on another connection does not take it over
	added := !member && !already && d.ID != n.ID() && len(n.waiting) < maxWaiting
	if added {
		n.waiting[d.ID] = &asking{req: j, info: d, c: c, at: now}
	}
	n.mu.Unlock()
	if added {
		n.notify()
	}
}

// joinFresh is how far a join request's time may be from this clock, and
// maxWaiting how many Devices may ask at once.
const (
	joinFresh  = 5 * time.Minute
	maxWaiting = 8
)

// onEnvelope opens e. The one envelope opened from outside the set is the
// chain that admits this Device, on the connection it asked to join on.
func (n *Node) onEnvelope(c *conn, e *wire.Envelope) bool {
	n.mu.Lock()
	p := n.pairing
	// A direct connection is still taken on Pong Cloud: while a set switches,
	// a Device not yet switched sends that way. Nothing is sent that way.
	pairingHere := p != nil && c != nil && p.c == c && !p.joined
	var target wire.DeviceInfo
	if pairingHere {
		target = p.target
	}
	set := n.members
	n.mu.Unlock()
	if pairingHere && e.From == target.ID {
		p := &pairing{target: target}
		m, err := n.opener.Open(e, wire.Members{target.ID: target}, n.cfg.Now())
		if err != nil {
			return false
		}
		cr, ok := m.(*wire.ChainRecords)
		if !ok {
			return false
		}
		members, err := cr.Records.Verify()
		if err != nil {
			return false
		}
		if t, ok := members[p.target.ID]; !ok || !bytes.Equal(t.SignKey, p.target.SignKey) {
			return false
		}
		if _, err := n.adopt(cr.Records, true); err != nil {
			return false
		}
		n.mu.Lock()
		if n.pairing != nil && n.pairing.c == c {
			n.pairing.joined = true
		}
		n.mu.Unlock()
		n.addPeer(target.ID, c)
		n.sendHello(c, target)
		n.notify()
		return true
	}
	m, err := n.opener.Open(e, set, n.cfg.Now())
	if err != nil {
		return false
	}
	n.mu.Lock()
	cur, ok := n.peers[e.From]
	n.mu.Unlock()
	if c != nil && (!ok || cur != c) {
		// When two connections race, both ends send on the same one, but
		// a message already on its way over the other is still taken.
		if n.addPeer(e.From, c) && !c.dialed {
			n.sendHello(c, set[e.From])
		}
	}
	n.mu.Lock()
	h := n.handlers[m.Kind()]
	_, still := n.members[e.From] // revoked while it was being opened
	n.mu.Unlock()
	if !still {
		return false
	}
	if h != nil {
		h(e.From, m)
	}
	return true
}

// dialLoop connects to every computer found that is not connected yet.
func (n *Node) dialLoop() {
	defer n.wg.Done()
	t := time.NewTicker(n.cfg.DialEvery)
	defer t.Stop()
	for {
		addrs := n.cfg.Discovery.Browse()
		if n.active() {
			addrs = nil // on Pong Cloud nothing goes over the network listener
		}
		for _, addr := range addrs {
			n.mu.Lock()
			busy := n.dialing[addr] || n.own[addr] || addr == n.NetAddr || n.cfg.Now().Before(n.refused[addr])
			if !busy {
				n.dialing[addr] = true
			}
			n.mu.Unlock()
			if busy {
				continue
			}
			go n.dial(addr)
		}
		select {
		case <-n.closing:
			return
		case <-t.C:
		}
	}
}

func (n *Node) dial(addr string) {
	c, err := n.connect(addr)
	if err != nil {
		n.mu.Lock()
		delete(n.dialing, addr)
		n.mu.Unlock()
		return
	}
	n.serve(c)
}

func (n *Node) connect(addr string) (*conn, error) {
	cfg, err := websocket.NewConfig("ws://"+addr+"/wire", "http://"+addr)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, err := cfg.DialContext(ctx)
	if err != nil {
		return nil, err
	}
	ws.MaxPayloadBytes = maxFrame
	return &conn{ws: ws, dialed: true, addr: addr}, nil
}

// Found is a computer on the network and who it says it is.
type Found struct {
	Address string          `json:"address"`
	Device  wire.DeviceInfo `json:"device"`
	InSet   bool            `json:"in_set"`
}

// Find asks every computer found on the network who it is.
func (n *Node) Find() []Found {
	var out []Found
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, addr := range n.cfg.Discovery.Browse() {
		n.mu.Lock()
		own := n.own[addr]
		n.mu.Unlock()
		if own || addr == n.NetAddr {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := n.connect(addr)
			if err != nil {
				return
			}
			defer c.close()
			c.ws.SetReadDeadline(time.Now().Add(3 * time.Second))
			var s string
			if websocket.Message.Receive(c.ws, &s) != nil {
				return
			}
			f, err := decodeFrame([]byte(s))
			if err != nil || f.Device == nil || f.Device.ID == n.ID() {
				return
			}
			n.mu.Lock()
			_, in := n.members[f.Device.ID]
			n.mu.Unlock()
			mu.Lock()
			out = append(out, Found{Address: addr, Device: *f.Device, InSet: in})
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

// ErrInASet is returned when this Device already shares a set with others.
var ErrInASet = errors.New("this computer is already in a set with other Devices; remove it from that set first")

// Pair asks the computer at addr to admit this Device and returns the code
// both show. The set changes when a human approves it there.
func (n *Node) Pair(addr string) (code string, target wire.DeviceInfo, err error) {
	n.mu.Lock()
	alone := len(n.members) == 1
	n.mu.Unlock()
	if !alone {
		return "", target, ErrInASet
	}
	c, err := n.connect(addr)
	if err != nil {
		return "", target, err
	}
	c.ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var s string
	if err := websocket.Message.Receive(c.ws, &s); err != nil {
		c.close()
		return "", target, err
	}
	c.ws.SetReadDeadline(time.Time{})
	f, err := decodeFrame([]byte(s))
	if err != nil || f.Device == nil || wire.DeviceID(f.Device.SignKey) != f.Device.ID || f.Device.ID == n.ID() {
		c.close()
		return "", target, fmt.Errorf("%s is not a Pong Engine", addr)
	}
	c.mu.Lock()
	c.remote = f.Device
	c.mu.Unlock()
	self := n.self()
	j := wire.NewJoinRequest(n.keys, self.Name, self.Platform, self.Engine, n.cfg.Now().UnixMilli())
	n.mu.Lock()
	if len(n.members) != 1 {
		n.mu.Unlock()
		c.close()
		return "", target, ErrInASet
	}
	if n.pairing != nil && n.pairing.c != nil {
		n.pairing.c.close()
	}
	n.pairing = &pairing{c: c, target: *f.Device, code: j.Code()}
	n.dialing[addr] = true
	c.addr = addr
	n.mu.Unlock()
	go n.serve(c) // greets first
	if err := c.send(frame{Join: &j}); err != nil {
		c.close()
		return "", target, err
	}
	return j.Code(), *f.Device, nil
}

// ErrNotWaiting is returned when approving a Device that is not asking.
var ErrNotWaiting = errors.New("no Device with that identifier is asking to join")

// Approve admits a waiting Device: it writes an admit record and sends the
// whole chain to the new Device and to every connected Device in the set.
func (n *Node) Approve(id string) error {
	n.mu.Lock()
	a, ok := n.waiting[id]
	if !ok {
		n.mu.Unlock()
		return ErrNotWaiting
	}
	if len(a.req.Set) > 0 && !bytes.Equal(a.req.Set, n.chain[0].Hash()) {
		n.mu.Unlock()
		return ErrOtherSet
	}
	next, err := n.chain.Append(n.keys, wire.Admit, a.info, n.cfg.Now().UnixMilli())
	if err != nil {
		n.mu.Unlock()
		return err
	}
	n.chain = next
	n.members, _ = next.Verify()
	n.refused = map[string]time.Time{}
	delete(n.waiting, id)
	err = n.saveChain()
	c := append(wire.Chain{}, next...)
	n.mu.Unlock()
	if err != nil {
		return err
	}
	if a.c != nil {
		n.sendOn(a.c, a.info, &wire.ChainRecords{Records: c}) // it says hello back, which makes the connection its
	}
	n.Broadcast(&wire.ChainRecords{Records: c}, id)
	n.chainChanged() // a Device asking through a carrier may fetch it there
	n.notify()
	return nil
}

// backOff stops dialling addr for a while: it answered as a computer
// outside the set.
func (n *Node) backOff(addr string) {
	if addr == "" {
		return
	}
	n.mu.Lock()
	n.refused[addr] = n.cfg.Now().Add(30 * time.Second)
	n.mu.Unlock()
}

// ErrNotMember is returned when revoking a Device that is not in the set.
var ErrNotMember = errors.New("no Device with that identifier is in the set")

// ErrRevokeSelf is returned when this Device is asked to revoke itself.
var ErrRevokeSelf = errors.New("a Device cannot revoke itself; revoke it from another Device")

// Revoke writes a revoke record for a Device in the set and sends the chain
// to every other connected Device. The revoked Device is sent nothing more:
// its connection is closed and its envelopes no longer open.
func (n *Node) Revoke(id string) error {
	if id == n.ID() {
		return ErrRevokeSelf
	}
	n.mu.Lock()
	d, ok := n.members[id]
	if !ok {
		n.mu.Unlock()
		return ErrNotMember
	}
	next, err := n.chain.Append(n.keys, wire.Revoke, d, n.cfg.Now().UnixMilli())
	if err != nil {
		n.mu.Unlock()
		return err
	}
	n.chain = next
	n.members, _ = next.Verify()
	kept := n.outbox[:0:0]
	for _, q := range n.outbox {
		if q.To != id {
			kept = append(kept, q)
		}
	}
	n.outbox = kept
	n.saveOutbox()
	if c, ok := n.peers[id]; ok {
		c.close()
		delete(n.peers, id)
	}
	err = n.saveChain()
	n.mu.Unlock()
	if err != nil {
		return err
	}
	n.Broadcast(&wire.ChainRecords{Records: next}, id)
	n.chainChanged()
	n.notify()
	return nil
}

// ErrNoOneToTell is returned when this Device leaves the set while no other
// Device is connected to receive the record.
var ErrNoOneToTell = errors.New("no other Device in the set can be reached to take this computer out")

// Leave takes this Device out of the set: it writes a revoke record about
// itself and sends it to every connected Device, which pass it on. It
// returns how many Devices it told. A set of one has nothing to leave.
func (n *Node) Leave() (int, error) {
	n.mu.Lock()
	if len(n.members) == 1 {
		n.mu.Unlock()
		return 0, nil
	}
	self := n.members[n.ID()]
	next, err := n.chain.Append(n.keys, wire.Revoke, self, n.cfg.Now().UnixMilli())
	if err != nil {
		n.mu.Unlock()
		return 0, err
	}
	n.chain = next // under the lock, so no admit or revoke here forks from it
	var peers []*conn
	var infos []wire.DeviceInfo
	for id, c := range n.peers {
		peers = append(peers, c)
		infos = append(infos, n.members[id])
	}
	n.mu.Unlock()
	told := 0
	if n.active() {
		told += n.carrier.Leave(next)
	}
	for i, c := range peers {
		if n.sendOn(c, infos[i], &wire.ChainRecords{Records: next}) == nil {
			told++
		}
	}
	if told == 0 {
		return 0, ErrNoOneToTell
	}
	time.Sleep(500 * time.Millisecond) // let the record leave before the Engine is stopped
	return told, nil
}

// ErrOtherSet is returned when approving a request made for another set: the
// relay showed the new Device a set that is not this one.
var ErrOtherSet = errors.New("this Device asked to join a set that is not yours: Pong's server may have shown it another set; do not approve it")

// askingCode is the code to show for a request: through the relay, computed
// with this Device's own genesis, so a request made for another set shows a
// code that differs from the new Device's.
func (n *Node) askingCode(j wire.JoinRequest) string {
	if len(j.Set) > 0 && len(n.chain) > 0 {
		j.Set = n.chain[0].Hash()
	}
	return j.Code()
}
