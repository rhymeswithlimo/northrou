package transcode

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/transcode/hwaccel"
)

// TestHLSArgs_FastStartupSegments locks in the startup-latency behavior: short,
// keyframe-forced segments so the first segment (and playback) appears quickly
// instead of waiting a full default GOP.
func TestHLSArgs_FastStartupSegments(t *testing.T) {
	sm := NewSessionManager(hwaccel.Capabilities{Backend: hwaccel.Software})
	s := NewStreamer("ffmpeg", t.TempDir(), hwaccel.Capabilities{Backend: hwaccel.Software}, sm, false, false, 0, nil)

	dec := Decision{Mode: ModeVideoTranscode, VideoCodec: "h264", HWBackend: "software", sourceHeight: 1080}
	args := s.hlsArgs(dec, "/media/movie.mkv", t.TempDir(), topRung(dec, 0))
	joined := strings.Join(args, " ")

	if !strings.Contains(joined, "-hls_time "+strconv.Itoa(hlsSegmentSeconds)) {
		t.Errorf("expected hls_time %d, args: %s", hlsSegmentSeconds, joined)
	}
	if !strings.Contains(joined, "-force_key_frames") {
		t.Errorf("expected forced keyframes at segment boundaries, args: %s", joined)
	}
	if !slices.Contains(args, "expr:gte(t,n_forced*"+strconv.Itoa(hlsSegmentSeconds)+")") {
		t.Errorf("keyframe expression should align to segment length, args: %s", joined)
	}
}
