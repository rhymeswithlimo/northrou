package relay

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"time"
)

// Provider delivers a rendered message to a recipient. Implementations must not
// log the pin (it is inside body): only the log-stub provider, used for local
// testing, is permitted to surface it.
type Provider interface {
	Send(ctx context.Context, to, subject, body string) error
}

// LogProvider is the zero-config default: it logs that a pin would be sent, and
// (loudly) the pin itself, so the relay can be exercised without a real mail
// account. It must never be used in production; SMTPProvider is the real path.
type LogProvider struct{}

func (LogProvider) Send(_ context.Context, to, subject, body string) error {
	slog.Warn("relay has no mail provider configured; message logged for local/test use only (set RELAY_SMTP_HOST)",
		"to", to, "subject", subject, "body", body)
	return nil
}

// SMTPConfig configures the SMTP submission provider. Point it at a
// transactional provider's SMTP endpoint (SES, SendGrid, Mailgun, Postmark).
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
}

// SMTPProvider sends over SMTP submission. It deliberately uses net/smtp so the
// coordination module keeps its single third-party dependency and stays
// pure-Go; no provider SDKs.
type SMTPProvider struct{ cfg SMTPConfig }

// NewSMTPProvider builds an SMTP provider, or returns false if host is unset.
func NewSMTPProvider(cfg SMTPConfig) (*SMTPProvider, bool) {
	if cfg.Host == "" {
		return nil, false
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if cfg.From == "" {
		cfg.From = cfg.Username
	}
	return &SMTPProvider{cfg: cfg}, true
}

func (p *SMTPProvider) Send(ctx context.Context, to, subject, body string) error {
	c := p.cfg
	fromHeader := c.From
	if c.FromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", c.FromName, c.From)
	}
	msg := []byte("From: " + fromHeader + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" + body)

	var auth smtp.Auth
	if c.Username != "" {
		auth = smtp.PlainAuth("", c.Username, c.Password, c.Host)
	}
	hostPort := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))

	errCh := make(chan error, 1)
	go func() { errCh <- smtpSend(hostPort, c.Host, c.Port, auth, c.From, to, msg) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// smtpSend dials and delivers. Port 465 is implicit TLS; other ports use
// plaintext with an opportunistic STARTTLS upgrade (submission on 587).
func smtpSend(hostPort, host string, port int, auth smtp.Auth, from, to string, msg []byte) error {
	dialer := net.Dialer{Timeout: 15 * time.Second}
	var (
		c   *smtp.Client
		err error
	)
	if port == 465 {
		conn, derr := tls.DialWithDialer(&dialer, "tcp", hostPort, &tls.Config{ServerName: host})
		if derr != nil {
			return derr
		}
		if c, err = smtp.NewClient(conn, host); err != nil {
			conn.Close()
			return err
		}
	} else {
		conn, derr := dialer.Dial("tcp", hostPort)
		if derr != nil {
			return derr
		}
		if c, err = smtp.NewClient(conn, host); err != nil {
			conn.Close()
			return err
		}
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
				return err
			}
		}
	}
	defer c.Close()

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
