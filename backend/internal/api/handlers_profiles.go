package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// handleListProfiles returns every profile on the account. Any signed-in
// profile may list them (for the switcher and profile management).
func (a *API) handleListProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := a.DB.ListProfiles(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"profiles": toProfileDTOs(profiles)})
}

type profileRequest struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

// handleCreateProfile adds a new viewer profile. Household management is not an
// admin action (Netflix-style): any signed-in profile may add one.
func (a *API) handleCreateProfile(w http.ResponseWriter, r *http.Request) {
	var req profileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "a profile name is required")
		return
	}
	id, err := a.DB.CreateProfile(r.Context(), name, strings.TrimSpace(req.Avatar))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "create profile failed")
		return
	}
	prof, err := a.DB.GetProfile(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusCreated, toProfileDTO(*prof))
}

// handleUpdateProfile renames a profile and/or changes its avatar.
func (a *API) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileIDParam(w, r)
	if !ok {
		return
	}
	var req profileRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "a profile name is required")
		return
	}
	if err := a.DB.RenameProfile(r.Context(), id, name, strings.TrimSpace(req.Avatar)); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no such profile")
			return
		}
		writeError(w, http.StatusInternalServerError, "update failed")
		return
	}
	prof, err := a.DB.GetProfile(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}
	writeJSON(w, http.StatusOK, toProfileDTO(*prof))
}

// handleDeleteProfile removes a profile and all its per-viewer data. The final
// profile cannot be deleted.
func (a *API) handleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := profileIDParam(w, r)
	if !ok {
		return
	}
	if err := a.DB.DeleteProfile(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, db.ErrLastProfile):
			writeError(w, http.StatusConflict, "cannot delete the last profile")
		case errors.Is(err, db.ErrNotFound):
			writeError(w, http.StatusNotFound, "no such profile")
		default:
			writeError(w, http.StatusInternalServerError, "delete failed")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func profileIDParam(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "invalid profile id")
		return 0, false
	}
	return id, true
}
