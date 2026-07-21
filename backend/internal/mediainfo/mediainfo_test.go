package mediainfo

import "testing"

func vid(idx int, codec, pix string, w int, attachedPic int) probeStream {
	return probeStream{
		Index: idx, CodecType: "video", CodecName: codec, PixFmt: pix,
		Width: w, Height: w * 9 / 16,
		Disposition: map[string]int{"attached_pic": attachedPic},
	}
}

func TestAssembleSkipsCoverArtAfterVideo(t *testing.T) {
	po := probeOutput{Streams: []probeStream{
		vid(0, "hevc", "yuv420p10le", 1920, 0),
		vid(30, "mjpeg", "yuvj444p", 600, 1), // embedded poster
	}}
	mf := assemble(po)
	if mf.Video.Codec != "hevc" {
		t.Fatalf("picked %q, want hevc", mf.Video.Codec)
	}
	if mf.Video.BitDepth != 10 {
		t.Errorf("BitDepth = %d, want 10", mf.Video.BitDepth)
	}
}

func TestAssembleSkipsCoverArtBeforeVideo(t *testing.T) {
	// A poster muxed as the FIRST video stream must not be chosen just because
	// it comes first: this is the exact bug the disposition check fixes.
	po := probeOutput{Streams: []probeStream{
		vid(0, "png", "rgb24", 680, 1),
		vid(1, "hevc", "yuv420p", 3840, 0),
	}}
	mf := assemble(po)
	if mf.Video.Codec != "hevc" {
		t.Fatalf("picked %q, want hevc", mf.Video.Codec)
	}
	if mf.Video.Width != 3840 {
		t.Errorf("Width = %d, want 3840", mf.Video.Width)
	}
	if mf.Video.BitDepth != 8 {
		t.Errorf("BitDepth = %d, want 8", mf.Video.BitDepth)
	}
}

func TestAssembleCoverOnlyFallsBack(t *testing.T) {
	// Audio file with only embedded artwork: keep the cover rather than nothing.
	po := probeOutput{Streams: []probeStream{
		vid(1, "mjpeg", "yuvj444p", 600, 1),
	}}
	mf := assemble(po)
	if mf.Video.Codec != "mjpeg" {
		t.Fatalf("fallback picked %q, want mjpeg", mf.Video.Codec)
	}
}

func TestAssembleAudioSelectionMetadata(t *testing.T) {
	po := probeOutput{Streams: []probeStream{
		vid(0, "hevc", "yuv420p10le", 1920, 0),
		{Index: 1, CodecType: "audio", CodecName: "dts", Channels: 6,
			Tags: map[string]string{"language": "eng"}, Disposition: map[string]int{"default": 1}},
		{Index: 2, CodecType: "audio", CodecName: "ac3", Channels: 6,
			Tags: map[string]string{"language": "eng"}},
		{Index: 3, CodecType: "audio", CodecName: "aac", Channels: 2,
			Tags: map[string]string{"language": "eng", "title": "Commentary by Director"}},
	}}
	mf := assemble(po)
	if len(mf.Audio) != 3 {
		t.Fatalf("got %d audio, want 3", len(mf.Audio))
	}
	if !mf.Audio[0].Default {
		t.Error("track 0 should be default")
	}
	if !mf.Audio[2].Commentary {
		t.Error("track 2 should be flagged commentary")
	}
	if mf.Audio[0].Commentary {
		t.Error("track 0 should not be commentary")
	}
}

func TestAssembleSubtitleSDH(t *testing.T) {
	po := probeOutput{Streams: []probeStream{
		vid(0, "hevc", "yuv420p", 1920, 0),
		{Index: 1, CodecType: "subtitle", CodecName: "subrip",
			Tags: map[string]string{"language": "eng", "title": "English SDH"}},
		{Index: 2, CodecType: "subtitle", CodecName: "subrip",
			Tags: map[string]string{"language": "eng"}, Disposition: map[string]int{"forced": 1}},
	}}
	mf := assemble(po)
	if len(mf.Subtitles) != 2 {
		t.Fatalf("got %d subs, want 2", len(mf.Subtitles))
	}
	if !mf.Subtitles[0].SDH {
		t.Error("subtitle 0 should be SDH")
	}
	if !mf.Subtitles[1].Forced {
		t.Error("subtitle 1 should be forced")
	}
}

func TestBitDepthOf(t *testing.T) {
	cases := map[string]int{
		"yuv420p10le": 10, "yuv420p": 8, "yuv420p12le": 12, "": 0, "yuvj444p": 8,
	}
	for in, want := range cases {
		if got := bitDepthOf(in); got != want {
			t.Errorf("bitDepthOf(%q) = %d, want %d", in, got, want)
		}
	}
}
