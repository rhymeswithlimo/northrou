// Package app assembles Northrou's runtime dependencies (config, database,
// auth, ffmpeg manager, HTTP API/server, and in later phases the scanner,
// transcoder, and remote peer) into a single lifecycle-managed unit.
package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/api"
	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/buildinfo"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/ffmpeg"
	"github.com/rhymeswithlimo/northrou/backend/internal/logging"
	"github.com/rhymeswithlimo/northrou/backend/internal/mediainfo"
	"github.com/rhymeswithlimo/northrou/backend/internal/metadata"
	"github.com/rhymeswithlimo/northrou/backend/internal/recommend"
	"github.com/rhymeswithlimo/northrou/backend/internal/remote"
	"github.com/rhymeswithlimo/northrou/backend/internal/scanner"
	"github.com/rhymeswithlimo/northrou/backend/internal/server"
	"github.com/rhymeswithlimo/northrou/backend/internal/subtitles"
	"github.com/rhymeswithlimo/northrou/backend/internal/transcode"
	"github.com/rhymeswithlimo/northrou/backend/internal/transcode/hwaccel"
	"github.com/rhymeswithlimo/northrou/backend/internal/update"
)

// App holds long-lived runtime dependencies shared across subsystems.
type App struct {
	Cfg        *config.Config
	ConfigPath string
	DB         *db.DB
	Auth       *auth.Service
	FFmpeg     *ffmpeg.Manager
	TMDB       *metadata.Client
	Images     *metadata.ImageCache
	Scanner    *scanner.Scanner
	API        *api.API
	Server     *server.Server
	FirstRun   bool

	// sessions is set once ensureFFmpeg builds the streamer. Read from
	// autoUpdate's goroutine to know whether it is safe to apply a pending
	// update, concurrently with ensureFFmpeg's write, hence atomic. Unset
	// (before ffmpeg is ready) means nothing can be streaming yet.
	sessions atomic.Pointer[transcode.SessionManager]

	// RunAsService marks that Run is executing under the OS service manager
	// (set by internal/service before calling Run) rather than a foreground
	// `northrou serve`. autoUpdate only runs in the former case: applying an
	// update and exiting relies on the service manager restarting the
	// process, which does not happen for a terminal-attached foreground run.
	RunAsService bool

	// Remote-peer lifecycle. bgCtx is the parent context for background
	// subsystems, set by StartBackground; remoteCancel is non-nil while the
	// peer goroutine runs. Guarded by remoteMu because SyncRemote is called
	// both at boot and from API handlers when setup or a settings PATCH flips
	// Remote.Enabled at runtime.
	remoteMu     sync.Mutex
	bgCtx        context.Context
	remoteCancel context.CancelFunc
}

// New builds an App from the config at configPath, opening and migrating the
// database and assembling the HTTP API. FirstRun is true when no config file
// existed yet.
func New(configPath string) (*App, error) {
	cfg, firstRun, err := config.LoadOrInit(configPath)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	dbPath := filepath.Join(cfg.Server.DataDir, "northrou.db")
	database, err := db.Open(dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	secret, err := auth.LoadOrCreateSecret(cfg.Server.DataDir)
	if err != nil {
		database.Close()
		return nil, fmt.Errorf("auth secret: %w", err)
	}
	authSvc := auth.NewService(database, secret)
	ffm := ffmpeg.NewManager(cfg.Server.DataDir, cfg.Transcode.PreferSystemFFmpeg)

	// The connection code is the sole credential remote clients pair with, so
	// every configured server must have one. A server upgraded from a build that
	// predates code-only auth (or that never enabled remote) may lack an id/code;
	// mint and persist them at boot so `northrou cc` works and the apps can pair.
	// First-run boxes have no config file yet: setup writes these instead.
	if !firstRun && (cfg.Remote.ServerID == "" || cfg.Remote.ConnectionCode == "") {
		if cfg.Remote.ServerID == "" {
			cfg.Remote.ServerID = randomServerID()
		}
		if cfg.Remote.ConnectionCode == "" {
			cfg.Remote.ConnectionCode = api.NewConnectionCode()
		}
		if err := cfg.Save(configPath); err != nil {
			slog.Warn("could not persist generated connection code", "err", err)
		} else {
			slog.Info("generated this server's connection code (share it to pair apps)", "code", cfg.Remote.ConnectionCode)
		}
	}

	tmdb := metadata.NewClient(cfg.TMDB.APIKey, cfg.TMDB.Language)
	images := metadata.NewImageCache(cfg.Server.DataDir)
	// Prober is attached once ffmpeg resolves (see Run/ensureFFmpeg).
	scan := scanner.New(database, tmdb, images, nil)
	rec := recommend.New(database)

	apiSrv := api.New(api.Deps{
		DB:         database,
		Auth:       authSvc,
		Cfg:        cfg,
		ConfigPath: configPath,
		Scanner:    scan,
		Recommend:  rec,
		TMDB:       tmdb,
		ImagesDir:  images.Dir(),
	})

	addr := net.JoinHostPort(cfg.Server.BindAddr, strconv.Itoa(cfg.Server.Port))
	httpSrv := server.New(addr, apiSrv)

	ver, _ := database.Version()
	slog.Info("northrou assembled",
		"version", buildinfo.Version,
		"platform", buildinfo.Platform(),
		"data_dir", cfg.Server.DataDir,
		"db_version", ver,
		"first_run", firstRun,
	)

	a := &App{
		Cfg:        cfg,
		ConfigPath: configPath,
		DB:         database,
		Auth:       authSvc,
		FFmpeg:     ffm,
		TMDB:       tmdb,
		Images:     images,
		Scanner:    scan,
		API:        apiSrv,
		Server:     httpSrv,
		FirstRun:   firstRun,
	}
	// Let the API start/stop the remote peer when setup or a settings PATCH
	// flips Remote.Enabled, and bounce it when the connection code rotates,
	// instead of either change waiting for a restart.
	apiSrv.SetRemoteSync(a.SyncRemote, a.RestartRemote)
	return a, nil
}

// Run starts the HTTP server and blocks until ctx is cancelled, then shuts
// down gracefully. It ensures ffmpeg is available in the background so a slow
// first-run download does not delay the API.
func (a *App) Run(ctx context.Context) error {
	if err := a.Server.Start(); err != nil {
		return err
	}
	a.StartBackground(ctx)

	<-ctx.Done()
	slog.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return a.Server.Shutdown(shutCtx)
}

// StartBackground launches the subsystems that accompany a serving App:
// the ffmpeg resolver, the remote peer (when enabled), and - under a service
// manager - the auto-updater. Split from Run so `northrou setup`, which owns
// the foreground with a TUI, gets the same fully-functional server.
func (a *App) StartBackground(ctx context.Context) {
	a.remoteMu.Lock()
	a.bgCtx = ctx
	a.remoteMu.Unlock()

	// From here on the daemon's log lines also land in data_dir/logs, where
	// `northrou logs` and the settings page read them back.
	logging.AttachFile(a.Cfg.Server.DataDir)

	go a.ensureFFmpeg(ctx)

	// Remote access: register with the coordination server and tunnel the API
	// over WebRTC. A browser served off this box talks to it directly; the apps
	// reach it through this tunnel.
	a.SyncRemote()

	if a.RunAsService {
		go a.autoUpdate(ctx)
	}
}

// SyncRemote starts or stops the remote peer so it matches Cfg.Remote.Enabled.
// Safe to call any time; before StartBackground it is a no-op (Run will sync).
// This is what makes "enable remote access" in setup or settings take effect
// immediately instead of on the next restart.
func (a *App) SyncRemote() {
	a.remoteMu.Lock()
	defer a.remoteMu.Unlock()

	enabled := a.Cfg.Remote.Enabled
	switch {
	case enabled && a.remoteCancel == nil && a.bgCtx != nil:
		ctx, cancel := context.WithCancel(a.bgCtx)
		a.remoteCancel = cancel
		go a.startRemote(ctx)
	case !enabled && a.remoteCancel != nil:
		a.remoteCancel()
		a.remoteCancel = nil
		slog.Info("remote access disabled; peer stopped")
	}
}

// RestartRemote stops a running remote peer and starts a fresh one, so it
// re-registers with the coordinator under the current (e.g. just-rotated)
// connection code. A no-op when remote access is off or nothing has started.
func (a *App) RestartRemote() {
	a.remoteMu.Lock()
	if a.remoteCancel != nil {
		a.remoteCancel()
		a.remoteCancel = nil
	}
	a.remoteMu.Unlock()
	a.SyncRemote()
}

// startRemote launches the WebRTC peer that brokers remote client connections
// through the coordination server.
func (a *App) startRemote(ctx context.Context) {
	code := a.Cfg.Remote.ConnectionCode
	serverID := a.Cfg.Remote.ServerID
	if code == "" || serverID == "" {
		slog.Warn("remote access enabled but connection code/server id missing; run setup")
		return
	}
	wsURL := coordinatorWSURL(config.DefaultCoordinationURL)
	peer := remote.NewPeer(wsURL, serverID, code, a.Server.Handler())
	slog.Info("remote access enabled", "coordinator", wsURL, "code", code)
	peer.Run(ctx)
}

// randomServerID returns a random 16-byte hex identifier for coordinator
// registration.
func randomServerID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// coordinatorWSURL converts a coordination base URL to its /ws WebSocket URL.
func coordinatorWSURL(base string) string {
	switch {
	case strings.HasPrefix(base, "https://"):
		return "wss://" + strings.TrimPrefix(base, "https://") + "/ws"
	case strings.HasPrefix(base, "http://"):
		return "ws://" + strings.TrimPrefix(base, "http://") + "/ws"
	case strings.HasPrefix(base, "ws://"), strings.HasPrefix(base, "wss://"):
		return strings.TrimRight(base, "/") + "/ws"
	default:
		return "wss://" + base + "/ws"
	}
}

// autoUpdate checks GitHub for a newer release every few hours and, once no
// stream is active, downloads, verifies, and applies it, then exits so the
// service manager restarts into the new binary (see internal/service, which
// configures each OS's manager to restart on exit). It never runs for dev
// builds, when disabled in config, or inside a container, where the update
// path is pulling a new image rather than self-mutating the running one.
func (a *App) autoUpdate(ctx context.Context) {
	if buildinfo.Version == "" || buildinfo.Version == "dev" {
		return
	}
	if a.Cfg.Update.AutoUpdateDisabled {
		slog.Info("auto-update disabled by config")
		return
	}
	if inContainer() {
		slog.Info("auto-update skipped: running in a container; update by pulling a new image instead")
		return
	}
	u := update.New(update.DefaultRepo, buildinfo.Version)
	update.NewWatcher(u, a.activeSessions).Run(ctx)
}

// activeSessions reports the current stream count for autoUpdate's quiet-
// window check. Before ffmpeg is ready (sessions not yet built) nothing can be
// streaming, so it reports zero rather than blocking on a subsystem that does
// not exist yet.
func (a *App) activeSessions() int {
	sm := a.sessions.Load()
	if sm == nil {
		return 0
	}
	return sm.Count()
}

// inContainer reports whether the process is running inside a Docker/Podman
// container, where self-replacing the binary would be lost on the next
// `docker compose up` and updates should instead come from a new image tag.
func inContainer() bool {
	for _, p := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// ensureFFmpeg makes the managed ffmpeg available, logging progress. Failure is
// non-fatal: the API still serves; streaming just cannot transcode until it is
// resolved.
func (a *App) ensureFFmpeg(ctx context.Context) {
	paths, err := a.FFmpeg.EnsureInstalled(ctx)
	if err != nil {
		slog.Warn("ffmpeg not available; transcoding/probing disabled until resolved", "err", err)
		return
	}
	// ffprobe is now available: attach it to the scanner for technical metadata.
	a.Scanner.SetProber(mediainfo.New(paths.FFprobe, mediainfo.WithDeepDolbyVision(a.Cfg.Transcode.ProbeDolbyVision)))

	// Wire the subtitle pipeline now that ffmpeg exists. Tesseract is optional;
	// without it, PGS tracks are marked "skipped".
	binDir := filepath.Join(a.Cfg.Server.DataDir, "bin")
	tess := subtitles.DetectTesseract(binDir)
	if tess == "" {
		slog.Info("tesseract not found; PGS subtitle OCR disabled (SRT/ASS still work)")
	}
	ex := subtitles.New(a.DB, paths.FFmpeg, tess, a.Cfg.Server.DataDir)
	ex.Start(ctx)
	a.Scanner.SetSubtitleExtractor(ex)

	// Detect hardware acceleration and build the streamer.
	hw := hwaccel.Detect(ctx, paths.FFmpeg, a.Cfg.Transcode.HWAccel)
	sm := transcode.NewSessionManager(hw)
	streamer := transcode.NewStreamer(paths.FFmpeg, a.Cfg.Server.DataDir, hw, sm,
		a.Cfg.Transcode.Tonemap, a.Cfg.Transcode.AllowSoftware4K, a.Cfg.Transcode.MaxBitrateKbps,
		a.Cfg.Media.PreferredAudioLangs)
	a.API.SetStreamer(streamer)
	a.sessions.Store(sm)

	// Playback is the priority on weak hardware: pause background scan and
	// subtitle OCR work while any stream is active (they resume when idle).
	a.Scanner.SetPlaybackGate(sm.Count)
	ex.SetPlaybackGate(sm.Count)

	if v, err := a.FFmpeg.Version(ctx); err == nil {
		slog.Info("ffmpeg ready", "version", v)
	}
}

// Close releases all resources held by the App.
func (a *App) Close() error {
	if a.DB != nil {
		return a.DB.Close()
	}
	return nil
}

// ListenAddr returns the configured HTTP address.
func (a *App) ListenAddr() string {
	return net.JoinHostPort(a.Cfg.Server.BindAddr, strconv.Itoa(a.Cfg.Server.Port))
}
