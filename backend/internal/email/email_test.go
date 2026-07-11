package email

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
)

// relayProbe is an httptest relay that reports each received send on a channel.
func relayProbe(t *testing.T) (*httptest.Server, chan map[string]string) {
	t.Helper()
	got := make(chan map[string]string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		got <- body
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func newSender(email config.EmailConfig, serverID string) *Sender {
	cfg := &config.Config{Email: email}
	cfg.Remote.ServerID = serverID
	return New(cfg)
}

func TestRelayUsedByDefault(t *testing.T) {
	srv, got := relayProbe(t)
	s := newSender(config.EmailConfig{RelayURL: srv.URL}, "srv-123")
	if err := s.SendPin(context.Background(), "user@example.com", "123456"); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-got:
		if body["server_id"] != "srv-123" || body["email"] != "user@example.com" || body["pin"] != "123456" {
			t.Errorf("unexpected relay payload: %v", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("relay was not called")
	}
}

func TestDefaultedConfigUsesRelay(t *testing.T) {
	srv, got := relayProbe(t)
	// A fresh install with nothing configured for email: ApplyDefaults populates
	// RelayURL, no SMTP is set, relay is not disabled. Swap only the host for the
	// probe so the defaulted selection path is what is under test.
	cfg := config.Default()
	if cfg.Email.RelayURL == "" || cfg.Email.RelayDisabled || cfg.Email.SMTPHost != "" {
		t.Fatalf("unexpected defaulted email config: %+v", cfg.Email)
	}
	cfg.Email.RelayURL = srv.URL
	cfg.Remote.ServerID = "srv-default"
	s := New(cfg)
	if err := s.SendPin(context.Background(), "user@example.com", "123456"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-got:
		// expected: defaulted config routes to the relay
	case <-time.After(2 * time.Second):
		t.Fatal("defaulted config did not use the relay")
	}
}

func TestSMTPTakesPrecedenceOverRelay(t *testing.T) {
	srv, got := relayProbe(t)
	// smtp_host set => SMTP path chosen; the relay must not be called. The SMTP
	// send targets an unroutable host and fails in its detached goroutine, which
	// is fine: we are only asserting the relay is bypassed.
	s := newSender(config.EmailConfig{
		RelayURL: srv.URL,
		SMTPHost: "127.0.0.1", SMTPPort: 1, SMTPUsername: "x", FromAddress: "x@example.com",
	}, "srv-123")
	if err := s.SendPin(context.Background(), "user@example.com", "123456"); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-got:
		t.Fatalf("relay should not be called when SMTP is set, but got %v", body)
	case <-time.After(400 * time.Millisecond):
		// expected: relay untouched
	}
}

func TestLogFallbackWhenRelayDisabled(t *testing.T) {
	srv, got := relayProbe(t)
	// relay_disabled and no SMTP => log fallback; the relay must not be called.
	s := newSender(config.EmailConfig{RelayURL: srv.URL, RelayDisabled: true}, "srv-123")
	if err := s.SendPin(context.Background(), "user@example.com", "123456"); err != nil {
		t.Fatal(err)
	}
	select {
	case body := <-got:
		t.Fatalf("relay should not be called when disabled, but got %v", body)
	case <-time.After(400 * time.Millisecond):
		// expected: log fallback, relay untouched
	}
}
