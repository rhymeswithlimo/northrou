// Package email delivers Northrou's transactional mail, currently just the
// one-time login pins. Delivery has two backends, chosen per send:
//
//   - relay (default): POST the pin to the coordination relay so a household
//     does not have to run any mail infrastructure. This is on out of the box.
//   - log: when the relay is disabled, log the pin at WARN for local single-box
//     use so a fully offline install is not locked out.
//
// There is deliberately no SMTP backend. The relay owns mail delivery and the
// email template; the box holds no mail credentials and speaks no SMTP. See
// config.EmailConfig for why.
//
// Everything is standard-library (net/http) so the binary stays pure-Go. Sends
// run detached from the caller so a login request never blocks on the network
// and the response time does not leak whether the address exists (see
// auth.RequestPin). Pins are only ever logged on the log fallback, never on the
// relay path.
package email

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
)

// Sender delivers login pins. It reads delivery settings from the shared
// *Config live, so choices saved during first-run setup take effect without a
// restart.
type Sender struct {
	cfg  *config.Config
	http *http.Client
}

// New returns a Sender backed by the given config. The config pointer is shared
// with the rest of the app, so later edits are picked up on the next send.
func New(cfg *config.Config) *Sender {
	return &Sender{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}}
}

// SendPin delivers a login pin to addr. It returns immediately: the relay
// round-trip runs in a detached goroutine, and when the relay is disabled the
// pin is logged instead. Returning without waiting on the network keeps the
// caller's response time independent of whether the address exists, so
// request-pin does not become an account-enumeration timing oracle, and never
// blocks the HTTP handler on a slow relay. Delivery errors are logged, not
// surfaced (the API deliberately reveals nothing about delivery).
//
// The ctx argument is honored only for its cancellation of the (fast) local
// work; the send uses its own background context so it is not cut short when
// the originating HTTP request completes.
func (s *Sender) SendPin(_ context.Context, addr, pin string) error {
	ec := s.cfg.Email
	if ec.RelayDisabled || ec.RelayURL == "" {
		slog.Warn("relay disabled; login pin logged for local use only (set [email] relay_disabled=false to deliver by mail)",
			"email", addr, "pin", pin)
		return nil
	}
	serverID := s.cfg.Remote.ServerID
	go func() {
		if err := s.sendRelay(ec.RelayURL, ec.RelayToken, serverID, addr, pin); err != nil {
			// Loud (ERROR, not WARN) and actionable: a failed pin send means
			// nobody can sign in, and it is otherwise invisible to the operator
			// because request-pin deliberately reveals nothing to the client.
			slog.Error("login pin NOT delivered; sign-in will not work until this is fixed",
				"err", err, "relay", ec.RelayURL)
		}
	}()
	return nil
}

// sendRelay asks the relay to deliver the pin. The relay owns the email
// template; we send only the destination and the pin (tagged with our server
// id for the relay's rate limiting). The status code is logged so a self-hoster
// can diagnose a silent non-delivery.
func (s *Sender) sendRelay(relayURL, token, serverID, addr, pin string) error {
	body, _ := json.Marshal(map[string]string{
		"server_id": serverID,
		"email":     addr,
		"pin":       pin,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		relayURL+"/v1/pin/send", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		// The single most common misconfiguration: the box's relay_token does
		// not match the relay's RELAY_TOKEN. Say exactly how to fix it.
		return fmt.Errorf("relay rejected the token (HTTP 401): set [email] relay_token " +
			"in config.toml to match your relay's RELAY_TOKEN, then restart")
	}
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay returned HTTP %d", resp.StatusCode)
	}
	return nil
}
