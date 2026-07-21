// Package subtitles extracts subtitle tracks from media files during scanning.
// Text tracks (SRT/ASS) are converted to WebVTT immediately with ffmpeg;
// image-based PGS tracks are queued for background OCR with Tesseract. When
// Tesseract is unavailable, PGS tracks are marked "skipped" and the pipeline
// degrades gracefully.
package subtitles

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// Extractor converts and OCRs subtitle tracks.
type Extractor struct {
	db          *db.DB
	ffmpegPath  string
	tesseract   string // "" when unavailable
	dir         string // dataDir/subtitles

	jobs    chan ocrJob
	startMu sync.Mutex
	started bool

	playback func() int // active stream count; nil until wired. Guarded by startMu.
}

// ocrYieldInterval is how long an OCR worker parks between checks while playback
// is active.
const ocrYieldInterval = 500 * time.Millisecond

// ocr job kinds.
const (
	ocrPGS           = "pgs"             // embedded PGS via .sup
	ocrVobSubEmbed   = "vobsub-embedded" // embedded dvd_subtitle, extracted with ffmpeg
	ocrVobSubExtern  = "vobsub-external" // external .idx/.sub pair
)

type ocrJob struct {
	trackID     int64
	kind        string
	filePath    string // media file (PGS / embedded VobSub)
	streamIndex int
	language    string
	idxPath     string // external VobSub .idx
	subPath     string // external VobSub .sub
}

// New builds an Extractor. dataDir/subtitles holds generated WebVTT files.
// tesseractPath may be "" to disable OCR.
func New(database *db.DB, ffmpegPath, tesseractPath, dataDir string) *Extractor {
	return &Extractor{
		db:         database,
		ffmpegPath: ffmpegPath,
		tesseract:  tesseractPath,
		dir:        filepath.Join(dataDir, "subtitles"),
		jobs:       make(chan ocrJob, 256),
	}
}

// SetPlaybackGate wires a function reporting the number of active streams. While
// it returns > 0, OCR workers park so per-cue Tesseract runs (CPU-heavy) do not
// starve a live stream on a weak box.
func (e *Extractor) SetPlaybackGate(activeStreams func() int) {
	e.startMu.Lock()
	e.playback = activeStreams
	e.startMu.Unlock()
}

// waitWhilePlaying blocks until no streams are active or ctx is cancelled.
func (e *Extractor) waitWhilePlaying(ctx context.Context) {
	e.startMu.Lock()
	gate := e.playback
	e.startMu.Unlock()
	if gate == nil {
		return
	}
	for gate() > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(ocrYieldInterval):
		}
	}
}

// Start launches OCR workers and requeues any tracks left pending from a prior
// run. Idempotent.
func (e *Extractor) Start(ctx context.Context) {
	e.startMu.Lock()
	if e.started {
		e.startMu.Unlock()
		return
	}
	e.started = true
	e.startMu.Unlock()

	for range 2 {
		go e.ocrWorker(ctx)
	}
	go e.requeuePending(ctx)
}

// isTextCodec reports whether an ffprobe subtitle codec is text-based.
func isTextCodec(codec string) bool {
	switch codec {
	case "subrip", "srt", "ass", "ssa", "webvtt", "mov_text", "text":
		return true
	}
	return false
}

// normalizeFormat maps ffprobe codec names to our stored format tag.
func normalizeFormat(codec string) string {
	switch codec {
	case "subrip", "srt":
		return "subrip"
	case "ass", "ssa":
		return "ass"
	case "hdmv_pgs_subtitle", "pgs":
		return "pgs"
	case "dvd_subtitle", "dvdsub":
		return "dvdsub"
	default:
		return codec
	}
}

// ExtractForFile processes all subtitle tracks of a media file. Text tracks are
// converted synchronously; PGS tracks are enqueued for OCR. Implements
// scanner.SubtitleExtractor.
func (e *Extractor) ExtractForFile(ctx context.Context, fileID int64, mf *model.MediaFile) error {
	for _, s := range mf.Subtitles {
		format := normalizeFormat(s.Codec)
		track := &db.SubtitleTrack{
			FileID:     fileID,
			TrackIndex: s.Index,
			Language:   s.Language,
			Title:      s.Title,
			Format:     format,
			Source:     "embedded",
			Forced:     s.Forced,
			SDH:        s.SDH,
			OCRStatus:  "none",
		}

		switch {
		case isTextCodec(s.Codec):
			trackID, err := e.db.UpsertSubtitleTrack(ctx, track)
			if err != nil {
				return err
			}
			if err := e.convertTextTrack(ctx, trackID, mf.Path, s.Index); err != nil {
				slog.Debug("text subtitle conversion failed", "path", mf.Path, "index", s.Index, "err", err)
				_ = e.db.SetSubtitleVTT(ctx, trackID, "", "failed")
			}
		case format == "pgs":
			if e.tesseract == "" {
				track.OCRStatus = "skipped"
				if _, err := e.db.UpsertSubtitleTrack(ctx, track); err != nil {
					return err
				}
				continue
			}
			track.OCRStatus = "queued"
			trackID, err := e.db.UpsertSubtitleTrack(ctx, track)
			if err != nil {
				return err
			}
			e.enqueue(ocrJob{trackID: trackID, kind: ocrPGS, filePath: mf.Path, streamIndex: s.Index, language: s.Language})
		case format == "dvdsub":
			// Embedded DVD/VobSub: OCR it when Tesseract is available, else skip.
			if e.tesseract == "" {
				track.OCRStatus = "skipped"
				if _, err := e.db.UpsertSubtitleTrack(ctx, track); err != nil {
					return err
				}
				continue
			}
			track.OCRStatus = "queued"
			trackID, err := e.db.UpsertSubtitleTrack(ctx, track)
			if err != nil {
				return err
			}
			e.enqueue(ocrJob{trackID: trackID, kind: ocrVobSubEmbed, filePath: mf.Path, streamIndex: s.Index, language: s.Language})
		default:
			track.OCRStatus = "skipped"
			if _, err := e.db.UpsertSubtitleTrack(ctx, track); err != nil {
				return err
			}
		}
	}
	return nil
}

// ExtractSidecars discovers external subtitle files next to videoPath and
// converts the text ones to WebVTT. External VobSub (.sub) is recorded but not
// OCR'd yet, mirroring embedded dvd_subtitle. Idempotent: an already-converted
// sidecar whose VTT still exists is skipped, so re-scans are cheap even though
// sidecars are invisible to the video's mtime-based NeedsScan check.
func (e *Extractor) ExtractSidecars(ctx context.Context, fileID int64, videoPath string) error {
	for _, sub := range DiscoverSidecars(videoPath) {
		track := &db.SubtitleTrack{
			FileID:    fileID,
			TrackIndex: 0,
			ExtPath:   sub.Path,
			Language:  sub.Language,
			Format:    sub.Format,
			Source:    "external",
			Forced:    sub.Forced,
			SDH:       sub.SDH,
			OCRStatus: "none",
		}
		if sub.Format == "vobsub" {
			// External VobSub needs its .idx sibling and Tesseract to OCR.
			idxPath := strings.TrimSuffix(sub.Path, filepath.Ext(sub.Path)) + ".idx"
			if e.tesseract == "" || !fileExists(idxPath) {
				track.OCRStatus = "skipped"
				if _, err := e.db.UpsertSubtitleTrack(ctx, track); err != nil {
					return err
				}
				continue
			}
			if existing, err := e.db.GetExternalSubtitle(ctx, fileID, sub.Path); err == nil &&
				existing.OCRStatus == "done" && fileExists(existing.VTTPath) {
				continue
			}
			track.OCRStatus = "queued"
			trackID, err := e.db.UpsertSubtitleTrack(ctx, track)
			if err != nil {
				return err
			}
			e.enqueue(ocrJob{trackID: trackID, kind: ocrVobSubExtern, idxPath: idxPath, subPath: sub.Path, language: sub.Language})
			continue
		}
		if existing, err := e.db.GetExternalSubtitle(ctx, fileID, sub.Path); err == nil &&
			existing.OCRStatus == "done" && fileExists(existing.VTTPath) {
			continue
		}
		trackID, err := e.db.UpsertSubtitleTrack(ctx, track)
		if err != nil {
			return err
		}
		if err := e.convertExternalTrack(ctx, trackID, sub.Path); err != nil {
			slog.Debug("external subtitle conversion failed", "path", sub.Path, "err", err)
			_ = e.db.SetSubtitleVTT(ctx, trackID, "", "failed")
		}
	}
	return nil
}

// convertExternalTrack normalizes a sidecar's charset to UTF-8 and transcodes it
// to WebVTT. ffmpeg assumes UTF-8 input, so cp1252/Latin-1 files (common in
// scene/YIFY releases) must be transcoded first or they mojibake.
func (e *Extractor) convertExternalTrack(ctx context.Context, trackID int64, subPath string) error {
	raw, err := os.ReadFile(subPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(e.dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(e.dir, "in-"+strconv.FormatInt(trackID, 10)+strings.ToLower(filepath.Ext(subPath)))
	if err := os.WriteFile(tmp, toUTF8(raw), 0o644); err != nil {
		return err
	}
	defer os.Remove(tmp)

	out := e.vttPath(trackID)
	cmd := exec.CommandContext(ctx, e.ffmpegPath, "-y", "-i", tmp, "-c:s", "webvtt", out)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg webvtt: %w: %s", err, lastLine(output))
	}
	return e.db.SetSubtitleVTT(ctx, trackID, out, "done")
}

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	info, err := os.Stat(p)
	return err == nil && !info.IsDir()
}

// convertTextTrack extracts one subtitle stream and transcodes it to WebVTT.
func (e *Extractor) convertTextTrack(ctx context.Context, trackID int64, mediaPath string, streamIndex int) error {
	out := e.vttPath(trackID)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, e.ffmpegPath,
		"-y", "-i", mediaPath,
		"-map", "0:"+strconv.Itoa(streamIndex),
		"-c:s", "webvtt",
		out,
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("ffmpeg webvtt: %w: %s", err, lastLine(output))
	}
	return e.db.SetSubtitleVTT(ctx, trackID, out, "done")
}

// vttPath returns the on-disk WebVTT path for a track.
func (e *Extractor) vttPath(trackID int64) string {
	return filepath.Join(e.dir, strconv.FormatInt(trackID, 10)+".vtt")
}

func (e *Extractor) enqueue(j ocrJob) {
	select {
	case e.jobs <- j:
	default:
		slog.Warn("OCR queue full; dropping subtitle job", "track", j.trackID)
	}
}

func (e *Extractor) requeuePending(ctx context.Context) {
	tracks, err := e.db.PendingOCRTracks(ctx)
	if err != nil {
		return
	}
	for _, t := range tracks {
		if t.Source == "external" { // external VobSub .idx/.sub
			idxPath := strings.TrimSuffix(t.ExtPath, filepath.Ext(t.ExtPath)) + ".idx"
			e.enqueue(ocrJob{trackID: t.ID, kind: ocrVobSubExtern, idxPath: idxPath, subPath: t.ExtPath, language: t.Language})
			continue
		}
		mf, err := e.db.GetMediaFile(ctx, t.FileID)
		if err != nil {
			continue
		}
		kind := ocrPGS
		if t.Format == "dvdsub" {
			kind = ocrVobSubEmbed
		}
		e.enqueue(ocrJob{trackID: t.ID, kind: kind, filePath: mf.Path, streamIndex: t.TrackIndex, language: t.Language})
	}
}

func lastLine(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		s = s[len(s)-200:]
	}
	return s
}
