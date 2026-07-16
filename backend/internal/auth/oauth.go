package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// OAuth sign-in, verified on the box.
//
// The household's account is one email address. A social sign-in therefore
// proves exactly one thing: that whoever is at the keyboard controls that
// address -- which is what the emailed pin already proves. It is a shortcut,
// never a second way in, so a verified identity that is not the account address
// is refused outright.
//
// The box holds no OAuth secrets. It receives a short-lived assertion minted by
// the coordination broker and verifies its ES256 signature against the broker's
// published JWKS. Verifying that signature is the whole security boundary:
// without it this endpoint would accept "I am you@example.com" from anyone.

var (
	// ErrAssertionInvalid covers every failure to trust an assertion. It is
	// deliberately one error: telling a caller *why* their forged token failed
	// only helps them forge a better one.
	ErrAssertionInvalid = errors.New("invalid sign-in assertion")

	// ErrNotAccountEmail means the provider verified someone, but not the person
	// this server belongs to.
	ErrNotAccountEmail = errors.New("that account is not this server's account")
)

const assertionAudience = "northrou-server"

// OAuthVerifier checks broker assertions.
type OAuthVerifier struct {
	issuer  string // the broker's base URL; also the expected `iss`
	http    *http.Client
	nowFunc func() time.Time

	mu       sync.Mutex
	keys     map[string]*ecdsa.PublicKey
	fetched  time.Time
	usedJTIs map[string]time.Time
}

// NewOAuthVerifier builds a verifier for assertions from `issuer`.
func NewOAuthVerifier(issuer string) *OAuthVerifier {
	return &OAuthVerifier{
		issuer:   strings.TrimRight(issuer, "/"),
		http:     &http.Client{Timeout: 10 * time.Second},
		nowFunc:  time.Now,
		keys:     map[string]*ecdsa.PublicKey{},
		usedJTIs: map[string]time.Time{},
	}
}

// assertionClaims mirrors coordination/internal/oauth.Assertion.
type assertionClaims struct {
	Email    string `json:"email"`
	Nonce    string `json:"nonce"`
	Provider string `json:"provider"`
	jwt.RegisteredClaims
}

// VerifyAssertion checks an assertion and returns the verified email address.
//
// `nonce` is the value this client generated when it started the flow. Binding
// the assertion to it means one captured in some other session cannot be
// replayed into this one.
func (v *OAuthVerifier) VerifyAssertion(ctx context.Context, raw, nonce string) (string, error) {
	var claims assertionClaims

	tok, err := jwt.ParseWithClaims(raw, &claims, func(t *jwt.Token) (any, error) {
		// Pin the algorithm. Accepting whatever the token asks for is the
		// classic JWT hole: "alg":"none", or an RS256 verifier handed an HMAC
		// token keyed with the public key.
		if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		kid, _ := t.Header["kid"].(string)
		return v.publicKey(ctx, kid)
	},
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience(assertionAudience),
		jwt.WithExpirationRequired(),
		jwt.WithValidMethods([]string{"ES256"}),
	)
	if err != nil || !tok.Valid {
		return "", ErrAssertionInvalid
	}

	if claims.Nonce == "" || claims.Nonce != nonce {
		return "", ErrAssertionInvalid
	}
	if claims.ID == "" || !v.claimJTI(claims.ID, claims.ExpiresAt) {
		// Already used. An assertion is a one-shot credential; without this a
		// captured one works repeatedly until it expires.
		return "", ErrAssertionInvalid
	}

	email := normalize(claims.Email)
	if email == "" {
		return "", ErrAssertionInvalid
	}
	return email, nil
}

// claimJTI records an assertion id, returning false if it has been seen before.
func (v *OAuthVerifier) claimJTI(jti string, exp *jwt.NumericDate) bool {
	v.mu.Lock()
	defer v.mu.Unlock()

	now := v.nowFunc()
	// Entries are only useful until the assertion expires anyway, so sweep as
	// we go rather than growing forever.
	for id, until := range v.usedJTIs {
		if now.After(until) {
			delete(v.usedJTIs, id)
		}
	}

	if _, seen := v.usedJTIs[jti]; seen {
		return false
	}
	until := now.Add(5 * time.Minute)
	if exp != nil {
		until = exp.Time
	}
	v.usedJTIs[jti] = until
	return true
}

// publicKey returns the broker's key for `kid`, fetching the JWKS if needed.
func (v *OAuthVerifier) publicKey(ctx context.Context, kid string) (*ecdsa.PublicKey, error) {
	v.mu.Lock()
	key, ok := v.keys[kid]
	fresh := v.nowFunc().Sub(v.fetched) < 5*time.Minute
	v.mu.Unlock()

	if ok {
		return key, nil
	}
	// An unknown kid means either a rotation or a forgery. Refetch once, but
	// not on every bad token, or an attacker can drive load onto the broker.
	if fresh {
		return nil, ErrAssertionInvalid
	}
	if err := v.refreshKeys(ctx); err != nil {
		return nil, err
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if key, ok := v.keys[kid]; ok {
		return key, nil
	}
	return nil, ErrAssertionInvalid
}

type jwksResponse struct {
	Keys []struct {
		Kty string `json:"kty"`
		Crv string `json:"crv"`
		Kid string `json:"kid"`
		X   string `json:"x"`
		Y   string `json:"y"`
	} `json:"keys"`
}

func (v *OAuthVerifier) refreshKeys(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.issuer+"/oauth/jwks", nil)
	if err != nil {
		return err
	}
	res, err := v.http.Do(req)
	if err != nil {
		return fmt.Errorf("fetch jwks: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("jwks returned %d", res.StatusCode)
	}

	var body jwksResponse
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode jwks: %w", err)
	}

	keys := map[string]*ecdsa.PublicKey{}
	for _, k := range body.Keys {
		if k.Kty != "EC" || k.Crv != "P-256" {
			continue
		}
		x, err := base64.RawURLEncoding.DecodeString(k.X)
		if err != nil {
			continue
		}
		y, err := base64.RawURLEncoding.DecodeString(k.Y)
		if err != nil {
			continue
		}
		keys[k.Kid] = &ecdsa.PublicKey{
			Curve: elliptic.P256(),
			X:     new(big.Int).SetBytes(x),
			Y:     new(big.Int).SetBytes(y),
		}
	}
	if len(keys) == 0 {
		return errors.New("jwks contained no usable keys")
	}

	v.mu.Lock()
	v.keys = keys
	v.fetched = v.nowFunc()
	v.mu.Unlock()
	return nil
}

// SignInWithAssertion exchanges a verified assertion for a session.
//
// The identity must be the account's own address. Anything else is someone
// else's Google account, and this server has exactly one account.
func (s *Service) SignInWithAssertion(ctx context.Context, v *OAuthVerifier, assertion, nonce string) ([]model.Profile, *model.Profile, *TokenPair, error) {
	email, err := v.VerifyAssertion(ctx, assertion, nonce)
	if err != nil {
		return nil, nil, nil, err
	}

	account, err := s.db.GetAccount(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if account == nil || normalize(account.Email) != email {
		return nil, nil, nil, ErrNotAccountEmail
	}

	return s.signInAccount(ctx)
}
