package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/remote"
)

type setupStatusResponse struct {
	NeedsSetup bool   `json:"needs_setup"`
	Version    string `json:"version"`
	ServerName string `json:"server_name,omitempty"`
}

// handleSetupStatus reports whether first-run setup is still required (no
// account exists yet).
func (a *API) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	exists, err := a.DB.AccountExists(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "status check failed")
		return
	}
	writeJSON(w, http.StatusOK, setupStatusResponse{
		NeedsSetup: !exists,
		ServerName: a.Cfg.Server.Name,
	})
}

// setupCompleteRequest is the first-run payload, sent by the TUI setup wizard.
//
// It carries no account email (there is no email anywhere in Northrou). It DOES
// carry media folders, which is a deliberate exception to "folders are never
// settable over the API": setup is local-only and once-ever, and routing them
// through the daemon writes them into the daemon's own config.toml - which may
// be a different file from the wizard process's (a service running as root has
// its own config path), so a direct file write from the wizard could silently
// land somewhere the daemon never reads.
type setupCompleteRequest struct {
	ServerName   string   `json:"server_name"`  // human-facing name; optional
	ProfileName  string   `json:"profile_name"` // first profile; optional
	TMDBAPIKey   string   `json:"tmdb_api_key"`
	EnableRemote bool     `json:"enable_remote"`
	MovieDirs    []string `json:"movie_dirs"`
	ShowDirs     []string `json:"show_dirs"`
}

type setupCompleteResponse struct {
	Profile        profileDTO `json:"profile"`
	ConnectionCode string     `json:"connection_code"`
	AccessToken    string     `json:"access_token"`
	RefreshToken   string     `json:"refresh_token"`
}

// handleSetupComplete performs first-run setup: creates the first profile,
// persists the TMDB key to config, issues the server connection code (the sole
// credential remote clients use to pair), and returns a signed-in token pair. It
// is only allowed while no account exists, and only from a local request (setup
// cannot be driven remotely over the tunnel).
func (a *API) handleSetupComplete(w http.ResponseWriter, r *http.Request) {
	if !remote.IsLocal(r) {
		writeError(w, http.StatusForbidden, "setup must be run locally on the server")
		return
	}
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
	// Setup is local-only, so the submitted folder paths describe this box's
	// own filesystem and can be checked right now rather than reported as an
	// empty library an hour later.
	for _, dir := range append(append([]string{}, req.MovieDirs...), req.ShowDirs...) {
		if err := checkMediaDir(dir); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	if err := a.DB.CreateAccount(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "create account failed")
		return
	}
	name := strings.TrimSpace(req.ProfileName)
	if name == "" {
		name = "Me"
	}
	pid, err := a.DB.CreateProfile(r.Context(), name, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create profile failed")
		return
	}

	// Persist config. Media folders may also have been written to disk by the
	// TUI Library tab (`northrou admin`) before setup ran, while the daemon's
	// in-memory a.Cfg.Media stayed empty from boot. Pull them back from disk so
	// saving here does not erase them, then merge in what the wizard sent. Same
	// reload the config PATCH and scan paths do, for the same reason.
	if onDisk, err := config.Load(a.ConfigPath); err == nil {
		a.Cfg.Media = onDisk.Media
	}
	a.Cfg.Media.MovieDirs = mergeDirs(a.Cfg.Media.MovieDirs, req.MovieDirs)
	a.Cfg.Media.ShowDirs = mergeDirs(a.Cfg.Media.ShowDirs, req.ShowDirs)
	if name := strings.TrimSpace(req.ServerName); name != "" {
		a.Cfg.Server.Name = name
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
	// Bring the remote peer up right away so the connection code the wizard is
	// about to show actually works, instead of waiting for a restart.
	a.syncRemote()

	prof, _ := a.DB.GetProfile(r.Context(), pid)
	// Setup runs locally, so sign the operator straight in (ephemerally: the
	// wizard's session must not linger in the paired-devices list).
	pair, err := a.Auth.IssueSetupSession(r.Context(), pid)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "auto-login failed")
		return
	}

	writeJSON(w, http.StatusCreated, setupCompleteResponse{
		Profile:        profileDTO{ID: prof.ID, Name: prof.Name, Avatar: prof.Avatar},
		ConnectionCode: a.Cfg.Remote.ConnectionCode,
		AccessToken:    pair.AccessToken,
		RefreshToken:   pair.RefreshToken,
	})
}

// checkMediaDir rejects a library folder the scanner could never read: not
// absolute (the daemon's working directory is the service manager's business,
// not the operator's), missing, or not a directory.
func checkMediaDir(dir string) error {
	if dir == "" {
		return errors.New("empty folder path")
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("media folder must be an absolute path: %s", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot read media folder %s: %v", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("not a folder: %s", dir)
	}
	return nil
}

// mergeDirs appends add to have, cleaning and dropping duplicates.
func mergeDirs(have, add []string) []string {
	for _, d := range add {
		d = filepath.Clean(d)
		if !slices.Contains(have, d) {
			have = append(have, d)
		}
	}
	return have
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// connectionCode returns a human-shareable pairing code like "NR-3F9A-K2X7Q".
// It is the sole credential a remote client uses to pair, so it is drawn from a
// 10-character (~50-bit) space over an ambiguity-free alphabet; brute force is
// bounded further by rate limiting on both the box and the coordinator.
func connectionCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 32 chars, no ambiguous ones
	const n = 10
	b := make([]byte, n)
	_, _ = rand.Read(b)
	out := make([]byte, n)
	for i := range b {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return "NR-" + string(out[:5]) + "-" + string(out[5:])
}

// NewConnectionCode generates a fresh connection code. Exported so boot-time
// migration can mint one for an upgraded server that predates connection codes.
func NewConnectionCode() string { return connectionCode() }
