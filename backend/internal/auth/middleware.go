package auth

import (
	"context"
	"crypto/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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

// RequireAdmin wraps a handler so only OTP-elevated sessions may access it. The
// admin capability is carried as the "adm" claim, minted by verifying an emailed
// admin pin (see VerifyAdminOTP) and short-lived. Must be used inside Middleware.
func (s *Service) RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, ok := ClaimsFrom(r.Context())
		if !ok || !c.Admin {
			http.Error(w, "admin elevation required", http.StatusForbidden)
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
