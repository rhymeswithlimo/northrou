package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// This endpoint is the one place a bug means "anyone can sign in as the owner",
// so these tests attack it rather than just exercising the happy path.

const testAudience = "northrou-server"

func brokerFor(t *testing.T) (issuer string, key *ecdsa.PrivateKey, kid string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kid = "test-key"

	mux := http.NewServeMux()
	mux.HandleFunc("/oauth/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"kty": "EC", "crv": "P-256", "kid": kid, "alg": "ES256", "use": "sig",
			"x": base64.RawURLEncoding.EncodeToString(key.PublicKey.X.Bytes()),
			"y": base64.RawURLEncoding.EncodeToString(key.PublicKey.Y.Bytes()),
		}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, key, kid
}

type mintOpts struct {
	email    string
	nonce    string
	issuer   string
	audience string
	exp      time.Duration
	jti      string
	kid      string
	key      *ecdsa.PrivateKey
	method   jwt.SigningMethod
}

func mintAssertion(t *testing.T, o mintOpts) string {
	t.Helper()
	now := time.Now()
	claims := assertionClaims{
		Email:    o.email,
		Nonce:    o.nonce,
		Provider: "google",
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    o.issuer,
			Audience:  jwt.ClaimStrings{o.audience},
			Subject:   o.email,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(o.exp)),
			ID:        o.jti,
		},
	}
	method := o.method
	if method == nil {
		method = jwt.SigningMethodES256
	}
	tok := jwt.NewWithClaims(method, claims)
	tok.Header["kid"] = o.kid

	var signed string
	var err error
	if _, isHMAC := method.(*jwt.SigningMethodHMAC); isHMAC {
		signed, err = tok.SignedString([]byte("attacker-chosen"))
	} else {
		signed, err = tok.SignedString(o.key)
	}
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestVerifyAssertion(t *testing.T) {
	issuer, key, kid := brokerFor(t)
	ctx := context.Background()

	base := func() mintOpts {
		return mintOpts{
			email: "tomas@example.com", nonce: "n1", issuer: issuer,
			audience: testAudience, exp: 2 * time.Minute, jti: "jti-1",
			kid: kid, key: key,
		}
	}

	t.Run("a genuine assertion verifies", func(t *testing.T) {
		v := NewOAuthVerifier(issuer)
		email, err := v.VerifyAssertion(ctx, mintAssertion(t, base()), "n1")
		if err != nil {
			t.Fatalf("verify: %v", err)
		}
		if email != "tomas@example.com" {
			t.Errorf("email = %q", email)
		}
	})

	// Sign with a key the broker never published: the whole point of the
	// signature is that this fails.
	t.Run("an assertion signed by someone else is refused", func(t *testing.T) {
		attacker, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		o := base()
		o.key = attacker
		v := NewOAuthVerifier(issuer)
		if _, err := v.VerifyAssertion(ctx, mintAssertion(t, o), "n1"); err == nil {
			t.Fatal("a forged signature was accepted")
		}
	})

	// The classic JWT hole: hand an asymmetric verifier an HMAC token.
	t.Run("an HMAC-signed assertion is refused", func(t *testing.T) {
		o := base()
		o.method = jwt.SigningMethodHS256
		v := NewOAuthVerifier(issuer)
		if _, err := v.VerifyAssertion(ctx, mintAssertion(t, o), "n1"); err == nil {
			t.Fatal("an algorithm-confusion token was accepted")
		}
	})

	t.Run("an expired assertion is refused", func(t *testing.T) {
		o := base()
		o.exp = -time.Minute
		v := NewOAuthVerifier(issuer)
		if _, err := v.VerifyAssertion(ctx, mintAssertion(t, o), "n1"); err == nil {
			t.Fatal("an expired assertion was accepted")
		}
	})

	t.Run("an assertion from another issuer is refused", func(t *testing.T) {
		o := base()
		o.issuer = "https://evil.example"
		v := NewOAuthVerifier(issuer)
		if _, err := v.VerifyAssertion(ctx, mintAssertion(t, o), "n1"); err == nil {
			t.Fatal("a wrong-issuer assertion was accepted")
		}
	})

	// An assertion minted for something else must not be reusable here.
	t.Run("an assertion for another audience is refused", func(t *testing.T) {
		o := base()
		o.audience = "some-other-service"
		v := NewOAuthVerifier(issuer)
		if _, err := v.VerifyAssertion(ctx, mintAssertion(t, o), "n1"); err == nil {
			t.Fatal("a wrong-audience assertion was accepted")
		}
	})

	// Captured from another session and replayed into this one.
	t.Run("an assertion with the wrong nonce is refused", func(t *testing.T) {
		v := NewOAuthVerifier(issuer)
		if _, err := v.VerifyAssertion(ctx, mintAssertion(t, base()), "a-different-nonce"); err == nil {
			t.Fatal("an assertion bound to another nonce was accepted")
		}
	})

	t.Run("an assertion is single use", func(t *testing.T) {
		v := NewOAuthVerifier(issuer)
		raw := mintAssertion(t, base())
		if _, err := v.VerifyAssertion(ctx, raw, "n1"); err != nil {
			t.Fatalf("first use: %v", err)
		}
		if _, err := v.VerifyAssertion(ctx, raw, "n1"); err == nil {
			t.Fatal("the same assertion was accepted twice; a captured one would work until it expired")
		}
	})

	t.Run("garbage is refused", func(t *testing.T) {
		v := NewOAuthVerifier(issuer)
		for _, raw := range []string{"", "not-a-jwt", "a.b.c"} {
			if _, err := v.VerifyAssertion(ctx, raw, "n1"); err == nil {
				t.Errorf("accepted %q", raw)
			}
		}
	})
}

// The point of the whole design: a valid Google identity that is not this
// server's account must not get in. Otherwise "sign in with Google" would mean
// anyone with any Google account could open anyone's library.
func TestSignInWithAssertionRejectsAnotherIdentity(t *testing.T) {
	issuer, key, kid := brokerFor(t)
	ctx := context.Background()
	svc, database, _ := newTestService(t)
	setupAccount(t, database, "owner@example.com", "Owner")

	v := NewOAuthVerifier(issuer)

	// A real, correctly signed assertion -- for the wrong person.
	stranger := mintAssertion(t, mintOpts{
		email: "someone.else@example.com", nonce: "n1", issuer: issuer,
		audience: testAudience, exp: time.Minute, jti: "jti-a", kid: kid, key: key,
	})
	if _, _, _, err := svc.SignInWithAssertion(ctx, v, stranger, "n1"); err != ErrNotAccountEmail {
		t.Fatalf("a stranger's verified identity got err=%v, want ErrNotAccountEmail", err)
	}

	// The owner, same flow, signs in.
	owner := mintAssertion(t, mintOpts{
		email: "owner@example.com", nonce: "n2", issuer: issuer,
		audience: testAudience, exp: time.Minute, jti: "jti-b", kid: kid, key: key,
	})
	profiles, selected, pair, err := svc.SignInWithAssertion(ctx, v, owner, "n2")
	if err != nil {
		t.Fatalf("the account owner could not sign in: %v", err)
	}
	if len(profiles) != 1 || selected == nil || pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatalf("owner sign-in returned profiles=%d selected=%v pair=%+v", len(profiles), selected, pair)
	}
}
