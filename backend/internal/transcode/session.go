package transcode

import (
	"sync"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/transcode/hwaccel"
)

// StreamSession describes one active stream, surfaced on the admin dashboard.
type StreamSession struct {
	ID          string    `json:"id"`
	FileID      int64     `json:"file_id"`
	Title       string    `json:"title"`
	Mode        Mode      `json:"mode"`
	SourceVideo string    `json:"source_video"`
	SourceAudio string    `json:"source_audio"`
	TargetVideo string    `json:"target_video"`
	TargetAudio string    `json:"target_audio"`
	HWBackend   string    `json:"hw_backend"`
	Client      string    `json:"client"`
	Remote      bool      `json:"remote"`
	StartedAt   time.Time `json:"started_at"`
}

// SessionManager tracks active streams and estimates capacity.
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[string]*StreamSession
	hw       hwaccel.Capabilities
}

// NewSessionManager creates a session manager for the given hardware profile.
func NewSessionManager(hw hwaccel.Capabilities) *SessionManager {
	return &SessionManager{sessions: map[string]*StreamSession{}, hw: hw}
}

// Add registers a session.
func (m *SessionManager) Add(s *StreamSession) {
	m.mu.Lock()
	m.sessions[s.ID] = s
	m.mu.Unlock()
}

// Remove ends a session.
func (m *SessionManager) Remove(id string) {
	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()
}

// List returns a snapshot of active sessions.
func (m *SessionManager) List() []StreamSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]StreamSession, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, *s)
	}
	return out
}

// Count returns the number of active sessions.
func (m *SessionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// ActiveTranscodes counts sessions currently doing video transcoding (the
// expensive path that consumes encoder capacity).
func (m *SessionManager) ActiveTranscodes() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n := 0
	for _, s := range m.sessions {
		if s.Mode == ModeVideoTranscode {
			n++
		}
	}
	return n
}

// Hardware returns the detected acceleration capabilities.
func (m *SessionManager) Hardware() hwaccel.Capabilities {
	return m.hw
}

// EstimatedCapacity is a rough estimate of how many simultaneous 4K video
// transcodes the detected hardware can sustain in real time. Direct play,
// remux, and audio-only transcode are effectively unbounded and not counted.
func (m *SessionManager) EstimatedCapacity() int {
	switch m.hw.Backend {
	case hwaccel.NVENC:
		return 4 // modern NVENC handles several 4K sessions
	case hwaccel.QSV, hwaccel.VAAPI:
		return 2
	case hwaccel.VideoToolbox:
		return 2
	case hwaccel.AMF:
		return 2
	default:
		return 0 // software: not real-time for 4K
	}
}
