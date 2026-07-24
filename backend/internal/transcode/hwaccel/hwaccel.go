// Package hwaccel detects the hardware video-encoding backend available to
// ffmpeg at startup (NVIDIA NVENC, Intel Quick Sync, Apple VideoToolbox, AMD
// AMF, or VA-API on Linux) and falls back to software with a clear warning.
package hwaccel

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"
	"time"
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

// probeTimeout bounds each hardware test-encode. Detection runs in the async
// ffmpeg-init path (app.ensureFFmpeg), so a hung probe must not stall startup.
var probeTimeout = 8 * time.Second

// driRenderNode is the VA-API / QSV render node the Linux probes exercise. It
// matches the device inputArgs uses for the real transcode pipeline, so a probe
// that passes reflects a device the pipeline can actually use.
var driRenderNode = "/dev/dri/renderD128"

// Detect determines which acceleration backends this machine can actually use
// and picks the best one. override, when set to a known backend or
// "none"/"software", forces the choice.
//
// A static ffmpeg build (the kind Northrou downloads) has *every* hardware
// encoder compiled in - h264_nvenc, h264_qsv, h264_amf and so on are all
// present regardless of what silicon is in the box. So the encoder list only
// tells us what ffmpeg *could* do; it says nothing about the GPU. Each
// compiled-in candidate is therefore confirmed with a trivial test-encode that
// really initializes the device, and only the ones that succeed are reported.
// Without this an Intel-only laptop happily claims NVENC.
func Detect(ctx context.Context, ffmpegPath, override string) Capabilities {
	compiled := listEncoders(ctx, ffmpegPath)
	verify := func(b Backend, enc string) (bool, string) {
		return verifyEncoder(ctx, ffmpegPath, b, enc)
	}

	available := selectBackends(compiled, verify)

	chosen := chooseBackend(available, override)
	caps := Capabilities{
		Backend:   chosen,
		Available: available,
		H264:      encoderFor[chosen][0],
		HEVC:      encoderFor[chosen][1],
	}
	// AV1 encode is a per-GPU-generation feature (only the newest silicon has
	// it), so a compiled-in av1_* encoder is even less trustworthy than the
	// H.264 one. Confirm hardware AV1 with the same test-encode; software AV1
	// (libsvtav1) always works if it is built in.
	if enc, ok := av1EncoderFor[chosen]; ok && compiled[enc] {
		if chosen == Software {
			caps.AV1 = enc
		} else if ok, detail := verify(chosen, enc); ok {
			caps.AV1 = enc
		} else {
			slog.Debug("av1 encoder compiled in but not usable on this machine",
				"backend", chosen, "encoder", enc, "detail", detail)
		}
	}
	if chosen == Software {
		slog.Warn("no hardware video acceleration detected; using software encoding (4K transcoding will not be real-time)",
			"available", available)
	} else {
		slog.Info("hardware acceleration selected", "backend", chosen, "available", available)
	}
	return caps
}

// selectBackends returns the backends that are both compiled into ffmpeg and
// confirmed usable by verify, best-effort ordered as probed, with Software
// always appended. Split out from Detect so the compiled-in vs. actually-usable
// intersection is unit-testable without a real ffmpeg.
func selectBackends(compiled map[string]bool, verify func(Backend, string) (bool, string)) []Backend {
	var available []Backend
	for _, b := range []Backend{NVENC, QSV, VideoToolbox, AMF, VAAPI} {
		enc := encoderFor[b][0]
		if !compiled[enc] {
			continue
		}
		if ok, detail := verify(b, enc); ok {
			available = append(available, b)
		} else {
			// Log at info so a user who expected hardware accel can see *why* it
			// was rejected - "No NVENC capable devices" (no such GPU) reads very
			// differently from "Permission denied /dev/dri/renderD128" (a service
			// user not in the render/video group, a real footgun that otherwise
			// silently degrades to software).
			slog.Info("hardware encoder compiled into ffmpeg but not usable on this machine; skipping",
				"backend", b, "encoder", enc, "detail", detail)
		}
	}
	return append(available, Software) // always available
}

// verifyEncoder runs a one-frame test-encode with the given encoder and returns
// whether it succeeded, plus a short reason (the tail of ffmpeg's stderr) when
// it did not. A clean exit means the GPU and its driver are actually usable, not
// merely that the encoder is compiled in.
func verifyEncoder(ctx context.Context, ffmpegPath string, b Backend, encoder string) (bool, string) {
	if ffmpegPath == "" {
		return false, "ffmpeg not available"
	}
	pctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	args := append(probeInputArgs(b), "-c:v", encoder, "-f", "null", "-")
	cmd := exec.CommandContext(pctx, ffmpegPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()

	if pctx.Err() == context.DeadlineExceeded {
		return false, "probe timed out"
	}
	if err != nil {
		return false, probeErrorDetail(stderr.String())
	}
	return true, ""
}

// probeInputArgs builds the ffmpeg args up to (but not including) the encoder
// for a backend's test-encode: a tiny synthetic source, uploaded to the GPU for
// the backends that require frames in device memory. NVENC, AMF and
// VideoToolbox accept system-memory frames directly; VA-API and QSV must be
// pointed at a real device and have the frames uploaded, which is exactly what
// makes their probe fail truthfully when the device is missing or inaccessible.
func probeInputArgs(b Backend) []string {
	const src = "color=black:s=256x256:d=0.1" // a couple of frames, even dimensions
	base := []string{"-hide_banner", "-loglevel", "error", "-nostats"}
	switch b {
	case VAAPI:
		return append(base,
			"-vaapi_device", driRenderNode,
			"-f", "lavfi", "-i", src,
			"-vf", "format=nv12,hwupload")
	case QSV:
		return append(base,
			"-init_hw_device", "qsv=hw", "-filter_hw_device", "hw",
			"-f", "lavfi", "-i", src,
			"-vf", "format=nv12,hwupload=extra_hw_frames=16")
	default: // NVENC, AMF, VideoToolbox
		return append(base, "-f", "lavfi", "-i", src)
	}
}

// probeErrorDetail returns the last non-empty line of ffmpeg's stderr, where the
// actual failure ("No NVENC capable devices found", "Permission denied", "Cannot
// load libmfx") lands, capped so a log line stays readable.
func probeErrorDetail(stderr string) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			if len(s) > 200 {
				s = s[:200]
			}
			return s
		}
	}
	return "encoder initialization failed"
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

// listEncoders returns the set of video encoder names compiled into ffmpeg.
// This is only a prefilter: a name here means ffmpeg *can* run the encoder, not
// that the hardware exists (see Detect, which then verifies each candidate).
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
