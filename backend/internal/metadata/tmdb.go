// Package metadata fetches movie/TV metadata from The Movie Database (TMDB)
// and caches poster/backdrop images to local disk. All network access is
// rate-limited and gracefully degrades when no API key is configured.
package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// ErrNoAPIKey indicates TMDB features are disabled because no key is set.
var ErrNoAPIKey = errors.New("no TMDB API key configured")

const defaultBaseURL = "https://api.themoviedb.org/3"

// Client is a thin TMDB API client.
type Client struct {
	apiKey   string
	language string
	baseURL  string
	http     *http.Client
	limiter  *limiter
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the API base URL (used in tests).
func WithBaseURL(u string) Option { return func(c *Client) { c.baseURL = u } }

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

// NewClient builds a TMDB client. An empty apiKey yields a client whose calls
// return ErrNoAPIKey.
func NewClient(apiKey, language string, opts ...Option) *Client {
	if language == "" {
		language = "en-US"
	}
	c := &Client{
		apiKey:   apiKey,
		language: language,
		baseURL:  defaultBaseURL,
		http:     &http.Client{Timeout: 15 * time.Second},
		limiter:  newLimiter(20 * time.Millisecond), // ~50 req/s cap
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Enabled reports whether an API key is configured.
func (c *Client) Enabled() bool { return c.apiKey != "" }

// --- Wire types (subset) ---

type SearchResult struct {
	Page    int          `json:"page"`
	Results []SearchItem `json:"results"`
}

type SearchItem struct {
	ID           int64  `json:"id"`
	Title        string `json:"title"`         // movies
	Name         string `json:"name"`          // tv
	ReleaseDate  string `json:"release_date"`  // movies
	FirstAirDate string `json:"first_air_date"` // tv
	Overview     string `json:"overview"`
	Popularity   float64 `json:"popularity"`
}

type Genre struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Collection struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
}

type Credits struct {
	Cast []CastMember `json:"cast"`
	Crew []CrewMember `json:"crew"`
}

type CastMember struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Character string `json:"character"`
	Order     int    `json:"order"`
}

type CrewMember struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Job        string `json:"job"`
	Department string `json:"department"`
}

type MovieDetails struct {
	ID               int64       `json:"id"`
	Title            string      `json:"title"`
	ReleaseDate      string      `json:"release_date"`
	Overview         string      `json:"overview"`
	Runtime          int         `json:"runtime"`
	OriginalLanguage string      `json:"original_language"`
	PosterPath       string      `json:"poster_path"`
	BackdropPath     string      `json:"backdrop_path"`
	Genres           []Genre     `json:"genres"`
	Collection       *Collection `json:"belongs_to_collection"`
	Credits          Credits     `json:"credits"`
	VoteAverage      float64     `json:"vote_average"`
	VoteCount        int         `json:"vote_count"`
	Popularity       float64     `json:"popularity"`
	Revenue          int64       `json:"revenue"`
	ProductionCountries []struct {
		ISO31661 string `json:"iso_3166_1"`
	} `json:"production_countries"`
}

// PrimaryCountry returns the first production country code, or "".
func (m *MovieDetails) PrimaryCountry() string {
	if len(m.ProductionCountries) > 0 {
		return m.ProductionCountries[0].ISO31661
	}
	return ""
}

type TVDetails struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	FirstAirDate     string   `json:"first_air_date"`
	Overview         string   `json:"overview"`
	OriginalLanguage string   `json:"original_language"`
	PosterPath       string   `json:"poster_path"`
	BackdropPath     string   `json:"backdrop_path"`
	Genres           []Genre  `json:"genres"`
	Credits          Credits  `json:"credits"`
	VoteAverage      float64  `json:"vote_average"`
	Popularity       float64  `json:"popularity"`
	OriginCountry    []string `json:"origin_country"`
}

// PrimaryCountry returns the first origin country code, or "".
func (t *TVDetails) PrimaryCountry() string {
	if len(t.OriginCountry) > 0 {
		return t.OriginCountry[0]
	}
	return ""
}

type EpisodeDetails struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	Overview      string `json:"overview"`
	EpisodeNumber int    `json:"episode_number"`
	SeasonNumber  int    `json:"season_number"`
	Runtime       int    `json:"runtime"`
}

// SearchMovie returns candidate movies for a title (and optional year).
func (c *Client) SearchMovie(ctx context.Context, title string, year int) ([]SearchItem, error) {
	q := url.Values{"query": {title}}
	if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}
	var out SearchResult
	if err := c.get(ctx, "/search/movie", q, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// MovieDetails fetches full movie metadata including credits and collection.
func (c *Client) MovieDetails(ctx context.Context, id int64) (*MovieDetails, error) {
	q := url.Values{"append_to_response": {"credits"}}
	var out MovieDetails
	if err := c.get(ctx, fmt.Sprintf("/movie/%d", id), q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchTV returns candidate shows for a title (and optional first-air year).
func (c *Client) SearchTV(ctx context.Context, title string, year int) ([]SearchItem, error) {
	q := url.Values{"query": {title}}
	if year > 0 {
		q.Set("first_air_date_year", strconv.Itoa(year))
	}
	var out SearchResult
	if err := c.get(ctx, "/search/tv", q, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// TVDetails fetches full show metadata including credits.
func (c *Client) TVDetails(ctx context.Context, id int64) (*TVDetails, error) {
	q := url.Values{"append_to_response": {"credits"}}
	var out TVDetails
	if err := c.get(ctx, fmt.Sprintf("/tv/%d", id), q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// EpisodeDetails fetches metadata for a single episode.
func (c *Client) EpisodeDetails(ctx context.Context, showID int64, season, episode int) (*EpisodeDetails, error) {
	var out EpisodeDetails
	path := fmt.Sprintf("/tv/%d/season/%d/episode/%d", showID, season, episode)
	if err := c.get(ctx, path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// get performs a rate-limited GET and decodes JSON into out.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	if !c.Enabled() {
		return ErrNoAPIKey
	}
	if err := c.limiter.wait(ctx); err != nil {
		return err
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("api_key", c.apiKey)
	q.Set("language", c.language)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("tmdb rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tmdb %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// ReleaseYear parses a "YYYY-MM-DD" date into a year, 0 on failure.
func ReleaseYear(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, _ := strconv.Atoi(date[:4])
	return y
}

// limiter enforces a minimum interval between requests.
type limiter struct {
	mu       sync.Mutex
	interval time.Duration
	last     time.Time
}

func newLimiter(interval time.Duration) *limiter {
	return &limiter{interval: interval}
}

func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	wait := l.interval - now.Sub(l.last)
	if wait < 0 {
		wait = 0
	}
	l.last = now.Add(wait)
	l.mu.Unlock()

	if wait == 0 {
		return nil
	}
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
