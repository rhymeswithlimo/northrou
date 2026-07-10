// Package auth provides password hashing, JWT access tokens, rotating refresh
// tokens (stored hashed and revocable in the database), and HTTP middleware.
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
	"golang.org/x/crypto/bcrypt"
)

var (
	// ErrInvalidCredentials is returned when a username/password check fails.
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

// NewService constructs an auth Service with the given signing secret.
func NewService(database *db.DB, secret []byte) *Service {
	return &Service{
		db:         database,
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

// HashPassword returns a bcrypt hash of the plaintext password.
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(b), err
}

// Authenticate verifies a username/password and issues a token pair.
func (s *Service) Authenticate(ctx context.Context, username, password string) (*model.User, *TokenPair, error) {
	user, err := s.db.GetUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// Run a dummy compare to reduce username-enumeration timing signal.
			_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinvalidinv"), []byte(password))
			return nil, nil, ErrInvalidCredentials
		}
		return nil, nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, nil, ErrInvalidCredentials
	}
	pair, err := s.issue(ctx, user)
	if err != nil {
		return nil, nil, err
	}
	return user, pair, nil
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
