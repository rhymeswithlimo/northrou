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
	// streamTTL is how long a media stream token stays valid. A browser <video>
	// element bakes the token into the media URL (it can't send an Authorization
	// header), and it can't refresh it mid-playback, so the token has to outlast
	// a long movie plus pauses. It is safe to make it this long because a
	// stream-scoped token can only fetch media bytes, and only for the one file
	// it was minted for - never the rest of the API.
	streamTTL = 12 * time.Hour
)

// scopeStream marks a token that may fetch media bytes but nothing else. A
// normal (full-access) token carries no scope.
const scopeStream = "stream"

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
//
// Scope is empty for a normal session token and "stream" for a media stream
// token (see IssueStreamToken); FileID, when set on a stream token, binds it to
// a single media file so a leaked stream URL can't range over the whole library.
type Claims struct {
	ProfileID int64  `json:"pid"`
	Scope     string `json:"scope,omitempty"`
	FileID    int64  `json:"fid,omitempty"`
	jwt.RegisteredClaims
}

// AllowsFile reports whether these claims may fetch bytes for fileID. A normal
// session token (no scope) may fetch anything; a file-bound stream token may
// fetch only its own file.
func (c *Claims) AllowsFile(fileID int64) bool {
	if c.Scope == scopeStream && c.FileID != 0 {
		return c.FileID == fileID
	}
	return true
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
	claims, err := s.parseToken(tokenString)
	if err != nil {
		return nil, err
	}
	// A stream token is media-only and long-lived; it must never authenticate a
	// full API session (its whole safety argument is that leaking one in a media
	// URL grants nothing but that file's bytes).
	if claims.Scope != "" {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// VerifyMedia verifies a token presented on a media byte route. It accepts both
// a normal session token and a stream token, but nothing with an unknown scope.
func (s *Service) VerifyMedia(tokenString string) (*Claims, error) {
	claims, err := s.parseToken(tokenString)
	if err != nil {
		return nil, err
	}
	if claims.Scope != "" && claims.Scope != scopeStream {
		return nil, ErrInvalidToken
	}
	return claims, nil
}

// parseToken verifies a JWT's signature and expiry and returns its claims,
// without any scope check.
func (s *Service) parseToken(tokenString string) (*Claims, error) {
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

// issueAccess mints a signed full-access JWT scoped to a profile, valid for ttl.
func (s *Service) issueAccess(profileID int64, ttl time.Duration) (string, time.Time, error) {
	return s.issueToken(profileID, "", 0, ttl)
}

// IssueStreamToken mints a long-lived, media-only token bound to a single file.
// It is what a browser <video>/HLS player carries in the media URL, since those
// requests can't send an Authorization header. It can fetch bytes for fileID and
// nothing else - VerifyAccess rejects it on every other route.
func (s *Service) IssueStreamToken(profileID, fileID int64) (string, time.Time, error) {
	return s.issueToken(profileID, scopeStream, fileID, streamTTL)
}

// issueToken mints a signed JWT scoped to a profile, with an optional scope and
// file binding, valid for ttl.
func (s *Service) issueToken(profileID int64, scope string, fileID int64, ttl time.Duration) (string, time.Time, error) {
	now := time.Now()
	exp := now.Add(ttl)
	claims := Claims{
		ProfileID: profileID,
		Scope:     scope,
		FileID:    fileID,
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
