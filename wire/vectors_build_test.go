package wire

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"time"
)

// VectorDevice is one Device in the test vectors, with its private material
// so the other side can rebuild it. Only test Devices appear here.
type VectorDevice struct {
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Engine      bool   `json:"engine"`
	SealPrivate []byte `json:"seal_private"`
	SignSeed    []byte `json:"sign_seed"`
	SealPublic  []byte `json:"seal_public"`
	SignPublic  []byte `json:"sign_public"`
	ID          string `json:"id"`
}

// Vectors are the shared test vectors (testdata/vectors/wire.json).
type Vectors struct {
	Note          string           `json:"note"`
	Devices       []VectorDevice   `json:"devices"`
	Chain         Chain            `json:"chain"`
	ChainHashes   [][]byte         `json:"chain_hashes"`
	Members       []string         `json:"members"` // identifiers in the set after the chain, sorted
	InvalidChains []InvalidChain   `json:"invalid_chains"`
	Envelopes     []VectorEnvelope `json:"envelopes"`
	Join          VectorJoin       `json:"join"`
	Pairing       VectorPairing    `json:"pairing"`
}

// VectorJoin is a join request with the identifier it yields.
type VectorJoin struct {
	Request JoinRequest `json:"request"`
	ID      string      `json:"id"`
}

// VectorPairing is the exchange that follows the join request: Laptop
// answers Stranger, each with a fixed value, and both show Code.
type VectorPairing struct {
	AskValue    []byte   `json:"ask_value"`
	AnswerValue []byte   `json:"answer_value"`
	Commit      PairStep `json:"commit"`
	Nonce       PairStep `json:"nonce"`
	Reveal      PairStep `json:"reveal"`
	Code        string   `json:"code"`
}

// InvalidChain is a chain every Device must refuse.
type InvalidChain struct {
	Why     string `json:"why"`
	Records Chain  `json:"records"`
}

// VectorEnvelope is an envelope with who must open it and what is inside,
// or the reason it must be dropped.
type VectorEnvelope struct {
	Why       string          `json:"why"`
	Recipient string          `json:"recipient"` // Device name
	Now       int64           `json:"now"`       // Unix ms to check it at
	Members   []string        `json:"members"`   // the recipient's set, by identifier
	Envelope  Envelope        `json:"envelope"`
	Plaintext json.RawMessage `json:"plaintext,omitempty"` // set when it must open
	Dropped   bool            `json:"dropped"`
}

func vectorSeed(label string) []byte {
	h := sha256.Sum256([]byte("pong test vector " + label))
	return h[:]
}

// BuildVectors builds the vectors. Envelope randomness comes from the
// caller's random source, so the test that compares them fixes it.
func BuildVectors() (*Vectors, error) {
	type dev struct {
		name, platform string
		engine         bool
	}
	devs := []dev{{"Laptop", PlatformMac, true}, {"Box", PlatformLinux, true}, {"Phone", PlatformIOS, false}, {"Stranger", PlatformMac, true}}
	v := &Vectors{Note: "Written by Go (wire, go test -run TestVectors -update). Byte strings are standard base64; times are Unix milliseconds."}
	keys := map[string]*Keys{}
	info := map[string]DeviceInfo{}
	for _, d := range devs {
		k, err := KeysFromSeeds(vectorSeed(d.name+" seal"), vectorSeed(d.name+" sign"))
		if err != nil {
			return nil, err
		}
		keys[d.name] = k
		info[d.name] = infoFor(k, d.name, d.platform, d.engine)
		sp, ss := k.seeds()
		p := k.Public()
		v.Devices = append(v.Devices, VectorDevice{d.name, d.platform, d.engine, sp, ss, p.Seal, p.Sign, k.ID()})
	}
	t0 := int64(1795000000000)
	c := NewChain(keys["Laptop"], "Laptop", PlatformMac, true, t0)
	var err error
	if c, err = c.Append(keys["Laptop"], Admit, info["Box"], t0+1000); err != nil {
		return nil, err
	}
	if c, err = c.Append(keys["Box"], Admit, info["Phone"], t0+2000); err != nil {
		return nil, err
	}
	if c, err = c.Append(keys["Phone"], Revoke, info["Box"], t0+3000); err != nil {
		return nil, err
	}
	v.Chain = c
	for i := range c {
		v.ChainHashes = append(v.ChainHashes, c[i].Hash())
	}
	m, _ := c.Verify()
	v.Members = sortedIDs(m)

	sign := func(w *Keys, r Record) Record { r.Sig = w.Sign(r.signedBytes()); return r }
	next := func(base Chain, w *Keys, kind string, d DeviceInfo, t int64) Chain {
		last := base[len(base)-1]
		return append(append(Chain{}, base...), sign(w, Record{Seq: last.Seq + 1, Prev: last.Hash(), Kind: kind, Device: d, Writer: w.ID(), Time: t}))
	}
	tampered := append(Chain{}, c[:2]...)
	tampered[1].Device.Name = "Renamed"
	v.InvalidChains = []InvalidChain{
		{"a record written by a Device that is not in the set", next(c[:1], keys["Stranger"], Admit, info["Stranger"], t0+1000)},
		{"a record written by a revoked Device", next(c, keys["Box"], Admit, info["Stranger"], t0+4000)},
		{"a record altered after signing", tampered},
		{"a record that does not link to the one before", func() Chain {
			x := next(c[:2], keys["Laptop"], Admit, info["Stranger"], t0+2000)
			x[2].Prev = c[0].Hash()
			x[2] = sign(keys["Laptop"], x[2])
			return x
		}()},
	}

	now := time.UnixMilli(t0 + 10000)
	members := sortedIDs(m)
	ping := Ping{Computer: keys["Laptop"].ID(), Ping: json.RawMessage(`{"id":"p_vector","question":"Ship it?"}`)}
	good, err := Seal(keys["Laptop"], info["Phone"], ping, true, now)
	if err != nil {
		return nil, err
	}
	plain, _ := Encode(ping)
	v.Envelopes = append(v.Envelopes, VectorEnvelope{Why: "a new Ping for the phone", Recipient: "Phone", Now: now.UnixMilli(), Members: members, Envelope: *good, Plaintext: plain})
	pong := Pong{PingID: "p_vector", Pong: json.RawMessage(`{"answer":"Yes"}`)}
	back, err := Seal(keys["Phone"], info["Laptop"], pong, false, now)
	if err != nil {
		return nil, err
	}
	plain, _ = Encode(pong)
	v.Envelopes = append(v.Envelopes, VectorEnvelope{Why: "a Pong from the phone", Recipient: "Laptop", Now: now.UnixMilli(), Members: members, Envelope: *back, Plaintext: plain})

	bad := *good
	bad.Body = append([]byte{}, good.Body...)
	bad.Body[len(bad.Body)-1] ^= 1
	flag := *good
	flag.Notify = false
	revoked, err := Seal(keys["Box"], info["Laptop"], pong, false, now)
	if err != nil {
		return nil, err
	}
	for _, d := range []VectorEnvelope{
		{Why: "the body was altered", Recipient: "Phone", Envelope: bad},
		{Why: "the notification flag was altered", Recipient: "Phone", Envelope: flag},
		{Why: "the sender was revoked", Recipient: "Laptop", Envelope: *revoked},
		{Why: "addressed to another Device", Recipient: "Laptop", Envelope: *good},
		{Why: "older than the replay window", Recipient: "Phone", Envelope: *good, Now: now.Add(ReplayWindow + time.Minute).UnixMilli()},
	} {
		if d.Now == 0 {
			d.Now = now.UnixMilli()
		}
		d.Members, d.Dropped = members, true
		v.Envelopes = append(v.Envelopes, d)
	}
	j := NewJoinRequest(keys["Stranger"], "Stranger", PlatformMac, true, t0+20000)
	v.Join = VectorJoin{Request: j, ID: keys["Stranger"].ID()}
	asking, err := j.Device()
	if err != nil {
		return nil, err
	}
	pv := VectorPairing{AskValue: vectorSeed("ask value"), AnswerValue: vectorSeed("answer value")}
	answer, commit := newPairAnswer(info["Laptop"], asking, pv.AnswerValue)
	ask, nonce, err := newPairAsk(keys["Stranger"], commit, pv.AskValue)
	if err != nil {
		return nil, err
	}
	reveal, ok := answer.Take(nonce)
	if !ok || !ask.Take(reveal) || ask.Code() != answer.Code() {
		return nil, errors.New("the pairing vector's two sides disagree")
	}
	pv.Commit, pv.Nonce, pv.Reveal, pv.Code = commit, nonce, reveal, ask.Code()
	v.Pairing = pv
	return v, nil
}

func sortedIDs(m Members) []string {
	return slices.Sorted(maps.Keys(m))
}
