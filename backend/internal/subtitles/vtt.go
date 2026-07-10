package subtitles

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Cue is a single subtitle cue with start/end times and text.
type Cue struct {
	Start time.Duration
	End   time.Duration
	Text  string
}

// WriteVTT writes cues to a WebVTT file at path, creating parent dirs.
func WriteVTT(path string, cues []Cue) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("WEBVTT\n\n")
	for i, c := range cues {
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n",
			i+1, vttTimestamp(c.Start), vttTimestamp(c.End), strings.TrimSpace(c.Text))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// vttTimestamp formats a duration as WebVTT "HH:MM:SS.mmm".
func vttTimestamp(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	d -= s * time.Second
	ms := d / time.Millisecond
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}
