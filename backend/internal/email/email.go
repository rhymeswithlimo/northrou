// Package email delivers Northrou's transactional mail, currently just the
// one-time login pins. Delivery has three backends, chosen per send:
//
//   - relay (default): POST the pin to the hosted relay so a household does not
//     have to run any mail infrastructure. This is on out of the box.
//   - smtp: send directly through the household's own mail server, when they
//     configure one. Takes precedence over the relay.
//   - log: if neither is available, log the pin at WARN for local single-box
//     use so a fully offline install is not locked out.
//
// Everything is standard-library (net/smtp, net/http) so the binary stays
// pure-Go. Sends run detached from the caller so a login request never blocks
// on the network and the response time does not leak whether the address
// exists (see auth.RequestPin). Pins are only ever logged on the log fallback,
// never on the relay or smtp paths.
package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
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
// with the rest of the app, so later edits (e.g. setup persisting SMTP) are
// picked up on the next send.
func New(cfg *config.Config) *Sender {
	return &Sender{cfg: cfg, http: &http.Client{Timeout: 15 * time.Second}}
}

// SendPin delivers a login pin to addr. It returns immediately: when SMTP is
// unconfigured the pin is logged (fallback for a single-box install not yet
// wired to mail); when it is configured the actual SMTP round-trip runs in a
// detached goroutine. Returning without waiting on the network keeps the
// caller's response time independent of whether the address exists, so
// request-pin does not become an account-enumeration timing oracle, and never
// blocks the HTTP handler on a slow mail server. Delivery errors are logged,
// not surfaced (the API deliberately reveals nothing about delivery).
//
// The ctx argument is honored only for its cancellation of the (fast) local
// work; the send uses its own background context so it is not cut short when
// the originating HTTP request completes.
func (s *Sender) SendPin(_ context.Context, addr, pin string) error {
	ec := s.cfg.Email
	switch {
	case ec.SMTPHost != "":
		s.sendSMTP(ec, addr, pin)
	case !ec.RelayDisabled && ec.RelayURL != "":
		serverID := s.cfg.Remote.ServerID
		go func() {
			if err := s.sendRelay(ec.RelayURL, ec.RelayToken, serverID, addr, pin); err != nil {
				slog.Warn("failed to deliver login pin via relay", "err", err)
			}
		}()
	default:
		slog.Warn("no email delivery configured; login pin logged for local use only (set [email] relay_disabled=false or smtp_host)",
			"email", addr, "pin", pin)
	}
	return nil
}

// sendSMTP delivers directly through the household's own mail server, detached
// so the login path never blocks on it.
func (s *Sender) sendSMTP(ec config.EmailConfig, addr, pin string) {
	from := ec.From()
	msg := buildMessage(from, ec.FromName, addr, pin)
	host := net.JoinHostPort(ec.SMTPHost, strconv.Itoa(ec.SMTPPort))
	var auth smtp.Auth
	if ec.SMTPUsername != "" {
		auth = smtp.PlainAuth("", ec.SMTPUsername, ec.SMTPPassword, ec.SMTPHost)
	}
	go func() {
		if err := send(host, ec.SMTPHost, ec.SMTPPort, auth, from, addr, msg); err != nil {
			slog.Warn("failed to send login pin email", "err", err)
		}
	}()
}

// sendRelay asks the hosted relay to deliver the pin. The relay owns the email
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
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// send dials the SMTP server and delivers msg. Port 465 uses implicit TLS;
// other ports use plaintext with an opportunistic STARTTLS upgrade (the common
// submission path on 587).
func send(hostPort, host string, port int, auth smtp.Auth, from, to string, msg []byte) error {
	if port == 465 {
		return sendImplicitTLS(hostPort, host, auth, from, to, msg)
	}

	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.Dial("tcp", hostPort)
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()

	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	return deliver(c, auth, from, to, msg)
}

// sendImplicitTLS delivers over a connection that is TLS from the first byte
// (SMTPS, port 465).
func sendImplicitTLS(hostPort, host string, auth smtp.Auth, from, to string, msg []byte) error {
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := tls.DialWithDialer(&d, "tcp", hostPort, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	return deliver(c, auth, from, to, msg)
}

// deliver runs the AUTH/MAIL/RCPT/DATA sequence on an established client.
func deliver(c *smtp.Client, auth smtp.Auth, from, to string, msg []byte) error {
	if auth != nil {
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				return err
			}
		}
	}
	if err := c.Mail(from); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// buildMessage assembles a minimal RFC 5322 message carrying the pin.
func buildMessage(from, fromName, to, pin string) []byte {
	fromHeader := from
	if fromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", fromName, from)
	}
	body := fmt.Sprintf(
		"Your Northrou sign-in code is:\r\n\r\n    %s\r\n\r\n"+
			"It expires in 10 minutes. If you did not request it, ignore this email.\r\n",
		pin)
	headers := "" +
		"From: " + fromHeader + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: Your Northrou sign-in code\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n"
	return []byte(headers + body)
}
