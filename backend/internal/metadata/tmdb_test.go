package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGet_CachesRepeatedRequests(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":27205,"title":"Inception"}`))
	}))
	defer srv.Close()

	c := NewClient("key", "en-US", WithBaseURL(srv.URL))

	for i := 0; i < 3; i++ {
		m, err := c.MovieDetails(context.Background(), 27205)
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if m.Title != "Inception" {
			t.Fatalf("unexpected title %q", m.Title)
		}
	}
	if got := hits.Load(); got != 1 {
		t.Errorf("server hit %d times, want 1 (rest served from cache)", got)
	}

	// ResetCache forces a refetch.
	c.ResetCache()
	if _, err := c.MovieDetails(context.Background(), 27205); err != nil {
		t.Fatal(err)
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("after reset, server hit %d times, want 2", got)
	}
}

func TestGet_RetriesOn429ThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"title":"OK"}`))
	}))
	defer srv.Close()

	c := NewClient("key", "en-US", WithBaseURL(srv.URL))
	start := time.Now()
	m, err := c.MovieDetails(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if m.Title != "OK" {
		t.Errorf("title = %q", m.Title)
	}
	if calls.Load() != 2 {
		t.Errorf("expected 2 calls (429 then 200), got %d", calls.Load())
	}
	if elapsed := time.Since(start); elapsed < time.Second {
		t.Errorf("expected to honor Retry-After ~1s, waited only %v", elapsed)
	}
}

func TestGet_GivesUpAfterPersistent429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0") // fall back to backoff, keep it quick-ish
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient("key", "en-US", WithBaseURL(srv.URL))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.MovieDetails(ctx, 1); err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
}

func TestSetAPIKey_TogglesEnabledAndKey(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.URL.Query().Get("api_key")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":27205,"title":"Inception"}`))
	}))
	defer srv.Close()

	// Start with no key: disabled, and calls short-circuit with ErrNoAPIKey.
	c := NewClient("", "en-US", WithBaseURL(srv.URL))
	if c.Enabled() {
		t.Fatal("expected disabled with empty key")
	}
	if _, err := c.MovieDetails(context.Background(), 27205); err != ErrNoAPIKey {
		t.Fatalf("empty key: want ErrNoAPIKey, got %v", err)
	}

	// Add a key at runtime: now enabled and the key reaches the wire.
	c.SetAPIKey("live-key")
	if !c.Enabled() {
		t.Fatal("expected enabled after SetAPIKey")
	}
	if _, err := c.MovieDetails(context.Background(), 27205); err != nil {
		t.Fatalf("after SetAPIKey: %v", err)
	}
	if gotKey != "live-key" {
		t.Errorf("request used api_key %q, want live-key", gotKey)
	}

	// Remove it again: back to disabled.
	c.SetAPIKey("")
	if c.Enabled() {
		t.Fatal("expected disabled after clearing the key")
	}
}
