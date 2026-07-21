package transcode

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
	"github.com/rhymeswithlimo/northrou/backend/internal/transcode/hwaccel"
)

func TestServeStream_DirectPlay(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "movie.mp4")
	content := []byte("fake mp4 bytes for direct play")
	if err := os.WriteFile(mediaPath, content, 0o644); err != nil {
		t.Fatal(err)
	}

	mf := &model.MediaFile{
		ID: 1, Path: mediaPath, Container: "mov,mp4",
		Video: model.VideoStream{Codec: "h264", Height: 1080},
		Audio: []model.AudioStream{{Codec: "aac", Channels: 2, Default: true}},
	}
	caps := ClientCapabilities{
		VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"},
		Containers: []string{"mp4"}, MaxResolution: 1080,
	}

	sm := NewSessionManager(hwaccel.Capabilities{Backend: hwaccel.Software})
	// ffmpegPath empty is fine for direct play (no processing).
	s := NewStreamer("", dir, hwaccel.Capabilities{Backend: hwaccel.Software}, sm, false, false, 0, nil)

	// Confirm the plan is direct play.
	if d := s.Plan(mf, caps); d.Mode != ModeDirectPlay {
		t.Fatalf("expected direct play, got %s", d.Mode)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/media/1/stream", nil)
	rec := httptest.NewRecorder()
	s.ServeStream(rec, req, mf, "Test Movie", caps)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if rec.Body.String() != string(content) {
		t.Errorf("body mismatch: got %q", rec.Body.String())
	}
	// Session should be cleaned up after the request.
	if sm.Count() != 0 {
		t.Errorf("expected 0 active sessions after direct play, got %d", sm.Count())
	}
}

func TestServeStream_RangeRequest(t *testing.T) {
	dir := t.TempDir()
	mediaPath := filepath.Join(dir, "movie.mp4")
	content := []byte("0123456789abcdef")
	_ = os.WriteFile(mediaPath, content, 0o644)

	mf := &model.MediaFile{
		ID: 2, Path: mediaPath, Container: "mov,mp4",
		Video: model.VideoStream{Codec: "h264", Height: 720},
		Audio: []model.AudioStream{{Codec: "aac", Default: true}},
	}
	caps := ClientCapabilities{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Containers: []string{"mp4"}}
	sm := NewSessionManager(hwaccel.Capabilities{Backend: hwaccel.Software})
	s := NewStreamer("", dir, hwaccel.Capabilities{Backend: hwaccel.Software}, sm, false, false, 0, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/media/2/stream", nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()
	s.ServeStream(rec, req, mf, "", caps)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("expected 206 Partial Content for range request, got %d", rec.Code)
	}
	if rec.Body.String() != "0123" {
		t.Errorf("expected first 4 bytes, got %q", rec.Body.String())
	}
}
