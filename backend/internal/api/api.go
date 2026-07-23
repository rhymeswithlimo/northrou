// Package api implements Northrou's HTTP/JSON API: authentication, the
// first-run setup wizard, and (in later phases) library, streaming, subtitle,
// recommendation, and admin endpoints. Handlers return clean DTOs decoupled
// from database rows so the frontend contract stays stable.
package api

import (
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/recommend"
	"github.com/rhymeswithlimo/northrou/backend/internal/scanner"
	"github.com/rhymeswithlimo/northrou/backend/internal/transcode"
)

// Deps are the dependencies the API handlers need. Later phases add the
// transcoder and recommendation engine here.
type Deps struct {
	DB         *db.DB
	Auth       *auth.Service
	Cfg        *config.Config
	ConfigPath string
	Scanner    *scanner.Scanner
	Recommend  *recommend.Engine
	ImagesDir  string
}

// API bundles handler dependencies and route registration.
type API struct {
	Deps

	// pairLimiter bounds connection-code guessing on /api/auth/pair. It is global
	// (see limiter.go for why per-IP is not possible on the box).
	pairLimiter *limiter

	mu       sync.RWMutex
	streamer *transcode.Streamer // set once ffmpeg is ready
}

// New constructs the API.
func New(d Deps) *API {
	return &API{
		Deps:        d,
		pairLimiter: newLimiter(time.Minute, 60),
	}
}

// SetStreamer attaches the transcoding streamer once ffmpeg becomes available.
func (a *API) SetStreamer(s *transcode.Streamer) {
	a.mu.Lock()
	a.streamer = s
	a.mu.Unlock()
}

// getStreamer returns the streamer or nil if ffmpeg is not ready yet.
func (a *API) getStreamer() *transcode.Streamer {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.streamer
}

// Mount registers all API routes on r under /api.
func (a *API) Mount(r chi.Router) {
	r.Route("/api", func(r chi.Router) {
		// Gzip JSON responses only. Media, HLS segments, images, and WebVTT are
		// already-compressed or binary; gzipping them wastes CPU on a weak box,
		// so the content-type gate deliberately excludes them.
		r.Use(middleware.Compress(5, "application/json"))

		r.Get("/health", a.handleHealth)

		// First-run setup (only usable while no users exist).
		r.Route("/setup", func(r chi.Router) {
			r.Get("/status", a.handleSetupStatus)
			r.Post("/complete", a.handleSetupComplete)
		})

		// Authentication: the server connection code is the sole credential.
		// A remote client presents it to /auth/pair (over the tunnel); a local
		// request is trusted and pairs without a code. Then pick a profile.
		r.Route("/auth", func(r chi.Router) {
			r.Post("/pair", a.handlePair)
			r.Post("/select-profile", a.handleSelectProfile)
			r.Post("/refresh", a.handleRefresh)
			r.Post("/logout", a.handleLogout)
		})

		// Authenticated routes.
		r.Group(func(r chi.Router) {
			r.Use(a.Auth.Middleware)
			r.Get("/me", a.handleMe)
			r.Post("/me/language", a.handleSetMyLanguage)

			// Profiles: any signed-in profile may list, add, or rename.
			// Deleting a profile is destructive (it wipes that viewer's watch
			// history) so it is gated on admin elevation (see the group below).
			r.Get("/profiles", a.handleListProfiles)
			r.Post("/profiles", a.handleCreateProfile)
			r.Patch("/profiles/{id}", a.handleUpdateProfile)

			// Library.
			r.Get("/movies", a.handleListMovies)
			r.Get("/movies/{id}", a.handleGetMovie)
			r.Get("/movies/{id}/similar", a.handleSimilarMovies)
			r.Get("/shows", a.handleListShows)
			r.Get("/shows/{id}", a.handleGetShow)
			r.Get("/shows/{id}/similar", a.handleSimilarShows)
			r.Get("/search", a.handleSearch)
			r.Get("/unmatched", a.handleListUnmatched)
			r.Get("/admin/tmdb-search", a.handleTMDBSearch)

			// Streaming.
			r.Get("/media/{id}/stream", a.handleStream)
			r.Get("/media/{id}/plan", a.handlePlan)
			r.Get("/media/{id}/hls/{session}/{file}", a.handleHLSFile)

			// Home / recommendations.
			r.Get("/home", a.handleHome)
			r.Get("/continue-watching", a.handleContinueWatching)
			r.Post("/watch", a.handleWatch)

			// Subtitles.
			r.Get("/media/{id}/subtitles", a.handleListSubtitles)
			r.Get("/media/{id}/subtitles/{track}.vtt", a.handleGetSubtitleVTT)

			// Cached metadata images.
			r.Handle("/images/*", a.imageHandler())

			// Admin reads: dashboard/status. Any signed-in profile may view
			// these; they expose no controls.
			r.Get("/admin/config", a.handleGetConfig)
			r.Get("/admin/scan", a.handleScanStatus)
			r.Get("/admin/streams", a.handleAdminStreams)
			r.Get("/admin/hardware", a.handleAdminHardware)
			r.Get("/admin/update", a.handleUpdateCheck)

			// Admin mutations: allowed only from a trusted local request (not
			// tunneled, and a loopback/private peer). Tunneled requests and
			// public-IP direct hits are refused. See RequireLocal / remote.IsLocal.
			r.Group(func(r chi.Router) {
				r.Use(a.Auth.RequireLocal)
				r.Patch("/admin/config", a.handlePatchConfig)
				r.Post("/admin/scan", a.handleStartScan)
				r.Post("/admin/match", a.handleManualMatch)
				r.Post("/admin/update", a.handleUpdateApply)
				r.Delete("/profiles/{id}", a.handleDeleteProfile)
			})
			// stream / subtitles / home mount here in P3-P6.
		})
	})
}
