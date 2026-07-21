package transcode

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
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

// errAtCapacity signals the box is already running its maximum number of
// transcodes; the request should be retried, not queued.
var errAtCapacity = errors.New("transcode capacity reached")

// Streamer serves media using the decision cascade and manages transcode
// sessions.
type Streamer struct {
	ffmpegPath     string
	hlsDir         string
	hw             hwaccel.Capabilities
	tonemap        bool
	allowSoft4K    bool
	maxBitrateKbps int
	preferredLangs []string // preferred audio languages (ISO-639), in order
	sessions       *SessionManager

	mu       sync.Mutex
	hlsSess  map[string]*hlsSession
}

// NewStreamer builds a Streamer. dataDir/hls holds transcode scratch space.
func NewStreamer(ffmpegPath, dataDir string, hw hwaccel.Capabilities, sm *SessionManager, tonemap, allowSoft4K bool, maxBitrateKbps int, preferredLangs []string) *Streamer {
	s := &Streamer{
		ffmpegPath:     ffmpegPath,
		hlsDir:         filepath.Join(dataDir, "hls"),
		hw:             hw,
		tonemap:        tonemap,
		allowSoft4K:    allowSoft4K,
		maxBitrateKbps: maxBitrateKbps,
		preferredLangs: preferredLangs,
		sessions:       sm,
		hlsSess:        map[string]*hlsSession{},
	}
	go s.reaper()
	return s
}

// Sessions exposes the session manager (for the admin API/TUI).
func (s *Streamer) Sessions() *SessionManager { return s.sessions }

// SetPreferredLangs updates the audio-language preference at runtime (from a
// settings change) so it takes effect without a restart.
func (s *Streamer) SetPreferredLangs(langs []string) {
	s.mu.Lock()
	s.preferredLangs = langs
	s.mu.Unlock()
}

// Plan computes the delivery decision for a media file and client.
func (s *Streamer) Plan(mf *model.MediaFile, caps ClientCapabilities) Decision {
	s.mu.Lock()
	langs := s.preferredLangs
	s.mu.Unlock()
	return Decide(mf, caps, Options{
		HWBackend:       string(s.hw.Backend),
		AllowSoftware4K: s.allowSoft4K,
		Tonemap:         s.tonemap,
		PreferredLangs:  langs,
		// Only offer AV1 as a target when a hardware AV1 encoder exists; software
		// AV1 is far too slow to transcode in real time.
		AV1Encode: s.hw.AV1 != "" && s.hw.Backend != hwaccel.Software,
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
		// Remux is a stream copy (cost 0, always admitted); an audio transcode
		// consumes a light budget slot released when the request ends.
		release, ok := s.sessions.TryAcquire(dec.Mode)
		if !ok {
			writeBusy(w)
			return
		}
		defer release()
		s.sessions.Add(sess)
		defer s.sessions.Remove(sessionID)
		s.serveProgressive(w, r, dec, mf)
	case ModeVideoTranscode:
		// HLS sessions live beyond this request; register and hand back a URL.
		s.serveHLSEntry(w, r, dec, mf, sess)
	}
}

// writeBusy rejects a stream the box has no capacity for. A short Retry-After
// tells the client to try again once a slot frees, rather than queueing a
// request that would hold a goroutine and the viewer's patience.
func writeBusy(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "5")
	http.Error(w, "server at transcode capacity; retry shortly", http.StatusServiceUnavailable)
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
	// Relay ffmpeg's fragmented-MP4 output through a bounded read-ahead buffer so
	// a briefly-slow client doesn't block ffmpeg and underrun playback. The path
	// is forward-only, so there are no seek/range concerns.
	ra := newReadAhead(stdout, readAheadChunkBytes, readAheadChunks)
	defer ra.Close()
	_, _ = io.Copy(w, ra)
	_ = cmd.Wait()
}

// serveHLSEntry ensures an HLS session exists and returns the playlist URL.
func (s *Streamer) serveHLSEntry(w http.ResponseWriter, r *http.Request, dec Decision, mf *model.MediaFile, sess *StreamSession) {
	if _, err := s.ensureHLS(context.Background(), dec, mf.Path, sess); err != nil {
		if errors.Is(err, errAtCapacity) {
			writeBusy(w)
			return
		}
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
// A new transcode consumes an admission slot; joining an existing session (the
// dedup path) does not, so a second viewer of the same file is free.
func (s *Streamer) ensureHLS(ctx context.Context, dec Decision, inputPath string, sess *StreamSession) (*hlsSession, error) {
	s.mu.Lock()
	if h, ok := s.hlsSess[sess.ID]; ok {
		s.mu.Unlock()
		h.touch()
		return h, nil
	}
	s.mu.Unlock()

	release, ok := s.sessions.TryAcquire(dec.Mode)
	if !ok {
		return nil, errAtCapacity
	}

	h, err := s.startHLS(ctx, dec, inputPath, sess.ID)
	if err != nil {
		release()
		return nil, err
	}
	h.release = release
	s.mu.Lock()
	if existing, raced := s.hlsSess[sess.ID]; raced {
		// Another request created this session while we were starting ffmpeg.
		// Discard ours (frees its slot and scratch dir) and use the winner.
		s.mu.Unlock()
		h.stop()
		existing.touch()
		return existing, nil
	}
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
