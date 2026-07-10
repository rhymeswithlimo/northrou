package transcode

import (
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

func file(codec string, height int, hdr model.HDRType, audio model.AudioStream, container string) *model.MediaFile {
	return &model.MediaFile{
		Container: container,
		Video:     model.VideoStream{Codec: codec, Height: height, Width: height * 16 / 9, HDR: hdr},
		Audio:     []model.AudioStream{audio},
	}
}

var (
	truehdAtmos = model.AudioStream{Codec: "truehd", Channels: 8, Atmos: true, Default: true}
	dtsHDMA     = model.AudioStream{Codec: "dts", Profile: "DTS-HD MA", Channels: 6, Default: true}
	aac51       = model.AudioStream{Codec: "aac", Channels: 6, Default: true}
)

// appleTV: HEVC/HDR/Atmos capable, but not lossless audio or MKV container.
var appleTV = ClientCapabilities{
	VideoCodecs: []string{"h264", "hevc"}, AudioCodecs: []string{"aac", "ac3", "eac3"},
	Containers: []string{"mp4"}, MaxResolution: 2160, HDR: true, Atmos: true,
}

// homeTheaterPC: plays everything natively.
var everything = ClientCapabilities{
	VideoCodecs: []string{"h264", "hevc", "av1"},
	AudioCodecs: []string{"aac", "ac3", "eac3", "truehd", "dts", "flac"},
	Containers:  []string{"mp4", "mkv", "webm"}, MaxResolution: 2160, HDR: true, Atmos: true,
}

// oldBrowser: H.264 + AAC in MP4 only, 1080p SDR.
var oldBrowser = ClientCapabilities{
	VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"},
	Containers: []string{"mp4"}, MaxResolution: 1080,
}

func TestDecide_DirectPlay(t *testing.T) {
	mf := file("hevc", 2160, model.HDR10, truehdAtmos, "matroska,webm")
	d := Decide(mf, everything, Options{HWBackend: "nvenc"})
	if d.Mode != ModeDirectPlay {
		t.Fatalf("expected direct play, got %s (%s)", d.Mode, d.Reason)
	}
}

func TestDecide_Remux(t *testing.T) {
	// H.264 + AAC in MKV, client wants MP4, so copy both and change container.
	mf := file("h264", 1080, model.HDRNone, aac51, "matroska,webm")
	d := Decide(mf, oldBrowser, Options{})
	if d.Mode != ModeRemux || d.TranscodeVideo || d.TranscodeAudio {
		t.Fatalf("expected remux (no transcode), got %+v", d)
	}
}

func TestDecide_AudioTranscodePreservesAtmos(t *testing.T) {
	// 4K HEVC HDR with TrueHD Atmos to Apple TV: video direct, audio -> E-AC3 JOC.
	mf := file("hevc", 2160, model.HDR10, truehdAtmos, "matroska,webm")
	d := Decide(mf, appleTV, Options{HWBackend: "videotoolbox"})
	if d.Mode != ModeAudioTranscode {
		t.Fatalf("expected audio transcode, got %s (%s)", d.Mode, d.Reason)
	}
	if d.TranscodeVideo {
		t.Error("video should be copied, not transcoded")
	}
	if d.AudioCodec != "eac3" {
		t.Errorf("expected Atmos preserved as eac3, got %s", d.AudioCodec)
	}
}

func TestDecide_AudioDownmixToAAC(t *testing.T) {
	// DTS-HD MA to an AAC-only client => transcode audio to AAC.
	mf := file("h264", 1080, model.HDRNone, dtsHDMA, "matroska,webm")
	d := Decide(mf, oldBrowser, Options{})
	if d.Mode != ModeAudioTranscode || d.AudioCodec != "aac" {
		t.Fatalf("expected audio transcode to aac, got %+v", d)
	}
}

func TestDecide_FullVideoTranscode(t *testing.T) {
	// HEVC to an H.264-only client => full transcode.
	mf := file("hevc", 1080, model.HDRNone, aac51, "matroska,webm")
	d := Decide(mf, oldBrowser, Options{HWBackend: "qsv"})
	if d.Mode != ModeVideoTranscode || !d.TranscodeVideo {
		t.Fatalf("expected full video transcode, got %+v", d)
	}
	if d.VideoCodec != "h264" {
		t.Errorf("expected target h264, got %s", d.VideoCodec)
	}
	if d.HWBackend != "qsv" {
		t.Errorf("expected qsv backend, got %s", d.HWBackend)
	}
}

func TestDecide_Software4KNotRealtime(t *testing.T) {
	// 4K HEVC to H.264 client with no hardware accel and software 4K disallowed.
	mf := file("hevc", 2160, model.HDRNone, aac51, "matroska,webm")
	d := Decide(mf, oldBrowser, Options{HWBackend: "software", AllowSoftware4K: false})
	if d.Mode != ModeVideoTranscode {
		t.Fatalf("expected video transcode, got %s", d.Mode)
	}
	if d.Realtime {
		t.Error("expected Realtime=false for software 4K transcode")
	}
}

func TestDecide_HDRTonemapForcesTranscode(t *testing.T) {
	// HDR source, client codec-capable but SDR only => must transcode + tonemap.
	sdrHEVCClient := ClientCapabilities{
		VideoCodecs: []string{"hevc"}, AudioCodecs: []string{"aac"},
		Containers: []string{"mp4"}, MaxResolution: 2160, HDR: false,
	}
	mf := file("hevc", 2160, model.HDR10, aac51, "mp4")
	d := Decide(mf, sdrHEVCClient, Options{HWBackend: "nvenc", Tonemap: true})
	if d.Mode != ModeVideoTranscode {
		t.Fatalf("expected transcode for HDR->SDR, got %s (%s)", d.Mode, d.Reason)
	}
	if !d.Tonemap {
		t.Error("expected tone mapping enabled")
	}
}
