// Package auth provides passwordless authentication: one-time sign-in pins
// (emailed to the account address, stored hashed and single-use), JWT access
// tokens, rotating refresh tokens (stored hashed and revocable in the
// database), and HTTP middleware.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

var (
	// ErrInvalidCredentials is returned when a pin check fails (wrong, expired,
	// exhausted, or no such account).
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrInvalidToken is returned when an access/refresh token is invalid or
	// expired.
	ErrInvalidToken = errors.New("invalid token")
)

const (
	defaultAccessTTL  = 15 * time.Minute
	defaultRefreshTTL = 30 * 24 * time.Hour

	// pinLength is the number of decimal digits in a sign-in pin.
	pinLength = 6
	// pinTTL is how long a pin stays valid after issue.
	pinTTL = 10 * time.Minute
	// maxPinAttempts caps wrong guesses before a pin is invalidated, bounding
	// online brute force against the small 6-digit space.
	maxPinAttempts = 5
	// pinCooldown throttles repeat pin requests for one address so a caller
	// cannot flood the inbox.
	pinCooldown = 60 * time.Second
)

// Mailer delivers a login pin to an email address.
type Mailer interface {
	SendPin(ctx context.Context, email, pin string) error
}

// Service issues and verifies tokens against the database and sends login pins.
type Service struct {
	db         *db.DB
	mailer     Mailer
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewService constructs an auth Service with the given signing secret and
// mailer. The secret also keys the HMAC used to hash stored pins, so a
// database-only leak cannot brute-force them offline.
func NewService(database *db.DB, secret []byte, mailer Mailer) *Service {
	return &Service{
		db:         database,
		mailer:     mailer,
		secret:     secret,
		accessTTL:  defaultAccessTTL,
		refreshTTL: defaultRefreshTTL,
	}
}

// Claims are the JWT claims carried in an access token.
type Claims struct {
	UserID  int64 `json:"uid"`
	IsAdmin bool  `json:"adm"`
	jwt.RegisteredClaims
}

// TokenPair is what a successful login/refresh returns.
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// NormalizeEmail lower-cases and trims an address so lookups and storage agree
// regardless of how the user typed it.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// RequestPin generates a fresh sign-in pin for the account with the given email
// and sends it via the mailer. To avoid account enumeration it reports no error
// when the address is unknown; the caller should respond identically in all
// cases. A recently-issued pin (within pinCooldown) is left in place rather than
// re-sent.
func (s *Service) RequestPin(ctx context.Context, email string) error {
	email = NormalizeEmail(email)
	user, err := s.db.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil // silent: do not reveal whether the address exists
		}
		return err
	}

	if existing, err := s.db.GetLoginPin(ctx, user.ID); err == nil {
		if time.Since(existing.CreatedAt) < pinCooldown {
			return nil // throttle: a pin was just sent
		}
	}

	pin, err := randomPin()
	if err != nil {
		return err
	}
	if err := s.db.ReplaceLoginPin(ctx, user.ID, s.hashPin(pin), time.Now().Add(pinTTL)); err != nil {
		return err
	}
	return s.mailer.SendPin(ctx, user.Email, pin)
}

// VerifyPin validates an emailed pin and, on success, consumes it and issues a
// token pair. Wrong, expired, or exhausted pins return ErrInvalidCredentials.
func (s *Service) VerifyPin(ctx context.Context, email, pin string) (*model.User, *TokenPair, error) {
	email = NormalizeEmail(email)
	user, err := s.db.GetUserByEmail(ctx, email)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	stored, err := s.db.GetLoginPin(ctx, user.ID)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	if time.Now().After(stored.ExpiresAt) || stored.Attempts >= maxPinAttempts {
		_ = s.db.DeleteLoginPin(ctx, stored.ID)
		return nil, nil, ErrInvalidCredentials
	}
	if !hmac.Equal([]byte(stored.Hash), []byte(s.hashPin(pin))) {
		_ = s.db.IncrementPinAttempts(ctx, stored.ID)
		return nil, nil, ErrInvalidCredentials
	}
	// Correct: consume the pin so it cannot be reused, then issue tokens.
	if err := s.db.DeleteLoginPin(ctx, stored.ID); err != nil {
		return nil, nil, err
	}
	pair, err := s.issue(ctx, user)
	if err != nil {
		return nil, nil, err
	}
	return user, pair, nil
}

// IssueForUser mints a token pair for an already-authenticated user. It exists
// for first-run setup, which creates the admin account and logs it straight in
// without a pin round-trip (there is no mailbox loop before mail is set up).
func (s *Service) IssueForUser(ctx context.Context, user *model.User) (*TokenPair, error) {
	return s.issue(ctx, user)
}

// issue mints an access JWT and a stored refresh token for the user.
func (s *Service) issue(ctx context.Context, user *model.User) (*TokenPair, error) {
	now := time.Now()
	exp := now.Add(s.accessTTL)
	claims := Claims{
		UserID:  user.ID,
		IsAdmin: user.IsAdmin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprint(user.ID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return nil, err
	}

	refresh, err := randomToken()
	if err != nil {
		return nil, err
	}
	if err := s.db.InsertRefreshToken(ctx, user.ID, hashToken(refresh), now.Add(s.refreshTTL)); err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresAt: exp}, nil
}

// Refresh validates a refresh token, rotates it (revoking the old one), and
// returns a fresh token pair.
func (s *Service) Refresh(ctx context.Context, rawRefresh string) (*TokenPair, error) {
	stored, err := s.db.GetRefreshToken(ctx, hashToken(rawRefresh))
	if err != nil {
		return nil, ErrInvalidToken
	}
	if stored.Revoked || time.Now().After(stored.ExpiresAt) {
		return nil, ErrInvalidToken
	}
	user, err := s.db.GetUser(ctx, stored.UserID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	// Rotate: revoke the presented token, issue a new pair.
	if err := s.db.RevokeRefreshToken(ctx, hashToken(rawRefresh)); err != nil {
		return nil, err
	}
	return s.issue(ctx, user)
}

// Logout revokes a refresh token.
func (s *Service) Logout(ctx context.Context, rawRefresh string) error {
	return s.db.RevokeRefreshToken(ctx, hashToken(rawRefresh))
}

// VerifyAccess parses and validates an access token, returning its claims.
func (s *Service) VerifyAccess(tokenString string) (*Claims, error) {
	claims := &Claims{}
	tok, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, ErrInvalidToken
		}
		return s.secret, nil
	})
	if err != nil || !tok.Valid {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// hashPin returns a keyed (HMAC-SHA256) hash of a pin. Keying with the server
// secret means the pin cannot be recovered from a database leak alone.
func (s *Service) hashPin(pin string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(pin))
	return hex.EncodeToString(mac.Sum(nil))
}

// randomPin returns a zero-padded decimal pin of pinLength digits.
func randomPin() (string, error) {
	max := big.NewInt(1)
	for range pinLength {
		max.Mul(max, big.NewInt(10))
	}
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", pinLength, n), nil
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
