// Package hwaccel detects the hardware video-encoding backend available to
// ffmpeg at startup (NVIDIA NVENC, Intel Quick Sync, Apple VideoToolbox, AMD
// AMF, or VA-API on Linux) and falls back to software with a clear warning.
package hwaccel

import (
	"context"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
)

// Backend identifies an acceleration method.
type Backend string

const (
	NVENC        Backend = "nvenc"
	QSV          Backend = "qsv"
	VideoToolbox Backend = "videotoolbox"
	AMF          Backend = "amf"
	VAAPI        Backend = "vaapi"
	Software     Backend = "software"
)

// Capabilities is the detected acceleration state.
type Capabilities struct {
	Backend   Backend   `json:"backend"`
	Available []Backend `json:"available"`
	H264      string    `json:"h264_encoder"` // ffmpeg encoder name for H.264
	HEVC      string    `json:"hevc_encoder"` // ffmpeg encoder name for HEVC
	AV1       string    `json:"av1_encoder"`  // ffmpeg AV1 encoder name, "" if none
}

// encoderFor maps a backend to its ffmpeg H.264/HEVC encoder names.
var encoderFor = map[Backend][2]string{
	NVENC:        {"h264_nvenc", "hevc_nvenc"},
	QSV:          {"h264_qsv", "hevc_qsv"},
	VideoToolbox: {"h264_videotoolbox", "hevc_videotoolbox"},
	AMF:          {"h264_amf", "hevc_amf"},
	VAAPI:        {"h264_vaapi", "hevc_vaapi"},
	Software:     {"libx264", "libx265"},
}

// av1EncoderFor maps a backend to its ffmpeg AV1 encoder name. Only newer GPUs
// (NVIDIA 40-series, Intel Arc/Xe QSV, recent AMD/VA-API) have AV1 encode, so
// the name is only used when Detect confirms it is present. VideoToolbox has no
// AV1 encoder.
var av1EncoderFor = map[Backend]string{
	NVENC:    "av1_nvenc",
	QSV:      "av1_qsv",
	AMF:      "av1_amf",
	VAAPI:    "av1_vaapi",
	Software: "libsvtav1",
}

// preference orders backends best-first. VideoToolbox is preferred on macOS.
func preference() []Backend {
	if runtime.GOOS == "darwin" {
		return []Backend{VideoToolbox, Software}
	}
	return []Backend{NVENC, QSV, VAAPI, AMF, Software}
}

// Detect probes ffmpeg's encoder list to determine available backends and picks
// the best one. override, when set to a known backend or "none"/"software",
// forces the choice.
func Detect(ctx context.Context, ffmpegPath, override string) Capabilities {
	encoders := listEncoders(ctx, ffmpegPath)

	var available []Backend
	for _, b := range []Backend{NVENC, QSV, VideoToolbox, AMF, VAAPI} {
		enc := encoderFor[b][0]
		if encoders[enc] {
			available = append(available, b)
		}
	}
	available = append(available, Software) // always available

	chosen := chooseBackend(available, override)
	caps := Capabilities{
		Backend:   chosen,
		Available: available,
		H264:      encoderFor[chosen][0],
		HEVC:      encoderFor[chosen][1],
	}
	// AV1 encode is a per-GPU-generation feature, so confirm the encoder is
	// actually present (not just implied by the backend).
	if enc, ok := av1EncoderFor[chosen]; ok && encoders[enc] {
		caps.AV1 = enc
	}
	if chosen == Software {
		slog.Warn("no hardware video acceleration detected; using software encoding (4K transcoding will not be real-time)",
			"available", available)
	} else {
		slog.Info("hardware acceleration selected", "backend", chosen, "available", available)
	}
	return caps
}

func chooseBackend(available []Backend, override string) Backend {
	ov := Backend(strings.ToLower(strings.TrimSpace(override)))
	if ov == "none" {
		ov = Software
	}
	if ov != "" {
		for _, b := range available {
			if b == ov {
				return b
			}
		}
		slog.Warn("configured hw_accel not available; falling back", "requested", override, "available", available)
	}
	for _, pref := range preference() {
		for _, b := range available {
			if b == pref {
				return b
			}
		}
	}
	return Software
}

// listEncoders returns the set of ffmpeg encoder names available.
func listEncoders(ctx context.Context, ffmpegPath string) map[string]bool {
	out := map[string]bool{}
	if ffmpegPath == "" {
		return out
	}
	cmd := exec.CommandContext(ctx, ffmpegPath, "-hide_banner", "-encoders")
	data, err := cmd.Output()
	if err != nil {
		slog.Debug("could not list ffmpeg encoders", "err", err)
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		// Lines look like " V....D h264_nvenc  NVIDIA NVENC H.264 encoder".
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && strings.HasPrefix(fields[0], "V") {
			out[fields[1]] = true
		}
	}
	return out
}
