package wire

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hpke"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"
)

// Version is the envelope version this package writes and reads.
const Version = 1

// ReplayWindow is how long a recipient remembers envelope identifiers. An
// envelope whose time is further than this from the recipient's clock is
// dropped too, so a replay older than the memory cannot get through.
const ReplayWindow = 24 * time.Hour

// Envelope is what a carrier moves. Everything but Body is readable by the
// carrier; Body is sealed to the recipient.
type Envelope struct {
	V      int    `json:"v"`
	ID     string `json:"id"`   // 16 random bytes, lowercase hex
	From   string `json:"from"` // sender's Device identifier
	To     string `json:"to"`   // recipient's Device identifier
	Time   int64  `json:"time"` // Unix milliseconds
	Notify bool   `json:"notify"`
	Body   []byte `json:"body"` // HPKE enc (32 bytes) || ciphertext
	Sig    []byte `json:"sig"`
}

func (e *Envelope) signedBytes() []byte {
	var b buf
	b.raw([]byte("pong-envelope-v1\x00"))
	b.u8(uint8(e.V))
	b.str(e.ID)
	b.str(e.From)
	b.str(e.To)
	b.i64(e.Time)
	b.boolean(e.Notify)
	b.bytes(e.Body)
	return b
}

// hpkeInfo is the HPKE info: "pong-envelope-v1\x00", the version byte, then
// the sender's and recipient's identifiers as length-prefixed strings.
func hpkeInfo(v int, from, to string) []byte {
	var b buf
	b.raw([]byte("pong-envelope-v1\x00"))
	b.u8(uint8(v))
	b.str(from)
	b.str(to)
	return b
}

var (
	suiteKDF  = hpke.HKDFSHA256()
	suiteAEAD = hpke.ChaCha20Poly1305()
)

// Seal seals msg to the recipient and signs the envelope. notify is set on a
// new Ping that is not a Note.
func Seal(from *Keys, to DeviceInfo, msg Message, notify bool, now time.Time) (*Envelope, error) {
	plain, err := Encode(msg)
	if err != nil {
		return nil, err
	}
	pub, err := ecdh.X25519().NewPublicKey(to.SealKey)
	if err != nil {
		return nil, err
	}
	hpub, err := hpke.NewDHKEMPublicKey(pub)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 16)
	rand.Read(id)
	e := &Envelope{V: Version, ID: hex.EncodeToString(id), From: from.ID(), To: to.ID, Time: now.UnixMilli(), Notify: notify}
	if e.Body, err = hpke.Seal(hpub, suiteKDF, suiteAEAD, hpkeInfo(e.V, e.From, e.To), plain); err != nil {
		return nil, err
	}
	e.Sig = from.Sign(e.signedBytes())
	return e, nil
}

// ErrDropped is wrapped by every reason an envelope is dropped.
var ErrDropped = errors.New("wire: envelope dropped")

// Opener opens envelopes for one Device and remembers identifiers it has
// seen, keyed by sender. It is safe for concurrent use.
type Opener struct {
	keys *Keys
	path string
	mu   sync.Mutex
	seen map[string]time.Time
}

// NewOpener remembers identifiers in memory only (tests and the phone's
// short-lived extension).
func NewOpener(k *Keys) *Opener { return &Opener{keys: k, seen: map[string]time.Time{}} }

// NewPersistentOpener also keeps the identifiers in a file, so a replay
// within the window is dropped after the Engine restarts.
func NewPersistentOpener(k *Keys, path string) (*Opener, error) {
	o := NewOpener(k)
	o.path = path
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if len(b) > 0 {
		var m map[string]int64
		if err := json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		for id, t := range m {
			o.seen[id] = time.UnixMilli(t)
		}
	}
	return o, nil
}

// save writes the remembered identifiers; o.mu is held.
func (o *Opener) save() error {
	if o.path == "" {
		return nil
	}
	m := make(map[string]int64, len(o.seen))
	for id, t := range o.seen {
		m[id] = t.UnixMilli()
	}
	b, _ := json.Marshal(m)
	tmp := o.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, o.path)
}

// Open checks e against the recipient's current set and returns its message.
// It drops an envelope whose signature fails, whose sender is not in the
// set, whose recipient is not this Device, whose time is outside the replay
// window, or whose identifier was seen within the window.
func (o *Opener) Open(e *Envelope, set Members, now time.Time) (Message, error) {
	drop := func(why string) (Message, error) { return nil, fmt.Errorf("%w: %s", ErrDropped, why) }
	if e.V != Version {
		return drop("unknown version")
	}
	if e.To != o.keys.ID() {
		return drop("not for this Device")
	}
	sender, ok := set[e.From]
	if !ok {
		return drop("sender is not in the set")
	}
	if !ed25519.Verify(sender.SignKey, e.signedBytes(), e.Sig) {
		return drop("signature does not verify")
	}
	if d := now.Sub(time.UnixMilli(e.Time)); d > ReplayWindow || d < -ReplayWindow {
		return drop("too old or from the future")
	}
	o.mu.Lock()
	for id, t := range o.seen {
		if now.Sub(t) > ReplayWindow {
			delete(o.seen, id)
		}
	}
	key := e.From + "/" + e.ID
	if _, dup := o.seen[key]; dup {
		o.mu.Unlock()
		return drop("seen before")
	}
	o.mu.Unlock()
	priv, err := hpke.NewDHKEMPrivateKey(o.keys.seal)
	if err != nil {
		return nil, err
	}
	plain, err := hpke.Open(priv, suiteKDF, suiteAEAD, hpkeInfo(e.V, e.From, e.To), e.Body)
	if err != nil {
		return drop("body does not open")
	}
	m, err := Decode(plain)
	if err != nil {
		return drop(err.Error())
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, dup := o.seen[key]; dup {
		return drop("seen before")
	}
	o.seen[key] = now
	if err := o.save(); err != nil {
		delete(o.seen, key)
		return nil, err
	}
	return m, nil
}
