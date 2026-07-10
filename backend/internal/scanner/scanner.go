// Package scanner walks configured media directories, parses scene-release
// filenames, probes files with ffprobe for authoritative technical metadata,
// matches them against TMDB, and persists the results. Unmatched files are
// flagged for manual correction.
package scanner

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/mediainfo"
	"github.com/rhymeswithlimo/northrou/backend/internal/metadata"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
	"golang.org/x/sync/errgroup"
)

// mediaExts are the file extensions treated as playable media.
var mediaExts = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".avi": true, ".mov": true,
	".ts": true, ".m2ts": true, ".webm": true, ".wmv": true, ".mpg": true,
	".mpeg": true, ".flv": true,
}

// Progress is a snapshot of an in-flight or last-completed scan.
type Progress struct {
	Running     bool      `json:"running"`
	Total       int       `json:"total"`
	Processed   int       `json:"processed"`
	Matched     int       `json:"matched"`
	Unmatched   int       `json:"unmatched"`
	CurrentFile string    `json:"current_file"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  time.Time `json:"finished_at"`
}

// Scanner orchestrates library scanning.
type Scanner struct {
	db     *db.DB
	tmdb   *metadata.Client
	images *metadata.ImageCache
	prober *mediainfo.Prober // may be nil if ffprobe unavailable
	subs   SubtitleExtractor // may be nil until P3 wires it in

	workers int

	mu       sync.Mutex
	progress Progress
	showLock sync.Mutex   // serializes show resolution to dedupe TMDB searches
	playback func() int   // reports active stream count; nil until wired
}

// scanYieldInterval is how long a scan worker parks between checks while
// playback is active.
const scanYieldInterval = 500 * time.Millisecond

// SubtitleExtractor is implemented by the subtitle pipeline (P3). The scanner
// invokes it per media file after a successful probe. Kept as an interface to
// avoid a package cycle.
type SubtitleExtractor interface {
	ExtractForFile(ctx context.Context, fileID int64, mf *model.MediaFile) error
}

// New builds a Scanner. prober may be nil (files are recorded without technical
// metadata); tmdb may be disabled (all files become unmatched).
func New(database *db.DB, tmdb *metadata.Client, images *metadata.ImageCache, prober *mediainfo.Prober) *Scanner {
	return &Scanner{
		db:      database,
		tmdb:    tmdb,
		images:  images,
		prober:  prober,
		workers: 4,
	}
}

// SetSubtitleExtractor wires the subtitle pipeline in (called during assembly).
func (s *Scanner) SetSubtitleExtractor(ex SubtitleExtractor) { s.subs = ex }

// SetProber attaches the ffprobe-backed prober once ffmpeg becomes available
// (the managed binary may still be downloading at startup).
func (s *Scanner) SetProber(p *mediainfo.Prober) {
	s.mu.Lock()
	s.prober = p
	s.mu.Unlock()
}

// SetPlaybackGate wires a function reporting the number of active streams. While
// it returns > 0, scan workers park (see waitWhilePlaying) so a background scan
// (ffprobe + subtitle work) does not starve a live stream on a weak CPU/disk.
func (s *Scanner) SetPlaybackGate(activeStreams func() int) {
	s.mu.Lock()
	s.playback = activeStreams
	s.mu.Unlock()
}

// waitWhilePlaying blocks until no streams are active or ctx is cancelled.
// Playback is the priority; a paused scan resumes automatically once viewers
// stop.
func (s *Scanner) waitWhilePlaying(ctx context.Context) {
	s.mu.Lock()
	gate := s.playback
	s.mu.Unlock()
	if gate == nil {
		return
	}
	for gate() > 0 {
		select {
		case <-ctx.Done():
			return
		case <-time.After(scanYieldInterval):
		}
	}
}

// Progress returns a snapshot of scan status.
func (s *Scanner) Progress() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress
}

// Scan walks the given movie and show directories and processes every media
// file. It is safe to call again; already-scanned unchanged files are skipped.
func (s *Scanner) Scan(ctx context.Context, movieDirs, showDirs []string) error {
	s.mu.Lock()
	if s.progress.Running {
		s.mu.Unlock()
		return nil // a scan is already in progress
	}
	s.progress = Progress{Running: true, StartedAt: time.Now()}
	s.mu.Unlock()

	// Scope the TMDB response cache to this run (bounded memory, fresh data).
	if s.tmdb != nil {
		s.tmdb.ResetCache()
	}

	defer func() {
		s.mu.Lock()
		s.progress.Running = false
		s.progress.FinishedAt = time.Now()
		s.progress.CurrentFile = ""
		s.mu.Unlock()
	}()

	type job struct {
		path string
		kind model.MediaKind
	}
	var jobs []job
	for _, dir := range movieDirs {
		for _, p := range collectMedia(dir) {
			jobs = append(jobs, job{p, model.KindMovie})
		}
	}
	for _, dir := range showDirs {
		for _, p := range collectMedia(dir) {
			jobs = append(jobs, job{p, model.KindEpisode})
		}
	}

	s.mu.Lock()
	s.progress.Total = len(jobs)
	s.mu.Unlock()
	slog.Info("scan started", "files", len(jobs))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(s.workers)
	for _, j := range jobs {
		j := j
		g.Go(func() error {
			s.processFile(ctx, j.path, j.kind)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	p := s.Progress()
	slog.Info("scan complete", "processed", p.Processed, "matched", p.Matched, "unmatched", p.Unmatched)
	return nil
}

// processFile handles a single media file end to end.
func (s *Scanner) processFile(ctx context.Context, path string, kind model.MediaKind) {
	// Yield to any active playback before doing CPU/disk-heavy probe+match work.
	s.waitWhilePlaying(ctx)
	defer s.bump(&s.progress.Processed, path)

	info, err := os.Stat(path)
	if err != nil {
		return
	}
	need, _ := s.db.NeedsScan(ctx, path, info.Size(), info.ModTime())
	if !need {
		return
	}

	mf := &model.MediaFile{Path: path, SizeBytes: info.Size(), ModTime: info.ModTime()}
	if s.prober != nil {
		if probed, err := s.prober.Probe(ctx, path); err == nil {
			probed.SizeBytes = info.Size()
			probed.ModTime = info.ModTime()
			mf = probed
		} else {
			slog.Debug("ffprobe failed", "path", path, "err", err)
		}
	}

	fileID, err := s.db.UpsertMediaFile(ctx, mf)
	if err != nil {
		slog.Warn("store media file failed", "path", path, "err", err)
		return
	}
	mf.ID = fileID

	parsed := ParseFilename(path)
	// A file in a show dir that parsed as a movie is still an episode by intent.
	if kind == model.KindEpisode && !parsed.IsEpisode {
		parsed = enrichEpisodeFromPath(path, parsed)
	}

	var matchErr error
	if kind == model.KindEpisode || parsed.IsEpisode {
		matchErr = s.matchEpisode(ctx, parsed, mf)
	} else {
		matchErr = s.matchMovie(ctx, parsed, mf)
	}

	if matchErr != nil {
		s.flagUnmatched(ctx, path, kind, parsed, matchErr)
		return
	}

	if s.subs != nil {
		if err := s.subs.ExtractForFile(ctx, fileID, mf); err != nil {
			slog.Debug("subtitle extraction failed", "path", path, "err", err)
		}
	}

	_ = s.db.DeleteUnmatched(ctx, path)
	_ = s.db.MarkScanned(ctx, path, info.Size(), info.ModTime())
	s.inc(&s.progress.Matched)
}

func (s *Scanner) flagUnmatched(ctx context.Context, path string, kind model.MediaKind, p ParsedInfo, cause error) {
	slog.Debug("unmatched file", "path", path, "reason", cause)
	_ = s.db.InsertUnmatched(ctx, &model.UnmatchedFile{
		Path:        path,
		Kind:        kind,
		Reason:      cause.Error(),
		ParsedTitle: p.Title,
		ParsedYear:  p.Year,
	})
	s.inc(&s.progress.Unmatched)
}

// --- progress helpers ---

func (s *Scanner) bump(counter *int, current string) {
	s.mu.Lock()
	*counter++
	s.progress.CurrentFile = current
	s.mu.Unlock()
}

func (s *Scanner) inc(counter *int) {
	s.mu.Lock()
	*counter++
	s.mu.Unlock()
}

// collectMedia returns all media file paths under dir (recursive).
func collectMedia(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if mediaExts[strings.ToLower(filepath.Ext(path))] {
			out = append(out, path)
		}
		return nil
	})
	return out
}

// enrichEpisodeFromPath fills season/episode from a "Season N" folder and a
// trailing number when the filename lacked an SxxEyy marker.
func enrichEpisodeFromPath(path string, p ParsedInfo) ParsedInfo {
	p.IsEpisode = true
	parent := filepath.Base(filepath.Dir(path))
	low := strings.ToLower(parent)
	if strings.HasPrefix(low, "season") {
		fields := strings.Fields(low)
		if len(fields) == 2 {
			if n := atoiSafe(fields[1]); n > 0 {
				p.Season = n
			}
		}
	}
	return p
}
