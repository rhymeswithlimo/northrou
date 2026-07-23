package ffmpeg

import "testing"

func TestParseChecksumDoc(t *testing.T) {
	const h = "abc123abc123abc123abc123abc123abc123abc123abc123abc123abc1234567"
	cases := []struct {
		name, doc, want, wantName string
		ok                        bool
	}{
		{"bare", h, h, "x.tar.xz", true},
		{"sha256sum listing", h + "  ffmpeg-linux64.tar.xz\n" + "dead" + h[4:] + "  other.zip", h, "ffmpeg-linux64.tar.xz", true},
		{"name-colon-hex", "ffmpeg.zip: " + h, h, "ffmpeg.zip", true},
		{"missing", "notahex  ffmpeg.zip", "", "ffmpeg.zip", false},
	}
	for _, c := range cases {
		got, err := parseChecksumDoc(c.doc, c.wantName)
		if c.ok && (err != nil || got != c.want) {
			t.Errorf("%s: got %q err=%v, want %q", c.name, got, err, c.want)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: expected error, got %q", c.name, got)
		}
	}
}
