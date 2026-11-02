// Package recognizing implements the RecognizingEngine: a pure decision on
// whether a presented proof denotes a Member.
package recognizing

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"

	fweng "github.com/mixofreality-studio/archistrator-platform/framework-go/engine"
)

const (
	keyPrefix      = "mk1_"
	keyEncodedLen  = 43
	keyDecodedLen  = 32
	memberPrefix   = "mbr_"
	derivationSalt = "recognizing-engine/member/v1"
)

type recognizingEngine struct{}

func newRecognizingEngine() RecognizingEngine {
	return &recognizingEngine{}
}

// RecognizeActor is a pure function: a well-formed self-issued member key
// yields a deterministic Member; anything else is refused (fail closed).
func (e *recognizingEngine) RecognizeActor(_ fweng.Context, proof Proof) (Recognition, error) {
	key, ok := decodeKey(proof.Credential)
	if !ok {
		return refused(), nil
	}
	h := sha256.New()
	h.Write([]byte(derivationSalt))
	h.Write([]byte{0x00})
	h.Write(key)
	return Recognition{
		Outcome: RecognitionOutcomeRecognized,
		Member:  MemberID(memberPrefix + hex.EncodeToString(h.Sum(nil))),
	}, nil
}

func refused() Recognition {
	return Recognition{Outcome: RecognitionOutcomeRefused, Member: ""}
}

// decodeKey enforces the canonical credential grammar: "mk1_" + 43 unpadded
// base64url characters decoding to 32 bytes with zero trailing bits.
func decodeKey(credential string) ([]byte, bool) {
	if len(credential) != len(keyPrefix)+keyEncodedLen || credential[:len(keyPrefix)] != keyPrefix {
		return nil, false
	}
	// Strict decoding rejects non-zero trailing bits, padding, and any
	// character outside the base64url alphabet (including newlines).
	b, err := base64.RawURLEncoding.Strict().DecodeString(credential[len(keyPrefix):])
	if err != nil || len(b) != keyDecodedLen {
		return nil, false
	}
	return b, true
}
