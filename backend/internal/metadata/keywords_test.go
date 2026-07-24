package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Movies nest keywords under "keywords"; TV nests them under "results". Both the
// append_to_response and the dedicated endpoints must parse the right shape.
func TestKeywordParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/movie/") && strings.HasSuffix(r.URL.Path, "/keywords"):
			_, _ = w.Write([]byte(`{"id":1,"keywords":[{"id":9,"name":"heist"},{"id":10,"name":"dream"}]}`))
		case strings.HasPrefix(r.URL.Path, "/tv/") && strings.HasSuffix(r.URL.Path, "/keywords"):
			_, _ = w.Write([]byte(`{"id":2,"results":[{"id":11,"name":"anthology"}]}`))
		case strings.HasPrefix(r.URL.Path, "/movie/"):
			_, _ = w.Write([]byte(`{"id":1,"title":"Inception","keywords":{"keywords":[{"id":9,"name":"heist"}]}}`))
		case strings.HasPrefix(r.URL.Path, "/tv/"):
			_, _ = w.Write([]byte(`{"id":2,"name":"Fargo","keywords":{"results":[{"id":11,"name":"anthology"}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient("key", "en-US", WithBaseURL(srv.URL))
	ctx := context.Background()

	m, err := c.MovieDetails(ctx, 1)
	if err != nil {
		t.Fatalf("MovieDetails: %v", err)
	}
	if got := m.KeywordNames(); len(got) != 1 || got[0] != "heist" {
		t.Fatalf("movie details keywords = %v, want [heist]", got)
	}

	tv, err := c.TVDetails(ctx, 2)
	if err != nil {
		t.Fatalf("TVDetails: %v", err)
	}
	if got := tv.KeywordNames(); len(got) != 1 || got[0] != "anthology" {
		t.Fatalf("tv details keywords = %v, want [anthology]", got)
	}

	mk, err := c.MovieKeywords(ctx, 1)
	if err != nil {
		t.Fatalf("MovieKeywords: %v", err)
	}
	if len(mk) != 2 || mk[0] != "heist" || mk[1] != "dream" {
		t.Fatalf("MovieKeywords = %v, want [heist dream]", mk)
	}

	tk, err := c.TVKeywords(ctx, 2)
	if err != nil {
		t.Fatalf("TVKeywords: %v", err)
	}
	if len(tk) != 1 || tk[0] != "anthology" {
		t.Fatalf("TVKeywords = %v, want [anthology]", tk)
	}
}

// Production companies (movies + TV) and TV created_by must parse off the detail
// calls, feeding studio and "Created by" rows.
func TestCompanyAndCreatorParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/movie/") {
			_, _ = w.Write([]byte(`{"id":1,"title":"Iron Man","production_companies":[{"id":420,"name":"Marvel Studios"},{"id":4,"name":"Paramount"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":2,"name":"Stranger Things","production_companies":[{"id":2919,"name":"Netflix"}],"created_by":[{"id":1,"name":"Matt Duffer"},{"id":2,"name":"Ross Duffer"}]}`))
	}))
	defer srv.Close()

	c := NewClient("key", "en-US", WithBaseURL(srv.URL))
	ctx := context.Background()

	m, err := c.MovieDetails(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.CompanyNames(); len(got) != 2 || got[0] != "Marvel Studios" {
		t.Fatalf("movie companies = %v", got)
	}

	tv, err := c.TVDetails(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := tv.CompanyNames(); len(got) != 1 || got[0] != "Netflix" {
		t.Fatalf("tv companies = %v", got)
	}
	if got := tv.CreatorNames(); len(got) != 2 || got[1] != "Ross Duffer" {
		t.Fatalf("creators = %v", got)
	}
}
