package wire

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSwiftVectors opens what the phone's Swift wire sealed and signed
// (testdata/vectors/swift.json, written by app/Tests/PongCoreTests/WireVectorTests.swift
// with PONG_WRITE_SWIFT_VECTORS=1), so the two sides are held to each other both ways.
func TestSwiftVectors(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "vectors", "swift.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Envelope  Envelope        `json:"envelope"`
		Plaintext json.RawMessage `json:"plaintext"`
		Recipient string          `json:"recipient"`
		Now       int64           `json:"now"`
		Join      JoinRequest     `json:"join"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	vb, err := os.ReadFile(vectorsPath)
	if err != nil {
		t.Fatal(err)
	}
	var v Vectors
	if err := json.Unmarshal(vb, &v); err != nil {
		t.Fatal(err)
	}
	members, err := v.Chain[:3].Verify() // Laptop, Box and Phone, before Box is revoked
	if err != nil {
		t.Fatal(err)
	}
	var recipient *Keys
	for _, d := range v.Devices {
		if d.Name == s.Recipient {
			recipient, _ = KeysFromSeeds(d.SealPrivate, d.SignSeed)
		}
	}
	m, err := NewOpener(recipient).Open(&s.Envelope, members, time.UnixMilli(s.Now))
	if err != nil {
		t.Fatalf("the Swift envelope does not open: %v", err)
	}
	got, _ := Encode(m)
	if !jsonEqual(got, s.Plaintext) {
		t.Errorf("opened %s, want %s", got, s.Plaintext)
	}
	d, err := s.Join.Device()
	if err != nil || d.ID != DeviceID(s.Join.SignKey) || d.Platform != PlatformIOS {
		t.Errorf("the Swift join request does not verify: %v", err)
	}
}
