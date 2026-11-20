package wire

import "crypto/ed25519"

// SignedBy reports whether the envelope's outside is signed by signKey. A carrier checks it against the sender's registered key without opening the body.
func (e *Envelope) SignedBy(signKey []byte) bool {
	return len(signKey) == ed25519.PublicKeySize && ed25519.Verify(signKey, e.signedBytes(), e.Sig)
}
