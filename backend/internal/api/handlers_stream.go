package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/transcode"
)

// parseCapabilities builds a ClientCapabilities from query parameters. Absent
// parameters fall back to a conservative default profile.
func parseCapabilities(r *http.Request) transcode.ClientCapabilities {
	q := r.URL.Query()
	if q.Get("video") == "" && q.Get("audio") == "" {
		return transcode.DefaultCapabilities()
	}
	caps := transcode.ClientCapabilities{
		VideoCodecs:    splitCSV(q.Get("video")),
		AudioCodecs:    splitCSV(q.Get("audio")),
		Containers:     splitCSV(q.Get("containers")),
		MaxResolution:  atoiDefault(q.Get("max_resolution"), 0),
		HDR:            q.Get("hdr") == "1" || q.Get("hdr") == "true",
		DolbyVision:    q.Get("dolby_vision") == "1" || q.Get("dolby_vision") == "true",
		Atmos:          q.Get("atmos") == "1" || q.Get("atmos") == "true",
		MaxBitrateKbps: atoiDefault(q.Get("max_bitrate_kbps"), 0),
		Remote:         q.Get("remote") == "1" || q.Get("remote") == "true",
	}
	if len(caps.Containers) == 0 {
		caps.Containers = []string{"mp4"}
	}
	return caps
}

// handleStream serves a media file using the transcode decision cascade.
func (a *API) handleStream(w http.ResponseWriter, r *http.Request) {
	fileID, ok := mediaID(w, r)
	if !ok {
		return
	}
	mf, err := a.DB.GetMediaFile(r.Context(), fileID)
	if err != nil {
		notFoundOr500(w, err, "media lookup failed")
		return
	}
	streamer := a.getStreamer()
	if streamer == nil {
		writeError(w, http.StatusServiceUnavailable, "streaming not ready (ffmpeg initializing)")
		return
	}
	caps := parseCapabilities(r)
	caps.PreferredAudioLangs = a.profileAudioLangs(r)
	streamer.ServeStream(w, r, mf, "", caps)
}

// profileAudioLangs returns the signed-in viewer's preferred audio languages, or
// nil to let the server default apply.
func (a *API) profileAudioLangs(r *http.Request) []string {
	claims, ok := auth.ClaimsFrom(r.Context())
	if !ok {
		return nil
	}
	prof, err := a.DB.GetProfile(r.Context(), claims.ProfileID)
	if err != nil || prof.PreferredAudioLang == "" {
		return nil
	}
	return []string{prof.PreferredAudioLang}
}

// handleHLSFile serves a playlist or segment for an active HLS session.
func (a *API) handleHLSFile(w http.ResponseWriter, r *http.Request) {
	streamer := a.getStreamer()
	if streamer == nil {
		writeError(w, http.StatusServiceUnavailable, "streaming not ready")
		return
	}
	session := chi.URLParam(r, "session")
	file := chi.URLParam(r, "file")
	streamer.ServeHLSFile(w, r, session, file)
}

// handlePlan returns the decision the server would make for a client, without
// starting a stream (useful for the frontend to preflight).
func (a *API) handlePlan(w http.ResponseWriter, r *http.Request) {
	fileID, ok := mediaID(w, r)
	if !ok {
		return
	}
	mf, err := a.DB.GetMediaFile(r.Context(), fileID)
	if err != nil {
		notFoundOr500(w, err, "media lookup failed")
		return
	}
	streamer := a.getStreamer()
	if streamer == nil {
		writeError(w, http.StatusServiceUnavailable, "streaming not ready")
		return
	}
	caps := parseCapabilities(r)
	caps.PreferredAudioLangs = a.profileAudioLangs(r)
	writeJSON(w, http.StatusOK, streamer.Plan(mf, caps))
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
