// Package auth provides passwordless authentication for a single-account,
// multi-profile household. Sign-in and admin elevation both use one-time pins
// emailed to the account address (stored hashed and single-use). A successful
// sign-in yields a JWT access token scoped to a chosen profile plus a rotating
// refresh token that remembers the profile. Admin is not an identity: it is a
// short-lived capability minted by verifying a separate emailed pin, carried as
// the "adm" claim on an access token.
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
	// exhausted, or no account).
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrInvalidToken is returned when an access/refresh token is invalid or
	// expired.
	ErrInvalidToken = errors.New("invalid token")
)

const (
	defaultAccessTTL  = 15 * time.Minute
	defaultRefreshTTL = 30 * 24 * time.Hour
	// adminElevationTTL is how long an OTP-elevated access token can perform
	// admin mutations before another pin is required. Deliberately short.
	adminElevationTTL = 10 * time.Minute

	// pinLength is the number of decimal digits in a one-time pin.
	pinLength = 6
	// pinTTL is how long a pin stays valid after issue.
	pinTTL = 10 * time.Minute
	// maxPinAttempts caps wrong guesses before a pin is invalidated, bounding
	// online brute force against the small 6-digit space.
	maxPinAttempts = 5
	// pinCooldown throttles repeat pin requests so a caller cannot flood the
	// account inbox.
	pinCooldown = 60 * time.Second
)

// Mailer delivers a one-time pin to an email address.
type Mailer interface {
	SendPin(ctx context.Context, email, pin string) error
}

// Service issues and verifies tokens against the database and sends pins.
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

// Claims are the JWT claims carried in an access token. ProfileID scopes all
// per-viewer data; Admin marks an OTP-elevated, short-lived session.
type Claims struct {
	ProfileID int64 `json:"pid"`
	Admin     bool  `json:"adm"`
	jwt.RegisteredClaims
}

// TokenPair is what a successful login/refresh/profile-switch returns.
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// NormalizeEmail lower-cases and trims an address so lookups and storage agree
// regardless of how it was typed.
func NormalizeEmail(email string) string {
	return normalize(email)
}

// RequestLoginPin generates a sign-in pin for the account and emails it, if the
// supplied address matches the account email. To avoid revealing whether an
// address is the account's, it reports no error when the address does not match
// or no account exists; callers should respond identically in all cases. A
// recently-issued pin (within pinCooldown) is left in place rather than re-sent.
func (s *Service) RequestLoginPin(ctx context.Context, email string) error {
	acct, err := s.db.GetAccount(ctx)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil // silent: setup has not run
		}
		return err
	}
	if normalize(email) != acct.Email {
		return nil // silent: not the account address
	}
	if existing, err := s.db.GetPin(ctx, db.PinLogin); err == nil {
		if time.Since(existing.CreatedAt) < pinCooldown {
			return nil // throttle
		}
	}
	pin, err := randomPin()
	if err != nil {
		return err
	}
	if err := s.db.ReplacePin(ctx, db.PinLogin, s.hashPin(pin), time.Now().Add(pinTTL)); err != nil {
		return err
	}
	return s.mailer.SendPin(ctx, acct.Email, pin)
}

// VerifyLoginPin validates an emailed sign-in pin against the account and, on
// success, returns the profile list, the default profile (first created), and a
// token pair scoped to it. The client then shows the profile picker and may
// call SelectProfile to switch. Wrong/expired/exhausted pins return
// ErrInvalidCredentials.
func (s *Service) VerifyLoginPin(ctx context.Context, email, pin string) ([]model.Profile, *model.Profile, *TokenPair, error) {
	acct, err := s.db.GetAccount(ctx)
	if err != nil || normalize(email) != acct.Email {
		return nil, nil, nil, ErrInvalidCredentials
	}
	ok, err := s.consumePin(ctx, db.PinLogin, pin)
	if err != nil {
		return nil, nil, nil, err
	}
	if !ok {
		return nil, nil, nil, ErrInvalidCredentials
	}
	profiles, err := s.db.ListProfiles(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(profiles) == 0 {
		return nil, nil, nil, ErrInvalidCredentials
	}
	selected := profiles[0]
	pair, err := s.issuePair(ctx, selected.ID)
	if err != nil {
		return nil, nil, nil, err
	}
	return profiles, &selected, pair, nil
}

// SelectProfile switches the profile a device is signed in as. It validates the
// device's refresh token, rotates it, and returns a fresh pair scoped to the
// chosen profile. No pin is required: profiles are not a security boundary.
func (s *Service) SelectProfile(ctx context.Context, rawRefresh string, profileID int64) (*model.Profile, *TokenPair, error) {
	stored, err := s.db.GetRefreshToken(ctx, hashToken(rawRefresh))
	if err != nil || stored.Revoked || time.Now().After(stored.ExpiresAt) {
		return nil, nil, ErrInvalidToken
	}
	prof, err := s.db.GetProfile(ctx, profileID)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	if err := s.db.RevokeRefreshToken(ctx, hashToken(rawRefresh)); err != nil {
		return nil, nil, err
	}
	pair, err := s.issuePair(ctx, profileID)
	if err != nil {
		return nil, nil, err
	}
	return prof, pair, nil
}

// RequestAdminOTP emails a one-time admin-elevation code to the account address.
// Anyone signed in may request it; possession of the emailed code is what grants
// elevation. Throttled by pinCooldown.
func (s *Service) RequestAdminOTP(ctx context.Context) error {
	acct, err := s.db.GetAccount(ctx)
	if err != nil {
		return err
	}
	if existing, err := s.db.GetPin(ctx, db.PinAdmin); err == nil {
		if time.Since(existing.CreatedAt) < pinCooldown {
			return nil
		}
	}
	pin, err := randomPin()
	if err != nil {
		return err
	}
	if err := s.db.ReplacePin(ctx, db.PinAdmin, s.hashPin(pin), time.Now().Add(pinTTL)); err != nil {
		return err
	}
	return s.mailer.SendPin(ctx, acct.Email, pin)
}

// VerifyAdminOTP validates an admin-elevation code and, on success, mints a
// short-lived access token carrying the admin capability, scoped to the calling
// profile. There is no refresh token: elevation is deliberately ephemeral.
func (s *Service) VerifyAdminOTP(ctx context.Context, profileID int64, otp string) (string, time.Time, error) {
	ok, err := s.consumePin(ctx, db.PinAdmin, otp)
	if err != nil {
		return "", time.Time{}, err
	}
	if !ok {
		return "", time.Time{}, ErrInvalidCredentials
	}
	return s.issueAccess(profileID, true, adminElevationTTL)
}

// IssueSetupSession mints a signed-in session for first-run setup: an access
// token elevated for the setup window (so the operator can add media and scan
// immediately) plus a normal refresh token. Elevation lapses when the access
// token expires; later admin actions require an emailed OTP like anyone else.
func (s *Service) IssueSetupSession(ctx context.Context, profileID int64) (*TokenPair, error) {
	access, exp, err := s.issueAccess(profileID, true, s.accessTTL)
	if err != nil {
		return nil, err
	}
	refresh, err := s.newRefresh(ctx, profileID)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresAt: exp}, nil
}

// Refresh validates a refresh token, rotates it (revoking the old one), and
// returns a fresh pair scoped to the same profile the device was using.
func (s *Service) Refresh(ctx context.Context, rawRefresh string) (*TokenPair, error) {
	stored, err := s.db.GetRefreshToken(ctx, hashToken(rawRefresh))
	if err != nil {
		return nil, ErrInvalidToken
	}
	if stored.Revoked || time.Now().After(stored.ExpiresAt) || stored.ProfileID == 0 {
		return nil, ErrInvalidToken
	}
	if _, err := s.db.GetProfile(ctx, stored.ProfileID); err != nil {
		return nil, ErrInvalidToken // profile deleted
	}
	if err := s.db.RevokeRefreshToken(ctx, hashToken(rawRefresh)); err != nil {
		return nil, err
	}
	return s.issuePair(ctx, stored.ProfileID)
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

// issuePair mints a normal (non-elevated) access token and a stored refresh
// token, both scoped to profileID.
func (s *Service) issuePair(ctx context.Context, profileID int64) (*TokenPair, error) {
	access, exp, err := s.issueAccess(profileID, false, s.accessTTL)
	if err != nil {
		return nil, err
	}
	refresh, err := s.newRefresh(ctx, profileID)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresAt: exp}, nil
}

// issueAccess mints a signed access JWT scoped to a profile, optionally carrying
// the admin capability, valid for ttl.
func (s *Service) issueAccess(profileID int64, admin bool, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	claims := Claims{
		ProfileID: profileID,
		Admin:     admin,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprint(profileID),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(exp),
		},
	}
	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return access, exp, nil
}

// newRefresh creates and stores a refresh token bound to a profile, returning
// the raw token.
func (s *Service) newRefresh(ctx context.Context, profileID int64) (string, error) {
	refresh, err := randomToken()
	if err != nil {
		return "", err
	}
	if err := s.db.InsertRefreshToken(ctx, profileID, hashToken(refresh), time.Now().Add(s.refreshTTL)); err != nil {
		return "", err
	}
	return refresh, nil
}

// consumePin checks a submitted pin against the active pin for a purpose. On a
// correct pin it deletes it and returns true. Wrong pins increment the attempt
// counter; expired/exhausted pins are deleted. It never returns ErrInvalid; a
// false result means "no valid pin", which callers map to ErrInvalidCredentials.
func (s *Service) consumePin(ctx context.Context, purpose db.PinPurpose, pin string) (bool, error) {
	stored, err := s.db.GetPin(ctx, purpose)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if time.Now().After(stored.ExpiresAt) || stored.Attempts >= maxPinAttempts {
		_ = s.db.DeletePin(ctx, purpose)
		return false, nil
	}
	if !hmac.Equal([]byte(stored.Hash), []byte(s.hashPin(pin))) {
		_ = s.db.IncrementPinAttempts(ctx, purpose)
		return false, nil
	}
	if err := s.db.DeletePin(ctx, purpose); err != nil {
		return false, err
	}
	return true, nil
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

func normalize(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
