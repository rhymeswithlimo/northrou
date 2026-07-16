// Command coordinator is Northrou's lightweight, stateless remote-access broker.
// It relays only WebRTC signaling (SDP offers/answers and ICE candidates) so
// clients and home servers can hole-punch a direct peer-to-peer connection. It
// never relays media data. Self-hosters can run their own instance and point
// their server's remote.coordination_url at it.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/rhymeswithlimo/northrou/coordination/internal/broker"
	"github.com/rhymeswithlimo/northrou/coordination/internal/oauth"
	"strings"
)

func main() {
	addr := os.Getenv("COORD_ADDR")
	if addr == "" {
		addr = ":9000"
	}

	hub := broker.NewHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.ServeWS)

	// Sign-in broker. Optional: with no provider credentials it registers
	// nothing and the coordinator stays a pure signalling relay.
	if b := buildBroker(); b != nil {
		b.Routes(mux)
		slog.Info("oauth broker enabled")
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		servers, sessions := hub.Stats()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"servers": servers, "sessions": sessions})
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("coordination server listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("coordination server failed", "err", err)
		os.Exit(1)
	}
}

// buildBroker assembles the sign-in broker from the environment, or returns nil
// when nothing is configured.
//
// Env:
//
//	OAUTH_ISSUER            this service's public base URL (required)
//	OAUTH_SIGNING_KEY       ES256 private key, PEM. Without it a key is
//	                        generated per process, which breaks verification
//	                        across restarts: set it in production.
//	OAUTH_REDIRECTS         comma-separated allowed client redirect prefixes
//	OAUTH_GOOGLE_CLIENT_ID / OAUTH_GOOGLE_CLIENT_SECRET
//	OAUTH_APPLE_SERVICE_ID / OAUTH_APPLE_TEAM_ID / OAUTH_APPLE_KEY_ID /
//	OAUTH_APPLE_KEY         Apple's ES256 private key, PEM
func buildBroker() *oauth.Broker {
	issuer := os.Getenv("OAUTH_ISSUER")
	if issuer == "" {
		return nil
	}

	google := oauth.NewGoogle(os.Getenv("OAUTH_GOOGLE_CLIENT_ID"), os.Getenv("OAUTH_GOOGLE_CLIENT_SECRET"))

	var apple *oauth.AppleProvider
	if appleKey, err := oauth.ParseKey(os.Getenv("OAUTH_APPLE_KEY")); err != nil {
		slog.Error("oauth: OAUTH_APPLE_KEY is not a usable ES256 PEM key; Apple sign-in disabled", "err", err)
	} else {
		apple = oauth.NewApple(
			os.Getenv("OAUTH_APPLE_SERVICE_ID"),
			os.Getenv("OAUTH_APPLE_TEAM_ID"),
			os.Getenv("OAUTH_APPLE_KEY_ID"),
			appleKey,
		)
	}

	if google == nil && apple == nil {
		slog.Warn("oauth: OAUTH_ISSUER is set but no provider is configured; broker disabled")
		return nil
	}

	key, err := oauth.ParseKey(os.Getenv("OAUTH_SIGNING_KEY"))
	if err != nil {
		slog.Error("oauth: OAUTH_SIGNING_KEY is not a usable ES256 PEM key", "err", err)
		return nil
	}

	var redirects []string
	for _, r := range strings.Split(os.Getenv("OAUTH_REDIRECTS"), ",") {
		if r = strings.TrimSpace(r); r != "" {
			redirects = append(redirects, r)
		}
	}
	if len(redirects) == 0 {
		// An empty allow-list refuses everything, which is the safe failure:
		// an open redirector would launder a real Google login into any site.
		slog.Warn("oauth: OAUTH_REDIRECTS is empty; every redirect will be refused")
	}

	b, err := oauth.NewBroker(oauth.Config{Issuer: issuer, Key: key, AllowedRedirects: redirects}, google, apple)
	if err != nil {
		slog.Error("oauth: broker setup failed", "err", err)
		return nil
	}
	return b
}
