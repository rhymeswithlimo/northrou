// Command relay is Northrou's hosted sign-in-pin mail sender. Home servers keep
// their accounts and pins local and call this service only to deliver the pin
// email, so households do not have to configure their own SMTP. It never sees
// or stores accounts and holds no media. It is a separate binary from the
// coordinator on purpose: the coordinator is stateless and horizontally
// restartable, whereas this holds in-memory rate-limit state.
//
// Configure a mail provider with the RELAY_SMTP_* env vars to send for real;
// without them it runs with a log-only stub that prints pins (local/test only).
//
// Env:
//
//	RELAY_ADDR           listen address (default ":9100")
//	RELAY_TOKEN          optional bearer token required of callers (weak control)
//	RELAY_SMTP_HOST      SMTP host; empty => log-only stub provider
//	RELAY_SMTP_PORT      SMTP port (default 587; 465 = implicit TLS)
//	RELAY_SMTP_USERNAME  SMTP username
//	RELAY_SMTP_PASSWORD  SMTP password
//	RELAY_SMTP_FROM      From address (default: username)
//	RELAY_SMTP_FROM_NAME From display name (optional)
package main

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/rhymeswithlimo/northrou/coordination/internal/relay"
)

func main() {
	addr := envOr("RELAY_ADDR", ":9100")

	var provider relay.Provider
	if p, ok := relay.NewSMTPProvider(relay.SMTPConfig{
		Host:     os.Getenv("RELAY_SMTP_HOST"),
		Port:     atoiOr(os.Getenv("RELAY_SMTP_PORT"), 0),
		Username: os.Getenv("RELAY_SMTP_USERNAME"),
		Password: os.Getenv("RELAY_SMTP_PASSWORD"),
		From:     os.Getenv("RELAY_SMTP_FROM"),
		FromName: os.Getenv("RELAY_SMTP_FROM_NAME"),
	}); ok {
		provider = p
		slog.Info("relay using SMTP provider", "host", os.Getenv("RELAY_SMTP_HOST"))
	} else {
		provider = relay.LogProvider{}
		slog.Warn("relay using log-only stub provider; set RELAY_SMTP_HOST to deliver real mail")
	}

	h := relay.NewHandler(provider, relay.DefaultLimits(), os.Getenv("RELAY_TOKEN"))
	mux := http.NewServeMux()
	h.Routes(mux)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("relay listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("relay failed", "err", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
