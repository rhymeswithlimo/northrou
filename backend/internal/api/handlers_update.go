package api

import (
	"net/http"

	"github.com/rhymeswithlimo/northrou/backend/internal/buildinfo"
	"github.com/rhymeswithlimo/northrou/backend/internal/update"
)

const githubRepo = "rhymeswithlimo/northrou"

type updateCheckResponse struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"has_update"`
	Notes     string `json:"notes,omitempty"`
}

// handleUpdateCheck reports whether a newer release is available, so the web UI
// can surface an update notification.
func (a *API) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	u := update.New(githubRepo, buildinfo.Version)
	latest, err := u.Latest(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not reach update server")
		return
	}
	writeJSON(w, http.StatusOK, updateCheckResponse{
		Current:   buildinfo.Version,
		Latest:    latest.Version,
		HasUpdate: u.HasUpdate(latest),
		Notes:     latest.Notes,
	})
}

// handleUpdateApply downloads and installs the latest release, then instructs
// the caller to restart. Admin-only.
func (a *API) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	u := update.New(githubRepo, buildinfo.Version)
	latest, err := u.Latest(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, "could not reach update server")
		return
	}
	if !u.HasUpdate(latest) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "already up to date"})
		return
	}
	if err := u.Apply(r.Context(), latest); err != nil {
		writeError(w, http.StatusInternalServerError, "update failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "updated",
		"version": latest.Version,
		"message": "restart the service to run the new version",
	})
}
