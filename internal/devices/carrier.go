package devices

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/jacksonlw/pong/wire"
)

// A Carrier takes the set's messages between Devices that share no network.
// Without one, Devices meet only on Local. While a carrier is active every
// message goes through it and the direct connections stand aside.
type Carrier interface {
	// Start runs once the Node is ready. A carrier keeps its own state in
	// n.Dir() and runs its loops with n.Go.
	Start(n *Node) error
	// Active reports whether every message goes through the carrier now.
	Active() bool
	// Reachable reports whether the carrier can reach the Device id now.
	Reachable(id string) bool
	// Send seals m to a Device and hands it on, reporting whether it went.
	Send(to wire.DeviceInfo, m wire.Message) bool
	// ChainChanged is told the chain after it changed.
	ChainChanged(c wire.Chain)
	// Leave hands on this Device's revoke of itself and returns how many
	// Devices it told.
	Leave(c wire.Chain) int
	// Status adds what the carrier knows to the set's status.
	Status(s *Set)
	// Routes adds the carrier's own routes to the local API.
	Routes(mux *http.ServeMux)
}

// What a carrier may use of the Node. These are exported for carriers in
// this module only: the package is internal.

// Keys are this Device's keys.
func (n *Node) Keys() *wire.Keys { return n.keys }

// Dir is the Node's own directory, Dir/devices.
func (n *Node) Dir() string { return filepath.Join(n.cfg.Dir, "devices") }

// Now is the Node's clock.
func (n *Node) Now() time.Time { return n.cfg.Now() }

// Self is this Device as the set knows it.
func (n *Node) Self() wire.DeviceInfo { return n.self() }

// Closing is closed when the Node closes.
func (n *Node) Closing() <-chan struct{} { return n.closing }

// Go runs f in the background; Close waits for it.
func (n *Node) Go(f func()) {
	n.wg.Add(1)
	go func() { defer n.wg.Done(); f() }()
}

// Members returns the set's Devices.
func (n *Node) Members() wire.Members {
	n.mu.Lock()
	defer n.mu.Unlock()
	m := wire.Members{}
	for id, d := range n.members {
		m[id] = d
	}
	return m
}

// Chain returns the set's chain.
func (n *Node) Chain() wire.Chain {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append(wire.Chain{}, n.chain...)
}

// Adopt takes c as the chain, as when a carrier admits this Device.
func (n *Node) Adopt(c wire.Chain, replacingAlone bool) (bool, error) {
	return n.adopt(c, replacingAlone)
}

// DropPeers closes every direct connection, as a carrier turns active, and
// returns the Devices they were to.
func (n *Node) DropPeers() []string {
	n.mu.Lock()
	var gone []string
	for id, c := range n.peers {
		c.close()
		delete(n.peers, id)
		gone = append(gone, id)
	}
	n.mu.Unlock()
	return gone
}

// PeerChanged tells everything that waits on it that a Device connected
// (up) or dropped, and sends a connecting Device what waits for it.
func (n *Node) PeerChanged(id string, up bool) {
	n.peerChanged(id, up)
	if up {
		go n.flush(id)
	}
}

// Rejoined sends again what a connecting Device gets to every Device on a
// direct connection, as a carrier stands aside.
func (n *Node) Rejoined() {
	n.mu.Lock()
	var lan []string
	for id := range n.peers {
		lan = append(lan, id)
	}
	n.mu.Unlock()
	for _, id := range lan {
		n.PeerChanged(id, true)
	}
}

// Deliver opens an envelope the carrier brought, and reports whether it
// opened from a Device in the set.
func (n *Node) Deliver(e *wire.Envelope) bool { return n.onEnvelope(nil, e) }

// Asked takes a join request the carrier brought.
func (n *Node) Asked(j wire.JoinRequest) { n.onJoin(nil, j) }

// Notify tells everything that waits on it that the set changed.
func (n *Node) Notify() { n.notify() }
