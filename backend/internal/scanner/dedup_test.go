package scanner

import (
	"testing"

	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

func TestDedupKey(t *testing.T) {
	movie := dedupKey(model.KindMovie, ParsedInfo{Title: "Avatar", Year: 2009}, "/x.mkv")
	if dedupKey(model.KindMovie, ParsedInfo{Title: "avatar", Year: 2009}, "/y.mp4") != movie {
		t.Error("same movie, different case/path should share a key")
	}
	ep := dedupKey(model.KindEpisode, ParsedInfo{Title: "The Boys", Season: 2, Episode: 1, IsEpisode: true}, "/a.mkv")
	if dedupKey(model.KindEpisode, ParsedInfo{Title: "The Boys", Season: 2, Episode: 1, IsEpisode: true}, "/b.mp4") != ep {
		t.Error("same episode should share a key")
	}
	// Degenerate parse keys by path so distinct unparseable files never merge.
	if dedupKey(model.KindMovie, ParsedInfo{}, "/a.mkv") == dedupKey(model.KindMovie, ParsedInfo{}, "/b.mkv") {
		t.Error("empty-title files must not share a key")
	}
}

func TestClaimBestPicksHigherResolution(t *testing.T) {
	s := &Scanner{dedup: map[string]dupBest{}}
	mp4 := &model.MediaFile{Video: model.VideoStream{Height: 1080}, Duration: 100, SizeBytes: 1e9}
	mkv := &model.MediaFile{Video: model.VideoStream{Height: 2160}, Duration: 100, SizeBytes: 2e9}

	if !s.claimBest("avatar", quality(mp4, "/Avatar.mp4")) {
		t.Fatal("first copy should be claimed")
	}
	if !s.claimBest("avatar", quality(mkv, "/Avatar.mkv")) {
		t.Fatal("higher-resolution copy should win")
	}
	// The 1080p copy arriving now must lose to the already-claimed 2160p.
	if s.claimBest("avatar", quality(mp4, "/Avatar.mp4")) {
		t.Fatal("lower-resolution copy should lose")
	}
}

// A file must always be able to re-claim itself (same path), so an in-place
// re-encode/remux at equal or lower quality is still re-ingested.
func TestClaimBestSamePathAlwaysReclaims(t *testing.T) {
	s := &Scanner{dedup: map[string]dupBest{}}
	q := dupBest{height: 1080, bitrate: 5000, container: 4, path: "/Show.mkv"}
	if !s.claimBest("k", q) {
		t.Fatal("first claim should win")
	}
	// Same path, identical quality: must still re-claim (not treated as a dup).
	if !s.claimBest("k", q) {
		t.Fatal("same-path re-claim should succeed so the file can update itself")
	}
	// Same path, LOWER quality (e.g. re-encoded smaller): still re-claims.
	lower := dupBest{height: 720, bitrate: 3000, container: 4, path: "/Show.mkv"}
	if !s.claimBest("k", lower) {
		t.Fatal("same-path lower-quality re-claim should succeed")
	}
}

func TestBetterDupTiebreaks(t *testing.T) {
	base := dupBest{height: 1080, bitrate: 5000, container: 3, path: "/b"}
	// Equal resolution + bitrate, mkv beats mp4 on container rank.
	if !betterDup(dupBest{height: 1080, bitrate: 5000, container: 4, path: "/z"}, base) {
		t.Error("mkv (rank 4) should beat mp4 (rank 3)")
	}
	// Fully equal: deterministic lexicographic path tiebreak.
	if !betterDup(dupBest{height: 1080, bitrate: 5000, container: 3, path: "/a"}, base) {
		t.Error("lexicographically smaller path should win the tie")
	}
	if betterDup(base, base) {
		t.Error("identical should not be 'better'")
	}
}
