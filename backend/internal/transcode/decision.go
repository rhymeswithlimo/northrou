package transcode

import (
	"fmt"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// Mode is the chosen delivery path, in ascending order of CPU cost.
type Mode string

const (
	ModeDirectPlay     Mode = "direct"    // serve raw bytes, zero processing
	ModeRemux          Mode = "remux"     // copy streams, change container only
	ModeAudioTranscode Mode = "audio"     // copy video, transcode audio
	ModeVideoTranscode Mode = "video"     // full transcode (HEVC->H.264, etc.)
)

// Decision is the resolved plan for a single stream request.
type Decision struct {
	Mode           Mode   `json:"mode"`
	TranscodeVideo bool   `json:"transcode_video"`
	TranscodeAudio bool   `json:"transcode_audio"`
	Container      string `json:"container"`       // direct|fmp4|hls
	VideoCodec     string `json:"video_codec"`     // target (source if copied)
	AudioCodec     string `json:"audio_codec"`     // target (source if copied)
	AudioChannels  int    `json:"audio_channels"`
	AudioTrackIdx  int    `json:"audio_track_index"`
	Tonemap        bool   `json:"tonemap"`         // HDR->SDR tone mapping
	HWBackend      string `json:"hw_backend"`      // acceleration backend, or "software"
	Realtime       bool   `json:"realtime"`        // false => software 4K, not real-time
	Reason         string `json:"reason"`

	sourceHeight int // source video height, used to size the HLS ladder
}

// Options carries server-side preferences into the decision.
type Options struct {
	HWBackend       string // detected/overridden acceleration backend
	AllowSoftware4K bool
	Tonemap         bool // config toggle enabling HDR tone mapping
}

// Decide runs the layered cascade and returns the cheapest viable plan.
func Decide(mf *model.MediaFile, caps ClientCapabilities, opt Options) Decision {
	audio, hasAudio := pickAudio(mf)
	container := canonicalContainer(mf.Container)

	videoOK, videoReason := videoCompatible(mf.Video, caps)
	audioOK := !hasAudio || supports(caps.AudioCodecs, audio.Codec)
	containerOK := supports(caps.Containers, container)

	d := Decision{
		VideoCodec:    mf.Video.Codec,
		AudioCodec:    audio.Codec,
		AudioChannels: audio.Channels,
		AudioTrackIdx: audio.Index,
		HWBackend:     "software",
		Realtime:      true,
		sourceHeight:  mf.Video.Height,
	}

	switch {
	case videoOK && audioOK && containerOK:
		d.Mode = ModeDirectPlay
		d.Container = "direct"
		d.Reason = "client supports source video, audio, and container"
		return d

	case videoOK && audioOK && !containerOK:
		d.Mode = ModeRemux
		d.Container = "fmp4"
		d.Reason = fmt.Sprintf("container %q incompatible; remuxing to fMP4 (copy)", container)
		return d

	case videoOK && !audioOK:
		d.Mode = ModeAudioTranscode
		d.Container = "fmp4"
		d.TranscodeAudio = true
		d.AudioCodec, d.AudioChannels = chooseAudioTarget(audio, caps)
		d.Reason = fmt.Sprintf("audio %q incompatible; copying video, transcoding audio to %s", audio.Codec, d.AudioCodec)
		return d

	default: // !videoOK
		d.Mode = ModeVideoTranscode
		d.Container = "hls"
		d.TranscodeVideo = true
		d.VideoCodec = "h264" // most compatible target
		d.HWBackend = opt.HWBackend
		if d.HWBackend == "" {
			d.HWBackend = "software"
		}
		d.Tonemap = opt.Tonemap && mf.Video.HDR != model.HDRNone && !caps.HDR
		if !audioOK {
			d.TranscodeAudio = true
			d.AudioCodec, d.AudioChannels = chooseAudioTarget(audio, caps)
		}
		// Real-time 4K needs hardware acceleration.
		if mf.Video.Height >= 2160 && d.HWBackend == "software" && !opt.AllowSoftware4K {
			d.Realtime = false
		}
		d.Reason = "video " + videoReason + "; full transcode to H.264"
		return d
	}
}

// videoCompatible reports whether the client can direct-play the source video,
// and if not, why.
func videoCompatible(v model.VideoStream, caps ClientCapabilities) (bool, string) {
	if !supports(caps.VideoCodecs, v.Codec) {
		return false, fmt.Sprintf("codec %q unsupported", v.Codec)
	}
	if caps.MaxResolution > 0 && v.Height > caps.MaxResolution {
		return false, fmt.Sprintf("resolution %dp exceeds client max %dp", v.Height, caps.MaxResolution)
	}
	if v.HDR != model.HDRNone && !caps.HDR {
		return false, fmt.Sprintf("HDR (%s) unsupported by SDR client", v.HDR)
	}
	return true, ""
}

// chooseAudioTarget applies the Atmos-preserving policy: pass Atmos through as
// E-AC3 JOC when the client supports it, otherwise fall back down the ladder to
// AC-3 then AAC. Returns codec and channel count.
func chooseAudioTarget(src model.AudioStream, caps ClientCapabilities) (string, int) {
	channels := src.Channels
	if channels == 0 {
		channels = 2
	}
	switch {
	case src.Atmos && caps.Atmos:
		// Preserve Atmos via Dolby Digital Plus with Joint Object Coding.
		return "eac3", clampChannels(channels, 6)
	case supports(caps.AudioCodecs, "eac3"):
		return "eac3", clampChannels(channels, 6)
	case supports(caps.AudioCodecs, "ac3"):
		return "ac3", clampChannels(channels, 6)
	default:
		// Last resort: AAC. Keep 5.1 if the client can, else stereo.
		return "aac", clampChannels(channels, 6)
	}
}

func clampChannels(ch, max int) int {
	if ch > max {
		return max
	}
	if ch < 1 {
		return 2
	}
	return ch
}
