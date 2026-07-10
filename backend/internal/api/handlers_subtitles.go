package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

type subtitleDTO struct {
	ID        int64  `json:"id"`
	Language  string `json:"language"`
	Label     string `json:"label"`
	Format    string `json:"format"`
	Forced    bool   `json:"forced"`
	Status    string `json:"status"` // ready|processing|queued|unavailable
	URL       string `json:"url,omitempty"`
}

// formatPriority ranks subtitle formats; higher wins when the same language has
// multiple tracks (text is preferred over image-based PGS).
func formatPriority(format string) int {
	switch format {
	case "subrip", "webvtt":
		return 3
	case "ass":
		return 2
	case "pgs":
		return 1
	default:
		return 0
	}
}

// handleListSubtitles returns the playable subtitle tracks for a media file,
// preferring text tracks (SRT/ASS) over image-based PGS for the same language.
func (a *API) handleListSubtitles(w http.ResponseWriter, r *http.Request) {
	fileID, ok := mediaID(w, r)
	if !ok {
		return
	}
	tracks, err := a.DB.ListSubtitleTracks(r.Context(), fileID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list subtitles failed")
		return
	}

	// Choose the best track per language (forced tracks kept separately).
	best := map[string]db.SubtitleTrack{}
	var forced []db.SubtitleTrack
	for _, t := range tracks {
		if t.Forced {
			forced = append(forced, t)
			continue
		}
		key := t.Language
		if cur, exists := best[key]; !exists || formatPriority(t.Format) > formatPriority(cur.Format) {
			best[key] = t
		}
	}

	var out []subtitleDTO
	emit := func(t db.SubtitleTrack) {
		out = append(out, subtitleToDTO(fileID, t))
	}
	for _, t := range best {
		emit(t)
	}
	for _, t := range forced {
		emit(t)
	}
	writeJSON(w, http.StatusOK, out)
}

func subtitleToDTO(fileID int64, t db.SubtitleTrack) subtitleDTO {
	dto := subtitleDTO{
		ID: t.ID, Language: t.Language, Label: subtitleLabel(t),
		Format: t.Format, Forced: t.Forced,
	}
	switch {
	case t.VTTPath != "":
		dto.Status = "ready"
		dto.URL = "/api/media/" + strconv.FormatInt(fileID, 10) + "/subtitles/" + strconv.FormatInt(t.ID, 10) + ".vtt"
	case t.OCRStatus == "queued" || t.OCRStatus == "processing":
		dto.Status = t.OCRStatus
	default:
		dto.Status = "unavailable"
	}
	return dto
}

func subtitleLabel(t db.SubtitleTrack) string {
	if t.Title != "" {
		return t.Title
	}
	lang := t.Language
	if lang == "" {
		lang = "Unknown"
	}
	if t.Forced {
		return lang + " (Forced)"
	}
	return lang
}

// handleGetSubtitleVTT serves the generated WebVTT for a track via the HTML5
// <track> element.
func (a *API) handleGetSubtitleVTT(w http.ResponseWriter, r *http.Request) {
	trackID, err := strconv.ParseInt(chi.URLParam(r, "track"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid track id")
		return
	}
	track, err := a.DB.GetSubtitleTrack(r.Context(), trackID)
	if err != nil {
		notFoundOr500(w, err, "subtitle lookup failed")
		return
	}
	if track.VTTPath == "" {
		writeError(w, http.StatusNotFound, "subtitle not ready")
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	http.ServeFile(w, r, track.VTTPath)
}

// mediaID parses the {id} path param used by media (file) routes.
func mediaID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid media id")
		return 0, false
	}
	return id, true
}
