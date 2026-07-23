package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// deviceSessionDTO is one paired device as shown in settings and the TUI.
type deviceSessionDTO struct {
	ID          string `json:"id"`
	DeviceName  string `json:"device_name"`
	ProfileName string `json:"profile_name"`
	PairedAt    string `json:"paired_at"`
	LastSeenAt  string `json:"last_seen_at"`
}

// handleListSessions returns the devices currently paired with this server.
// A read: any signed-in session may see the list (it is status, and every
// paired device already holds the credential the list implies).
func (a *API) handleListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := a.DB.ListDeviceSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list devices")
		return
	}
	out := make([]deviceSessionDTO, 0, len(sessions))
	for _, s := range sessions {
		name := s.DeviceName
		if name == "" {
			name = "Unknown device"
		}
		out = append(out, deviceSessionDTO{
			ID:          s.Key,
			DeviceName:  name,
			ProfileName: s.ProfileName,
			PairedAt:    s.CreatedAt.UTC().Format(time.RFC3339),
			LastSeenAt:  s.LastUsedAt.UTC().Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRevokeSession signs one device out for good: its refresh tokens are
// revoked, so it is gone as soon as its current access token expires (minutes).
// Local-only, like every admin mutation.
func (a *API) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "id")
	if err := a.DB.RevokeDeviceSession(r.Context(), key); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no such device")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not revoke the device")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type rotateCodeResponse struct {
	ConnectionCode string `json:"connection_code"`
}

// handleRotateConnectionCode mints a fresh connection code and revokes every
// paired device's session: a rotation that left old devices connected would
// not be a rotation. The requesting local browser recovers on its own (local
// pairing needs no code); every remote device must enter the new code.
func (a *API) handleRotateConnectionCode(w http.ResponseWriter, r *http.Request) {
	// Disk-first, like the config PATCH: the TUI may have edited [media] in
	// this same file, and saving a stale in-memory copy would clobber it.
	cfg, err := config.Load(a.ConfigPath)
	if err != nil {
		c := *a.Cfg
		cfg = &c
	}
	cfg.Remote.ConnectionCode = connectionCode()
	if err := cfg.Save(a.ConfigPath); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the new code")
		return
	}
	*a.Cfg = *cfg

	if err := a.DB.RevokeAllTokens(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "code rotated but revoking sessions failed; restart the server")
		return
	}

	// Re-register with the coordinator under the new code, so pairing with it
	// works immediately.
	a.restartRemote()

	writeJSON(w, http.StatusOK, rotateCodeResponse{ConnectionCode: a.Cfg.Remote.ConnectionCode})
}
