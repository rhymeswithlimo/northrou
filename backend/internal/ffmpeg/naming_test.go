package ffmpeg

import "testing"

// These verify platform binary naming independent of the host OS, so Windows
// behavior is checked even when the tests run on Linux/macOS.

func TestBinaryName(t *testing.T) {
	cases := []struct {
		base, goos, want string
	}{
		{"ffmpeg", "linux", "ffmpeg"},
		{"ffmpeg", "darwin", "ffmpeg"},
		{"ffmpeg", "windows", "ffmpeg.exe"},
		{"ffprobe", "windows", "ffprobe.exe"},
		{"ffprobe", "linux", "ffprobe"},
		{"tesseract", "windows", "tesseract.exe"},
		// Already-suffixed names are not double-suffixed.
		{"ffmpeg.exe", "windows", "ffmpeg.exe"},
	}
	for _, c := range cases {
		if got := binaryName(c.base, c.goos); got != c.want {
			t.Errorf("binaryName(%q, %q) = %q, want %q", c.base, c.goos, got, c.want)
		}
	}
}

func TestReleaseTableCoversAllTargets(t *testing.T) {
	// Every supported release target must have a download source so no user is
	// left without a managed ffmpeg (or an explicit system-fallback path).
	targets := []string{
		"linux/amd64", "linux/arm64", "linux/arm",
		"darwin/amd64", "darwin/arm64", "windows/amd64",
	}
	for _, tgt := range targets {
		r, ok := releases[tgt]
		if !ok {
			t.Errorf("no ffmpeg release entry for %s", tgt)
			continue
		}
		if r.Bundle == nil && (r.FFmpeg == nil || r.FFprobe == nil) {
			t.Errorf("%s: release must define a Bundle or both FFmpeg+FFprobe assets", tgt)
		}
	}
}
