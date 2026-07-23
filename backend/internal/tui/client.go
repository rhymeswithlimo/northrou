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

// pair signs the TUI in and stores the access token. The admin runs on (or
// directly connects to) the box, so the request is local: the connection code is
// the credential for remote clients only, and a local pair needs none. The pair
// is ephemeral - the operator's own dashboard must not show up in the
// paired-devices list it displays.
func (c *client) pair(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/auth/pair",
		bytes.NewReader([]byte(`{"ephemeral":true,"device_name":"Northrou admin"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// The body usually says exactly what is wrong ("server has no profiles
		// yet; finish setup first"); pass that on instead of a bare status.
		var body struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&body) == nil && body.Error != "" {
			return fmt.Errorf("%s", body.Error)
		}
		return fmt.Errorf("could not connect to the server (HTTP %d)", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	c.token = out.AccessToken
	return nil
}

// setupStatus reports whether the server still needs first-run setup.
func (c *client) setupStatus(ctx context.Context) (needsSetup bool, err error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/setup/status", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("could not check setup status (HTTP %d)", resp.StatusCode)
	}
	var out struct {
		NeedsSetup bool `json:"needs_setup"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return false, err
	}
	return out.NeedsSetup, nil
}

// setupRequest mirrors the server's setupCompleteRequest.
type setupRequest struct {
	ServerName   string   `json:"server_name"`
	TMDBAPIKey   string   `json:"tmdb_api_key"`
	EnableRemote bool     `json:"enable_remote"`
	MovieDirs    []string `json:"movie_dirs"`
	ShowDirs     []string `json:"show_dirs"`
}

// setupComplete performs first-run setup and signs the TUI in with the session
// the server returns.
func (c *client) setupComplete(ctx context.Context, r setupRequest) (connectionCode string, err error) {
	body, _ := json.Marshal(r)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/api/setup/complete", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		var e struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return "", fmt.Errorf("%s", e.Error)
		}
		return "", fmt.Errorf("setup failed (HTTP %d)", resp.StatusCode)
	}
	var out struct {
		ConnectionCode string `json:"connection_code"`
		AccessToken    string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	c.token = out.AccessToken
	return out.ConnectionCode, nil
}

// serverInfo is the slice of /api/admin/config the summary screen shows.
type serverInfo struct {
	ServerName     string `json:"server_name"`
	ConnectionCode string `json:"connection_code"`
	RemoteEnabled  bool   `json:"remote_enabled"`
}

// fetchServerInfo returns the running server's name, connection code, and
// remote state (requires a paired session).
func (c *client) fetchServerInfo(ctx context.Context) (serverInfo, error) {
	var info serverInfo
	err := c.get(ctx, "/api/admin/config", &info)
	return info, err
}

// startScan asks the server to begin a library scan.
func (c *client) startScan(ctx context.Context) error {
	return c.send(ctx, http.MethodPost, "/api/admin/scan", []byte(`{}`), nil)
}

// devicePayload mirrors the server's paired-device DTO.
type devicePayload struct {
	ID          string `json:"id"`
	DeviceName  string `json:"device_name"`
	ProfileName string `json:"profile_name"`
	PairedAt    string `json:"paired_at"`
	LastSeenAt  string `json:"last_seen_at"`
}

// revokeDevice signs one paired device out for good.
func (c *client) revokeDevice(ctx context.Context, id string) error {
	return c.send(ctx, http.MethodDelete, "/api/admin/sessions/"+id, nil, nil)
}

// rotateCode replaces the connection code, revoking every session.
func (c *client) rotateCode(ctx context.Context) (string, error) {
	var out struct {
		ConnectionCode string `json:"connection_code"`
	}
	err := c.send(ctx, http.MethodPost, "/api/admin/connection-code/rotate", []byte(`{}`), &out)
	return out.ConnectionCode, err
}

// setRemoteEnabled flips remote access on or off.
func (c *client) setRemoteEnabled(ctx context.Context, enabled bool) error {
	body := fmt.Appendf(nil, `{"remote_enabled":%t}`, enabled)
	return c.send(ctx, http.MethodPatch, "/api/admin/config", body, nil)
}

// send performs an authenticated mutating request, surfacing the server's own
// error text when it has any.
func (c *client) send(ctx context.Context, method, path string, body []byte, out any) error {
	req, _ := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return fmt.Errorf("%s", e.Error)
		}
		return fmt.Errorf("%s %s failed (HTTP %d)", method, path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
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
	info     serverInfo
	devices  []devicePayload
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
	_ = c.get(ctx, "/api/admin/config", &d.info)
	_ = c.get(ctx, "/api/admin/sessions", &d.devices)

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
