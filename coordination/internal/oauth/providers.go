package oauth

import (
	"crypto/ecdsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Both providers are OIDC, so the email arrives inside the id_token rather than
// from a userinfo call. We read it out of the token we just received over TLS
// from the provider's own token endpoint, which is why the id_token's signature
// is not re-verified here: the transport already authenticated the issuer, and
// the code is single-use and bound to our client secret.

type idTokenClaims struct {
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"` // Google sends bool, Apple sends "true"
	jwt.RegisteredClaims
}

// emailVerified normalises the two shapes providers use.
func (c idTokenClaims) emailVerified() bool {
	switch v := c.EmailVerified.(type) {
	case bool:
		return v
	case string:
		return v == "true"
	default:
		return false
	}
}

func emailFromIDToken(raw string) (string, error) {
	var claims idTokenClaims
	// The token came straight from the provider's token endpoint over TLS, so
	// parse without re-verifying the signature; there is no untrusted hop.
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	if _, _, err := parser.ParseUnverified(raw, &claims); err != nil {
		return "", fmt.Errorf("parse id_token: %w", err)
	}
	if claims.Email == "" {
		return "", fmt.Errorf("id_token carried no email")
	}
	if !claims.emailVerified() {
		// An unverified address proves nothing: anyone can type it in.
		return "", fmt.Errorf("provider has not verified %q", claims.Email)
	}
	return strings.ToLower(strings.TrimSpace(claims.Email)), nil
}

/* ==================== Google ==================== */

// GoogleProvider implements Sign in with Google.
type GoogleProvider struct {
	ClientID     string
	ClientSecret string
	HTTP         *http.Client
}

// NewGoogle returns a provider, or nil when unconfigured so the broker can
// simply not offer it.
func NewGoogle(clientID, clientSecret string) *GoogleProvider {
	if clientID == "" || clientSecret == "" {
		return nil
	}
	return &GoogleProvider{ClientID: clientID, ClientSecret: clientSecret, HTTP: httpClient()}
}

func (g *GoogleProvider) Name() string { return "google" }

func (g *GoogleProvider) AuthURL(state, redirectURI string) string {
	q := url.Values{
		"client_id":     {g.ClientID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {"openid email"},
		"state":         {state},
		// The broker needs one identity assertion, not ongoing access, so it
		// never asks for a refresh token or offline access.
		"prompt": {"select_account"},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + q.Encode()
}

func (g *GoogleProvider) Exchange(code, redirectURI string) (string, error) {
	if code == "" {
		return "", fmt.Errorf("no code")
	}
	form := url.Values{
		"code":          {code},
		"client_id":     {g.ClientID},
		"client_secret": {g.ClientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}
	return postForToken(g.HTTP, "https://oauth2.googleapis.com/token", form)
}

/* ==================== Apple ==================== */

// AppleProvider implements Sign in with Apple.
//
// Apple is the awkward one: there is no static client secret. It has to be a
// short-lived ES256 JWT signed with the private key from the developer account,
// minted per request. That key, and the $99/yr account behind it, is a large
// part of why this broker exists rather than every household registering its own
// OAuth client.
type AppleProvider struct {
	ServiceID string // the "client id": a Services ID, not the bundle id
	TeamID    string
	KeyID     string
	Key       *ecdsa.PrivateKey
	HTTP      *http.Client
}

// NewApple returns a provider, or nil when unconfigured.
func NewApple(serviceID, teamID, keyID string, key *ecdsa.PrivateKey) *AppleProvider {
	if serviceID == "" || teamID == "" || keyID == "" || key == nil {
		return nil
	}
	return &AppleProvider{ServiceID: serviceID, TeamID: teamID, KeyID: keyID, Key: key, HTTP: httpClient()}
}

func (a *AppleProvider) Name() string { return "apple" }

func (a *AppleProvider) AuthURL(state, redirectURI string) string {
	q := url.Values{
		"client_id":     {a.ServiceID},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {"email"},
		"state":         {state},
		// Apple only POSTs the callback when a scope is requested, and only
		// form_post is allowed with a scope.
		"response_mode": {"form_post"},
	}
	return "https://appleid.apple.com/auth/authorize?" + q.Encode()
}

// clientSecret mints Apple's per-request client secret.
func (a *AppleProvider) clientSecret() (string, error) {
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.RegisteredClaims{
		Issuer:    a.TeamID,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(5 * time.Minute)),
		Audience:  jwt.ClaimStrings{"https://appleid.apple.com"},
		Subject:   a.ServiceID,
	})
	tok.Header["kid"] = a.KeyID
	return tok.SignedString(a.Key)
}

func (a *AppleProvider) Exchange(code, redirectURI string) (string, error) {
	if code == "" {
		return "", fmt.Errorf("no code")
	}
	secret, err := a.clientSecret()
	if err != nil {
		return "", fmt.Errorf("apple client secret: %w", err)
	}
	form := url.Values{
		"code":          {code},
		"client_id":     {a.ServiceID},
		"client_secret": {secret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}
	return postForToken(a.HTTP, "https://appleid.apple.com/auth/token", form)
}

/* ==================== shared ==================== */

func httpClient() *http.Client { return &http.Client{Timeout: 15 * time.Second} }

func postForToken(client *http.Client, endpoint string, form url.Values) (string, error) {
	res, err := client.PostForm(endpoint, form)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", res.StatusCode, string(body))
	}

	var out struct {
		IDToken string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if out.IDToken == "" {
		return "", fmt.Errorf("token response carried no id_token")
	}
	return emailFromIDToken(out.IDToken)
}
