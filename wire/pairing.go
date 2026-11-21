package wire

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"errors"
)

// The pairing code. A Device that asks to join and each Device that answers
// make a six-digit code together, and a human checks that both show the same
// one before either acts on it:
//
//	asking → answering   the join request
//	answering → asking   commit: the answering Device, and a hash that fixes its random value
//	asking → answering   nonce: the asking Device's random value
//	answering → asking   reveal: the answering Device's random value
//
// The code is a hash of both Devices' keys and both random values. Each side
// gives its value before it can know the other's, so neither can choose
// values to reach a code, and a Device between the two shows each a
// different one.

// PairStep is one step after the join request. Exactly one of Commit, Nonce
// and Reveal is set.
type PairStep struct {
	From   string      `json:"from"`
	To     string      `json:"to"`
	Device *DeviceInfo `json:"device,omitempty"` // with Commit: the Device that answers
	Commit []byte      `json:"commit,omitempty"`
	Nonce  []byte      `json:"nonce,omitempty"`
	Reveal []byte      `json:"reveal,omitempty"`
}

const pairValueLen = 32

// PairCommit is what the answering Device sends before it learns the asking
// Device's value: SHA-256 of "pong-pair-commit-v2\x00" and, each with its
// length, the answering signing key, the asking signing key and the value.
func PairCommit(answerSign, askSign, value []byte) []byte {
	var b buf
	b.raw([]byte("pong-pair-commit-v2\x00"))
	b.bytes(answerSign)
	b.bytes(askSign)
	b.bytes(value)
	h := sha256.Sum256(b)
	return h[:]
}

// PairCode is the six-digit code: SHA-256 of "pong-pair-v2\x00" and, each
// with its length, the asking Device's seal and signing keys, the answering
// Device's signing key, the asking value and the answering value; its first
// four bytes as a big-endian integer, modulo one million, zero-padded.
func PairCode(ask Public, answerSign, askValue, answerValue []byte) string {
	var b buf
	b.raw([]byte("pong-pair-v2\x00"))
	b.bytes(ask.Seal)
	b.bytes(ask.Sign)
	b.bytes(answerSign)
	b.bytes(askValue)
	b.bytes(answerValue)
	s := sha256.Sum256(b)
	n := (uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])) % 1000000
	d := []byte("000000")
	for i := 5; i >= 0; i-- {
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d)
}

func pairValue() []byte {
	v := make([]byte, pairValueLen)
	if _, err := rand.Read(v); err != nil {
		panic(err)
	}
	return v
}

// PairAnswer is the answering Device's side of the exchange for one Device
// that asks.
type PairAnswer struct {
	self  DeviceInfo
	ask   DeviceInfo
	value []byte
	code  string
}

// NewPairAnswer starts the exchange with the Device ask, and returns the
// commit step to send it.
func NewPairAnswer(self, ask DeviceInfo) (*PairAnswer, PairStep) {
	return newPairAnswer(self, ask, pairValue())
}

func newPairAnswer(self, ask DeviceInfo, value []byte) (*PairAnswer, PairStep) {
	a := &PairAnswer{self: self, ask: ask, value: value}
	return a, PairStep{From: self.ID, To: ask.ID, Device: &self, Commit: PairCommit(self.SignKey, ask.SignKey, value)}
}

// Take takes the asking Device's nonce step and returns the reveal step to
// send back. Only the first nonce counts: a second one, sent once the
// answering value is known, could be chosen to reach a code.
func (a *PairAnswer) Take(s PairStep) (PairStep, bool) {
	if a.code != "" || s.From != a.ask.ID || s.To != a.self.ID || len(s.Nonce) != pairValueLen {
		return PairStep{}, false
	}
	a.code = PairCode(Public{Seal: a.ask.SealKey, Sign: a.ask.SignKey}, a.self.SignKey, s.Nonce, a.value)
	return PairStep{From: a.self.ID, To: a.ask.ID, Reveal: a.value}, true
}

// Code is the code to show, or "" until the asking Device gave its value.
func (a *PairAnswer) Code() string { return a.code }

// PairAsk is the asking Device's side of the exchange with one Device that
// answers.
type PairAsk struct {
	self   Public
	device DeviceInfo
	commit []byte
	value  []byte
	code   string
}

// NewPairAsk takes an answering Device's commit step and returns the nonce
// step to send it.
func NewPairAsk(self *Keys, s PairStep) (*PairAsk, PairStep, error) {
	return newPairAsk(self, s, pairValue())
}

func newPairAsk(self *Keys, s PairStep, value []byte) (*PairAsk, PairStep, error) {
	d := s.Device
	if d == nil || len(d.SignKey) != ed25519.PublicKeySize || DeviceID(d.SignKey) != d.ID || s.From != d.ID ||
		s.To != self.ID() || d.ID == self.ID() || len(s.Commit) != sha256.Size {
		return nil, PairStep{}, errors.New("wire: not a commit step for this Device")
	}
	a := &PairAsk{self: self.Public(), device: *d, commit: s.Commit, value: value}
	return a, PairStep{From: self.ID(), To: d.ID, Nonce: value}, nil
}

// Take takes the answering Device's reveal step, and reports whether it is
// the value the Device committed to. The code is known from then.
func (a *PairAsk) Take(s PairStep) bool {
	if a.code != "" || s.From != a.device.ID || len(s.Reveal) != pairValueLen ||
		!bytes.Equal(PairCommit(a.device.SignKey, a.self.Sign, s.Reveal), a.commit) {
		return false
	}
	a.code = PairCode(a.self, a.device.SignKey, a.value, s.Reveal)
	return true
}

// Code is the code to show, or "" until the answering Device revealed its
// value.
func (a *PairAsk) Code() string { return a.code }

// Device is the answering Device.
func (a *PairAsk) Device() DeviceInfo { return a.device }
