// Package transcode implements Northrou's streaming decision cascade and the
// ffmpeg pipelines behind it: direct play, remux, audio-only transcode, and
// full video transcode, with hardware acceleration, Atmos handling, HDR tone
// mapping, and adaptive HLS.
package transcode

import (
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/language"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// ClientCapabilities describes what a client player can handle natively. The
// frontend sends this with each stream request; the server uses it to choose
// the cheapest viable delivery path.
type ClientCapabilities struct {
	VideoCodecs    []string `json:"video_codecs"`    // e.g. ["h264","hevc","av1"]
	AudioCodecs    []string `json:"audio_codecs"`    // e.g. ["aac","ac3","eac3"]
	Containers     []string `json:"containers"`      // e.g. ["mp4","mkv"]
	MaxResolution  int      `json:"max_resolution"`  // max height (0 = unlimited)
	HDR            bool     `json:"hdr"`             // supports HDR passthrough
	DolbyVision    bool     `json:"dolby_vision"`    // renders Dolby Vision natively (incl. profile 7)
	Atmos          bool     `json:"atmos"`           // supports Dolby Atmos (E-AC3 JOC/TrueHD)
	MaxBitrateKbps int      `json:"max_bitrate_kbps"` // 0 = unlimited (remote adaptive)
	Remote         bool     `json:"remote"`          // stream is over the remote tunnel
	// PreferredAudioLangs is the viewer's per-request audio language preference
	// (from their profile). When empty the server default applies.
	PreferredAudioLangs []string `json:"-"`
}

// canonicalContainer maps an ffprobe format_name to a short canonical name
// matching what clients report.
func canonicalContainer(format string) string {
	f := strings.ToLower(format)
	switch {
	case strings.Contains(f, "matroska"):
		return "mkv"
	case strings.Contains(f, "mp4"), strings.Contains(f, "mov"):
		return "mp4"
	case strings.Contains(f, "webm"):
		return "webm"
	case strings.Contains(f, "avi"):
		return "avi"
	case strings.Contains(f, "mpegts"), strings.Contains(f, "ts"):
		return "ts"
	default:
		return f
	}
}

func supports(list []string, item string) bool {
	for _, v := range list {
		if strings.EqualFold(v, item) {
			return true
		}
	}
	return false
}

// pickAudio returns the audio stream to serve. Preference order: a
// non-commentary track in the household's preferred language (earlier codes
// win), then the container's default track, then the first non-commentary
// track, then the first track. Returns false when there is no audio.
func pickAudio(mf *model.MediaFile, preferredLangs []string) (model.AudioStream, bool) {
	if len(mf.Audio) == 0 {
		return model.AudioStream{}, false
	}
	// Preferred language, honoring the order of the preference list.
	for _, want := range preferredLangs {
		for _, a := range mf.Audio {
			if !a.Commentary && language.Match(a.Language, want) {
				return a, true
			}
		}
	}
	// Default-flagged, non-commentary.
	for _, a := range mf.Audio {
		if a.Default && !a.Commentary {
			return a, true
		}
	}
	// First non-commentary track.
	for _, a := range mf.Audio {
		if !a.Commentary {
			return a, true
		}
	}
	return mf.Audio[0], true
}

// DefaultCapabilities is a conservative profile (broad browser) used when a
// client sends none: H.264 + AAC in MP4, SDR, up to 1080p.
func DefaultCapabilities() ClientCapabilities {
	return ClientCapabilities{
		VideoCodecs:   []string{"h264"},
		AudioCodecs:   []string{"aac"},
		Containers:    []string{"mp4"},
		MaxResolution: 1080,
	}
}
