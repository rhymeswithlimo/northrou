package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
)

type setupStatusResponse struct {
	NeedsSetup bool   `json:"needs_setup"`
	Version    string `json:"version"`
}

// handleSetupStatus reports whether first-run setup is still required (no
// accounts exist yet).
func (a *API) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := a.DB.CountUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "status check failed")
		return
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{NeedsSetup: n == 0})
}

type setupCompleteRequest struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	MovieDirs  []string `json:"movie_dirs"`
	ShowDirs   []string `json:"show_dirs"`
	TMDBAPIKey string   `json:"tmdb_api_key"`
	EnableRemote bool   `json:"enable_remote"`
}

type setupCompleteResponse struct {
	User           userDTO `json:"user"`
	ConnectionCode string  `json:"connection_code"`
	AccessToken    string  `json:"access_token"`
	RefreshToken   string  `json:"refresh_token"`
}

// handleSetupComplete performs first-run setup: creates the admin account,
// persists media folders and TMDB key to config, issues a remote connection
// code, and returns a logged-in token pair. It is only allowed while no
// accounts exist.
func (a *API) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	n, err := a.DB.CountUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "setup failed")
		return
	}
	if n > 0 {
		writeError(w, http.StatusConflict, "setup already completed")
		return
	}

	var req setupCompleteRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "username required and password must be at least 8 characters")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "hash failed")
		return
	}
	uid, err := a.DB.CreateUser(r.Context(), req.Username, hash, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create user failed")
		return
	}

	// Persist config.
	a.Cfg.Media.MovieDirs = req.MovieDirs
	a.Cfg.Media.ShowDirs = req.ShowDirs
	a.Cfg.TMDB.APIKey = req.TMDBAPIKey
	a.Cfg.Remote.Enabled = req.EnableRemote
	if a.Cfg.Remote.ServerID == "" {
		a.Cfg.Remote.ServerID = randomHex(16)
	}
	if a.Cfg.Remote.ConnectionCode == "" {
		a.Cfg.Remote.ConnectionCode = connectionCode()
	}
	if err := a.Cfg.Save(a.ConfigPath); err != nil {
		writeError(w, http.StatusInternalServerError, "save config failed")
		return
	}

	user, _ := a.DB.GetUser(r.Context(), uid)
	_, pair, err := a.Auth.Authenticate(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "auto-login failed")
		return
	}

	writeJSON(w, http.StatusCreated, setupCompleteResponse{
		User:           userDTO{ID: user.ID, Username: user.Username, IsAdmin: true},
		ConnectionCode: a.Cfg.Remote.ConnectionCode,
		AccessToken:    pair.AccessToken,
		RefreshToken:   pair.RefreshToken,
	})
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// connectionCode returns a human-shareable pairing code like "NR-3F9A-K2X7".
func connectionCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no ambiguous chars
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	out := make([]byte, 8)
	for i := range b {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "NR-" + string(out[:4]) + "-" + string(out[4:])
}
