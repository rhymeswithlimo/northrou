package transcode

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
	"github.com/rhymeswithlimo/northrou/backend/internal/transcode/hwaccel"
)

// Streamer serves media using the decision cascade and manages transcode
// sessions.
type Streamer struct {
	ffmpegPath     string
	hlsDir         string
	hw             hwaccel.Capabilities
	tonemap        bool
	allowSoft4K    bool
	maxBitrateKbps int
	sessions       *SessionManager

	mu       sync.Mutex
	hlsSess  map[string]*hlsSession
}

// NewStreamer builds a Streamer. dataDir/hls holds transcode scratch space.
func NewStreamer(ffmpegPath, dataDir string, hw hwaccel.Capabilities, sm *SessionManager, tonemap, allowSoft4K bool, maxBitrateKbps int) *Streamer {
	s := &Streamer{
		ffmpegPath:     ffmpegPath,
		hlsDir:         filepath.Join(dataDir, "hls"),
		hw:             hw,
		tonemap:        tonemap,
		allowSoft4K:    allowSoft4K,
		maxBitrateKbps: maxBitrateKbps,
		sessions:       sm,
		hlsSess:        map[string]*hlsSession{},
	}
	go s.reaper()
	return s
}

// Sessions exposes the session manager (for the admin API/TUI).
func (s *Streamer) Sessions() *SessionManager { return s.sessions }

// Plan computes the delivery decision for a media file and client.
func (s *Streamer) Plan(mf *model.MediaFile, caps ClientCapabilities) Decision {
	return Decide(mf, caps, Options{
		HWBackend:       string(s.hw.Backend),
		AllowSoftware4K: s.allowSoft4K,
		Tonemap:         s.tonemap,
	})
}

// ServeStream serves a media file to the client, choosing direct play, remux,
// audio-only transcode, or an HLS transcode. For HLS it responds with the
// playlist URL the client should load next.
func (s *Streamer) ServeStream(w http.ResponseWriter, r *http.Request, mf *model.MediaFile, title string, caps ClientCapabilities) {
	if s.ffmpegPath == "" && needsFFmpeg(s.Plan(mf, caps)) {
		http.Error(w, "transcoding unavailable: ffmpeg not ready", http.StatusServiceUnavailable)
		return
	}
	dec := s.Plan(mf, caps)
	sessionID := s.sessionID(mf.ID, caps)

	sess := &StreamSession{
		ID: sessionID, FileID: mf.ID, Title: title, Mode: dec.Mode,
		SourceVideo: mf.Video.Codec, SourceAudio: dec.AudioCodec,
		TargetVideo: dec.VideoCodec, TargetAudio: dec.AudioCodec,
		HWBackend: dec.HWBackend, Remote: caps.Remote, Client: r.UserAgent(),
		StartedAt: time.Now(),
	}

	switch dec.Mode {
	case ModeDirectPlay:
		s.sessions.Add(sess)
		defer s.sessions.Remove(sessionID)
		s.serveDirect(w, r, mf)
	case ModeRemux, ModeAudioTranscode:
		s.sessions.Add(sess)
		defer s.sessions.Remove(sessionID)
		s.serveProgressive(w, r, dec, mf)
	case ModeVideoTranscode:
		// HLS sessions live beyond this request; register and hand back a URL.
		s.serveHLSEntry(w, r, dec, mf, sess)
	}
}

// serveDirect streams the raw file with HTTP range support (zero processing).
func (s *Streamer) serveDirect(w http.ResponseWriter, r *http.Request, mf *model.MediaFile) {
	f, err := os.Open(mf.Path)
	if err != nil {
		http.Error(w, "cannot open media", http.StatusNotFound)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		http.Error(w, "cannot stat media", http.StatusInternalServerError)
		return
	}
	http.ServeContent(w, r, filepath.Base(mf.Path), info.ModTime(), f)
}

// serveProgressive pipes an ffmpeg remux/audio-transcode as fragmented MP4.
func (s *Streamer) serveProgressive(w http.ResponseWriter, r *http.Request, dec Decision, mf *model.MediaFile) {
	args := progressiveArgs(dec, mf.Path)
	cmd := exec.CommandContext(r.Context(), s.ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "transcode setup failed", http.StatusInternalServerError)
		return
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		http.Error(w, "transcode start failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, stdout)
	_ = cmd.Wait()
}

// serveHLSEntry ensures an HLS session exists and returns the playlist URL.
func (s *Streamer) serveHLSEntry(w http.ResponseWriter, r *http.Request, dec Decision, mf *model.MediaFile, sess *StreamSession) {
	if _, err := s.ensureHLS(context.Background(), dec, mf.Path, sess); err != nil {
		slog.Error("hls start failed", "err", err)
		http.Error(w, "transcode failed", http.StatusInternalServerError)
		return
	}
	playlist := fmt.Sprintf("/api/media/%d/hls/%s/stream.m3u8", mf.ID, sess.ID)
	writeJSONStream(w, map[string]any{
		"mode":     dec.Mode,
		"playlist": playlist,
		"decision": dec,
	})
}

// ensureHLS returns the running HLS session for this key, creating it if needed.
func (s *Streamer) ensureHLS(ctx context.Context, dec Decision, inputPath string, sess *StreamSession) (*hlsSession, error) {
	s.mu.Lock()
	if h, ok := s.hlsSess[sess.ID]; ok {
		s.mu.Unlock()
		h.touch()
		return h, nil
	}
	s.mu.Unlock()

	h, err := s.startHLS(ctx, dec, inputPath, sess.ID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.hlsSess[sess.ID] = h
	s.mu.Unlock()
	s.sessions.Add(sess)
	return h, nil
}

// ServeHLSFile serves a playlist or segment file for an HLS session.
func (s *Streamer) ServeHLSFile(w http.ResponseWriter, r *http.Request, sessionID, file string) {
	s.mu.Lock()
	h, ok := s.hlsSess[sessionID]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "no such session", http.StatusNotFound)
		return
	}
	h.touch()
	// Constrain to the session dir; reject path traversal.
	clean := filepath.Base(file)
	http.ServeFile(w, r, filepath.Join(h.dir, clean))
}

// reaper stops idle HLS sessions and frees their scratch space.
func (s *Streamer) reaper() {
	t := time.NewTicker(1 * time.Minute)
	defer t.Stop()
	for range t.C {
		s.mu.Lock()
		for id, h := range s.hlsSess {
			if h.idle() > 5*time.Minute {
				h.stop()
				delete(s.hlsSess, id)
				s.sessions.Remove(id)
				slog.Info("reaped idle HLS session", "session", id)
			}
		}
		s.mu.Unlock()
	}
}

// sessionID is a stable id for a (file, capability-profile) pair so repeated
// requests reuse the same transcode.
func (s *Streamer) sessionID(fileID int64, caps ClientCapabilities) string {
	h := sha1.New()
	fmt.Fprintf(h, "%d|%v|%v|%v|%d|%v", fileID, caps.VideoCodecs, caps.AudioCodecs,
		caps.Containers, caps.MaxResolution, caps.HDR)
	return strconv.FormatInt(fileID, 10) + "-" + hex.EncodeToString(h.Sum(nil))[:12]
}

func needsFFmpeg(dec Decision) bool {
	return dec.Mode != ModeDirectPlay
}

func writeJSONStream(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
