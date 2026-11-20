package devices

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/jacksonlw/pong/wire"
)

// Queued is a message waiting in the outbox for a Device that is not
// connected. It is sealed only when it goes, so it is never stale.
type Queued struct {
	Key  string          `json:"key"` // the sender's name for it, to find or take it back
	To   string          `json:"to"`
	Kind string          `json:"kind"`
	Body json.RawMessage `json:"body"` // the message as wire.Encode writes it
}

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

// SendOrQueue sends m to the Device id now if it is connected and nothing
// queued for it is still waiting to go, or keeps it in the outbox, across
// restarts, until the two meet; messages for one Device go in the order they
// were given. It reports whether it went now. A message queued under a key
// replaces one queued under the same key.
func (n *Node) SendOrQueue(id, key string, m wire.Message) (bool, error) {
	n.mu.Lock()
	queued := false
	for _, q := range n.outbox {
		queued = queued || q.To == id
	}
	n.mu.Unlock()
	if !queued && n.Send(id, m) {
		return true, nil
	}
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
		if q.Key != key {
			out = append(out, q)
		}
	}
	n.outbox = append(out, Queued{Key: key, To: id, Kind: m.Kind(), Body: body})
	err = n.saveOutbox()
	_, connected := n.peers[id]
	n.mu.Unlock()
	if connected {
		go n.flush(id) // it connected meanwhile
	}
	return false, err
}

// Unqueue takes back a message still waiting in the outbox, and reports
// whether it was there. Once flush has taken a message to send it, it is no
// longer there.
func (n *Node) Unqueue(key string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, q := range n.outbox {
		if q.Key == key {
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

// flush sends everything waiting for id, in order, now that it is
// connected. Each message leaves the outbox before it is sent, so it cannot
// also be taken back, and returns to the front if the send fails.
func (n *Node) flush(id string) {
	n.flushMu.Lock()
	defer n.flushMu.Unlock()
	for {
		n.mu.Lock()
		i := -1
		for j, q := range n.outbox {
			if q.To == id {
				i = j
				break
			}
		}
		if i < 0 {
			n.mu.Unlock()
			return
		}
		q := n.outbox[i]
		n.outbox = append(n.outbox[:i:i], n.outbox[i+1:]...)
		n.saveOutbox()
		n.mu.Unlock()
		m, err := wire.Decode(q.Body)
		if err != nil {
			continue // written by an older version and unreadable: dropped
		}
		if !n.Send(id, m) {
			n.mu.Lock()
			n.outbox = append([]Queued{q}, n.outbox...)
			n.saveOutbox()
			n.mu.Unlock()
			return
		}
	}
}
