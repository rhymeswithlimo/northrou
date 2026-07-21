package transcode

import (
	"fmt"
	"strconv"

	"github.com/rhymeswithlimo/northrou/backend/internal/transcode/hwaccel"
)

// Rung is one rung of an adaptive bitrate ladder.
type Rung struct {
	Height     int
	BitrateKbps int
	Name       string
}

// fullLadder is the canonical 4K→480p ladder. Rungs above the source height or
// above the client's max bitrate are pruned by LadderRungs.
var fullLadder = []Rung{
	{2160, 16000, "4K"},
	{1080, 8000, "1080p"},
	{720, 4000, "720p"},
	{480, 2000, "480p"},
}

// LadderRungs returns the bitrate ladder for a source of the given height,
// capped at maxBitrateKbps (0 = uncapped). The top rung never exceeds the
// source resolution; at least one rung is always returned.
func LadderRungs(sourceHeight, maxBitrateKbps int) []Rung {
	var out []Rung
	for _, r := range fullLadder {
		if r.Height > sourceHeight {
			continue
		}
		if maxBitrateKbps > 0 && r.BitrateKbps > maxBitrateKbps {
			continue
		}
		out = append(out, r)
	}
	if len(out) == 0 {
		// Source smaller than 480p, or a very tight bitrate cap: emit one rung.
		h := sourceHeight
		if h == 0 {
			h = 480
		}
		br := 2000
		if maxBitrateKbps > 0 && maxBitrateKbps < br {
			br = maxBitrateKbps
		}
		out = []Rung{{Height: h, BitrateKbps: br, Name: strconv.Itoa(h) + "p"}}
	}
	return out
}

// inputArgs returns ffmpeg args placed before -i to enable hardware-accelerated
// decoding for the chosen backend.
func inputArgs(backend string) []string {
	switch hwaccel.Backend(backend) {
	case hwaccel.NVENC:
		return []string{"-hwaccel", "cuda"}
	case hwaccel.QSV:
		return []string{"-hwaccel", "qsv"}
	case hwaccel.VideoToolbox:
		return []string{"-hwaccel", "videotoolbox"}
	case hwaccel.VAAPI:
		return []string{"-hwaccel", "vaapi", "-hwaccel_output_format", "vaapi",
			"-vaapi_device", "/dev/dri/renderD128"}
	default:
		return nil
	}
}

// videoEncoder returns the ffmpeg encoder name for a target codec on a backend.
func videoEncoder(codec, backend string) string {
	if codec == "av1" {
		switch hwaccel.Backend(backend) {
		case hwaccel.NVENC:
			return "av1_nvenc"
		case hwaccel.QSV:
			return "av1_qsv"
		case hwaccel.AMF:
			return "av1_amf"
		case hwaccel.VAAPI:
			return "av1_vaapi"
		default:
			return "libsvtav1"
		}
	}
	switch hwaccel.Backend(backend) {
	case hwaccel.NVENC:
		return "h264_nvenc"
	case hwaccel.QSV:
		return "h264_qsv"
	case hwaccel.VideoToolbox:
		return "h264_videotoolbox"
	case hwaccel.AMF:
		return "h264_amf"
	case hwaccel.VAAPI:
		return "h264_vaapi"
	default:
		return "libx264"
	}
}

// videoFilter builds the -vf chain for scaling and HDR tone mapping. Height 0
// means keep source height.
func videoFilter(dec Decision, height int) string {
	// Software tone mapping (portable across backends) when downconverting HDR.
	if dec.Tonemap {
		f := "zscale=t=linear:npl=100,format=gbrpf32le,tonemap=hable,zscale=t=bt709:m=bt709:r=tv,format=yuv420p"
		if height > 0 {
			f = fmt.Sprintf("scale=-2:%d,", height) + f
		}
		return f
	}
	if height > 0 {
		return fmt.Sprintf("scale=-2:%d", height)
	}
	return ""
}

// audioBitrateKbps picks a target bitrate for the codec and channel count.
func audioBitrateKbps(codec string, channels int) int {
	switch codec {
	case "eac3":
		if channels > 2 {
			return 768 // DD+ / Atmos JOC
		}
		return 256
	case "ac3":
		if channels > 2 {
			return 640
		}
		return 256
	default: // aac
		if channels > 2 {
			return 384
		}
		return 192
	}
}

// audioArgs builds ffmpeg audio args for the decision.
func audioArgs(dec Decision) []string {
	if !dec.TranscodeAudio {
		return []string{"-c:a", "copy"}
	}
	ch := dec.AudioChannels
	if ch == 0 {
		ch = 2
	}
	br := audioBitrateKbps(dec.AudioCodec, ch)
	return []string{
		"-c:a", dec.AudioCodec,
		"-ac", strconv.Itoa(ch),
		"-b:a", strconv.Itoa(br) + "k",
	}
}

// progressiveArgs builds the full ffmpeg argument list for remux / audio-only
// transcode delivered as a fragmented MP4 stream to stdout (pipe:1).
func progressiveArgs(dec Decision, inputPath string) []string {
	args := []string{"-hide_banner", "-loglevel", "error"}
	args = append(args, "-i", inputPath)
	args = append(args, "-map", "0:v:0", "-map", "0:a:0")
	args = append(args, "-c:v", "copy") // remux/audio path never touches video
	args = append(args, audioArgs(dec)...)
	args = append(args, "-movflags", "frag_keyframe+empty_moov+default_base_moof")
	args = append(args, "-f", "mp4", "pipe:1")
	return args
}
