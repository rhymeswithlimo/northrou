package scanner

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/metadata"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

const (
	posterSize   = "w500"
	backdropSize = "w1280"
	// Headshots render as ~150px circles and stills as ~160px thumbs, so the
	// small sizes are plenty and keep the image cache from ballooning across a
	// large cast list.
	profileSize = "w185"
	stillSize   = "w300"
	maxCast     = 20
)

// crewJobs are the crew roles we persist (drives director/creator affinity).
var crewJobs = map[string]bool{
	"Director": true, "Writer": true, "Creator": true,
	"Screenplay": true, "Producer": true,
}

// matchMovie resolves a parsed movie against TMDB and stores it.
func (s *Scanner) matchMovie(ctx context.Context, p ParsedInfo, mf *model.MediaFile) error {
	if !s.tmdb.Enabled() {
		return errors.New("no TMDB API key configured")
	}
	if strings.TrimSpace(p.Title) == "" {
		return errors.New("could not parse a title from filename")
	}
	results, err := s.tmdb.SearchMovie(ctx, p.Title, p.Year)
	if err != nil {
		return fmt.Errorf("tmdb search: %w", err)
	}
	best := pickBestMovie(results, p)
	if best == nil {
		return fmt.Errorf("no TMDB match for %q (%d)", p.Title, p.Year)
	}
	return s.storeMovieByID(ctx, best.ID, mf)
}

// SearchTMDBResult is a candidate title for the manual-match UI.
type SearchTMDBResult struct {
	TMDBID int64  `json:"tmdb_id"`
	Title  string `json:"title"`
	Year   int    `json:"year"`
}

// SearchTMDB searches TMDB by free-text query for the manual-match flow. kind
// selects movie vs TV search.
func (s *Scanner) SearchTMDB(ctx context.Context, query string, kind model.MediaKind) ([]SearchTMDBResult, error) {
	if !s.tmdb.Enabled() {
		return nil, errors.New("no TMDB API key configured")
	}
	var items []metadata.SearchItem
	var err error
	if kind == model.KindEpisode {
		items, err = s.tmdb.SearchTV(ctx, query, 0)
	} else {
		items, err = s.tmdb.SearchMovie(ctx, query, 0)
	}
	if err != nil {
		return nil, err
	}
	out := make([]SearchTMDBResult, 0, len(items))
	for _, it := range items {
		title, date := it.Title, it.ReleaseDate
		if kind == model.KindEpisode {
			title, date = it.Name, it.FirstAirDate
		}
		out = append(out, SearchTMDBResult{TMDBID: it.ID, Title: title, Year: metadata.ReleaseYear(date)})
	}
	return out, nil
}

// MatchMovieByID forces a file to a specific TMDB movie, bypassing the
// filename-based search. Used by the manual-correction ("fix match") flow.
func (s *Scanner) MatchMovieByID(ctx context.Context, mf *model.MediaFile, tmdbID int64) error {
	if !s.tmdb.Enabled() {
		return errors.New("no TMDB API key configured")
	}
	return s.storeMovieByID(ctx, tmdbID, mf)
}

// storeMovieByID fetches full TMDB details for a movie id and persists it linked
// to mf.
func (s *Scanner) storeMovieByID(ctx context.Context, tmdbID int64, mf *model.MediaFile) error {
	details, err := s.tmdb.MovieDetails(ctx, tmdbID)
	if err != nil {
		return fmt.Errorf("tmdb details: %w", err)
	}

	movie := &model.Movie{
		TMDBID:        details.ID,
		Title:         details.Title,
		Year:          metadata.ReleaseYear(details.ReleaseDate),
		Overview:      details.Overview,
		Runtime:       details.Runtime,
		OriginalLang:  details.OriginalLanguage,
		Genres:        genreNames(details.Genres),
		Keywords:      details.KeywordNames(),
		PosterPath:    s.cacheImage(ctx, details.PosterPath, posterSize),
		BackdropPath:  s.cacheImage(ctx, details.BackdropPath, backdropSize),
		Tagline:       details.Tagline,
		Certification: details.ReleaseDates.Certification(),
		Cast:          s.topCast(ctx, details.Credits.Cast),
		Crew:          s.keyCrew(ctx, details.Credits.Crew),
		Rating:        details.VoteAverage,
		Votes:         details.VoteCount,
		Popularity:    details.Popularity,
		Revenue:       details.Revenue,
		Country:       details.PrimaryCountry(),
		File:          mf,
	}
	if details.Collection != nil {
		movie.CollectionID = details.Collection.ID
		_ = s.db.UpsertCollection(ctx, details.Collection.ID, details.Collection.Name,
			s.cacheImage(ctx, details.Collection.PosterPath, posterSize),
			s.cacheImage(ctx, details.Collection.BackdropPath, backdropSize))
	}
	if _, err := s.db.UpsertMovie(ctx, movie); err != nil {
		return fmt.Errorf("store movie: %w", err)
	}
	return nil
}

// matchEpisode resolves the show, season, and episode, then stores the episode.
func (s *Scanner) matchEpisode(ctx context.Context, p ParsedInfo, mf *model.MediaFile) error {
	if !s.tmdb.Enabled() {
		return errors.New("no TMDB API key configured")
	}
	if strings.TrimSpace(p.Title) == "" || p.Season == 0 {
		return errors.New("could not parse show title/season from filename")
	}

	showID, err := s.resolveShow(ctx, p.Title, p.Year)
	if err != nil {
		return err
	}
	seasonID, err := s.db.UpsertSeason(ctx, showID, p.Season)
	if err != nil {
		return fmt.Errorf("store season: %w", err)
	}

	ep := &model.Episode{
		ShowID:   showID,
		SeasonID: seasonID,
		Season:   p.Season,
		Number:   p.Episode,
		File:     mf,
	}
	// Episode-level metadata is best-effort.
	if tmdbShowID, e := s.db.GetShow(ctx, showID); e == nil {
		if det, e := s.tmdb.EpisodeDetails(ctx, tmdbShowID.TMDBID, p.Season, p.Episode); e == nil {
			ep.Title = det.Name
			ep.Overview = det.Overview
			ep.Runtime = det.Runtime
			ep.AirDate = det.AirDate
			ep.StillPath = s.cacheImage(ctx, det.StillPath, stillSize)
		}
	}
	if _, err := s.db.UpsertEpisode(ctx, ep); err != nil {
		return fmt.Errorf("store episode: %w", err)
	}
	return nil
}

// MatchEpisodeByID forces a file to a specific TMDB show + season/episode,
// bypassing the filename-based search. Used by the manual-correction flow.
func (s *Scanner) MatchEpisodeByID(ctx context.Context, mf *model.MediaFile, showTMDBID int64, season, episode int) error {
	if !s.tmdb.Enabled() {
		return errors.New("no TMDB API key configured")
	}
	if season == 0 || episode == 0 {
		return errors.New("season and episode are required")
	}
	s.showLock.Lock()
	showID, err := s.resolveShowByTMDB(ctx, showTMDBID)
	s.showLock.Unlock()
	if err != nil {
		return err
	}
	seasonID, err := s.db.UpsertSeason(ctx, showID, season)
	if err != nil {
		return fmt.Errorf("store season: %w", err)
	}
	ep := &model.Episode{ShowID: showID, SeasonID: seasonID, Season: season, Number: episode, File: mf}
	if show, e := s.db.GetShow(ctx, showID); e == nil {
		if det, e := s.tmdb.EpisodeDetails(ctx, show.TMDBID, season, episode); e == nil {
			ep.Title = det.Name
			ep.Overview = det.Overview
			ep.Runtime = det.Runtime
			ep.AirDate = det.AirDate
			ep.StillPath = s.cacheImage(ctx, det.StillPath, stillSize)
		}
	}
	if _, err := s.db.UpsertEpisode(ctx, ep); err != nil {
		return fmt.Errorf("store episode: %w", err)
	}
	return nil
}

// resolveShow finds-or-creates the show for a title/year, serialized so
// concurrent episodes of the same show don't each hit TMDB.
func (s *Scanner) resolveShow(ctx context.Context, title string, year int) (int64, error) {
	s.showLock.Lock()
	defer s.showLock.Unlock()

	results, err := s.tmdb.SearchTV(ctx, title, year)
	if err != nil {
		return 0, fmt.Errorf("tmdb tv search: %w", err)
	}
	best := pickBestTV(results, title, year)
	if best == nil {
		return 0, fmt.Errorf("no TMDB show match for %q", title)
	}
	return s.resolveShowByTMDB(ctx, best.ID)
}

// resolveShowByTMDB finds-or-creates a show by its TMDB id. The caller must hold
// showLock (both resolveShow and MatchEpisodeByID do).
func (s *Scanner) resolveShowByTMDB(ctx context.Context, tmdbID int64) (int64, error) {
	if id, err := s.db.FindShowByTMDB(ctx, tmdbID); err == nil {
		return id, nil
	}
	details, err := s.tmdb.TVDetails(ctx, tmdbID)
	if err != nil {
		return 0, fmt.Errorf("tmdb tv details: %w", err)
	}
	show := &model.Show{
		TMDBID:        details.ID,
		Title:         details.Name,
		Year:          metadata.ReleaseYear(details.FirstAirDate),
		Overview:      details.Overview,
		OriginalLang:  details.OriginalLanguage,
		Genres:        genreNames(details.Genres),
		Keywords:      details.KeywordNames(),
		PosterPath:    s.cacheImage(ctx, details.PosterPath, posterSize),
		BackdropPath:  s.cacheImage(ctx, details.BackdropPath, backdropSize),
		Tagline:       details.Tagline,
		Certification: details.ContentRatings.Certification(),
		Cast:          s.topCast(ctx, details.Credits.Cast),
		Crew:          s.keyCrew(ctx, details.Credits.Crew),
		Rating:        details.VoteAverage,
		Popularity:    details.Popularity,
		Country:       details.PrimaryCountry(),
	}
	return s.db.UpsertShow(ctx, show)
}

// cacheImage downloads a TMDB image and returns the local path (empty on any
// failure, since images are non-essential).
func (s *Scanner) cacheImage(ctx context.Context, tmdbPath, size string) string {
	if tmdbPath == "" {
		return ""
	}
	local, err := s.images.Fetch(ctx, tmdbPath, size)
	if err != nil {
		return ""
	}
	return local
}

// --- selection heuristics ---

func pickBestMovie(results []metadata.SearchItem, p ParsedInfo) *metadata.SearchItem {
	var fallback *metadata.SearchItem
	for i := range results {
		r := &results[i]
		if fallback == nil {
			fallback = r
		}
		if strings.EqualFold(r.Title, p.Title) {
			if p.Year == 0 || metadata.ReleaseYear(r.ReleaseDate) == p.Year {
				return r
			}
		}
	}
	return fallback // TMDB sorts by popularity; first is a sane default
}

func pickBestTV(results []metadata.SearchItem, title string, year int) *metadata.SearchItem {
	var fallback *metadata.SearchItem
	for i := range results {
		r := &results[i]
		if fallback == nil {
			fallback = r
		}
		if strings.EqualFold(r.Name, title) {
			if year == 0 || metadata.ReleaseYear(r.FirstAirDate) == year {
				return r
			}
		}
	}
	return fallback
}

func genreNames(gs []metadata.Genre) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		out = append(out, g.Name)
	}
	return out
}

// topCast keeps the first maxCast billed actors and caches their headshots.
func (s *Scanner) topCast(ctx context.Context, cast []metadata.CastMember) []model.Credit {
	if len(cast) > maxCast {
		cast = cast[:maxCast]
	}
	out := make([]model.Credit, 0, len(cast))
	for _, c := range cast {
		out = append(out, model.Credit{
			PersonID:    c.ID,
			Name:        c.Name,
			Role:        c.Character,
			Order:       c.Order,
			ProfilePath: s.cacheImage(ctx, c.ProfilePath, profileSize),
		})
	}
	return out
}

// keyCrew keeps only the jobs worth showing (see crewJobs) and caches their
// headshots.
func (s *Scanner) keyCrew(ctx context.Context, crew []metadata.CrewMember) []model.Credit {
	var out []model.Credit
	for _, c := range crew {
		if crewJobs[c.Job] {
			out = append(out, model.Credit{
				PersonID:    c.ID,
				Name:        c.Name,
				Role:        c.Job,
				ProfilePath: s.cacheImage(ctx, c.ProfilePath, profileSize),
			})
		}
	}
	return out
}
