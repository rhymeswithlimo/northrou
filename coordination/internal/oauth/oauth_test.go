package oauth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fakeProvider stands in for Google/Apple: the real ones are an HTTP round trip
// to a service we cannot reach from a test, and what matters here is the
// broker's own behaviour, not their token endpoints.
type fakeProvider struct {
	email string
	err   error
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) AuthURL(state, redirectURI string) string {
	return "https://provider.example/auth?state=" + url.QueryEscape(state)
}
func (f *fakeProvider) Exchange(code, redirectURI string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.email, nil
}

func newTestBroker(t *testing.T, p Provider) (*Broker, *httptest.Server) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	b, err := NewBroker(Config{
		Issuer:           srv.URL,
		Key:              key,
		AllowedRedirects: []string{"northrou://auth", "http://localhost:5173/"},
	}, p)
	if err != nil {
		t.Fatal(err)
	}
	b.Routes(mux)
	return b, srv
}

// noRedirect keeps the 302 so the test can read Location instead of following it.
func noRedirect() *http.Client {
	return &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
}

func TestStartRequiresAllowedRedirect(t *testing.T) {
	_, srv := newTestBroker(t, &fakeProvider{email: "a@b.com"})
	client := noRedirect()

	tests := []struct {
		name     string
		redirect string
		want     int
	}{
		{"an allow-listed redirect starts the flow", "northrou://auth", http.StatusFound},
		{"an allow-listed prefix starts the flow", "http://localhost:5173/login.html", http.StatusFound},
		// Without this the broker is an open redirector: an attacker sends a
		// victim through a genuine Google login and collects the assertion.
		{"an unknown redirect is refused", "https://evil.example/steal", http.StatusBadRequest},
		{"a lookalike prefix is refused", "northrou://authX", http.StatusBadRequest},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := client.Get(srv.URL + "/oauth/fake/start?nonce=n1&redirect=" + url.QueryEscape(tc.redirect))
			if err != nil {
				t.Fatal(err)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.want {
				t.Errorf("status = %d, want %d", res.StatusCode, tc.want)
			}
		})
	}
}

func TestStartRequiresNonceAndRedirect(t *testing.T) {
	_, srv := newTestBroker(t, &fakeProvider{email: "a@b.com"})
	client := noRedirect()
	for _, q := range []string{"", "?nonce=n1", "?redirect=northrou://auth"} {
		res, err := client.Get(srv.URL + "/oauth/fake/start" + q)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("query %q: status = %d, want 400", q, res.StatusCode)
		}
	}
}

// The full flow: start, callback, and an assertion that actually verifies.
func TestCallbackMintsVerifiableAssertion(t *testing.T) {
	b, srv := newTestBroker(t, &fakeProvider{email: "Tomas@Example.com "})
	client := noRedirect()

	res, err := client.Get(srv.URL + "/oauth/fake/start?nonce=n-123&redirect=" + url.QueryEscape("northrou://auth"))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()

	// Pull the state the broker generated out of the provider redirect.
	loc, err := url.Parse(res.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("no state in the provider redirect")
	}

	cb, err := client.Get(srv.URL + "/oauth/fake/callback?state=" + url.QueryEscape(state) + "&code=abc")
	if err != nil {
		t.Fatal(err)
	}
	cb.Body.Close()
	if cb.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", cb.StatusCode)
	}

	back := cb.Header.Get("Location")
	if !strings.HasPrefix(back, "northrou://auth#") {
		t.Fatalf("redirected to %q, want the client's redirect with a fragment", back)
	}
	// The assertion must ride in the fragment: a query string lands in access
	// logs and Referer headers, and this is a bearer credential.
	frag := strings.SplitN(back, "#", 2)[1]
	vals, err := url.ParseQuery(frag)
	if err != nil {
		t.Fatal(err)
	}
	raw := vals.Get("assertion")
	if raw == "" {
		t.Fatalf("no assertion in %q", frag)
	}

	// Verify it the way a box would.
	var claims Assertion
	tok, err := jwt.ParseWithClaims(raw, &claims, func(*jwt.Token) (any, error) {
		return &b.key.PublicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"}))
	if err != nil || !tok.Valid {
		t.Fatalf("the minted assertion does not verify: %v", err)
	}

	if claims.Email != "tomas@example.com" {
		t.Errorf("email = %q, want it normalised to tomas@example.com", claims.Email)
	}
	if claims.Nonce != "n-123" {
		t.Errorf("nonce = %q, want the one the client started with", claims.Nonce)
	}
	if got := claims.Audience; len(got) != 1 || got[0] != audience {
		t.Errorf("audience = %v, want [%s]", got, audience)
	}
	if claims.Issuer != srv.URL {
		t.Errorf("issuer = %q, want %q", claims.Issuer, srv.URL)
	}
	if claims.ID == "" {
		t.Error("no jti; the box needs one to refuse a replayed assertion")
	}
	if d := time.Until(claims.ExpiresAt.Time); d > assertionTTL+time.Second {
		t.Errorf("expires in %v, want <= %v", d, assertionTTL)
	}
	if tok.Header["kid"] == nil {
		t.Error("no kid; the box cannot pick a key from the JWKS without one")
	}
}

// A state is one authorization. Replaying it must not mint a second assertion.
func TestCallbackStateIsSingleUse(t *testing.T) {
	_, srv := newTestBroker(t, &fakeProvider{email: "a@b.com"})
	client := noRedirect()

	res, _ := client.Get(srv.URL + "/oauth/fake/start?nonce=n1&redirect=" + url.QueryEscape("northrou://auth"))
	res.Body.Close()
	loc, _ := url.Parse(res.Header.Get("Location"))
	state := loc.Query().Get("state")

	first, _ := client.Get(srv.URL + "/oauth/fake/callback?state=" + url.QueryEscape(state) + "&code=abc")
	first.Body.Close()
	if first.StatusCode != http.StatusFound {
		t.Fatalf("first callback = %d, want 302", first.StatusCode)
	}

	second, _ := client.Get(srv.URL + "/oauth/fake/callback?state=" + url.QueryEscape(state) + "&code=abc")
	second.Body.Close()
	if second.StatusCode != http.StatusBadRequest {
		t.Errorf("replayed state = %d, want 400", second.StatusCode)
	}
}

func TestCallbackRejectsUnknownState(t *testing.T) {
	_, srv := newTestBroker(t, &fakeProvider{email: "a@b.com"})
	res, err := noRedirect().Get(srv.URL + "/oauth/fake/callback?state=made-up&code=abc")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", res.StatusCode)
	}
}

func TestJWKSPublishesTheVerifyingKey(t *testing.T) {
	b, srv := newTestBroker(t, &fakeProvider{email: "a@b.com"})
	res, err := http.Get(srv.URL + "/oauth/jwks")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}

	var body struct {
		Keys []map[string]any `json:"keys"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Keys) != 1 {
		t.Fatalf("got %d keys, want 1", len(body.Keys))
	}
	k := body.Keys[0]
	if k["kid"] != b.keyID {
		t.Errorf("kid = %v, want %v", k["kid"], b.keyID)
	}
	if k["crv"] != "P-256" || k["kty"] != "EC" || k["alg"] != "ES256" {
		t.Errorf("unexpected key params: %v", k)
	}
	// A private key must never appear in a public JWKS.
	if _, leaked := k["d"]; leaked {
		t.Fatal("the JWKS leaked the private key component")
	}
}
