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
	"sync"

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
}

type ocrJob struct {
	trackID     int64
	filePath    string
	streamIndex int
	language    string
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

	for i := 0; i < 2; i++ {
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
			e.enqueue(ocrJob{trackID: trackID, filePath: mf.Path, streamIndex: s.Index, language: s.Language})
		default:
			// Other image formats (dvdsub) are recorded but not OCR'd yet.
			track.OCRStatus = "skipped"
			if _, err := e.db.UpsertSubtitleTrack(ctx, track); err != nil {
				return err
			}
		}
	}
	return nil
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
		mf, err := e.db.GetMediaFile(ctx, t.FileID)
		if err != nil {
			continue
		}
		e.enqueue(ocrJob{trackID: t.ID, filePath: mf.Path, streamIndex: t.TrackIndex, language: t.Language})
	}
}

func lastLine(b []byte) string {
	s := string(b)
	if len(s) > 200 {
		s = s[len(s)-200:]
	}
	return s
}
