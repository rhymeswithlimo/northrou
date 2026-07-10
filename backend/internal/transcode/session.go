package transcode

import (
	"runtime"
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

	// inFlightCost is the sum of admission cost for transcodes currently
	// holding a budget slot. Guarded by mu.
	inFlightCost int
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

// videoCapacity is the number of concurrent full video transcodes the box is
// allowed to run. Hardware backends use the encoder estimate; software (no GPU,
// the worst-case target) is derived from CPU cores with a floor of 1 so a single
// stream always plays.
func (m *SessionManager) videoCapacity() int {
	if c := m.EstimatedCapacity(); c > 0 {
		return c
	}
	if c := runtime.NumCPU() / 2; c > 0 {
		return c
	}
	return 1
}

// Admission cost per delivery mode, in budget units. Direct play and remux are
// stream copies (near-free) and never consume budget. Audio-only transcode is
// light; full video transcode is the expensive path. A video transcode costs
// half the total budget so videoCapacity() of them fit exactly.
func modeCost(mode Mode) int {
	switch mode {
	case ModeVideoTranscode:
		return 2
	case ModeAudioTranscode:
		return 1
	default: // direct, remux
		return 0
	}
}

// TryAcquire reserves a budget slot for a transcode of the given mode. It
// returns a release func and true when admitted, or nil and false when the box
// is already at capacity. Zero-cost modes (direct, remux) are always admitted.
// The release func is safe to call exactly once; callers should defer it (or,
// for HLS sessions that outlive the request, call it on session teardown).
func (m *SessionManager) TryAcquire(mode Mode) (func(), bool) {
	cost := modeCost(mode)
	if cost == 0 {
		return func() {}, true
	}
	budget := 2 * m.videoCapacity()
	m.mu.Lock()
	if m.inFlightCost+cost > budget {
		m.mu.Unlock()
		return nil, false
	}
	m.inFlightCost += cost
	m.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			m.inFlightCost -= cost
			m.mu.Unlock()
		})
	}, true
}
