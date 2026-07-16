package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
)

// --- DTOs (decoupled from DB rows so the frontend contract is stable) ---

type creditDTO struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Role       string `json:"role,omitempty"`
	ProfileURL string `json:"profile_url,omitempty"`
}

type movieDTO struct {
	ID            int64         `json:"id"`
	TMDBID        int64         `json:"tmdb_id"`
	Title         string        `json:"title"`
	Year          int           `json:"year"`
	Overview      string        `json:"overview,omitempty"`
	Tagline       string        `json:"tagline,omitempty"`
	Certification string        `json:"certification,omitempty"`
	Runtime       int           `json:"runtime,omitempty"`
	Rating        float64       `json:"rating,omitempty"`
	Genres        []string      `json:"genres,omitempty"`
	CollectionID  int64         `json:"collection_id,omitempty"`
	PosterURL     string        `json:"poster_url,omitempty"`
	BackdropURL   string        `json:"backdrop_url,omitempty"`
	StreamURL     string        `json:"stream_url,omitempty"`
	Cast          []creditDTO   `json:"cast,omitempty"`
	Crew          []creditDTO   `json:"crew,omitempty"`
	MediaInfo     *mediaInfoDTO `json:"media_info,omitempty"`
}

type showDTO struct {
	ID            int64       `json:"id"`
	TMDBID        int64       `json:"tmdb_id"`
	Title         string      `json:"title"`
	Year          int         `json:"year"`
	Overview      string      `json:"overview,omitempty"`
	Tagline       string      `json:"tagline,omitempty"`
	Certification string      `json:"certification,omitempty"`
	Rating        float64     `json:"rating,omitempty"`
	Genres        []string    `json:"genres,omitempty"`
	PosterURL     string      `json:"poster_url,omitempty"`
	BackdropURL   string      `json:"backdrop_url,omitempty"`
	Cast          []creditDTO `json:"cast,omitempty"`
	Crew          []creditDTO `json:"crew,omitempty"`
	Seasons       []seasonDTO `json:"seasons,omitempty"`
}

type seasonDTO struct {
	Number   int          `json:"number"`
	Episodes []episodeDTO `json:"episodes"`
}

type episodeDTO struct {
	ID        int64  `json:"id"`
	Season    int    `json:"season"`
	Number    int    `json:"number"`
	Title     string `json:"title,omitempty"`
	Overview  string `json:"overview,omitempty"`
	Runtime   int    `json:"runtime,omitempty"`
	AirDate   string `json:"air_date,omitempty"`
	StillURL  string `json:"still_url,omitempty"`
	StreamURL string `json:"stream_url,omitempty"`
}

type mediaInfoDTO struct {
	Container  string  `json:"container"`
	Duration   float64 `json:"duration"`
	VideoCodec string  `json:"video_codec"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	HDR        string  `json:"hdr,omitempty"`
	AudioCodec string  `json:"audio_codec,omitempty"`
	Atmos      bool    `json:"atmos,omitempty"`
}

func (a *API) imageURL(rel string) string {
	if rel == "" {
		return ""
	}
	return "/api/images/" + rel
}

func (a *API) handleListMovies(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePaging(r)
	movies, err := a.DB.ListMovies(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list movies failed")
		return
	}
	out := make([]movieDTO, 0, len(movies))
	for i := range movies {
		out = append(out, a.movieToDTO(&movies[i], false))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleGetMovie(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := a.DB.GetMovie(r.Context(), id)
	if err != nil {
		notFoundOr500(w, err, "get movie failed")
		return
	}
	writeJSON(w, http.StatusOK, a.movieToDTO(m, true))
}

// creditsToDTO maps credits, rewriting cached headshot paths into API image
// URLs. The client never talks to TMDB.
func (a *API) creditsToDTO(cs []model.Credit) []creditDTO {
	if len(cs) == 0 {
		return nil
	}
	out := make([]creditDTO, 0, len(cs))
	for _, c := range cs {
		out = append(out, creditDTO{
			ID:         c.PersonID,
			Name:       c.Name,
			Role:       c.Role,
			ProfileURL: a.imageURL(c.ProfilePath),
		})
	}
	return out
}

func (a *API) movieToDTO(m *model.Movie, detail bool) movieDTO {
	dto := movieDTO{
		ID: m.ID, TMDBID: m.TMDBID, Title: m.Title, Year: m.Year,
		Rating:      m.Rating,
		Genres:      m.Genres,
		PosterURL:   a.imageURL(m.PosterPath),
		BackdropURL: a.imageURL(m.BackdropPath),
	}
	if detail {
		dto.Overview = m.Overview
		dto.Runtime = m.Runtime
		dto.Tagline = m.Tagline
		dto.Certification = m.Certification
		dto.CollectionID = m.CollectionID
		dto.Cast = a.creditsToDTO(m.Cast)
		dto.Crew = a.creditsToDTO(m.Crew)
	}
	if m.File != nil && m.File.ID != 0 {
		dto.StreamURL = "/api/media/" + strconv.FormatInt(m.File.ID, 10) + "/stream"
		if detail && m.File.Container != "" {
			dto.MediaInfo = mediaInfoToDTO(m.File)
		}
	}
	return dto
}

func mediaInfoToDTO(mf *model.MediaFile) *mediaInfoDTO {
	dto := &mediaInfoDTO{
		Container: mf.Container, Duration: mf.Duration,
		VideoCodec: mf.Video.Codec, Width: mf.Video.Width, Height: mf.Video.Height,
		HDR: string(mf.Video.HDR),
	}
	if len(mf.Audio) > 0 {
		dto.AudioCodec = mf.Audio[0].Codec
		dto.Atmos = mf.Audio[0].Atmos
	}
	return dto
}

func (a *API) handleListShows(w http.ResponseWriter, r *http.Request) {
	limit, offset := parsePaging(r)
	shows, err := a.DB.ListShows(r.Context(), limit, offset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list shows failed")
		return
	}
	out := make([]showDTO, 0, len(shows))
	for i := range shows {
		out = append(out, a.showToDTO(&shows[i], false))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleGetShow(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s, err := a.DB.GetShow(r.Context(), id)
	if err != nil {
		notFoundOr500(w, err, "get show failed")
		return
	}
	writeJSON(w, http.StatusOK, a.showToDTO(s, true))
}

func (a *API) showToDTO(s *model.Show, detail bool) showDTO {
	dto := showDTO{
		ID: s.ID, TMDBID: s.TMDBID, Title: s.Title, Year: s.Year,
		Rating:      s.Rating,
		Genres:      s.Genres,
		PosterURL:   a.imageURL(s.PosterPath),
		BackdropURL: a.imageURL(s.BackdropPath),
	}
	if detail {
		dto.Overview = s.Overview
		dto.Tagline = s.Tagline
		dto.Certification = s.Certification
		dto.Cast = a.creditsToDTO(s.Cast)
		dto.Crew = a.creditsToDTO(s.Crew)
		for _, sea := range s.Seasons {
			sd := seasonDTO{Number: sea.Number}
			for _, e := range sea.Episodes {
				ed := episodeDTO{
					ID: e.ID, Season: e.Season, Number: e.Number,
					Title: e.Title, Overview: e.Overview, Runtime: e.Runtime,
					AirDate:  e.AirDate,
					StillURL: a.imageURL(e.StillPath),
				}
				if e.File != nil && e.File.ID != 0 {
					ed.StreamURL = "/api/media/" + strconv.FormatInt(e.File.ID, 10) + "/stream"
				}
				sd.Episodes = append(sd.Episodes, ed)
			}
			dto.Seasons = append(dto.Seasons, sd)
		}
	}
	return dto
}

func (a *API) handleListUnmatched(w http.ResponseWriter, r *http.Request) {
	list, err := a.DB.ListUnmatched(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list unmatched failed")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// --- Scan control (admin) ---

func (a *API) handleStartScan(w http.ResponseWriter, r *http.Request) {
	if a.Scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "scanner unavailable")
		return
	}
	// Read from disk, not a.Cfg: the TUI writes media folders into config.toml
	// while the daemon runs, so the in-memory copy is stale as soon as the
	// operator adds one, and a scan would quietly walk the old folders.
	movieDirs, showDirs := a.mediaDirs()
	if len(movieDirs)+len(showDirs) == 0 {
		// Nothing to walk. Without this the scan "succeeds" instantly and the
		// library stays empty, which reads as a broken scanner.
		writeError(w, http.StatusBadRequest,
			"no media folders configured; add them on the server with `northrou admin`")
		return
	}
	go func() {
		_ = a.Scanner.Scan(context.Background(), movieDirs, showDirs)
		// The catalog may have changed; drop cached home screens for everyone.
		if a.Recommend != nil {
			a.Recommend.InvalidateAll()
		}
	}()
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "scan started"})
}

func (a *API) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	if a.Scanner == nil {
		writeError(w, http.StatusServiceUnavailable, "scanner unavailable")
		return
	}
	writeJSON(w, http.StatusOK, a.Scanner.Progress())
}

// imageHandler serves cached metadata images from the images directory. Images
// are content-addressed (TMDB file path + size), so their bytes never change;
// a long immutable Cache-Control lets clients skip re-fetching posters entirely.
// The header is applied only to successful (200) responses so a missing image
// (e.g. a download that failed on a flaky scan) is not cached as permanently
// absent.
func (a *API) imageHandler() http.Handler {
	fs := http.FileServer(http.Dir(a.ImagesDir))
	stripped := http.StripPrefix("/api/images/", fs)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stripped.ServeHTTP(&cacheOnOK{ResponseWriter: w}, r)
	})
}

// cacheOnOK adds an immutable Cache-Control header only when the response is a
// full 200 (implicit or explicit), never on 404/304/etc.
type cacheOnOK struct {
	http.ResponseWriter
	wrote bool
}

const immutableImageCacheControl = "public, max-age=2592000, immutable" // 30 days

func (c *cacheOnOK) WriteHeader(status int) {
	if !c.wrote {
		c.wrote = true
		if status == http.StatusOK {
			c.Header().Set("Cache-Control", immutableImageCacheControl)
		}
	}
	c.ResponseWriter.WriteHeader(status)
}

func (c *cacheOnOK) Write(b []byte) (int, error) {
	if !c.wrote {
		c.wrote = true
		c.Header().Set("Cache-Control", immutableImageCacheControl) // implicit 200
	}
	return c.ResponseWriter.Write(b)
}

// --- helpers ---

// parsePaging reads optional ?limit and ?offset query params. A missing or
// non-positive limit means "no limit" (the full list), so existing clients that
// send neither keep getting the whole library.
func parsePaging(r *http.Request) (limit, offset int) {
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	return limit, offset
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func notFoundOr500(w http.ResponseWriter, err error, msg string) {
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	writeError(w, http.StatusInternalServerError, msg)
}
