// Command coordinator is Northrou's lightweight, stateless remote-access broker.
// It relays only WebRTC signaling (SDP offers/answers and ICE candidates) so
// clients and home servers can hole-punch a direct peer-to-peer connection. It
// never relays media data. It is the single official coordinator; there is no
// sign-in broker and no self-hosting path.
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/rhymeswithlimo/northrou/coordination/internal/broker"
)

func main() {
	addr := os.Getenv("COORD_ADDR")
	if addr == "" {
		addr = ":9000"
	}

	hub := broker.NewHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", hub.ServeWS)

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
