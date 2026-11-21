package wire

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
)

// Record kinds.
const (
	Genesis = "genesis"
	Admit   = "admit"
	Revoke  = "revoke"
)

// Platforms.
const (
	PlatformMac   = "mac"
	PlatformLinux = "linux"
	PlatformIOS   = "ios"
)

// DeviceInfo is the Device a chain record concerns.
type DeviceInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	SealKey  []byte `json:"seal_key"` // X25519 public key
	SignKey  []byte `json:"sign_key"` // Ed25519 public key
	Engine   bool   `json:"engine"`
}

// Record is one link of a set's chain.
type Record struct {
	Seq    uint64     `json:"seq"`
	Prev   []byte     `json:"prev"` // SHA-256 of the previous record; 32 zero bytes for genesis
	Kind   string     `json:"kind"`
	Device DeviceInfo `json:"device"`
	Writer string     `json:"writer"` // identifier of the Device that wrote it
	Time   int64      `json:"time"`   // Unix milliseconds
	Sig    []byte     `json:"sig"`
}

// signedBytes is what the writer signs.
func (r *Record) signedBytes() []byte {
	var b buf
	b.raw([]byte("pong-chain-v1\x00"))
	b.u64(r.Seq)
	b.bytes(r.Prev)
	b.str(r.Kind)
	b.str(r.Device.ID)
	b.str(r.Device.Name)
	b.str(r.Device.Platform)
	b.bytes(r.Device.SealKey)
	b.bytes(r.Device.SignKey)
	b.boolean(r.Device.Engine)
	b.str(r.Writer)
	b.i64(r.Time)
	return b
}

// Hash is the SHA-256 of the signed bytes followed by the signature.
func (r *Record) Hash() []byte {
	h := sha256.New()
	h.Write(r.signedBytes())
	h.Write(r.Sig)
	return h.Sum(nil)
}

// Chain is a set's records, oldest first.
type Chain []Record

var zeroHash = make([]byte, 32)

// ErrChain is wrapped by every reason a chain is refused.
var ErrChain = errors.New("wire: chain refused")

// NewChain starts a set: the first Device writes genesis about itself.
func NewChain(k *Keys, name, platform string, engine bool, timeMs int64) Chain {
	r := Record{Seq: 0, Prev: zeroHash, Kind: Genesis, Device: infoFor(k, name, platform, engine), Writer: k.ID(), Time: timeMs}
	r.Sig = k.Sign(r.signedBytes())
	return Chain{r}
}

func infoFor(k *Keys, name, platform string, engine bool) DeviceInfo {
	p := k.Public()
	return DeviceInfo{ID: k.ID(), Name: name, Platform: platform, SealKey: p.Seal, SignKey: p.Sign, Engine: engine}
}

// Append writes a record of kind about d, signed by the writer w, and
// returns the longer chain. It is checked like any other chain.
func (c Chain) Append(w *Keys, kind string, d DeviceInfo, timeMs int64) (Chain, error) {
	if len(c) == 0 {
		return nil, fmt.Errorf("%w: empty", ErrChain)
	}
	last := c[len(c)-1]
	r := Record{Seq: last.Seq + 1, Prev: last.Hash(), Kind: kind, Device: d, Writer: w.ID(), Time: timeMs}
	r.Sig = w.Sign(r.signedBytes())
	out := append(append(Chain{}, c...), r)
	if _, err := out.Verify(); err != nil {
		return nil, err
	}
	return out, nil
}

// Members is the set after the whole chain: every Device admitted and not
// revoked, by identifier.
type Members map[string]DeviceInfo

// Verify checks every link and signature and returns the set after the
// last record. A record's writer must be in the set just before it;
// genesis is written by its own Device; a Device is admitted at most once
// and never after being revoked.
func (c Chain) Verify() (Members, error) {
	if len(c) == 0 {
		return nil, fmt.Errorf("%w: empty", ErrChain)
	}
	members := Members{}
	seen := map[string]bool{}
	prev := zeroHash
	for i := range c {
		r := &c[i]
		bad := func(why string) (Members, error) {
			return nil, fmt.Errorf("%w: record %d: %s", ErrChain, i, why)
		}
		if r.Seq != uint64(i) || !bytes.Equal(r.Prev, prev) {
			return bad("does not link to the record before it")
		}
		if len(r.Device.SignKey) != ed25519.PublicKeySize || len(r.Device.SealKey) != 32 || DeviceID(r.Device.SignKey) != r.Device.ID {
			return bad("device keys do not match its identifier")
		}
		switch r.Device.Platform {
		case PlatformMac, PlatformLinux, PlatformIOS:
		default:
			return bad("unknown platform")
		}
		var writerKey []byte
		switch r.Kind {
		case Genesis:
			if i != 0 || r.Writer != r.Device.ID {
				return bad("genesis must be first and written by its own Device")
			}
			writerKey = r.Device.SignKey
		case Admit, Revoke:
			if i == 0 {
				return bad("chain must start with genesis")
			}
			w, ok := members[r.Writer]
			if !ok {
				return bad("writer is not in the set")
			}
			writerKey = w.SignKey
			if r.Kind == Admit && seen[r.Device.ID] {
				return bad("device was already admitted")
			}
			if _, in := members[r.Device.ID]; r.Kind == Revoke && !in {
				return bad("device is not in the set")
			}
		default:
			return bad("unknown kind")
		}
		if !ed25519.Verify(writerKey, r.signedBytes(), r.Sig) {
			return bad("signature does not verify")
		}
		if r.Kind == Revoke {
			if len(members) == 1 {
				return bad("the last Device cannot be revoked")
			}
			delete(members, r.Device.ID)
		} else {
			members[r.Device.ID] = r.Device
			seen[r.Device.ID] = true
		}
		prev = r.Hash()
	}
	return members, nil
}

// Extend returns the chain a Device holding c should keep after being
// offered o: o when it is valid, extends c (c is a prefix of it) and is
// longer; c otherwise.
//
// When o forks from c, a revoke settles it. If o, from the fork on, revokes
// every Device that wrote c's records from the fork on, o is kept, though it
// drops those records: a Device revoked cannot keep its place by writing at
// the place of its own revoke. If c revokes the writers of o's records, c is
// kept and the error is a *Fork that picks "ours", so the holder sends c
// back. When each revokes the other's writers, or neither does (two Devices
// changed the set at the same time), only the human can say which stands:
// c is kept, and the error is a *Fork that picks nothing.
func (c Chain) Extend(o Chain) (Chain, error) {
	if _, err := o.Verify(); err != nil {
		return c, err
	}
	n := min(len(c), len(o))
	for i := 0; i < n; i++ {
		if bytes.Equal(c[i].Hash(), o[i].Hash()) {
			continue
		}
		ourWriters, ourRevokes := branch(c[i:])
		theirWriters, theirRevokes := branch(o[i:])
		theirsWins := subset(ourWriters, theirRevokes)
		oursWins := subset(theirWriters, ourRevokes)
		f := &Fork{At: i, Ours: ourWriters, Theirs: theirWriters, Mutual: theirsWins && oursWins}
		switch {
		case theirsWins && !oursWins:
			return o, nil
		case oursWins && !theirsWins:
			f.Pick = "ours"
		}
		return c, f
	}
	if len(o) > len(c) {
		return o, nil
	}
	return c, nil
}

// Fork is an offered chain that forks from the one held.
type Fork struct {
	At     int      // the first record the two differ at
	Ours   []string // the Devices that wrote the held chain's records from At
	Theirs []string // the Devices that wrote the offered chain's records from At
	// Pick is "ours" when the held chain revokes the writers of the offered
	// one, so it stands; "" when only the human can say.
	Pick string
	// Mutual is set when each chain revokes the other's writers.
	Mutual bool
}

func (f *Fork) Error() string {
	switch {
	case f.Pick == "ours":
		return fmt.Sprintf("wire: the offered chain forks at record %d, and the held one revokes its writers", f.At)
	case f.Mutual:
		return fmt.Sprintf("wire: the chains fork at record %d, each revoking the other's writers", f.At)
	}
	return fmt.Sprintf("wire: the chains fork at record %d", f.At)
}

func (f *Fork) Unwrap() error { return ErrChain }

// branch returns the writers of a chain's records, and the Devices it
// revokes, each sorted.
func branch(rs Chain) (writers, revoked []string) {
	w, r := map[string]bool{}, map[string]bool{}
	for _, x := range rs {
		w[x.Writer] = true
		if x.Kind == Revoke {
			r[x.Device.ID] = true
		}
	}
	return sortedKeys(w), sortedKeys(r)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func subset(a, b []string) bool {
	for _, x := range a {
		if !slices.Contains(b, x) {
			return false
		}
	}
	return len(a) > 0
}
