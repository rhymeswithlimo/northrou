package api

import (
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/rhymeswithlimo/northrou/backend/internal/logging"
	"github.com/rhymeswithlimo/northrou/backend/internal/transcode"
)

// adminStreamsResponse is the active-streams payload for the dashboard/TUI.
type adminStreamsResponse struct {
	Count   int                       `json:"count"`
	Streams []transcode.StreamSession `json:"streams"`
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

// handleAdminLogs returns the tail of the server's log file as plain text, for
// the settings page's log viewer and remote troubleshooting. An admin read like
// the rest of this file: status, not control.
func (a *API) handleAdminLogs(w http.ResponseWriter, r *http.Request) {
	n := 200
	if q := r.URL.Query().Get("n"); q != "" {
		v, err := strconv.Atoi(q)
		if err != nil || v < 1 || v > 5000 {
			writeError(w, http.StatusBadRequest, "n must be between 1 and 5000")
			return
		}
		n = v
	}
	tail, err := logging.Tail(logging.Path(a.Cfg.Server.DataDir), n)
	if errors.Is(err, os.ErrNotExist) {
		tail = []byte("No log file yet.\n")
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read the log file")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(tail)
}
