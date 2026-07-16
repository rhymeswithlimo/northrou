// Package metadata fetches movie/TV metadata from The Movie Database (TMDB)
// and caches poster/backdrop images to local disk. All network access is
// rate-limited and gracefully degrades when no API key is configured.
package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	cache    *respCache
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
		cache:    newRespCache(),
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
	ID           int64   `json:"id"`
	Title        string  `json:"title"`          // movies
	Name         string  `json:"name"`           // tv
	ReleaseDate  string  `json:"release_date"`   // movies
	FirstAirDate string  `json:"first_air_date"` // tv
	Overview     string  `json:"overview"`
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
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Character   string `json:"character"`
	Order       int    `json:"order"`
	ProfilePath string `json:"profile_path"`
}

type CrewMember struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Job         string `json:"job"`
	Department  string `json:"department"`
	ProfilePath string `json:"profile_path"`
}

// ReleaseDates is the append_to_response=release_dates payload: per-country
// certification for a movie.
type ReleaseDates struct {
	Results []struct {
		ISO31661     string `json:"iso_3166_1"`
		ReleaseDates []struct {
			Certification string `json:"certification"`
			Type          int    `json:"type"`
		} `json:"release_dates"`
	} `json:"results"`
}

// ContentRatings is the append_to_response=content_ratings payload: per-country
// certification for a show.
type ContentRatings struct {
	Results []struct {
		ISO31661 string `json:"iso_3166_1"`
		Rating   string `json:"rating"`
	} `json:"results"`
}

// certPreference is the country order used to pick one certification to show.
// TMDB returns every country's rating; the UI has room for a single badge.
var certPreference = []string{"US", "GB", "CA", "AU"}

// Certification resolves the per-country release dates to one rating.
func (r *ReleaseDates) Certification() string {
	byCountry := map[string]string{}
	for _, res := range r.Results {
		for _, rd := range res.ReleaseDates {
			if rd.Certification != "" {
				byCountry[res.ISO31661] = rd.Certification
				break
			}
		}
	}
	for _, c := range certPreference {
		if v := byCountry[c]; v != "" {
			return v
		}
	}
	// No preferred country: any rating beats none, but map order is random, so
	// pick deterministically.
	keys := make([]string, 0, len(byCountry))
	for k := range byCountry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		return byCountry[keys[0]]
	}
	return ""
}

// Certification resolves the per-country content ratings to one rating.
func (r *ContentRatings) Certification() string {
	byCountry := map[string]string{}
	for _, res := range r.Results {
		if res.Rating != "" {
			byCountry[res.ISO31661] = res.Rating
		}
	}
	for _, c := range certPreference {
		if v := byCountry[c]; v != "" {
			return v
		}
	}
	keys := make([]string, 0, len(byCountry))
	for k := range byCountry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) > 0 {
		return byCountry[keys[0]]
	}
	return ""
}

type MovieDetails struct {
	ID                  int64        `json:"id"`
	Title               string       `json:"title"`
	ReleaseDate         string       `json:"release_date"`
	Overview            string       `json:"overview"`
	Runtime             int          `json:"runtime"`
	OriginalLanguage    string       `json:"original_language"`
	PosterPath          string       `json:"poster_path"`
	BackdropPath        string       `json:"backdrop_path"`
	Genres              []Genre      `json:"genres"`
	Collection          *Collection  `json:"belongs_to_collection"`
	Credits             Credits      `json:"credits"`
	Tagline             string       `json:"tagline"`
	ReleaseDates        ReleaseDates `json:"release_dates"`
	VoteAverage         float64      `json:"vote_average"`
	VoteCount           int          `json:"vote_count"`
	Popularity          float64      `json:"popularity"`
	Revenue             int64        `json:"revenue"`
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
	ID               int64          `json:"id"`
	Name             string         `json:"name"`
	FirstAirDate     string         `json:"first_air_date"`
	Overview         string         `json:"overview"`
	OriginalLanguage string         `json:"original_language"`
	PosterPath       string         `json:"poster_path"`
	BackdropPath     string         `json:"backdrop_path"`
	Genres           []Genre        `json:"genres"`
	Credits          Credits        `json:"credits"`
	Tagline          string         `json:"tagline"`
	ContentRatings   ContentRatings `json:"content_ratings"`
	VoteAverage      float64        `json:"vote_average"`
	Popularity       float64        `json:"popularity"`
	OriginCountry    []string       `json:"origin_country"`
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
	StillPath     string `json:"still_path"`
	AirDate       string `json:"air_date"`
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
	q := url.Values{"append_to_response": {"credits,release_dates"}}
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
	q := url.Values{"append_to_response": {"credits,content_ratings"}}
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

// tmdbRetries is how many times a rate-limited (429) or transient request is
// retried before giving up and letting the file go unmatched.
const tmdbRetries = 4

// get performs a rate-limited, cached GET and decodes JSON into out. Identical
// requests within a scan are served from the response cache (no network, no
// rate-limit slot).
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	if !c.Enabled() {
		return ErrNoAPIKey
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("language", c.language)

	// Cache key excludes the API key (same request, any key => same data).
	key := path + "?" + q.Encode()
	if body, ok := c.cache.get(key); ok {
		return json.Unmarshal(body, out)
	}

	q.Set("api_key", c.apiKey)
	body, err := c.doGet(ctx, path, q)
	if err != nil {
		return err
	}
	c.cache.set(key, body)
	return json.Unmarshal(body, out)
}

// doGet issues the HTTP request with rate limiting, retrying on 429 (honoring
// Retry-After) and on transient network errors with exponential backoff.
func (c *Client) doGet(ctx context.Context, path string, q url.Values) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt < tmdbRetries; attempt++ {
		if err := c.limiter.wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err // transient network error: back off and retry
			if werr := sleepCtx(ctx, backoff(attempt)); werr != nil {
				return nil, werr
			}
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			wait := retryAfter(resp, backoff(attempt))
			resp.Body.Close()
			lastErr = fmt.Errorf("tmdb rate limited")
			if werr := sleepCtx(ctx, wait); werr != nil {
				return nil, werr
			}
			continue
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("tmdb %s: HTTP %d", path, resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		return body, nil
	}
	return nil, lastErr
}

// backoff returns an exponential delay for the given retry attempt (0-based):
// 500ms, 1s, 2s, ...
func backoff(attempt int) time.Duration {
	return 500 * time.Millisecond * time.Duration(1<<attempt)
}

// retryAfter reads the Retry-After header (seconds) if present, else falls back
// to the given backoff.
func retryAfter(resp *http.Response, fallback time.Duration) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return fallback
}

// sleepCtx sleeps for d unless ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// ResetCache clears the response cache. The scanner calls this at the start of a
// scan so cached data is scoped to a single run (bounded memory, fresh data).
func (c *Client) ResetCache() { c.cache.reset() }

// respCache is a simple concurrency-safe map of request key -> raw JSON body.
type respCache struct {
	mu sync.RWMutex
	m  map[string][]byte
}

func newRespCache() *respCache { return &respCache{m: map[string][]byte{}} }

func (r *respCache) get(key string) ([]byte, bool) {
	r.mu.RLock()
	b, ok := r.m[key]
	r.mu.RUnlock()
	return b, ok
}

func (r *respCache) set(key string, body []byte) {
	r.mu.Lock()
	r.m[key] = body
	r.mu.Unlock()
}

func (r *respCache) reset() {
	r.mu.Lock()
	clear(r.m)
	r.mu.Unlock()
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
