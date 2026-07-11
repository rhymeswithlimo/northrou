package tui

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mockServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/request-pin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":"sent"}`))
	})
	mux.HandleFunc("/api/auth/verify-pin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","user":{"is_admin":true}}`))
	})
	mux.HandleFunc("/api/admin/streams", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"count":1,"streams":[{"mode":"audio","title":"Inception","source_video":"hevc","target_video":"hevc","target_audio":"eac3","hw_backend":"videotoolbox","remote":false}]}`))
	})
	mux.HandleFunc("/api/admin/hardware", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"backend":"videotoolbox","available":["videotoolbox","software"],"estimated_capacity":2,"active_transcodes":0,"ffmpeg_ready":true}`))
	})
	mux.HandleFunc("/api/admin/scan", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"running":false,"total":10,"processed":10,"matched":9,"unmatched":1}`))
	})
	mux.HandleFunc("/api/movies", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{},{},{}]`))
	})
	mux.HandleFunc("/api/shows", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{}]`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestClientLoginAndFetch(t *testing.T) {
	srv := mockServer(t)
	c := newClient(srv.URL)
	if err := c.requestPin(context.Background(), "admin@example.com"); err != nil {
		t.Fatalf("request pin: %v", err)
	}
	if err := c.verifyPin(context.Background(), "admin@example.com", "123456"); err != nil {
		t.Fatalf("verify pin: %v", err)
	}
	d := c.fetchAll(context.Background())
	if d.err != nil {
		t.Fatalf("fetch: %v", d.err)
	}
	if d.streams.Count != 1 || d.streams.Streams[0].TargetAudio != "eac3" {
		t.Errorf("unexpected streams: %+v", d.streams)
	}
	if d.hardware.Backend != "videotoolbox" || d.hardware.EstimatedCapacity != 2 {
		t.Errorf("unexpected hardware: %+v", d.hardware)
	}
	if d.movies != 3 || d.shows != 1 {
		t.Errorf("expected 3 movies / 1 show, got %d/%d", d.movies, d.shows)
	}
}

func TestClientRejectsNonAdmin(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/verify-pin", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"tok","user":{"is_admin":false}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := newClient(srv.URL)
	if err := c.verifyPin(context.Background(), "user@example.com", "123456"); err == nil {
		t.Error("expected non-admin login to be rejected")
	}
}

func TestDashboardRenders(t *testing.T) {
	m := newModel("http://localhost:8674")
	m.state = viewDashboard
	m.data = dashboardData{
		streams:  streamsPayload{Count: 1, Streams: []streamSession{{Mode: "direct", Title: "Dune", SourceVideo: "hevc", TargetVideo: "hevc", TargetAudio: "truehd", HWBackend: "none"}}},
		hardware: hardwarePayload{Backend: "nvenc", Available: []string{"nvenc", "software"}, EstimatedCapacity: 4, FFmpegReady: true},
		scan:     scanPayload{Total: 5, Processed: 5, Matched: 5},
		movies:   42, shows: 7,
	}
	// Each tab should render without panicking and include key content.
	for tab, want := range map[int]string{0: "Dune", 1: "nvenc", 2: "42"} {
		m.tab = tab
		out := m.View()
		if !strings.Contains(out, want) {
			t.Errorf("tab %d view missing %q:\n%s", tab, want, out)
		}
	}
}

func TestLoginViewRenders(t *testing.T) {
	m := newModel("http://localhost:8674")
	out := m.View()
	if !strings.Contains(out, "Northrou Admin") || !strings.Contains(out, "Email") {
		t.Errorf("login view missing expected content:\n%s", out)
	}
}
