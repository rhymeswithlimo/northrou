package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/mail"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
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

// setupCompleteRequest is the first-run payload.
//
// It carries no media folders and no mail settings. Folders are added on the
// box with `northrou admin` -> Library, and sign-in pins are delivered by the
// coordination relay, which needs no configuration here.
type setupCompleteRequest struct {
	Email        string `json:"email"`
	ProfileName  string `json:"profile_name"` // first profile; optional
	TMDBAPIKey   string `json:"tmdb_api_key"`
	EnableRemote bool   `json:"enable_remote"`
}

type setupCompleteResponse struct {
	Account        accountDTO `json:"account"`
	Profile        profileDTO `json:"profile"`
	ConnectionCode string     `json:"connection_code"`
	AccessToken    string     `json:"access_token"`
	RefreshToken   string     `json:"refresh_token"`
}

// handleSetupComplete performs first-run setup: establishes the account email
// and its first profile, persists the TMDB key to config, issues a remote
// connection code, and returns a signed-in token pair elevated for the setup
// window. It is only allowed while no account exists.
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

	// Persist config. Media folders are owned by the TUI, which may have
	// written some to disk (`northrou admin`) before this browser setup ran,
	// while the daemon's in-memory a.Cfg.Media stayed empty from boot. Pull
	// them back from disk so saving here does not erase them. Same reload the
	// config PATCH and scan paths do, for the same reason.
	if onDisk, err := config.Load(a.ConfigPath); err == nil {
		a.Cfg.Media = onDisk.Media
	}
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
