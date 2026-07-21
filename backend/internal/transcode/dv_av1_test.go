package transcode

import (
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

func dvFile(profile, compat int) *model.MediaFile {
	return &model.MediaFile{
		Container: "matroska,webm",
		Video: model.VideoStream{
			Codec: "hevc", Height: 2160, HDR: model.HDRDolbyVision,
			DVProfile: profile, DVBLCompat: compat,
		},
		Audio: []model.AudioStream{{Codec: "eac3", Channels: 6}},
	}
}

func TestDolbyVisionDecisions(t *testing.T) {
	// A DV-native client (e.g. Apple TV) direct-plays any profile.
	dvClient := ClientCapabilities{VideoCodecs: []string{"hevc"}, AudioCodecs: []string{"eac3"},
		Containers: []string{"mkv"}, HDR: true, DolbyVision: true}
	if d := Decide(dvFile(7, 0), dvClient, Options{}); d.Mode != ModeDirectPlay {
		t.Errorf("DV-native client should direct-play profile 7, got %s (%s)", d.Mode, d.Reason)
	}

	// HDR10 client, no DV: profile 8.1 (HDR10-compatible base) direct-plays...
	hdrClient := ClientCapabilities{VideoCodecs: []string{"hevc"}, AudioCodecs: []string{"eac3"},
		Containers: []string{"mkv"}, HDR: true}
	if d := Decide(dvFile(8, 1), hdrClient, Options{}); d.Mode != ModeDirectPlay {
		t.Errorf("HDR client should direct-play cross-compatible profile 8.1, got %s (%s)", d.Mode, d.Reason)
	}
	// ...but profile 7 (dual-layer) must transcode for a non-DV client.
	if d := Decide(dvFile(7, 0), hdrClient, Options{}); d.Mode != ModeVideoTranscode {
		t.Errorf("HDR client should transcode profile 7, got %s (%s)", d.Mode, d.Reason)
	}
	// ...and profile 5 (DV-only base) also transcodes on a non-DV client.
	if d := Decide(dvFile(5, 0), hdrClient, Options{}); d.Mode != ModeVideoTranscode {
		t.Errorf("HDR client should transcode DV-only profile 5, got %s (%s)", d.Mode, d.Reason)
	}

	// SDR client: DV transcodes and tone maps.
	sdr := ClientCapabilities{VideoCodecs: []string{"hevc"}, AudioCodecs: []string{"eac3"}, Containers: []string{"mkv"}}
	d := Decide(dvFile(8, 1), sdr, Options{Tonemap: true})
	if d.Mode != ModeVideoTranscode || !d.Tonemap {
		t.Errorf("SDR client should transcode+tonemap DV, got mode=%s tonemap=%v", d.Mode, d.Tonemap)
	}
}

func TestAV1TranscodeTarget(t *testing.T) {
	// HEVC source, client wants h264+av1, hardware AV1 encoder available -> AV1.
	src := &model.MediaFile{Container: "matroska,webm",
		Video: model.VideoStream{Codec: "hevc", Height: 1080},
		Audio: []model.AudioStream{{Codec: "aac", Channels: 2}}}
	av1Client := ClientCapabilities{VideoCodecs: []string{"h264", "av1"}, AudioCodecs: []string{"aac"}, Containers: []string{"mp4"}}

	d := Decide(src, av1Client, Options{HWBackend: "nvenc", AV1Encode: true})
	if d.Mode != ModeVideoTranscode || d.VideoCodec != "av1" {
		t.Errorf("expected AV1 transcode, got mode=%s codec=%s", d.Mode, d.VideoCodec)
	}
	// No AV1 encoder -> fall back to H.264.
	d = Decide(src, av1Client, Options{HWBackend: "software"})
	if d.VideoCodec != "h264" {
		t.Errorf("without AV1 encoder expected h264, got %s", d.VideoCodec)
	}
	// AV1 encoder present but client can't play AV1 -> H.264.
	h264Only := ClientCapabilities{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Containers: []string{"mp4"}}
	d = Decide(src, h264Only, Options{HWBackend: "nvenc", AV1Encode: true})
	if d.VideoCodec != "h264" {
		t.Errorf("client without AV1 should get h264, got %s", d.VideoCodec)
	}
}
