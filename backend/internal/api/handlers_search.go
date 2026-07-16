package api

import (
	"net/http"
	"strconv"

	"github.com/rhymeswithlimo/northrou/backend/internal/db"
)

// searchItemDTO is one library hit. It matches the home-row item shape
// ({kind, id, title, year, poster_url}) so the client renders search results
// with the same card component as everything else.
type searchItemDTO struct {
	Kind      string `json:"kind"`
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Year      int    `json:"year,omitempty"`
	PosterURL string `json:"poster_url,omitempty"`
}

func (a *API) searchResultsToDTO(rs []db.SearchResult) []searchItemDTO {
	out := make([]searchItemDTO, 0, len(rs))
	for _, r := range rs {
		out = append(out, searchItemDTO{
			Kind:      string(r.Kind),
			ID:        r.ID,
			Title:     r.Title,
			Year:      r.Year,
			PosterURL: a.imageURL(r.PosterPath),
		})
	}
	return out
}

// handleSearch searches movie and show titles.
func (a *API) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	// An empty query is an empty result, not an error: the client fires this on
	// every keystroke, including the one that clears the box.
	results, err := a.DB.Search(r.Context(), q, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	writeJSON(w, http.StatusOK, a.searchResultsToDTO(results))
}

// handleSimilarMovies returns titles related to a movie.
func (a *API) handleSimilarMovies(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := a.DB.GetMovie(r.Context(), id); err != nil {
		notFoundOr500(w, err, "get movie failed")
		return
	}
	results, err := a.DB.SimilarMovies(r.Context(), id, 12)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "similar failed")
		return
	}
	writeJSON(w, http.StatusOK, a.searchResultsToDTO(results))
}

// handleSimilarShows returns shows related to a show.
func (a *API) handleSimilarShows(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := a.DB.GetShow(r.Context(), id); err != nil {
		notFoundOr500(w, err, "get show failed")
		return
	}
	results, err := a.DB.SimilarShows(r.Context(), id, 12)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "similar failed")
		return
	}
	writeJSON(w, http.StatusOK, a.searchResultsToDTO(results))
}
