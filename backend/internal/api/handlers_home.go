package api

import (
	"net/http"

	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/recommend"
)

// handleHome returns the personalized, rotated home-screen rows.
func (a *API) handleHome(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFrom(r.Context())
	rows, err := a.Recommend.Home(r.Context(), claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "home generation failed")
		return
	}
	if rows == nil {
		rows = []recommend.Row{}
	}
	writeJSON(w, http.StatusOK, rows)
}

type watchRequest struct {
	MovieID  int64   `json:"movie_id"`
	Position float64 `json:"position"`
	Duration float64 `json:"duration"`
}

// handleWatch records playback progress and updates the taste profile.
func (a *API) handleWatch(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFrom(r.Context())
	var req watchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.MovieID == 0 {
		writeError(w, http.StatusBadRequest, "movie_id required")
		return
	}
	if err := a.Recommend.RecordWatch(r.Context(), claims.UserID, req.MovieID, req.Position, req.Duration); err != nil {
		writeError(w, http.StatusInternalServerError, "record watch failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
