package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// client is a minimal authenticated client for the local admin API.
type client struct {
	base  string
	token string
	http  *http.Client
}

func newClient(base string) *client {
	return &client{base: base, http: &http.Client{Timeout: 5 * time.Second}}
}

// requestPin asks the server to email a one-time sign-in code to the address.
// The endpoint intentionally returns 200 even for unknown emails.
func (c *client) requestPin(ctx context.Context, email string) error {
	body, _ := json.Marshal(map[string]string{"email": email})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/auth/request-pin", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("could not request a code (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// verifyPin exchanges an emailed pin for an access token and stores it.
func (c *client) verifyPin(ctx context.Context, email, pin string) error {
	body, _ := json.Marshal(map[string]string{"email": email, "pin": pin})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/auth/verify-pin", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("invalid or expired code (HTTP %d)", resp.StatusCode)
	}
	// The dashboard is read-only, so a plain profile token suffices; admin
	// elevation is only needed for mutations the TUI does not perform.
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	c.token = out.AccessToken
	return nil
}

func (c *client) get(ctx context.Context, path string, out any) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// --- API payloads (mirror the server DTOs) ---

type streamSession struct {
	Mode        string `json:"mode"`
	Title       string `json:"title"`
	SourceVideo string `json:"source_video"`
	TargetVideo string `json:"target_video"`
	TargetAudio string `json:"target_audio"`
	HWBackend   string `json:"hw_backend"`
	Remote      bool   `json:"remote"`
	Client      string `json:"client"`
}

type streamsPayload struct {
	Count   int             `json:"count"`
	Streams []streamSession `json:"streams"`
}

type hardwarePayload struct {
	Backend           string   `json:"backend"`
	Available         []string `json:"available"`
	EstimatedCapacity int      `json:"estimated_capacity"`
	ActiveTranscodes  int      `json:"active_transcodes"`
	FFmpegReady       bool     `json:"ffmpeg_ready"`
}

type scanPayload struct {
	Running   bool `json:"running"`
	Total     int  `json:"total"`
	Processed int  `json:"processed"`
	Matched   int  `json:"matched"`
	Unmatched int  `json:"unmatched"`
}

// dashboardData is a snapshot of everything the dashboard shows.
type dashboardData struct {
	streams  streamsPayload
	hardware hardwarePayload
	scan     scanPayload
	movies   int
	shows    int
	err      error
}

// fetchAll pulls all dashboard data in one shot.
func (c *client) fetchAll(ctx context.Context) dashboardData {
	var d dashboardData
	if err := c.get(ctx, "/api/admin/streams", &d.streams); err != nil {
		d.err = err
		return d
	}
	_ = c.get(ctx, "/api/admin/hardware", &d.hardware)
	_ = c.get(ctx, "/api/admin/scan", &d.scan)

	var movies []json.RawMessage
	if c.get(ctx, "/api/movies", &movies) == nil {
		d.movies = len(movies)
	}
	var shows []json.RawMessage
	if c.get(ctx, "/api/shows", &shows) == nil {
		d.shows = len(shows)
	}
	return d
}
