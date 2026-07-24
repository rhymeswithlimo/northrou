package auth

import (
	"context"
	"crypto/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/remote"
)

type ctxKey int

const claimsKey ctxKey = iota

// Middleware returns HTTP middleware that requires a valid Bearer access token
// and stashes the claims in the request context.
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		if tok == "" {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		claims, err := s.VerifyAccess(tok)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// MediaMiddleware guards the media byte routes (stream, HLS segments). Unlike
// Middleware it also accepts the token as an `access_token` query parameter,
// because a browser <video> element and native HLS fetch these URLs directly and
// can't attach an Authorization header. It accepts a normal session token or a
// stream token (see VerifyMedia); the per-file binding is enforced in the
// handlers, which know the requested media id. Must be used in place of, not on
// top of, Middleware.
func (s *Service) MediaMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		if tok == "" {
			tok = strings.TrimSpace(r.URL.Query().Get("access_token"))
		}
		if tok == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		claims, err := s.VerifyMedia(tok)
		if err != nil {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), claimsKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireLocal wraps a handler so only trusted local requests may proceed: a
// request that did not arrive over the tunnel and whose peer is loopback or a
// private/link-local address (see remote.IsLocal). Requests tunneled in from a
// remote client, or arriving on the direct path from a public IP, are refused.
// This is the sole admin gate: admin is a property of local access, not a token
// capability. Must be used inside Middleware.
func (s *Service) RequireLocal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !remote.IsLocal(r) {
			http.Error(w, "admin actions are only available on the local network or via the CLI", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ClaimsFrom extracts auth claims placed by Middleware.
func ClaimsFrom(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey).(*Claims)
	return c, ok
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// LoadOrCreateSecret returns the JWT signing secret stored in dataDir, creating
// a new random 32-byte secret on first use. The file is written with 0600
// permissions.
func LoadOrCreateSecret(dataDir string) ([]byte, error) {
	path := filepath.Join(dataDir, "jwt.secret")
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return b, nil
	}
	secret := make([]byte, 48)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		return nil, err
	}
	return secret, nil
}
