package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// localAPI is a minimal authenticated client for the running daemon's local
// API, for CLI commands that prefer driving the daemon over touching its
// files. Local requests are trusted, so pairing needs no code.
type localAPI struct {
	base  string
	token string
	http  *http.Client
}

// newLocalAPI connects to the daemon on the given port and signs in.
func newLocalAPI(ctx context.Context, port int) (*localAPI, error) {
	c := &localAPI{
		base: fmt.Sprintf("http://localhost:%d", port),
		http: &http.Client{Timeout: 5 * time.Second},
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	// Ephemeral: the CLI's own housekeeping must not appear in the
	// paired-devices list it exists to manage.
	if err := c.do(ctx, http.MethodPost, "/api/auth/pair", []byte(`{"ephemeral":true,"device_name":"Northrou CLI"}`), &out); err != nil {
		return nil, fmt.Errorf("could not connect to the running server: %w", err)
	}
	c.token = out.AccessToken
	return c, nil
}

func (c *localAPI) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, nil, out)
}

func (c *localAPI) post(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodPost, path, []byte(`{}`), out)
}

func (c *localAPI) patch(ctx context.Context, path string, body []byte, out any) error {
	return c.do(ctx, http.MethodPatch, path, body, out)
}

func (c *localAPI) del(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

func (c *localAPI) do(ctx context.Context, method, path string, body []byte, out any) error {
	var rd *bytes.Reader
	if body == nil {
		rd = bytes.NewReader(nil)
	} else {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
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
		return fmt.Errorf("%s %s: HTTP %d", method, path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
