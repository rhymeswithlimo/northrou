package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

type userDTO struct {
	ID      int64  `json:"id"`
	Email   string `json:"email"`
	IsAdmin bool   `json:"is_admin"`
}

type requestPinRequest struct {
	Email string `json:"email"`
}

// handleRequestPin emails a one-time sign-in pin to the address if an account
// exists for it. It always returns 200 with the same body so callers cannot use
// it to discover which emails are registered.
func (a *API) handleRequestPin(w http.ResponseWriter, r *http.Request) {
	var req requestPinRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Email) == "" {
		writeError(w, http.StatusBadRequest, "email required")
		return
	}
	if err := a.Auth.RequestPin(r.Context(), req.Email); err != nil {
		// Log server-side but do not surface: the response is identical whether
		// or not the address exists.
		slog.Warn("request pin failed", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "If an account exists for that email, a sign-in code has been sent.",
	})
}

type verifyPinRequest struct {
	Email string `json:"email"`
	Pin   string `json:"pin"`
}

type loginResponse struct {
	User         userDTO `json:"user"`
	AccessToken  string  `json:"access_token"`
	RefreshToken string  `json:"refresh_token"`
	ExpiresAt    string  `json:"expires_at"`
}

// handleVerifyPin exchanges a valid emailed pin for a token pair.
func (a *API) handleVerifyPin(w http.ResponseWriter, r *http.Request) {
	var req verifyPinRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	user, pair, err := a.Auth.VerifyPin(r.Context(), req.Email, strings.TrimSpace(req.Pin))
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid or expired code")
			return
		}
		writeError(w, http.StatusInternalServerError, "login failed")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{
		User:         userDTO{ID: user.ID, Email: user.Email, IsAdmin: user.IsAdmin},
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresAt:    pair.ExpiresAt.UTC().Format(http.TimeFormat),
	})
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (a *API) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	pair, err := a.Auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		ExpiresAt:    pair.ExpiresAt.UTC().Format(http.TimeFormat),
	})
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	_ = a.Auth.Logout(r.Context(), req.RefreshToken)
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated")
		return
	}
	user, err := a.DB.GetUser(r.Context(), claims.UserID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "user not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, userDTO{ID: user.ID, Email: user.Email, IsAdmin: user.IsAdmin})
}
