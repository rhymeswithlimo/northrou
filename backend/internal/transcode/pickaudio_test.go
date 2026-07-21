package transcode

import (
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

func TestPickAudioPreferredLanguage(t *testing.T) {
	mf := &model.MediaFile{Audio: []model.AudioStream{
		{Index: 1, Codec: "eac3", Language: "spa", Default: true},
		{Index: 2, Codec: "eac3", Language: "eng"},
		{Index: 3, Codec: "aac", Language: "eng", Commentary: true},
	}}
	// Prefer English: track 2 wins over the Spanish default and the commentary.
	got, ok := pickAudio(mf, []string{"en"})
	if !ok || got.Index != 2 {
		t.Fatalf("got index %d ok=%v, want 2", got.Index, ok)
	}
}

func TestPickAudioFallsBackToDefault(t *testing.T) {
	mf := &model.MediaFile{Audio: []model.AudioStream{
		{Index: 1, Codec: "eac3", Language: "fra"},
		{Index: 2, Codec: "eac3", Language: "ita", Default: true},
	}}
	// No preferred-language match: the default track wins.
	got, _ := pickAudio(mf, []string{"en"})
	if got.Index != 2 {
		t.Errorf("got index %d, want 2 (default)", got.Index)
	}
}

func TestPickAudioSkipsCommentaryFirst(t *testing.T) {
	mf := &model.MediaFile{Audio: []model.AudioStream{
		{Index: 1, Codec: "aac", Commentary: true},
		{Index: 2, Codec: "eac3"},
	}}
	// No prefs, no default: first NON-commentary track wins, not the first.
	got, _ := pickAudio(mf, nil)
	if got.Index != 2 {
		t.Errorf("got index %d, want 2 (skip commentary)", got.Index)
	}
}

func TestPickAudioNone(t *testing.T) {
	if _, ok := pickAudio(&model.MediaFile{}, []string{"en"}); ok {
		t.Error("expected ok=false with no audio")
	}
}

// A viewer's per-request (per-profile) audio language overrides the server
// default in the full decision.
func TestDecideProfileAudioOverride(t *testing.T) {
	mf := &model.MediaFile{
		Container: "matroska,webm",
		Video:     model.VideoStream{Codec: "h264", Height: 1080},
		Audio: []model.AudioStream{
			{Index: 1, Codec: "aac", Language: "eng", Channels: 2, Default: true},
			{Index: 2, Codec: "aac", Language: "spa", Channels: 2},
		},
	}
	caps := ClientCapabilities{
		VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, Containers: []string{"mkv"},
		PreferredAudioLangs: []string{"es"}, // this viewer prefers Spanish
	}
	// Server default is English, but the viewer's Spanish preference wins.
	d := Decide(mf, caps, Options{PreferredLangs: []string{"en"}})
	if d.AudioTrackIdx != 2 {
		t.Errorf("expected Spanish track (index 2), got %d", d.AudioTrackIdx)
	}
}
