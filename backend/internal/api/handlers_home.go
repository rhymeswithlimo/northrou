package api

import (
	"net/http"
	"strconv"

	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
	"github.com/rhymeswithlimo/northrou/backend/internal/recommend"
)

// handleHome returns the personalized, rotated home-screen rows.
func (a *API) handleHome(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFrom(r.Context())
	rows, err := a.Recommend.Home(r.Context(), claims.ProfileID)
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
	// MediaKind is "movie" or "episode". Empty means "movie", so the original
	// {movie_id, position, duration} body keeps working.
	MediaKind string  `json:"media_kind"`
	MediaID   int64   `json:"media_id"`
	MovieID   int64   `json:"movie_id"` // deprecated alias for media_id
	Position  float64 `json:"position"`
	Duration  float64 `json:"duration"`
}

// handleWatch records playback progress and updates the taste profile.
func (a *API) handleWatch(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFrom(r.Context())
	var req watchRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	id := req.MediaID
	if id == 0 {
		id = req.MovieID
	}
	if id == 0 {
		writeError(w, http.StatusBadRequest, "media_id required")
		return
	}

	kind := model.MediaKind(req.MediaKind)
	if kind == "" {
		kind = model.KindMovie
	}
	if kind != model.KindMovie && kind != model.KindEpisode {
		writeError(w, http.StatusBadRequest, `media_kind must be "movie" or "episode"`)
		return
	}

	if err := a.Recommend.RecordWatch(r.Context(), claims.ProfileID, kind, id, req.Position, req.Duration); err != nil {
		writeError(w, http.StatusInternalServerError, "record watch failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// continueItemDTO is one Continue Watching card. For an episode, id/title are
// the show's (what the card opens) while episode_id and season/number identify
// what actually resumes.
type continueItemDTO struct {
	Kind        string  `json:"kind"` // "movie" or "episode"
	ID          int64   `json:"id"`
	ShowID      int64   `json:"show_id,omitempty"`
	Title       string  `json:"title"`
	Season      int     `json:"season,omitempty"`
	Number      int     `json:"number,omitempty"`
	PositionSec float64 `json:"position_sec"`
	DurationSec float64 `json:"duration_sec"`
	BackdropURL string  `json:"backdrop_url,omitempty"`
	StreamURL   string  `json:"stream_url,omitempty"`
}

// handleContinueWatching lists what this profile has started but not finished.
func (a *API) handleContinueWatching(w http.ResponseWriter, r *http.Request) {
	claims, _ := auth.ClaimsFrom(r.Context())
	items, err := a.DB.ListInProgress(r.Context(), claims.ProfileID, 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "continue watching failed")
		return
	}
	out := make([]continueItemDTO, 0, len(items))
	for _, it := range items {
		dto := continueItemDTO{
			Kind:        string(it.Kind),
			ID:          it.ID,
			ShowID:      it.ShowID,
			Title:       it.Title,
			Season:      it.Season,
			Number:      it.Number,
			PositionSec: it.PositionSec,
			DurationSec: it.DurationSec,
			BackdropURL: a.imageURL(it.BackdropPath),
		}
		if it.FileID != 0 {
			dto.StreamURL = "/api/media/" + strconv.FormatInt(it.FileID, 10) + "/stream"
		}
		out = append(out, dto)
	}
	writeJSON(w, http.StatusOK, out)
}
