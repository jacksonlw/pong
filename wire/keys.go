// Package wire is what Devices send each other: each Device's keys and
// identifier, the set's chain of records, and sealed, signed envelopes carrying
// one message. The vectors in testdata/vectors hold every side to the same bytes.
package wire

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// Keys are one Device's private keys: X25519 for sealing and Ed25519 for
// signing. They never leave the Device and have no JSON form.
type Keys struct {
	seal *ecdh.PrivateKey
	sign ed25519.PrivateKey
}

// Public is what other Devices know about a Device's keys.
type Public struct {
	Seal []byte // X25519, 32 bytes
	Sign []byte // Ed25519, 32 bytes
}

// NewKeys creates a Device's keys.
func NewKeys() (*Keys, error) {
	seal, err := ecdh.X25519().GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	_, sign, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	return &Keys{seal: seal, sign: sign}, nil
}

// KeysFromSeeds rebuilds keys from their 32-byte private scalars/seeds.
func KeysFromSeeds(sealPriv, signSeed []byte) (*Keys, error) {
	if len(signSeed) != ed25519.SeedSize {
		return nil, errors.New("wire: signing seed must be 32 bytes")
	}
	seal, err := ecdh.X25519().NewPrivateKey(sealPriv)
	if err != nil {
		return nil, err
	}
	return &Keys{seal: seal, sign: ed25519.NewKeyFromSeed(signSeed)}, nil
}

// seeds returns the private material for a key store.
func (k *Keys) seeds() (sealPriv, signSeed []byte) {
	return k.seal.Bytes(), k.sign.Seed()
}

// Public returns the Device's public keys.
func (k *Keys) Public() Public {
	return Public{Seal: k.seal.PublicKey().Bytes(), Sign: []byte(k.sign.Public().(ed25519.PublicKey))}
}

// ID returns the Device's identifier.
func (k *Keys) ID() string { return DeviceID(k.Public().Sign) }

// Sign signs b with the Device's Ed25519 key.
func (k *Keys) Sign(b []byte) []byte { return ed25519.Sign(k.sign, b) }

// DeviceID is the first 16 bytes of the SHA-256 of a signing public key, in
// lowercase hex.
func DeviceID(signPub []byte) string {
	h := sha256.Sum256(signPub)
	return hex.EncodeToString(h[:16])
}

// JoinRequest is what a new Device sends to ask to join a set, signed by its
// new signing key. It is the only thing besides an envelope a network
// listener accepts.
type JoinRequest struct {
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Engine   bool   `json:"engine"`
	SealKey  []byte `json:"seal_key"`
	SignKey  []byte `json:"sign_key"`
	Time     int64  `json:"time"` // Unix milliseconds
	// Set, when asking through the relay, is the hash of the genesis record
	// of the set the new Device fetched from the relay: a Device of another
	// set does not approve the request.
	Set []byte `json:"set,omitempty"`
	Sig []byte `json:"sig"`
}

func (j *JoinRequest) signedBytes() []byte {
	var b buf
	b.raw([]byte("pong-join-v1\x00"))
	b.str(j.Name)
	b.str(j.Platform)
	b.boolean(j.Engine)
	b.bytes(j.SealKey)
	b.bytes(j.SignKey)
	b.i64(j.Time)
	if len(j.Set) > 0 {
		b.bytes(j.Set)
	}
	return b
}

// NewJoinRequest signs a request to join for the Device with keys k. set is
// the genesis hash of the set it asks to join through the relay, or nil.
func NewJoinRequest(k *Keys, name, platform string, engine bool, timeMs int64, set ...[]byte) JoinRequest {
	p := k.Public()
	j := JoinRequest{Name: name, Platform: platform, Engine: engine, SealKey: p.Seal, SignKey: p.Sign, Time: timeMs}
	if len(set) > 0 {
		j.Set = set[0]
	}
	j.Sig = k.Sign(j.signedBytes())
	return j
}

// Device checks the request's signature and returns the Device it asks to
// admit.
func (j *JoinRequest) Device() (DeviceInfo, error) {
	if len(j.SignKey) != ed25519.PublicKeySize || len(j.SealKey) != 32 || !ed25519.Verify(j.SignKey, j.signedBytes(), j.Sig) {
		return DeviceInfo{}, errors.New("wire: join request signature does not verify")
	}
	switch j.Platform {
	case PlatformMac, PlatformLinux, PlatformIOS:
	default:
		return DeviceInfo{}, errors.New("wire: join request has an unknown platform")
	}
	return DeviceInfo{ID: DeviceID(j.SignKey), Name: j.Name, Platform: j.Platform, SealKey: j.SealKey, SignKey: j.SignKey, Engine: j.Engine}, nil
}
