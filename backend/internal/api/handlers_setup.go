package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/mail"
	"strings"

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
// account exists yet).
func (a *API) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	exists, err := a.DB.AccountExists(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "status check failed")
		return
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{NeedsSetup: !exists})
}

type setupCompleteRequest struct {
	Email       string   `json:"email"`
	ProfileName string   `json:"profile_name"` // first profile; optional
	MovieDirs   []string `json:"movie_dirs"`
	ShowDirs    []string `json:"show_dirs"`
	TMDBAPIKey  string   `json:"tmdb_api_key"`
	EnableRemote bool    `json:"enable_remote"`

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
	Account        accountDTO `json:"account"`
	Profile        profileDTO `json:"profile"`
	ConnectionCode string     `json:"connection_code"`
	AccessToken    string     `json:"access_token"`
	RefreshToken   string     `json:"refresh_token"`
}

// handleSetupComplete performs first-run setup: establishes the account email
// and its first profile, persists media folders and TMDB key to config, issues
// a remote connection code, and returns a signed-in token pair elevated for the
// setup window. It is only allowed while no account exists.
func (a *API) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	exists, err := a.DB.AccountExists(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "setup failed")
		return
	}
	if exists {
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

	if err := a.DB.SetAccountEmail(r.Context(), email); err != nil {
		writeError(w, http.StatusInternalServerError, "create account failed")
		return
	}
	name := strings.TrimSpace(req.ProfileName)
	if name == "" {
		name = defaultProfileName(email)
	}
	pid, err := a.DB.CreateProfile(r.Context(), name, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create profile failed")
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

	prof, _ := a.DB.GetProfile(r.Context(), pid)
	// First run has no mailbox loop yet: sign the operator straight in with a
	// setup-elevated session instead of sending a pin.
	pair, err := a.Auth.IssueSetupSession(r.Context(), pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "auto-login failed")
		return
	}

	writeJSON(w, http.StatusCreated, setupCompleteResponse{
		Account:        accountDTO{Email: email},
		Profile:        profileDTO{ID: prof.ID, Name: prof.Name, Avatar: prof.Avatar},
		ConnectionCode: a.Cfg.Remote.ConnectionCode,
		AccessToken:    pair.AccessToken,
		RefreshToken:   pair.RefreshToken,
	})
}

// defaultProfileName derives a friendly first-profile name from the account
// email's local-part (e.g. "ada@example.com" -> "ada").
func defaultProfileName(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return "Me"
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
