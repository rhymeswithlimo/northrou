// Package auth provides authentication for a single-household, multi-profile
// media server. There is one credential: the server's connection code. A device
// that presents the code (remote clients) or that reaches the box directly
// (a same-origin browser on the LAN, or the CLI) is issued a JWT access token
// scoped to a chosen profile plus a rotating refresh token that remembers the
// profile. There is no email, no password, and no sign-in provider.
//
// Admin is not a token capability: it is a property of *how* a request reached
// the box. Admin mutations are allowed only for local (non-tunnel) requests;
// see RequireLocal in middleware.go. The connection-code check itself lives in
// the API handler (which owns the config); this package only issues, rotates,
// and verifies tokens.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

var (
	// ErrInvalidCredentials is returned when there is no profile to sign in as.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrInvalidToken is returned when an access/refresh token is invalid or
	// expired.
	ErrInvalidToken = errors.New("invalid token")
)

const (
	defaultAccessTTL  = 15 * time.Minute
	defaultRefreshTTL = 30 * 24 * time.Hour
)

// Service issues and verifies tokens against the database.
type Service struct {
	db         *db.DB
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

// NewService constructs an auth Service with the given JWT signing secret.
func NewService(database *db.DB, secret []byte) *Service {
	return &Service{
		db:         database,
		secret:     secret,
		accessTTL:  defaultAccessTTL,
		refreshTTL: defaultRefreshTTL,
	}
}

// Claims are the JWT claims carried in an access token. ProfileID scopes all
// per-viewer data. There is deliberately no admin claim: admin is derived from
// the request transport, not carried in the token.
type Claims struct {
	ProfileID int64 `json:"pid"`
	jwt.RegisteredClaims
}

// TokenPair is what a successful pair/refresh/profile-switch returns.
type TokenPair struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Device describes the client a session belongs to, so the operator can see
// and revoke paired devices. ID is minted at pair time and inherited across
// token rotations; Name is a human label (typically derived from the client's
// User-Agent).
type Device struct {
	ID   string
	Name string
}

// NewDeviceID mints a random identifier for a newly pairing device.
func NewDeviceID() string {
	id, err := randomToken()
	if err != nil {
		return ""
	}
	return id[:16]
}

// IssueSession issues a signed-in session once access has been proven (the
// caller verified the connection code, or the request is local). It defaults to
// the first profile and hands back the full list so the client can show the
// profile picker.
func (s *Service) IssueSession(ctx context.Context, device Device) ([]model.Profile, *model.Profile, *TokenPair, error) {
	profiles, err := s.db.ListProfiles(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(profiles) == 0 {
		return nil, nil, nil, ErrInvalidCredentials
	}
	selected := profiles[0]
	pair, err := s.issuePair(ctx, selected.ID, device)
	if err != nil {
		return nil, nil, nil, err
	}
	return profiles, &selected, pair, nil
}

// IssueEphemeralSession issues an access token with NO stored refresh token:
// nothing persists and nothing shows up in the paired-devices list. This is
// for the operator's own short-lived tooling (`northrou status`, the admin
// TUI, the CLI's local API client), which would otherwise mint a permanent
// "device" every time the operator so much as looked at the server. The
// invariant it protects: **the paired-devices list means streaming clients,
// not the admin's own terminal.** Local-only by construction - the caller
// must not offer it to remote clients, which would use it to stream while
// dodging the device list.
func (s *Service) IssueEphemeralSession(ctx context.Context) ([]model.Profile, *model.Profile, *TokenPair, error) {
	profiles, err := s.db.ListProfiles(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(profiles) == 0 {
		return nil, nil, nil, ErrInvalidCredentials
	}
	selected := profiles[0]
	access, exp, err := s.issueAccess(selected.ID, s.accessTTL)
	if err != nil {
		return nil, nil, nil, err
	}
	return profiles, &selected, &TokenPair{AccessToken: access, ExpiresAt: exp}, nil
}

// SelectProfile switches the profile a device is signed in as. It validates the
// device's refresh token, atomically rotates it to the chosen profile, and
// returns a fresh pair. No re-auth is required: profiles are not a security
// boundary.
func (s *Service) SelectProfile(ctx context.Context, rawRefresh string, profileID int64) (*model.Profile, *TokenPair, error) {
	prof, err := s.db.GetProfile(ctx, profileID)
	if err != nil {
		return nil, nil, ErrInvalidCredentials
	}
	pair, err := s.rotate(ctx, rawRefresh, profileID)
	if err != nil {
		return nil, nil, err
	}
	return prof, pair, nil
}

// IssueSetupSession mints a signed-in session for first-run setup: an access
// token only, no stored refresh token. Setup is the operator's own terminal
// wizard, and its session must not linger in the paired-devices list as a
// phantom "Setup wizard" device (see IssueEphemeralSession).
func (s *Service) IssueSetupSession(ctx context.Context, profileID int64) (*TokenPair, error) {
	access, exp, err := s.issueAccess(profileID, s.accessTTL)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, ExpiresAt: exp}, nil
}

// Refresh validates a refresh token, atomically rotates it (revoking the old
// one), and returns a fresh pair scoped to the same profile the device was
// using.
func (s *Service) Refresh(ctx context.Context, rawRefresh string) (*TokenPair, error) {
	return s.rotate(ctx, rawRefresh, 0)
}

// rotate atomically consumes rawRefresh and issues a fresh pair bound to the same
// device. profileID > 0 re-scopes to that profile (a profile switch); 0 keeps
// the device's current profile. Rotation and issuance are one DB transaction, so
// concurrent use of the same token cannot double-spend, and replay of an
// already-rotated token trips reuse detection (the whole device family is
// revoked). All failure modes collapse to ErrInvalidToken for the caller.
func (s *Service) rotate(ctx context.Context, rawRefresh string, profileID int64) (*TokenPair, error) {
	newRefresh, err := randomToken()
	if err != nil {
		return nil, err
	}
	rotated, err := s.db.RotateRefreshToken(ctx, hashToken(rawRefresh), hashToken(newRefresh), time.Now().Add(s.refreshTTL), profileID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if _, err := s.db.GetProfile(ctx, rotated.ProfileID); err != nil {
		return nil, ErrInvalidToken // profile deleted
	}
	access, exp, err := s.issueAccess(rotated.ProfileID, s.accessTTL)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: newRefresh, ExpiresAt: exp}, nil
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

// issuePair mints an access token and a stored refresh token, both scoped to
// profileID and bound to the device.
func (s *Service) issuePair(ctx context.Context, profileID int64, device Device) (*TokenPair, error) {
	access, exp, err := s.issueAccess(profileID, s.accessTTL)
	if err != nil {
		return nil, err
	}
	refresh, err := s.newRefresh(ctx, profileID, device)
	if err != nil {
		return nil, err
	}
	return &TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresAt: exp}, nil
}

// issueAccess mints a signed access JWT scoped to a profile, valid for ttl.
func (s *Service) issueAccess(profileID int64, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	claims := Claims{
		ProfileID: profileID,
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

// newRefresh creates and stores a refresh token bound to a profile and device,
// returning the raw token. A device without an ID (a fresh pair) gets one
// minted here so rotations have something stable to inherit.
func (s *Service) newRefresh(ctx context.Context, profileID int64, device Device) (string, error) {
	refresh, err := randomToken()
	if err != nil {
		return "", err
	}
	if device.ID == "" {
		device.ID = NewDeviceID()
	}
	if err := s.db.InsertRefreshToken(ctx, profileID, hashToken(refresh), time.Now().Add(s.refreshTTL), device.ID, device.Name); err != nil {
		return "", err
	}
	return refresh, nil
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
