// Package server wires the chi router, middleware stack, and http.Server, and
// manages graceful startup/shutdown of Northrou's HTTP API.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rhymeswithlimo/northrou/backend/internal/api"
	"github.com/rhymeswithlimo/northrou/backend/internal/web"
)

// Server owns the HTTP listener and router.
type Server struct {
	httpServer *http.Server
	addr       string
}

// New builds a Server bound to addr (host:port) serving the given API.
func New(addr string, a *api.API) *Server {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(cors)
	r.Use(slogLogger)

	a.Mount(r)

	// Serve the embedded setup wizard (and any built-in static assets) at the
	// root. The full Tauri frontend replaces this later. The /api routes above
	// take precedence via chi's most-specific-match.
	r.Handle("/*", web.Handler())

	return &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           r,
			ReadHeaderTimeout: 10 * time.Second,
			// No write timeout: long-lived media/HLS responses need to stream.
		},
		addr: addr,
	}
}

// Handler exposes the router (used by the remote peer to serve the same API
// over the WebRTC data-channel tunnel in P7).
func (s *Server) Handler() http.Handler { return s.httpServer.Handler }

// Start begins serving in the background and returns once the listener is open
// (or immediately with an error if binding fails).
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.addr, err)
	}
	slog.Info("http server listening", "addr", ln.Addr().String())
	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "err", err)
		}
	}()
	return nil
}

// Shutdown gracefully stops the server within the context deadline.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
