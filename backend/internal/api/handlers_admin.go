package api

import (
	"net/http"

	"github.com/rhymeswithlimo/northrou/backend/internal/transcode"
)

// adminStreamsResponse is the active-streams payload for the dashboard/TUI.
type adminStreamsResponse struct {
	Count   int                        `json:"count"`
	Streams []transcode.StreamSession  `json:"streams"`
}

func (a *API) handleAdminStreams(w http.ResponseWriter, r *http.Request) {
	streamer := a.getStreamer()
	if streamer == nil {
		writeJSON(w, http.StatusOK, adminStreamsResponse{})
		return
	}
	sessions := streamer.Sessions().List()
	writeJSON(w, http.StatusOK, adminStreamsResponse{Count: len(sessions), Streams: sessions})
}

// adminHardwareResponse reports detected acceleration and estimated capacity.
type adminHardwareResponse struct {
	Backend           string   `json:"backend"`
	Available         []string `json:"available"`
	EstimatedCapacity int      `json:"estimated_capacity"`
	ActiveTranscodes  int      `json:"active_transcodes"`
	FFmpegReady       bool     `json:"ffmpeg_ready"`
}

func (a *API) handleAdminHardware(w http.ResponseWriter, r *http.Request) {
	streamer := a.getStreamer()
	if streamer == nil {
		writeJSON(w, http.StatusOK, adminHardwareResponse{Backend: "initializing"})
		return
	}
	sm := streamer.Sessions()
	hw := sm.Hardware()
	avail := make([]string, 0, len(hw.Available))
	for _, b := range hw.Available {
		avail = append(avail, string(b))
	}
	writeJSON(w, http.StatusOK, adminHardwareResponse{
		Backend:           string(hw.Backend),
		Available:         avail,
		EstimatedCapacity: sm.EstimatedCapacity(),
		ActiveTranscodes:  sm.ActiveTranscodes(),
		FFmpegReady:       true,
	})
}
