package wire

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"testing"
	"testing/cryptotest"
	"time"
)

var update = flag.Bool("update", false, "rewrite testdata/vectors/wire.json")

const vectorsPath = "testdata/vectors/wire.json"

// TestVectors regenerates the shared vectors and compares them with the
// checked-in file, so the Go side cannot drift from what Swift checks.
func TestVectors(t *testing.T) {
	cryptotest.SetGlobalRandom(t, 2)
	v, err := BuildVectors()
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	if *update {
		if err := os.WriteFile(vectorsPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s is out of date; run go test ./wire -run TestVectors -update", vectorsPath)
	}
}

// TestVectorsCheck reads the file as the Swift side does and checks every
// expectation in it, using only exported API.
func TestVectorsCheck(t *testing.T) {
	b, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var v Vectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	keys := map[string]*Keys{}
	for _, d := range v.Devices {
		k, err := KeysFromSeeds(d.SealPrivate, d.SignSeed)
		if err != nil {
			t.Fatal(err)
		}
		p := k.Public()
		if !bytes.Equal(p.Seal, d.SealPublic) || !bytes.Equal(p.Sign, d.SignPublic) || DeviceID(d.SignPublic) != d.ID {
			t.Errorf("device %s: keys or identifier differ", d.Name)
		}
		keys[d.Name] = k
	}
	m, err := v.Chain.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if got := sortedIDs(m); !equalStrings(got, v.Members) {
		t.Errorf("members %v, want %v", got, v.Members)
	}
	for i := range v.Chain {
		if !bytes.Equal(v.Chain[i].Hash(), v.ChainHashes[i]) {
			t.Errorf("record %d hash differs", i)
		}
	}
	for _, ic := range v.InvalidChains {
		if _, err := ic.Records.Verify(); err == nil {
			t.Errorf("invalid chain accepted: %s", ic.Why)
		}
	}
	for _, ve := range v.Envelopes {
		set := Members{}
		for _, id := range ve.Members {
			set[id] = m[id]
		}
		e := ve.Envelope
		msg, err := NewOpener(keys[ve.Recipient]).Open(&e, set, time.UnixMilli(ve.Now))
		if ve.Dropped {
			if err == nil {
				t.Errorf("%s: opened, want dropped", ve.Why)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", ve.Why, err)
			continue
		}
		plain, _ := Encode(msg)
		if !jsonEqual(plain, ve.Plaintext) {
			t.Errorf("%s: plaintext %s, want %s", ve.Why, plain, ve.Plaintext)
		}
	}
}

func TestVectorJoin(t *testing.T) {
	b, _ := os.ReadFile(vectorsPath)
	var v Vectors
	json.Unmarshal(b, &v)
	d, err := v.Join.Request.Device()
	if err != nil || d.ID != v.Join.ID {
		t.Fatalf("join vector: %v %+v", err, d)
	}
	bad := v.Join.Request
	bad.Name = "Other"
	if _, err := bad.Device(); err == nil {
		t.Fatal("an altered join request was accepted")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func jsonEqual(a, b []byte) bool {
	var x, y any
	json.Unmarshal(a, &x)
	json.Unmarshal(b, &y)
	xa, _ := json.Marshal(x)
	ya, _ := json.Marshal(y)
	return bytes.Equal(xa, ya)
}
