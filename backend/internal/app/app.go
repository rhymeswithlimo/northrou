// Package app assembles Northrou's runtime dependencies (config, database,
// auth, ffmpeg manager, HTTP API/server, and in later phases the scanner,
// transcoder, and remote peer) into a single lifecycle-managed unit.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/api"
	"github.com/rhymeswithlimo/northrou/backend/internal/auth"
	"github.com/rhymeswithlimo/northrou/backend/internal/buildinfo"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/db"
	"github.com/rhymeswithlimo/northrou/backend/internal/email"
	"github.com/rhymeswithlimo/northrou/backend/internal/ffmpeg"
	"github.com/rhymeswithlimo/northrou/backend/internal/mediainfo"
	"github.com/rhymeswithlimo/northrou/backend/internal/metadata"
	"github.com/rhymeswithlimo/northrou/backend/internal/recommend"
	"github.com/rhymeswithlimo/northrou/backend/internal/remote"
	"github.com/rhymeswithlimo/northrou/backend/internal/scanner"
	"github.com/rhymeswithlimo/northrou/backend/internal/server"
	"github.com/rhymeswithlimo/northrou/backend/internal/subtitles"
	"github.com/rhymeswithlimo/northrou/backend/internal/transcode"
	"github.com/rhymeswithlimo/northrou/backend/internal/transcode/hwaccel"
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
	authSvc := auth.NewService(database, secret, email.New(cfg))
	ffm := ffmpeg.NewManager(cfg.Server.DataDir, cfg.Transcode.PreferSystemFFmpeg)

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

	return &App{
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
	}, nil
}

// Run starts the HTTP server and blocks until ctx is cancelled, then shuts
// down gracefully. It ensures ffmpeg is available in the background so a slow
// first-run download does not delay the API.
func (a *App) Run(ctx context.Context) error {
	if err := a.Server.Start(); err != nil {
		return err
	}

	go a.ensureFFmpeg(ctx)

	// Remote access: register with the coordination server and tunnel the API
	// over WebRTC. Local-network clients bypass this entirely by connecting to
	// the server's LAN address directly.
	if a.Cfg.Remote.Enabled {
		go a.startRemote(ctx)
	}

	<-ctx.Done()
	slog.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return a.Server.Shutdown(shutCtx)
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
	wsURL := coordinatorWSURL(a.Cfg.Remote.CoordinationURL)
	peer := remote.NewPeer(wsURL, serverID, code, a.Server.Handler())
	slog.Info("remote access enabled", "coordinator", wsURL, "code", code)
	peer.Run(ctx)
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
	a.Scanner.SetProber(mediainfo.New(paths.FFprobe))

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
		a.Cfg.Transcode.Tonemap, a.Cfg.Transcode.AllowSoftware4K, a.Cfg.Transcode.MaxBitrateKbps)
	a.API.SetStreamer(streamer)

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
