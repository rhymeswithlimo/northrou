package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureProvider records what it was asked to send.
type captureProvider struct {
	mu               sync.Mutex
	calls            int
	lastTo, lastBody string
}

func (p *captureProvider) Send(_ context.Context, to, _, body string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.lastTo, p.lastBody = to, body
	return nil
}

func testHandler(t *testing.T, provider Provider, token string) *Handler {
	t.Helper()
	// Loose limits so the happy-path tests are not throttled; the rate-limit
	// test builds its own tight handler.
	return NewHandler(provider, Limits{
		PerRecipientWindow: time.Minute, PerRecipientMax: 100,
		PerServerWindow: time.Minute, PerServerMax: 100,
		GlobalWindow: time.Minute, GlobalMax: 1000,
	}, token)
}

func post(t *testing.T, h *Handler, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Routes(mux)
	req := httptest.NewRequest(http.MethodPost, "/v1/pin/send", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestSendPinOK(t *testing.T) {
	p := &captureProvider{}
	h := testHandler(t, p, "")
	rec := post(t, h, "", `{"server_id":"abc","email":"User@Example.com","pin":"123456"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	if p.calls != 1 {
		t.Fatalf("provider not called once: %d", p.calls)
	}
	if p.lastTo != "user@example.com" {
		t.Errorf("recipient not normalized: %q", p.lastTo)
	}
	if !strings.Contains(p.lastBody, "123456") {
		t.Errorf("pin missing from body: %q", p.lastBody)
	}
}

func TestSendPinValidation(t *testing.T) {
	p := &captureProvider{}
	h := testHandler(t, p, "")
	cases := []string{
		`{"server_id":"abc","email":"not-an-email","pin":"123456"}`,
		`{"server_id":"abc","email":"user@example.com","pin":"12345"}`,  // 5 digits
		`{"server_id":"abc","email":"user@example.com","pin":"12a456"}`, // non-digit
		`{"server_id":"","email":"user@example.com","pin":"123456"}`,    // no server id
		`{not json`,
	}
	for i, body := range cases {
		rec := post(t, h, "", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("case %d: want 400, got %d", i, rec.Code)
		}
	}
	if p.calls != 0 {
		t.Errorf("provider should not have been called: %d", p.calls)
	}
}

func TestSendPinPerRecipientRateLimit(t *testing.T) {
	p := &captureProvider{}
	h := NewHandler(p, Limits{
		PerRecipientWindow: time.Hour, PerRecipientMax: 1, // one per recipient
		PerServerWindow: time.Hour, PerServerMax: 100,
		GlobalWindow: time.Hour, GlobalMax: 100,
	}, "")

	first := post(t, h, "", `{"server_id":"abc","email":"victim@example.com","pin":"111111"}`)
	if first.Code != http.StatusAccepted {
		t.Fatalf("first send want 202, got %d", first.Code)
	}
	// Second send to the same recipient (even from a different server) is capped.
	second := post(t, h, "", `{"server_id":"other","email":"victim@example.com","pin":"222222"}`)
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second send want 429, got %d", second.Code)
	}
	if p.calls != 1 {
		t.Errorf("only the first send should reach the provider, got %d", p.calls)
	}
	// A different recipient is unaffected.
	other := post(t, h, "", `{"server_id":"abc","email":"someone@example.com","pin":"333333"}`)
	if other.Code != http.StatusAccepted {
		t.Errorf("different recipient want 202, got %d", other.Code)
	}
}

func TestSendPinToken(t *testing.T) {
	p := &captureProvider{}
	h := testHandler(t, p, "s3cret")
	if rec := post(t, h, "", `{"server_id":"abc","email":"user@example.com","pin":"123456"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("missing token want 401, got %d", rec.Code)
	}
	if rec := post(t, h, "s3cret", `{"server_id":"abc","email":"user@example.com","pin":"123456"}`); rec.Code != http.StatusAccepted {
		t.Errorf("valid token want 202, got %d", rec.Code)
	}
}
