// Package oauth is Northrou's hosted sign-in broker.
//
// Why this exists at all: Google and Apple both require a registered OAuth
// client with pre-declared redirect URIs. A self-hosted Northrou lives at an
// arbitrary address (192.168.1.50:7777, a LAN name, an app), which is not
// something you can register, and shipping a client secret inside an
// open-source binary would publish it. So the credentials live here, on
// infrastructure that has one stable URL, and the box never sees them.
//
// What the box gets back is an *assertion*: a short-lived JWT saying "the
// provider proved this person controls this email address". It is signed with
// ES256, so the box only ever needs the public key and this service's private
// key never leaves it. Without a signature the endpoint would be an open door:
// anyone could POST "I am you@example.com" to a home server and be let in.
//
// This broker never sees a household's media, library, or tokens. It learns one
// thing: that an email address authenticated. It has no idea which box, if any,
// that address belongs to.
package oauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// Assertions are handed straight to the box by a client that just completed
	// the flow, so they need to live for seconds, not minutes. A tight window is
	// the main defence against one being captured and replayed.
	assertionTTL = 2 * time.Minute

	// Audience is fixed: an assertion is only ever meant for a Northrou server.
	audience = "northrou-server"

	stateTTL = 10 * time.Minute
)

// Provider is one identity source (Google, Apple).
type Provider interface {
	Name() string
	// AuthURL is where the user is sent to authenticate.
	AuthURL(state, redirectURI string) string
	// Exchange turns the callback's code into a verified email address.
	Exchange(code, redirectURI string) (email string, err error)
}

// pending is one in-flight authorization, keyed by the state parameter.
type pending struct {
	nonce    string
	redirect string
	expires  time.Time
}

// Broker runs the OAuth flows and mints assertions.
type Broker struct {
	providers map[string]Provider
	key       *ecdsa.PrivateKey
	keyID     string
	issuer    string

	// Redirect targets must be allow-listed. Without this the broker is an open
	// redirector: anyone could send a user through a real Google login and have
	// the assertion delivered to a site they control.
	allowedRedirects []string

	mu      sync.Mutex
	pending map[string]pending
}

// Config configures a Broker.
type Config struct {
	Issuer           string // this service's public base URL
	Key              *ecdsa.PrivateKey
	AllowedRedirects []string
}

// NewBroker builds a Broker. A nil key generates an ephemeral one, which is fine
// for local development and useless in production: assertions stop verifying
// across restarts because the public key changes.
func NewBroker(cfg Config, providers ...Provider) (*Broker, error) {
	key := cfg.Key
	if key == nil {
		var err error
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		slog.Warn("oauth: no signing key configured; generated an ephemeral one. " +
			"Assertions will not verify after a restart. Set OAUTH_SIGNING_KEY in production.")
	}

	b := &Broker{
		providers:        map[string]Provider{},
		key:              key,
		keyID:            thumbprint(&key.PublicKey),
		issuer:           strings.TrimRight(cfg.Issuer, "/"),
		allowedRedirects: cfg.AllowedRedirects,
		pending:          map[string]pending{},
	}
	for _, p := range providers {
		if p != nil {
			b.providers[p.Name()] = p
		}
	}
	go b.pruneLoop()
	return b, nil
}

// Enabled reports whether any provider is configured.
func (b *Broker) Enabled() bool { return len(b.providers) > 0 }

// Routes registers the broker's endpoints.
func (b *Broker) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /oauth/jwks", b.handleJWKS)
	mux.HandleFunc("GET /oauth/{provider}/start", b.handleStart)
	mux.HandleFunc("GET /oauth/{provider}/callback", b.handleCallback)
}

// handleStart begins a flow: remember the nonce and redirect, then send the
// user to the provider.
func (b *Broker) handleStart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	p, ok := b.providers[name]
	if !ok {
		http.Error(w, "unknown provider", http.StatusNotFound)
		return
	}

	nonce := r.URL.Query().Get("nonce")
	redirect := r.URL.Query().Get("redirect")
	if nonce == "" || redirect == "" {
		http.Error(w, "nonce and redirect are required", http.StatusBadRequest)
		return
	}
	if !b.redirectAllowed(redirect) {
		// Refusing unknown redirects is what stops this being an open
		// redirector that launders a real Google login into someone else's site.
		http.Error(w, "redirect not allowed", http.StatusBadRequest)
		return
	}

	state := randomToken()
	b.mu.Lock()
	b.pending[state] = pending{nonce: nonce, redirect: redirect, expires: time.Now().Add(stateTTL)}
	b.mu.Unlock()

	http.Redirect(w, r, p.AuthURL(state, b.callbackURI(name)), http.StatusFound)
}

// handleCallback finishes a flow: verify with the provider, mint an assertion,
// hand it back to the client.
func (b *Broker) handleCallback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	p, ok := b.providers[name]
	if !ok {
		http.Error(w, "unknown provider", http.StatusNotFound)
		return
	}

	state := r.URL.Query().Get("state")
	b.mu.Lock()
	pend, found := b.pending[state]
	delete(b.pending, state) // single use
	b.mu.Unlock()

	if !found || time.Now().After(pend.expires) {
		http.Error(w, "expired or unknown state", http.StatusBadRequest)
		return
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		// The user declined, or the provider refused. Send them back with a
		// reason rather than a blank page on a domain they don't recognise.
		b.redirectBack(w, r, pend.redirect, url.Values{"error": {errParam}})
		return
	}

	email, err := p.Exchange(r.URL.Query().Get("code"), b.callbackURI(name))
	if err != nil {
		slog.Warn("oauth exchange failed", "provider", name, "err", err)
		b.redirectBack(w, r, pend.redirect, url.Values{"error": {"exchange_failed"}})
		return
	}

	assertion, err := b.mint(email, pend.nonce, name)
	if err != nil {
		slog.Error("oauth: minting assertion failed", "err", err)
		b.redirectBack(w, r, pend.redirect, url.Values{"error": {"server_error"}})
		return
	}

	b.redirectBack(w, r, pend.redirect, url.Values{"assertion": {assertion}})
}

// redirectBack returns to the client. Values go in the fragment, never the
// query: a fragment is not sent to servers and does not land in access logs or
// Referer headers, and an assertion is a bearer credential.
func (b *Broker) redirectBack(w http.ResponseWriter, r *http.Request, redirect string, vals url.Values) {
	http.Redirect(w, r, redirect+"#"+vals.Encode(), http.StatusFound)
}

// Assertion is what the box receives and verifies.
type Assertion struct {
	Email    string `json:"email"`
	Nonce    string `json:"nonce"`
	Provider string `json:"provider"`
	jwt.RegisteredClaims
}

func (b *Broker) mint(email, nonce, provider string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	now := time.Now()
	claims := Assertion{
		Email:    email,
		Nonce:    nonce,
		Provider: provider,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    b.issuer,
			Audience:  jwt.ClaimStrings{audience},
			Subject:   email,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(assertionTTL)),
			// A unique id lets the box refuse a second use of the same
			// assertion inside its short lifetime.
			ID: randomToken(),
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = b.keyID
	return tok.SignedString(b.key)
}

// handleJWKS publishes the public key, so a box can verify assertions without
// ever holding a secret.
func (b *Broker) handleJWKS(w http.ResponseWriter, r *http.Request) {
	pub := b.key.PublicKey
	jwk := map[string]any{
		"kty": "EC",
		"crv": "P-256",
		"use": "sig",
		"alg": "ES256",
		"kid": b.keyID,
		"x":   b64(pub.X.Bytes()),
		"y":   b64(pub.Y.Bytes()),
	}
	w.Header().Set("Content-Type", "application/json")
	// Cached, but not for long: a key rotation has to reach boxes without a
	// deploy on their side.
	w.Header().Set("Cache-Control", "public, max-age=300")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwk}})
}

func (b *Broker) callbackURI(provider string) string {
	return fmt.Sprintf("%s/oauth/%s/callback", b.issuer, provider)
}

// redirectAllowed matches a redirect against the allow-list.
//
// A bare HasPrefix is not good enough: "northrou://auth" would then also allow
// "northrou://authX", and "https://app.example" would allow
// "https://app.example.evil.com". So an entry matches exactly, or it acts as a
// prefix only when it ends in "/" -- a real path boundary that cannot be
// extended into another host or scheme.
func (b *Broker) redirectAllowed(redirect string) bool {
	for _, allowed := range b.allowedRedirects {
		if redirect == allowed {
			return true
		}
		if strings.HasSuffix(allowed, "/") && strings.HasPrefix(redirect, allowed) {
			return true
		}
	}
	return false
}

// pruneLoop drops abandoned states: a user who starts a login and closes the tab
// would otherwise leak an entry forever.
func (b *Broker) pruneLoop() {
	for range time.Tick(time.Minute) {
		now := time.Now()
		b.mu.Lock()
		for k, v := range b.pending {
			if now.After(v.expires) {
				delete(b.pending, k)
			}
		}
		b.mu.Unlock()
	}
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func randomToken() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// thumbprint derives a stable key id from the public key, so a rotation changes
// the kid automatically and boxes can tell the keys apart.
func thumbprint(pub *ecdsa.PublicKey) string {
	sum := sha256.Sum256(append(pub.X.Bytes(), pub.Y.Bytes()...))
	return base64.RawURLEncoding.EncodeToString(sum[:8])
}

// ParseKey loads an ES256 private key from a PEM string (OAUTH_SIGNING_KEY).
func ParseKey(pemStr string) (*ecdsa.PrivateKey, error) {
	if strings.TrimSpace(pemStr) == "" {
		return nil, nil
	}
	return jwt.ParseECPrivateKeyFromPEM([]byte(pemStr))
}
