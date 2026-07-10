package transcode

import "testing"

func TestLadderRungs_PrunesAboveSource(t *testing.T) {
	rungs := LadderRungs(1080, 0)
	if len(rungs) == 0 {
		t.Fatal("expected at least one rung")
	}
	if rungs[0].Height != 1080 {
		t.Errorf("top rung should be 1080, got %d", rungs[0].Height)
	}
	for _, r := range rungs {
		if r.Height > 1080 {
			t.Errorf("rung %dp exceeds source 1080p", r.Height)
		}
	}
}

func TestLadderRungs_BitrateCap(t *testing.T) {
	rungs := LadderRungs(2160, 5000)
	for _, r := range rungs {
		if r.BitrateKbps > 5000 {
			t.Errorf("rung %s bitrate %d exceeds cap 5000", r.Name, r.BitrateKbps)
		}
	}
	if len(rungs) == 0 {
		t.Fatal("expected at least one rung under cap")
	}
}

func TestLadderRungs_TinySourceStillOne(t *testing.T) {
	rungs := LadderRungs(360, 0)
	if len(rungs) != 1 {
		t.Fatalf("expected exactly one rung for sub-480p source, got %d", len(rungs))
	}
}

func TestAudioBitrate(t *testing.T) {
	if got := audioBitrateKbps("eac3", 6); got != 768 {
		t.Errorf("eac3 5.1 should be 768k, got %d", got)
	}
	if got := audioBitrateKbps("aac", 2); got != 192 {
		t.Errorf("aac stereo should be 192k, got %d", got)
	}
	if got := audioBitrateKbps("ac3", 6); got != 640 {
		t.Errorf("ac3 5.1 should be 640k, got %d", got)
	}
}

func TestAudioArgsCopyVsTranscode(t *testing.T) {
	copyArgs := audioArgs(Decision{TranscodeAudio: false})
	if len(copyArgs) != 2 || copyArgs[1] != "copy" {
		t.Errorf("expected -c:a copy, got %v", copyArgs)
	}
	tc := audioArgs(Decision{TranscodeAudio: true, AudioCodec: "eac3", AudioChannels: 6})
	if tc[1] != "eac3" {
		t.Errorf("expected eac3 codec, got %v", tc)
	}
}
