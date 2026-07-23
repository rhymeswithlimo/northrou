package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// GenerateKeypair mints a fresh Ed25519 release-signing keypair, returning both
// halves base64-encoded. The maintainer runs this once: embed pub via the
// update.SigningPublicKey ldflag, keep priv offline, and use it to sign each
// release's checksums.txt (goreleaser `signs`, or `ed25519` sign of the file).
func GenerateKeypair() (pubB64, privB64 string, err error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(pub), base64.StdEncoding.EncodeToString(priv), nil
}

// Sign produces a base64 detached Ed25519 signature of msg using a base64 Ed25519
// private key. Exposed for the release tooling (and tests) to generate the
// checksums.txt.sig that verifySignature checks.
func Sign(msg []byte, privB64 string) (string, error) {
	priv, err := decodeB64(privB64)
	if err != nil {
		return "", fmt.Errorf("bad private key: %w", err)
	}
	if len(priv) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("private key must be %d bytes, got %d", ed25519.PrivateKeySize, len(priv))
	}
	return base64.StdEncoding.EncodeToString(ed25519.Sign(ed25519.PrivateKey(priv), msg)), nil
}

// verifySignature checks that sig is a valid Ed25519 signature of msg under the
// base64-encoded public key pubB64. Both the signature and the key are accepted
// as base64 (standard or raw), tolerating trailing whitespace/newlines that a
// released .sig / key file may carry. It is deliberately a plain detached
// Ed25519 signature over the raw checksums bytes: no CGo, no external tooling,
// verifiable with only the standard library.
func verifySignature(msg, sig []byte, pubB64 string) error {
	pub, err := decodeB64(pubB64)
	if err != nil {
		return fmt.Errorf("bad public key: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("public key must be %d bytes, got %d", ed25519.PublicKeySize, len(pub))
	}
	raw, err := decodeB64(strings.TrimSpace(string(sig)))
	if err != nil {
		return fmt.Errorf("bad signature encoding: %w", err)
	}
	if len(raw) != ed25519.SignatureSize {
		return fmt.Errorf("signature must be %d bytes, got %d", ed25519.SignatureSize, len(raw))
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), msg, raw) {
		return errors.New("signature does not match")
	}
	return nil
}

// decodeB64 decodes standard or raw (unpadded) base64.
func decodeB64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}
