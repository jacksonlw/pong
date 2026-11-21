// Package devices is how one Engine takes part in the human's set of
// Devices on Local: it keeps this Device's keys and the set's
// chain, listens on the network for envelopes and join requests, connects
// to every other computer in the set it finds, and pairs new Devices.
package devices

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jacksonlw/pong/wire"
)

// DefaultNetPort is the network port an Engine tries first.
const DefaultNetPort = 47830

// Config says how this Device takes part in the set.
type Config struct {
	Dir      string        // the Engine's data directory; the chain lives in Dir/devices
	Keys     wire.KeyStore // nil keeps the keys in a file in Dir/devices
	Name     string        // this computer's name
	Platform string        // wire.PlatformMac or wire.PlatformLinux
	// NetAddr is where the network listener listens, e.g. ":47830"; if
	// that port is taken a free one is used.
	NetAddr   string
	Discovery Discovery
	// DialEvery is how often found computers are connected to; 0 means 2s.
	DialEvery time.Duration
	Now       func() time.Time
	// Carrier takes the set's messages between Devices that share no
	// network; nil: Local only.
	Carrier Carrier
}

// Node is this Device in the set.
type Node struct {
	cfg    Config
	keys   *wire.Keys
	opener *wire.Opener
	srv    *http.Server
	ln     net.Listener
	// NetAddr is the address the network listener is on.
	NetAddr string

	mu        sync.Mutex
	chain     wire.Chain
	members   wire.Members
	peers     map[string]*conn // by Device identifier, once an envelope from it opened or its greeting matched the chain
	waiting   map[string]*asking
	pairing   *pairing
	fork      *heldFork            // a fork only the human can settle, while it waits
	suspended map[string]bool      // Devices whose envelopes wait for the human: they revoked a Device of this chain as it revoked them
	dialing   map[string]bool      // addresses being dialled or connected
	own       map[string]bool      // addresses found to be this Engine
	refused   map[string]time.Time // addresses of computers outside the set, not dialled until then
	conns     map[*conn]bool
	isClosing bool
	handlers  map[string]Handler
	changed   []func()
	peerFns   []func(id string, up bool)

	carrier Carrier

	outbox  []Queued
	flushMu sync.Mutex // one flush at a time keeps the order

	closing chan struct{}
	wg      sync.WaitGroup
}

// Handler handles one kind of message from a Device in the set.
type Handler func(from string, m wire.Message)

type asking struct {
	req    wire.JoinRequest
	info   wire.DeviceInfo
	c      *conn
	at     time.Time
	answer *wire.PairAnswer // this Device's side of the code; it has one once the Device gave its value
}

// pairing is this Device's own request to join the set of target. Nothing of
// that set is taken before the human said here that the codes match: a chain
// that comes sooner is held.
type pairing struct {
	c         *conn
	target    wire.DeviceInfo
	ask       *wire.PairAsk // nil until target committed to its value
	confirmed bool
	held      *wire.Envelope
	joined    bool
}

func (p *pairing) code() string {
	if p.ask == nil {
		return ""
	}
	return p.ask.Code()
}

// Start loads or creates this Device's keys and chain, starts the network
// listener and advertises it, and connects to the set.
func Start(cfg Config) (*Node, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.DialEvery == 0 {
		cfg.DialEvery = 2 * time.Second
	}
	if cfg.Discovery == nil {
		cfg.Discovery = Static(nil)
	}
	dir := filepath.Join(cfg.Dir, "devices")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if cfg.Keys == nil {
		cfg.Keys = wire.FileKeyStore{Path: filepath.Join(dir, "keys.json")}
	}
	keys, err := wire.LoadOrCreate(cfg.Keys)
	if err != nil {
		return nil, err
	}
	opener, err := wire.NewPersistentOpener(keys, filepath.Join(dir, "seen.json"))
	if err != nil {
		return nil, err
	}
	n := &Node{cfg: cfg, keys: keys, opener: opener, peers: map[string]*conn{}, waiting: map[string]*asking{},
		dialing: map[string]bool{}, own: map[string]bool{}, refused: map[string]time.Time{}, conns: map[*conn]bool{}, handlers: map[string]Handler{}, closing: make(chan struct{})}
	if err := n.loadChain(); err != nil {
		return nil, err
	}
	if err := n.loadOutbox(); err != nil {
		return nil, err
	}
	n.handlers["hello"] = n.onHello
	n.handlers["chain"] = n.onChain
	n.handlers["ack"] = n.onAck

	ln, err := net.Listen("tcp", cfg.NetAddr)
	if err != nil {
		host, _, _ := net.SplitHostPort(cfg.NetAddr)
		if ln, err = net.Listen("tcp", net.JoinHostPort(host, "0")); err != nil {
			return nil, err
		}
	}
	n.ln = ln
	n.NetAddr = ln.Addr().String()
	n.srv = &http.Server{Handler: n.listener(), ReadHeaderTimeout: 10 * time.Second}
	n.wg.Add(1)
	go func() { defer n.wg.Done(); n.srv.Serve(ln) }()
	if err := cfg.Discovery.Advertise(keys.ID(), ln.Addr().(*net.TCPAddr).Port); err != nil {
		fmt.Fprintln(os.Stderr, "pong-engine: advertising on the network:", err)
	}
	n.wg.Add(1)
	go n.dialLoop()
	n.Go(func() { n.resendEvery(10 * time.Second) })
	if cfg.Carrier != nil {
		n.carrier = cfg.Carrier
		if err := n.carrier.Start(n); err != nil {
			n.Close()
			return nil, err
		}
	}
	return n, nil
}

// Close stops listening and drops every connection.
func (n *Node) Close() error {
	close(n.closing)
	n.cfg.Discovery.Close()
	err := n.srv.Close()
	n.mu.Lock()
	n.isClosing = true
	for c := range n.conns {
		c.close()
	}
	n.mu.Unlock()
	n.wg.Wait()
	return err
}

// ID is this Device's identifier.
func (n *Node) ID() string { return n.keys.ID() }

// Handle registers h for messages of kind.
func (n *Node) Handle(kind string, h Handler) {
	n.mu.Lock()
	n.handlers[kind] = h
	n.mu.Unlock()
}

// OnChange registers f to run after the set or a Device's reachability
// changes.
func (n *Node) OnChange(f func()) {
	n.mu.Lock()
	n.changed = append(n.changed, f)
	n.mu.Unlock()
}

// OnPeer registers f to run when a Device in the set connects (up, once an
// envelope from it opened) or its connection drops.
func (n *Node) OnPeer(f func(id string, up bool)) {
	n.mu.Lock()
	n.peerFns = append(n.peerFns, f)
	n.mu.Unlock()
}

func (n *Node) peerChanged(id string, up bool) {
	n.mu.Lock()
	fs := append([]func(string, bool){}, n.peerFns...)
	n.mu.Unlock()
	for _, f := range fs {
		f(id, up)
	}
}

// Member returns a Device in the set and whether it is reachable now.
func (n *Node) Member(id string) (d wire.DeviceInfo, reachable, ok bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	d, ok = n.members[id]
	_, reachable = n.peers[id]
	reachable = reachable || n.active() && n.carrier.Reachable(id)
	return d, reachable || id == n.ID(), ok
}

// Computers counts the computers (Devices running an Engine) in the set.
func (n *Node) Computers() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	c := 0
	for _, d := range n.members {
		if d.Engine {
			c++
		}
	}
	return c
}

// Peers lists the Devices connected now.
func (n *Node) Peers() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]string, 0, len(n.peers))
	for id := range n.peers {
		out = append(out, id)
	}
	return out
}

func (n *Node) notify() {
	n.mu.Lock()
	fs := append([]func(){}, n.changed...)
	n.mu.Unlock()
	for _, f := range fs {
		f()
	}
}

func (n *Node) chainPath() string { return filepath.Join(n.cfg.Dir, "devices", "chain.json") }

// loadChain reads the chain, or starts a set of one with genesis.
func (n *Node) loadChain() error {
	b, err := os.ReadFile(n.chainPath())
	if errors.Is(err, fs.ErrNotExist) {
		n.chain = wire.NewChain(n.keys, n.cfg.Name, n.cfg.Platform, true, n.cfg.Now().UnixMilli())
		n.members, _ = n.chain.Verify()
		return n.saveChain()
	}
	if err != nil {
		return err
	}
	var c wire.Chain
	if err := json.Unmarshal(b, &c); err != nil {
		return fmt.Errorf("devices: %s: %w", n.chainPath(), err)
	}
	m, err := c.Verify()
	if err != nil {
		return fmt.Errorf("devices: %s: %w", n.chainPath(), err)
	}
	n.chain, n.members = c, m
	return nil
}

// saveChain writes the chain; n.mu is held or the Node is starting.
func (n *Node) saveChain() error {
	b, _ := json.MarshalIndent(n.chain, "", "  ")
	tmp := n.chainPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, n.chainPath())
}

// heldFork is a chain that forks from this one in a way only the human can
// settle (wire.Fork): who wrote each side, and the offered chain to take if
// the human picks it.
type heldFork struct {
	fork  *wire.Fork
	offer wire.Chain
}

// adopt takes c as the chain if it is valid and extends the one held, wins a
// fork against it (wire.Chain.Extend), or replaces a set of one when pairing.
// It reports whether the chain changed. A fork only the human can settle is
// kept for them (Set's Fork) and returned as the error.
func (n *Node) adopt(c wire.Chain, replacingAlone bool) (bool, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	var next wire.Chain
	if replacingAlone {
		if _, err := c.Verify(); err != nil {
			return false, err
		}
		next = c
	} else {
		var err error
		if next, err = n.chain.Extend(c); err != nil {
			var f *wire.Fork
			if errors.As(err, &f) && f.Pick == "" && (n.fork == nil || n.fork.fork.At >= f.At) {
				n.fork = &heldFork{fork: f, offer: c}
				n.suspended = map[string]bool{}
				if f.Mutual {
					for _, id := range f.Ours {
						if id != n.ID() {
							n.suspended[id] = true
						}
					}
				}
				go n.notify()
			}
			return false, err
		}
		if bytes.Equal(next[len(next)-1].Hash(), n.chain[len(n.chain)-1].Hash()) {
			return false, nil
		}
	}
	m, _ := next.Verify()
	if _, in := m[n.ID()]; !in {
		return false, errors.New("devices: this Device is not in the offered set")
	}
	n.chain, n.members = next, m
	n.refused = map[string]time.Time{} // a stranger may be a member now
	for id, c := range n.peers {
		if _, ok := m[id]; !ok {
			c.close()
			delete(n.peers, id)
		}
	}
	return true, n.saveChain()
}

// Send seals m to the Device id and sends it if that Device is connected.
// It reports whether it was sent.
func (n *Node) Send(id string, m wire.Message) bool {
	if n.active() {
		n.mu.Lock()
		info, member := n.members[id]
		n.mu.Unlock()
		return member && n.carrier.Send(info, m)
	}
	n.mu.Lock()
	c, ok := n.peers[id]
	info, member := n.members[id]
	n.mu.Unlock()
	if !ok || !member {
		return false
	}
	return n.sendOn(c, info, m) == nil
}

// Broadcast sends m to every connected Device in the set but skip.
func (n *Node) Broadcast(m wire.Message, skip string) {
	cloud := n.active()
	n.mu.Lock()
	ids := make([]string, 0, len(n.members))
	if cloud {
		for id := range n.members {
			if id != skip && id != n.ID() {
				ids = append(ids, id)
			}
		}
	} else {
		for id := range n.peers {
			if id != skip {
				ids = append(ids, id)
			}
		}
	}
	n.mu.Unlock()
	for _, id := range ids {
		n.Send(id, m)
	}
}

func (n *Node) sendOn(c *conn, to wire.DeviceInfo, m wire.Message) error {
	notify := false
	e, err := wire.Seal(n.keys, to, m, notify, n.cfg.Now())
	if err != nil {
		return err
	}
	return c.send(frame{Envelope: e})
}

func (n *Node) onHello(from string, m wire.Message) {
	h := m.(*wire.Hello)
	n.mu.Lock()
	// A longer chain is sent; so is one of the same length that differs, a
	// fork the two must settle.
	differs := len(n.chain) == h.ChainLen && len(h.Head) > 0 && !bytes.Equal(h.Head, n.chain[len(n.chain)-1].Hash())
	send := len(n.chain) > h.ChainLen || differs
	c := append(wire.Chain{}, n.chain...)
	n.mu.Unlock()
	if send {
		n.Send(from, &wire.ChainRecords{Records: c})
	}
}

func (n *Node) onChain(from string, m wire.Message) {
	changed, err := n.adopt(m.(*wire.ChainRecords).Records, false)
	if err != nil {
		var f *wire.Fork
		if errors.As(err, &f) && f.Pick == "ours" {
			// This chain revokes the writers of the one offered: the sender takes it.
			n.mu.Lock()
			c := append(wire.Chain{}, n.chain...)
			n.mu.Unlock()
			n.Send(from, &wire.ChainRecords{Records: c})
			return
		}
		fmt.Fprintln(os.Stderr, "pong-engine: chain from", from, "refused:", err)
		return
	}
	if changed {
		n.mu.Lock()
		c := append(wire.Chain{}, n.chain...)
		n.mu.Unlock()
		n.Broadcast(&wire.ChainRecords{Records: c}, from)
		n.chainChanged()
		n.notify()
	}
}

// ErrNoFork is returned when settling a fork while none waits.
var ErrNoFork = errors.New("no fork waits for you to settle")

// SettleFork takes the human's word on a fork only they can settle: keep
// this chain ("ours"), or take the one offered ("theirs"). Either way every
// connected Device is sent the chain that stands, and suspended Devices are
// heard again.
func (n *Node) SettleFork(keep string) error {
	n.mu.Lock()
	hf := n.fork
	if hf == nil {
		n.mu.Unlock()
		return ErrNoFork
	}
	switch keep {
	case "ours":
	case "theirs":
		m, err := hf.offer.Verify()
		if err != nil {
			n.mu.Unlock()
			return err
		}
		if _, in := m[n.ID()]; !in {
			n.mu.Unlock()
			return errors.New("devices: the other chain leaves this Device out: keep this one, or pair this Device again")
		}
		n.chain, n.members = append(wire.Chain{}, hf.offer...), m
		for id, c := range n.peers {
			if _, ok := m[id]; !ok {
				c.close()
				delete(n.peers, id)
			}
		}
		if err := n.saveChain(); err != nil {
			n.mu.Unlock()
			return err
		}
	default:
		n.mu.Unlock()
		return errors.New(`devices: keep "ours" or "theirs"`)
	}
	n.fork, n.suspended = nil, nil
	c := append(wire.Chain{}, n.chain...)
	n.mu.Unlock()
	n.Broadcast(&wire.ChainRecords{Records: c}, "")
	n.chainChanged()
	n.notify()
	return nil
}

// active reports whether a carrier takes every message now.
func (n *Node) active() bool { return n.carrier != nil && n.carrier.Active() }

// chainChanged tells the carrier the chain after it changed.
func (n *Node) chainChanged() {
	if n.carrier != nil {
		n.carrier.ChainChanged(n.Chain())
	}
}

// DefaultName is this computer's name as the human knows it.
func DefaultName() string {
	h, _ := os.Hostname()
	h = strings.TrimSuffix(h, ".local")
	if h == "" {
		return "Computer"
	}
	return h
}
