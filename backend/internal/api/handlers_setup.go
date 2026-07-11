package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/mail"

	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
)

// validEmail reports whether s parses as a single RFC 5322 address.
func validEmail(s string) bool {
	addr, err := mail.ParseAddress(s)
	return err == nil && addr.Address == s
}

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
	Email      string   `json:"email"`
	MovieDirs  []string `json:"movie_dirs"`
	ShowDirs   []string `json:"show_dirs"`
	TMDBAPIKey string   `json:"tmdb_api_key"`
	EnableRemote bool   `json:"enable_remote"`

	// Optional SMTP settings so the admin can receive sign-in pins after setup.
	// If omitted, pins are logged to the server log until email is configured.
	SMTPHost     string `json:"smtp_host"`
	SMTPPort     int    `json:"smtp_port"`
	SMTPUsername string `json:"smtp_username"`
	SMTPPassword string `json:"smtp_password"`
	FromAddress  string `json:"from_address"`
	FromName     string `json:"from_name"`
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
	email := auth.NormalizeEmail(req.Email)
	if !validEmail(email) {
		writeError(w, http.StatusBadRequest, "a valid email address is required")
		return
	}

	uid, err := a.DB.CreateUser(r.Context(), email, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create user failed")
		return
	}

	// Persist config.
	a.Cfg.Media.MovieDirs = req.MovieDirs
	a.Cfg.Media.ShowDirs = req.ShowDirs
	a.Cfg.TMDB.APIKey = req.TMDBAPIKey
	a.Cfg.Remote.Enabled = req.EnableRemote
	a.Cfg.Email.SMTPHost = req.SMTPHost
	a.Cfg.Email.SMTPPort = req.SMTPPort
	a.Cfg.Email.SMTPUsername = req.SMTPUsername
	a.Cfg.Email.SMTPPassword = req.SMTPPassword
	a.Cfg.Email.FromAddress = req.FromAddress
	a.Cfg.Email.FromName = req.FromName
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
	// First run has no mailbox loop yet: log the new admin straight in by
	// minting tokens directly instead of sending a pin.
	pair, err := a.Auth.IssueForUser(r.Context(), user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "auto-login failed")
		return
	}

	writeJSON(w, http.StatusCreated, setupCompleteResponse{
		User:           userDTO{ID: user.ID, Email: user.Email, IsAdmin: true},
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
