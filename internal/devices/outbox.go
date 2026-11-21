package devices

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/jacksonlw/pong/wire"
)

// Queued is a message waiting in the outbox for a Device. It is sealed only
// when it goes, so it is never stale, and it stays until the Device
// acknowledges it (wire.Ack), or a carrier takes it: a message written to a
// connection that dies, or to one that is not the Device's, is sent again.
type Queued struct {
	Key  string          `json:"key"` // the sender's name for it, to find or take it back
	To   string          `json:"to"`
	Kind string          `json:"kind"`
	Body json.RawMessage `json:"body"` // the message as wire.Encode writes it
	// Sent is the envelope it last went in, and SentAt when; empty until it
	// first goes.
	Sent   string    `json:"sent,omitempty"`
	SentAt time.Time `json:"sent_at,omitempty"`
}

// resendAfter is how long a message sent and not acknowledged waits before
// it goes again.
const resendAfter = 30 * time.Second

func (n *Node) outboxPath() string { return filepath.Join(n.cfg.Dir, "devices", "outbox.json") }

// loadOutbox reads the outbox at start; n.mu is held or the Node is starting.
func (n *Node) loadOutbox() error {
	b, err := os.ReadFile(n.outboxPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return json.Unmarshal(b, &n.outbox)
}

// saveOutbox writes the outbox; n.mu is held. The file is readable only by
// the human's user, like the keys.
func (n *Node) saveOutbox() error {
	b, _ := json.Marshal(n.outbox)
	tmp := n.outboxPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, n.outboxPath())
}

// SendOrQueue keeps m in the outbox for the Device id, across restarts,
// until the Device acknowledges it, and sends it now if the Device is
// connected and nothing queued before it is still waiting to go. Messages
// for one Device go in the order they were given. It reports whether it
// went now. A message queued under a key replaces one not yet sent under
// the same key.
func (n *Node) SendOrQueue(id, key string, m wire.Message) (bool, error) {
	body, err := wire.Encode(m)
	if err != nil {
		return false, err
	}
	n.mu.Lock()
	if _, ok := n.members[id]; !ok {
		n.mu.Unlock()
		return false, ErrNotMember
	}
	out := n.outbox[:0:0]
	for _, q := range n.outbox {
		if q.Key != key || q.Sent != "" {
			out = append(out, q)
		}
	}
	n.outbox = append(out, Queued{Key: key, To: id, Kind: m.Kind(), Body: body})
	err = n.saveOutbox()
	n.mu.Unlock()
	if err != nil {
		return false, err
	}
	n.flush(id)
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, q := range n.outbox {
		if q.Key == key && q.To == id {
			return q.Sent != "", nil
		}
	}
	return true, nil // gone already: acknowledged, or taken by a carrier
}

// Unqueue takes back a message still waiting in the outbox, and reports
// whether it was there. One already sent cannot be taken back: the Device
// may have it.
func (n *Node) Unqueue(key string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, q := range n.outbox {
		if q.Key == key {
			if q.Sent != "" {
				return false
			}
			before := n.outbox
			n.outbox = append(n.outbox[:i:i], n.outbox[i+1:]...)
			if n.saveOutbox() != nil {
				n.outbox = before
				return false
			}
			return true
		}
	}
	return false
}

// Outbox lists what waits in the outbox.
func (n *Node) Outbox() []Queued {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]Queued{}, n.outbox...)
}

// flush sends what waits for id, in order: each message not yet sent, or
// sent longer than resendAfter ago and not acknowledged. Through a carrier
// a message leaves the outbox once the carrier takes it, since the carrier
// keeps it until the Device has it.
func (n *Node) flush(id string) {
	n.flushMu.Lock()
	defer n.flushMu.Unlock()
	now := n.cfg.Now()
	n.mu.Lock()
	var due []Queued
	for _, q := range n.outbox {
		if q.To == id && (q.Sent == "" || now.Sub(q.SentAt) >= resendAfter) {
			due = append(due, q)
		}
	}
	n.mu.Unlock()
	for _, q := range due {
		m, err := wire.Decode(q.Body)
		if err != nil {
			n.dropQueued(q.Key, id, "") // written by an older version and unreadable
			continue
		}
		envID, carried, ok := n.sendQueued(id, m)
		if !ok {
			return // the rest wait, in order
		}
		n.mu.Lock()
		for j := range n.outbox {
			if n.outbox[j].Key == q.Key && n.outbox[j].To == id && n.outbox[j].Sent == q.Sent {
				if carried {
					n.outbox = append(n.outbox[:j:j], n.outbox[j+1:]...)
				} else {
					n.outbox[j].Sent, n.outbox[j].SentAt = envID, now
				}
				break
			}
		}
		n.saveOutbox()
		n.mu.Unlock()
	}
}

// sendQueued sends m to id and returns the envelope it went in, and whether
// a carrier took it.
func (n *Node) sendQueued(id string, m wire.Message) (envID string, carried, ok bool) {
	n.mu.Lock()
	info, member := n.members[id]
	c, connected := n.peers[id]
	n.mu.Unlock()
	if !member {
		return "", false, false
	}
	if n.active() {
		return "", true, n.carrier.Send(info, m)
	}
	if !connected {
		return "", false, false
	}
	e, err := wire.Seal(n.keys, info, m, false, n.cfg.Now())
	if err != nil || c.send(frame{Envelope: e}) != nil {
		return "", false, false
	}
	return e.ID, false, true
}

// dropQueued takes a message out of the outbox: the one under key for to,
// or the one sent in envelope envID when key is empty.
func (n *Node) dropQueued(key, to, envID string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for j, q := range n.outbox {
		if q.To == to && (key != "" && q.Key == key || key == "" && envID != "" && q.Sent == envID) {
			n.outbox = append(n.outbox[:j:j], n.outbox[j+1:]...)
			n.saveOutbox()
			return
		}
	}
}

// onAck lets go of what the Device acknowledged.
func (n *Node) onAck(from string, m wire.Message) {
	for _, id := range m.(*wire.Ack).IDs {
		n.dropQueued("", from, id)
	}
}

// resendEvery sends again, now and then, what connected Devices have not
// acknowledged, until the Node closes.
func (n *Node) resendEvery(every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-n.closing:
			return
		}
		n.mu.Lock()
		var ids []string
		seen := map[string]bool{}
		for _, q := range n.outbox {
			if !seen[q.To] {
				seen[q.To] = true
				ids = append(ids, q.To)
			}
		}
		n.mu.Unlock()
		for _, id := range ids {
			n.flush(id)
		}
	}
}
