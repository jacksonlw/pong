package wire_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jacksonlw/pong/wire"
)

type dev struct {
	k    *wire.Keys
	info wire.DeviceInfo
}

func newDev(t *testing.T, name, platform string) dev {
	t.Helper()
	k, err := wire.NewKeys()
	if err != nil {
		t.Fatal(err)
	}
	p := k.Public()
	return dev{k, wire.DeviceInfo{ID: k.ID(), Name: name, Platform: platform, SealKey: p.Seal, SignKey: p.Sign, Engine: platform != wire.PlatformIOS}}
}

// set: laptop (genesis) admits box; box admits phone.
func set(t *testing.T) (laptop, box, phone dev, c wire.Chain) {
	laptop, box, phone = newDev(t, "Laptop", "mac"), newDev(t, "Box", "linux"), newDev(t, "Phone", "ios")
	c = wire.NewChain(laptop.k, "Laptop", "mac", true, 1)
	var err error
	if c, err = c.Append(laptop.k, wire.Admit, box.info, 2); err != nil {
		t.Fatal(err)
	}
	if c, err = c.Append(box.k, wire.Admit, phone.info, 3); err != nil {
		t.Fatal(err)
	}
	return
}

func TestIdentifierIsHashOfSigningKey(t *testing.T) {
	d := newDev(t, "x", "mac")
	id := d.k.ID()
	if len(id) != 32 || strings.ToLower(id) != id {
		t.Fatalf("id %q is not 16 bytes of lowercase hex", id)
	}
	if wire.DeviceID(d.info.SignKey) != id {
		t.Fatal("identifier is not derived from the signing key")
	}
}

func TestKeysAreCreatedOnceInAFileOnlyTheUserCanRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keys", "device.json")
	s := wire.FileKeyStore{Path: path}
	a, err := wire.LoadOrCreate(s)
	if err != nil {
		t.Fatal(err)
	}
	b, err := wire.LoadOrCreate(s)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID() != b.ID() {
		t.Fatal("keys were created twice")
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v, want 0600", fi.Mode().Perm())
	}
}

func TestKeysHaveNoJSONForm(t *testing.T) {
	d := newDev(t, "x", "mac")
	b, _ := json.Marshal(d.k)
	if string(b) != "{}" {
		t.Fatalf("keys marshal to %s", b)
	}
}

func TestChainAcceptsAValidSet(t *testing.T) {
	laptop, box, phone, c := set(t)
	m, err := c.Verify()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []dev{laptop, box, phone} {
		if _, ok := m[d.k.ID()]; !ok {
			t.Errorf("%s is not in the set", d.info.Name)
		}
	}
}

func TestChainRefusesAForgedRecord(t *testing.T) {
	_, _, _, c := set(t)
	stranger := newDev(t, "Stranger", "mac")
	if _, err := c.Append(stranger.k, wire.Admit, stranger.info, 4); !errors.Is(err, wire.ErrChain) {
		t.Fatalf("a Device outside the set admitted itself: %v", err)
	}
	forged := append(wire.Chain{}, c...)
	forged[2].Device.Name = "Evil"
	if _, err := forged.Verify(); !errors.Is(err, wire.ErrChain) {
		t.Fatalf("an altered record was accepted: %v", err)
	}
}

func TestChainRefusesARecordByARevokedDevice(t *testing.T) {
	laptop, box, _, c := set(t)
	c, err := c.Append(laptop.k, wire.Revoke, box.info, 4)
	if err != nil {
		t.Fatal(err)
	}
	other := newDev(t, "Other", "linux")
	if _, err := c.Append(box.k, wire.Admit, other.info, 5); !errors.Is(err, wire.ErrChain) {
		t.Fatalf("a revoked Device wrote a record: %v", err)
	}
	if _, err := c.Append(laptop.k, wire.Admit, box.info, 5); !errors.Is(err, wire.ErrChain) {
		t.Fatalf("a revoked Device was admitted again: %v", err)
	}
}

func TestLongestValidExtensionWinsAndForksAreRefused(t *testing.T) {
	laptop, _, phone, c := set(t)
	held := c[:2]
	got, err := held.Extend(c)
	if err != nil || len(got) != 3 {
		t.Fatalf("extension not taken: %v", err)
	}
	if got, _ := c.Extend(held); len(got) != 3 {
		t.Fatal("a shorter chain replaced a longer one")
	}
	a, err := c.Append(laptop.k, wire.Admit, newDev(t, "A", "linux").info, 4)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Append(phone.k, wire.Admit, newDev(t, "B", "linux").info, 4)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = b.Append(phone.k, wire.Admit, newDev(t, "C", "linux").info, 5)
	if got, err := a.Extend(b); !errors.Is(err, wire.ErrChain) || len(got) != len(a) {
		t.Fatalf("a fork was accepted: %v", err)
	}
}

func TestEveryMessageKindRoundTripsThroughSealAndOpen(t *testing.T) {
	laptop, _, phone, c := set(t)
	m, _ := c.Verify()
	o := wire.NewOpener(phone.k)
	now := time.Now()
	raw := json.RawMessage(`{"x":1}`)
	msgs := []wire.Message{
		&wire.Hello{ChainLen: 3, Want: "snapshot"}, &wire.ChainRecords{Records: c},
		&wire.Ping{Computer: laptop.k.ID(), Ping: raw, Project: raw, Session: raw},
		&wire.Snapshot{Computer: "c", Pings: []wire.Ping{{Computer: "c", Ping: raw}}, Threads: raw, Sessions: []json.RawMessage{raw}},
		&wire.Session{Computer: "c", Session: raw},
		&wire.Pong{PingID: "p", Pong: raw}, &wire.Draft{PingID: "p", Draft: raw},
		&wire.Withdraw{PingID: "p", PongID: "q"}, &wire.Dismiss{PingID: "p"},
		&wire.Delivery{PingID: "p", PongID: "q", State: wire.Undelivered, Winner: raw},
		&wire.ResourceGet{ResourceID: "r", Offset: 5}, &wire.Resource{ResourceID: "r", Total: 3, MIME: "text/plain", Data: []byte("abc")},
		&wire.ThreadGet{SessionID: "s"}, &wire.Thread{SessionID: "s", Thread: raw},
		&wire.Start{RequestID: "q", Start: raw}, &wire.FollowUp{RequestID: "q", Harness: "h", SessionID: "s", FollowUp: raw},
		&wire.Stop{RequestID: "q", Harness: "h", SessionID: "s"},
		&wire.StartResult{RequestID: "q", Reasons: []string{"not enabled"}},
		&wire.State{State: raw}, &wire.Project{Project: raw}, &wire.CloudGrant{Grant: "g", Expires: 9},
	}
	covered := map[string]bool{}
	for _, msg := range msgs {
		e, err := wire.Seal(laptop.k, phone.info, msg, false, now)
		if err != nil {
			t.Fatal(err)
		}
		got, err := o.Open(e, m, now)
		if err != nil {
			t.Fatalf("%s: %v", msg.Kind(), err)
		}
		if !reflect.DeepEqual(got, msg) {
			t.Errorf("%s: got %+v, want %+v", msg.Kind(), got, msg)
		}
		covered[msg.Kind()] = true
	}
	for _, k := range wire.Kinds() {
		if !covered[k] {
			t.Errorf("kind %s not covered", k)
		}
	}
}

func TestResourcePartsAreAtMost192KiB(t *testing.T) {
	laptop, _, phone, _ := set(t)
	if _, err := wire.Seal(laptop.k, phone.info, &wire.Resource{Data: make([]byte, wire.MaxResourcePart+1)}, false, time.Now()); err == nil {
		t.Fatal("an oversized resource part was sealed")
	}
}

func TestEnvelopesAreDropped(t *testing.T) {
	laptop, box, phone, c := set(t)
	m, _ := c.Verify()
	now := time.Now()
	seal := func(from dev, to dev, notify bool) *wire.Envelope {
		e, err := wire.Seal(from.k, to.info, &wire.Dismiss{PingID: "p"}, notify, now)
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	stranger := newDev(t, "Stranger", "mac")
	revoked, _ := c.Append(laptop.k, wire.Revoke, box.info, 4)
	afterRevoke, _ := revoked.Verify()

	cases := []struct {
		name string
		e    func() *wire.Envelope
		set  wire.Members
		at   time.Time
	}{
		{"tampered body", func() *wire.Envelope { e := seal(laptop, phone, false); e.Body[40] ^= 1; return e }, m, now},
		{"tampered notification flag", func() *wire.Envelope { e := seal(laptop, phone, true); e.Notify = false; return e }, m, now},
		{"body re-signed by another member", func() *wire.Envelope {
			e := seal(laptop, phone, false)
			e.From = box.k.ID()
			return e
		}, m, now},
		{"unknown sender", func() *wire.Envelope { return seal(stranger, phone, false) }, m, now},
		{"revoked sender", func() *wire.Envelope { return seal(box, phone, false) }, afterRevoke, now},
		{"wrong recipient", func() *wire.Envelope { return seal(laptop, box, false) }, m, now},
		{"older than a day", func() *wire.Envelope { return seal(laptop, phone, false) }, m, now.Add(25 * time.Hour)},
	}
	for _, tc := range cases {
		if _, err := wire.NewOpener(phone.k).Open(tc.e(), tc.set, tc.at); !errors.Is(err, wire.ErrDropped) {
			t.Errorf("%s: %v, want dropped", tc.name, err)
		}
	}
}

func TestAReplayWithinADayIsDropped(t *testing.T) {
	laptop, _, phone, c := set(t)
	m, _ := c.Verify()
	now := time.Now()
	e, _ := wire.Seal(laptop.k, phone.info, &wire.Dismiss{PingID: "p"}, false, now)
	o := wire.NewOpener(phone.k)
	if _, err := o.Open(e, m, now); err != nil {
		t.Fatal(err)
	}
	copy := *e
	if _, err := o.Open(&copy, m, now.Add(23*time.Hour)); !errors.Is(err, wire.ErrDropped) {
		t.Fatalf("replay opened: %v", err)
	}
}

func TestTheCarrierSeesNoContent(t *testing.T) {
	laptop, _, phone, _ := set(t)
	e, _ := wire.Seal(laptop.k, phone.info, &wire.Ping{Ping: json.RawMessage(`{"question":"secret-question-text"}`)}, true, time.Now())
	b, _ := json.Marshal(e)
	if bytes.Contains(b, []byte("secret-question-text")) {
		t.Fatal("the outside of an envelope shows the Ping's text")
	}
}

func TestPairingCodeIsSixDigits(t *testing.T) {
	d := newDev(t, "x", "mac")
	code := wire.PairingCode(d.k.Public())
	if len(code) != 6 || strings.Trim(code, "0123456789") != "" {
		t.Fatalf("code %q", code)
	}
}

func TestAnEnvelopeSealedBeforeARevokeIsDroppedAfterIt(t *testing.T) {
	laptop, box, phone, c := set(t)
	now := time.Now()
	e, _ := wire.Seal(box.k, phone.info, &wire.Dismiss{PingID: "p"}, false, now)
	c, _ = c.Append(laptop.k, wire.Revoke, box.info, 4)
	m, _ := c.Verify()
	if _, err := wire.NewOpener(phone.k).Open(e, m, now); !errors.Is(err, wire.ErrDropped) {
		t.Fatalf("opened: %v", err)
	}
}

func TestAReplayIsDroppedAfterARestart(t *testing.T) {
	laptop, _, phone, c := set(t)
	m, _ := c.Verify()
	now := time.Now()
	path := filepath.Join(t.TempDir(), "seen.json")
	e, _ := wire.Seal(laptop.k, phone.info, &wire.Dismiss{PingID: "p"}, false, now)
	o, err := wire.NewPersistentOpener(phone.k, path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := o.Open(e, m, now); err != nil {
		t.Fatal(err)
	}
	o2, _ := wire.NewPersistentOpener(phone.k, path)
	if _, err := o2.Open(e, m, now.Add(time.Hour)); !errors.Is(err, wire.ErrDropped) {
		t.Fatalf("replay after restart opened: %v", err)
	}
}

func TestTheLastDeviceCannotBeRevoked(t *testing.T) {
	d := newDev(t, "Solo", "mac")
	c := wire.NewChain(d.k, "Solo", "mac", true, 1)
	if _, err := c.Append(d.k, wire.Revoke, c[0].Device, 2); !errors.Is(err, wire.ErrChain) {
		t.Fatalf("revoked the last Device: %v", err)
	}
}
