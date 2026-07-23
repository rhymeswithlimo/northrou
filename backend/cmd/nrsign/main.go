// Command nrsign signs a file with the Northrou release-signing Ed25519 key and
// writes "<file>.sig" (base64) next to it. It is maintainer tooling, NOT shipped
// in releases (goreleaser only builds ./cmd/northrou). Pair it with the public
// key embedded via update.SigningPublicKey so `northrou update` can verify.
//
// Usage:
//
//	NORTHROU_SIGNING_KEY=<base64-ed25519-private> go run ./cmd/nrsign checksums.txt
//
// Generate a keypair once:
//
//	go run ./cmd/nrsign -genkey
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
)

func main() {
	genkey := flag.Bool("genkey", false, "generate a new keypair and print it")
	flag.Parse()

	if *genkey {
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fail(err)
		}
		fmt.Println("public  (embed via -X ...update.SigningPublicKey=):")
		fmt.Println(" ", base64.StdEncoding.EncodeToString(pub))
		fmt.Println("private (keep offline; set NORTHROU_SIGNING_KEY):")
		fmt.Println(" ", base64.StdEncoding.EncodeToString(priv))
		return
	}

	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: nrsign [-genkey] <file>")
		os.Exit(2)
	}
	privB64 := os.Getenv("NORTHROU_SIGNING_KEY")
	if privB64 == "" {
		fail(fmt.Errorf("NORTHROU_SIGNING_KEY is not set"))
	}
	priv, err := base64.StdEncoding.DecodeString(privB64)
	if err != nil || len(priv) != ed25519.PrivateKeySize {
		fail(fmt.Errorf("NORTHROU_SIGNING_KEY must be a base64 %d-byte Ed25519 private key", ed25519.PrivateKeySize))
	}
	path := flag.Arg(0)
	msg, err := os.ReadFile(path)
	if err != nil {
		fail(err)
	}
	sig := ed25519.Sign(ed25519.PrivateKey(priv), msg)
	out := path + ".sig"
	if err := os.WriteFile(out, []byte(base64.StdEncoding.EncodeToString(sig)), 0o644); err != nil {
		fail(err)
	}
	fmt.Println("wrote", out)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "nrsign:", err)
	os.Exit(1)
}
