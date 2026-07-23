package update

import "testing"

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv, err := GenerateKeypair()
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("hash1  northrou_1.2.3_linux_amd64.tar.gz\n")
	sig, err := Sign(msg, priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifySignature(msg, []byte(sig), pub); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	// Tampered message must fail.
	if err := verifySignature([]byte("tampered"), []byte(sig), pub); err == nil {
		t.Fatal("tampered message accepted")
	}
	// Wrong key must fail.
	otherPub, _, _ := GenerateKeypair()
	if err := verifySignature(msg, []byte(sig), otherPub); err == nil {
		t.Fatal("signature from a different key accepted")
	}
}
