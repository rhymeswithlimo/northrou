// Package scanner walks configured media directories, parses scene-release
// filenames, probes files with ffprobe for authoritative technical metadata,
// matches them against TMDB, and persists the results. Unmatched files are
// flagged for manual correction.
package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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

	// force, when set for a run, re-processes every file even if its size/mtime
	// is unchanged, so stored metadata (e.g. new artwork fields) is refetched
	// from TMDB. Set only while a Rescan is in flight; a scan holds the single-
	// run lock, so no concurrent read/write races the parallel workers.
	force atomic.Bool

	dedupMu sync.Mutex          // guards dedup; reset each scan
	dedup   map[string]dupBest  // identity -> best copy seen so far
}

// dupBest is the best copy of one logical title seen during a scan, used to keep
// duplicate files (e.g. the same movie as .mkv and .mp4) from collapsing to a
// nondeterministic last-writer-wins in the DB.
type dupBest struct {
	height    int
	bitrate   int64
	container int // preference rank: mkv > mp4 > m4v > other
	path      string
}

// scanYieldInterval is how long a scan worker parks between checks while
// playback is active.
const scanYieldInterval = 500 * time.Millisecond

// SubtitleExtractor is implemented by the subtitle pipeline (P3). The scanner
// invokes it per media file after a successful probe. Kept as an interface to
// avoid a package cycle. ExtractSidecars discovers external subtitle files on
// disk next to the video (embedded streams come via ExtractForFile).
type SubtitleExtractor interface {
	ExtractForFile(ctx context.Context, fileID int64, mf *model.MediaFile) error
	ExtractSidecars(ctx context.Context, fileID int64, videoPath string) error
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

// Rescan is Scan with the "already scanned, unchanged" skip disabled: every file
// is re-probed and re-matched against TMDB, so metadata that a plain scan would
// leave untouched (new artwork fields, refreshed logos/backdrops, corrected
// credits) is refetched. Use it after an upgrade that adds metadata the existing
// library predates. Heavier than Scan - it does a full TMDB pass - but otherwise
// identical, including duplicate resolution and deleted-title cleanup.
func (s *Scanner) Rescan(ctx context.Context, movieDirs, showDirs []string) error {
	s.force.Store(true)
	defer s.force.Store(false)
	return s.Scan(ctx, movieDirs, showDirs)
}

// Scan walks the given movie and show directories and processes every media
// file. It is safe to call again; already-scanned unchanged files are skipped
// (unless the run is a Rescan, which reprocesses everything).
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
	s.dedupMu.Lock()
	s.dedup = make(map[string]dupBest, len(jobs))
	s.dedupMu.Unlock()

	// Drop titles whose source file was deleted since the last scan, then seed
	// duplicate resolution with the files still linked so a present best copy is
	// never displaced by a lesser duplicate that happens to re-process.
	if n := s.reconcileDeleted(ctx); n > 0 {
		slog.Info("removed titles with deleted files", "count", n)
	}
	s.seedDedup(ctx)
	slog.Info("scan started", "files", len(jobs))

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.workers)
	for _, j := range jobs {
		g.Go(func() error {
			s.processFile(gctx, j.path, j.kind)
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	// Media files no longer linked to any movie/episode (losers of a duplicate
	// contest, or titles whose source file was deleted) are pruned so they do
	// not linger with stale subtitle rows. Use the parent ctx: the errgroup's is
	// already canceled once Wait returns.
	if n, err := s.db.PruneOrphanMediaFiles(ctx); err != nil {
		slog.Warn("prune orphan media files failed", "err", err)
	} else if n > 0 {
		slog.Info("pruned orphan media files", "count", n)
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
	// A forced rescan reprocesses everything; otherwise skip files whose
	// size/mtime is unchanged since they were last scanned.
	if !s.force.Load() {
		need, _ := s.db.NeedsScan(ctx, path, info.Size(), info.ModTime())
		if !need {
			// The video is unchanged, but sidecar subtitles are not covered by
			// its size/mtime, so a subtitle dropped in after the first scan would
			// never be found if we returned here. Reconcile externals for the
			// already linked file (cheap: discovery is skipped for subs already
			// converted).
			s.reconcileSidecars(ctx, path)
			return
		}
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

	parsed := ParseFilename(path)
	// A file in a show dir that parsed as a movie is still an episode by intent.
	if kind == model.KindEpisode && !parsed.IsEpisode {
		parsed = enrichEpisodeFromPath(path, parsed)
	} else if kind == model.KindMovie && parsed.Year == 0 {
		// Recover a year the filename omitted from an ancestor folder, e.g.
		// "2001 - Sorcerers Stone/Harry.Potter.mkv".
		parsed.Year = yearFromAncestors(path)
	}

	// Duplicate handling: when the same title exists as several files (e.g. an
	// .mkv and .mp4, or copies in two folders), only the best copy is linked. A
	// loser is dropped before any DB write (no orphan rows). It is marked scanned
	// so it does not re-probe every scan; promotion after the winner is deleted
	// is handled by clearing that folder's scan state in reconcileDeleted.
	if !s.claimBest(dedupKey(kind, parsed, path), quality(mf, path)) {
		_ = s.db.MarkScanned(ctx, path, info.Size(), info.ModTime())
		return
	}

	fileID, err := s.db.UpsertMediaFile(ctx, mf)
	if err != nil {
		slog.Warn("store media file failed", "path", path, "err", err)
		return
	}
	mf.ID = fileID

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
		if err := s.subs.ExtractSidecars(ctx, fileID, path); err != nil {
			slog.Debug("sidecar subtitle extraction failed", "path", path, "err", err)
		}
	}

	_ = s.db.DeleteUnmatched(ctx, path)
	_ = s.db.MarkScanned(ctx, path, info.Size(), info.ModTime())
	s.inc(&s.progress.Matched)
}

// reconcileDeleted removes media files whose source no longer exists on disk and
// then deletes any movie/episode left without a file. Returns titles removed.
func (s *Scanner) reconcileDeleted(ctx context.Context) int64 {
	files, err := s.db.AllMediaFiles(ctx)
	if err != nil {
		return 0
	}
	for _, f := range files {
		if _, err := os.Stat(f.Path); err != nil && os.IsNotExist(err) {
			if derr := s.db.DeleteMediaFile(ctx, f.ID); derr != nil {
				slog.Debug("delete missing media file failed", "path", f.Path, "err", derr)
				continue
			}
			// Forget the folder's scan state so a duplicate copy beside the
			// deleted file re-processes next scan and can be promoted.
			prefix := filepath.Dir(f.Path) + string(filepath.Separator)
			if cerr := s.db.ClearScanStateForPrefix(ctx, prefix); cerr != nil {
				slog.Debug("clear scan state failed", "prefix", prefix, "err", cerr)
			}
		}
	}
	n, err := s.db.DeleteTitlesWithoutFile(ctx)
	if err != nil {
		slog.Debug("delete orphan titles failed", "err", err)
	}
	return n
}

// seedDedup primes the duplicate map with the files already linked to a title,
// re-deriving each one's identity and quality, so this scan's winner comparison
// starts from what is currently the best copy.
func (s *Scanner) seedDedup(ctx context.Context) {
	linked, err := s.db.LinkedFiles(ctx)
	if err != nil {
		return
	}
	s.dedupMu.Lock()
	defer s.dedupMu.Unlock()
	for _, lf := range linked {
		parsed := ParseFilename(lf.Path)
		if lf.Kind == model.KindEpisode && !parsed.IsEpisode {
			parsed = enrichEpisodeFromPath(lf.Path, parsed)
		}
		cand := dupBest{height: lf.Height, bitrate: lf.BitRate, container: containerRank(lf.Path), path: lf.Path}
		if cand.bitrate == 0 && lf.Duration > 0 {
			cand.bitrate = int64(float64(lf.SizeBytes) / lf.Duration)
		}
		key := dedupKey(lf.Kind, parsed, lf.Path)
		if cur, ok := s.dedup[key]; !ok || betterDup(cand, cur) {
			s.dedup[key] = cand
		}
	}
}

// reconcileSidecars discovers/extracts external subtitles for an already-scanned
// file. It is the rescan path for subtitles added after the video was first
// indexed; a file with no media_files row (never matched, or a duplicate loser)
// is simply skipped.
func (s *Scanner) reconcileSidecars(ctx context.Context, path string) {
	if s.subs == nil {
		return
	}
	id, err := s.db.MediaFileIDByPath(ctx, path)
	if err != nil {
		return
	}
	if err := s.subs.ExtractSidecars(ctx, id, path); err != nil {
		slog.Debug("sidecar reconcile failed", "path", path, "err", err)
	}
}

// dedupKey identifies the logical title a file represents, so duplicate copies
// collapse to one. Degenerate parses (no title) key by path so distinct
// unparseable files are never merged.
func dedupKey(kind model.MediaKind, p ParsedInfo, path string) string {
	if kind == model.KindEpisode || p.IsEpisode {
		if p.Title == "" || p.Season == 0 {
			return "path|" + path
		}
		return fmt.Sprintf("ep|%s|%d|%d", strings.ToLower(p.Title), p.Season, p.Episode)
	}
	if p.Title == "" {
		return "path|" + path
	}
	return fmt.Sprintf("mv|%s|%d", strings.ToLower(p.Title), p.Year)
}

// quality scores a candidate file: higher resolution, then bitrate, then a
// container preference (mkv > mp4 > m4v > other), with the path as a stable
// final tiebreak so the winner is deterministic regardless of worker ordering.
func quality(mf *model.MediaFile, path string) dupBest {
	br := mf.Video.BitRate
	if br == 0 && mf.Duration > 0 {
		br = int64(float64(mf.SizeBytes) / mf.Duration) // bytes/sec proxy
	}
	return dupBest{height: mf.Video.Height, bitrate: br, container: containerRank(path), path: path}
}

func containerRank(path string) int {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mkv":
		return 4
	case ".mp4":
		return 3
	case ".m4v":
		return 2
	default:
		return 1
	}
}

// claimBest records cand as the winner for key when it beats the current best,
// returning whether the caller should proceed to link this file.
func (s *Scanner) claimBest(key string, cand dupBest) bool {
	s.dedupMu.Lock()
	defer s.dedupMu.Unlock()
	cur, ok := s.dedup[key]
	// A file must always be able to update itself: when the current best is the
	// same path (e.g. it was seeded from the DB and has now been re-encoded or
	// remuxed), re-claim so the new streams/metadata are re-ingested even if the
	// quality is equal or lower. Paths are unique within a scan, so this only
	// ever matches the same file re-processing.
	if !ok || cand.path == cur.path || betterDup(cand, cur) {
		s.dedup[key] = cand
		return true
	}
	return false
}

// ForceMatch links a file to a specific TMDB title chosen by the operator,
// bypassing filename parsing and duplicate selection. It probes the file if
// needed, stores it, forces the match, extracts subtitles, and clears any
// unmatched flag. This is the escape hatch for libraries that veer off the
// recommended naming so the long tail is never a dead end.
func (s *Scanner) ForceMatch(ctx context.Context, path string, kind model.MediaKind, tmdbID int64, season, episode int) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	mf := &model.MediaFile{Path: path, SizeBytes: info.Size(), ModTime: info.ModTime()}
	if s.prober != nil {
		if probed, err := s.prober.Probe(ctx, path); err == nil {
			probed.SizeBytes = info.Size()
			probed.ModTime = info.ModTime()
			mf = probed
		}
	}
	fileID, err := s.db.UpsertMediaFile(ctx, mf)
	if err != nil {
		return fmt.Errorf("store media file: %w", err)
	}
	mf.ID = fileID

	switch kind {
	case model.KindEpisode:
		err = s.MatchEpisodeByID(ctx, mf, tmdbID, season, episode)
	default:
		err = s.MatchMovieByID(ctx, mf, tmdbID)
	}
	if err != nil {
		return err
	}

	if s.subs != nil {
		_ = s.subs.ExtractForFile(ctx, fileID, mf)
		_ = s.subs.ExtractSidecars(ctx, fileID, path)
	}
	_ = s.db.DeleteUnmatched(ctx, path)
	_ = s.db.MarkScanned(ctx, path, info.Size(), info.ModTime())
	return nil
}

// betterDup reports whether a is a strictly better copy than cur.
func betterDup(a, cur dupBest) bool {
	if a.height != cur.height {
		return a.height > cur.height
	}
	if a.bitrate != cur.bitrate {
		return a.bitrate > cur.bitrate
	}
	if a.container != cur.container {
		return a.container > cur.container
	}
	return a.path < cur.path
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

// enrichEpisodeFromPath recovers season/episode/show-title from the directory
// structure when the filename lacked an SxxEyy/1x05 marker. It walks up from the
// file: the nearest ancestor that looks like a season folder ("Season 01",
// "S01", "Series 1", "Specials") sets the season, and the first non-generic
// folder above it (skipping containers like "MKV"/"Subs") names the show. A
// loose episode number ("E07", "Episode 7") in the filename sets the episode.
func enrichEpisodeFromPath(path string, p ParsedInfo) ParsedInfo {
	p.IsEpisode = true

	// Ancestor folder names, nearest first.
	var ancestors []string
	for dir := filepath.Dir(path); ; {
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		ancestors = append(ancestors, filepath.Base(dir))
		dir = parent
	}

	seasonIdx := -1
	for i, name := range ancestors {
		if n, ok := seasonFromFolder(name); ok {
			p.Season = n
			seasonIdx = i
			break
		}
	}

	// Show title: first non-generic folder above the season folder (or the
	// nearest non-generic ancestor when there is no season folder).
	start := seasonIdx + 1
	if seasonIdx < 0 {
		start = 0
	}
	for i := start; i < len(ancestors); i++ {
		if !isGenericFolder(ancestors[i]) {
			p.Title = cleanTitle(normalize(ancestors[i]))
			break
		}
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if n, ok := episodeFromName(base); ok {
		p.Episode = n
		p.Episodes = []int{n}
	}
	return p
}

// yearFromAncestors returns the first plausible year found in a folder above the
// file, nearest first, or 0. Used when a movie's filename omits the year.
func yearFromAncestors(path string) int {
	for dir := filepath.Dir(path); ; {
		parent := filepath.Dir(dir)
		if parent == dir {
			return 0
		}
		if y := yearFromFolder(filepath.Base(dir)); y != 0 {
			return y
		}
		dir = parent
	}
}
