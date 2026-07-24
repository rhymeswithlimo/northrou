package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/ffmpeg"
	"github.com/rhymeswithlimo/northrou/backend/internal/service"
	"github.com/rhymeswithlimo/northrou/backend/internal/subtitles"
	"github.com/spf13/cobra"
)

// doctor runs the checks a confused operator would otherwise do by hand, in
// dependency order, each with a pass/warn/fail line. It exits non-zero when a
// real failure is found so it can gate scripts, but warnings (optional pieces
// missing) do not fail it.
func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check this server's setup and report anything broken",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()
			failed, warned := 0, 0
			for _, c := range runChecks(ctx, flagConfigPath) {
				fmt.Println(c.render())
				switch c.level {
				case checkFail:
					failed++
				case checkWarn:
					warned++
				}
			}
			switch {
			case failed > 0:
				return fmt.Errorf("%d check(s) failed", failed)
			case warned > 0:
				notice("Nothing is broken. %d warning(s) above are worth a look.", warned)
			default:
				notice("Everything checks out.")
			}
			return nil
		},
	}
}

type checkLevel int

const (
	checkPass checkLevel = iota
	checkWarn
	checkFail
)

type checkResult struct {
	name  string
	level checkLevel
	msg   string
}

func (c checkResult) render() string {
	mark := "ok  "
	switch c.level {
	case checkWarn:
		mark = "warn"
	case checkFail:
		mark = "FAIL"
	}
	return fmt.Sprintf("[%s] %-14s %s", mark, c.name, c.msg)
}

// runChecks performs every diagnostic, in dependency order: config first,
// because everything after reads it.
func runChecks(ctx context.Context, configPath string) []checkResult {
	var out []checkResult
	add := func(name string, level checkLevel, format string, args ...any) {
		out = append(out, checkResult{name: name, level: level, msg: fmt.Sprintf(format, args...)})
	}

	// Config.
	cfg, err := config.Load(configPath)
	switch {
	case errors.Is(err, os.ErrNotExist):
		add("config", checkWarn, "no config yet at %s - run 'northrou setup'", configPath)
		cfg = config.Default()
	case err != nil:
		add("config", checkFail, "unreadable: %v", err)
		return out // everything else depends on it
	default:
		add("config", checkPass, "%s", configPath)
	}

	// Data directory.
	probe := filepath.Join(cfg.Server.DataDir, ".doctor-probe")
	if err := os.MkdirAll(cfg.Server.DataDir, 0o755); err != nil {
		add("data dir", checkFail, "cannot create %s: %v", cfg.Server.DataDir, err)
	} else if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		add("data dir", checkFail, "not writable: %v (if the service runs as root, try: sudo northrou doctor)", err)
	} else {
		os.Remove(probe)
		add("data dir", checkPass, "%s", cfg.Server.DataDir)
	}

	// Media folders.
	folders := 0
	for _, kind := range []struct {
		label string
		dirs  []string
	}{{"movies", cfg.Media.MovieDirs}, {"shows", cfg.Media.ShowDirs}} {
		for _, dir := range kind.dirs {
			folders++
			info, err := os.Stat(dir)
			switch {
			case err != nil:
				add("media", checkFail, "%s folder unreadable: %s (%v)", kind.label, dir, err)
			case !info.IsDir():
				add("media", checkFail, "not a folder: %s", dir)
			default:
				add("media", checkPass, "%s: %s", kind.label, dir)
			}
		}
	}
	if folders == 0 {
		add("media", checkWarn, "no folders configured - add them with 'northrou setup' or 'northrou admin'")
	}

	// Port / running server.
	port := cfg.Server.Port
	if alreadyServing(port) {
		add("server", checkPass, "running and healthy on port %d (this only confirms "+
			"something answers on %d, not that it's using this config's data_dir: %s)",
			port, port, cfg.Server.DataDir)
	} else if ln, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(port))); err == nil {
		ln.Close()
		add("server", checkWarn, "not running (port %d is free) - 'northrou start' or 'northrou serve'", port)
	} else {
		add("server", checkFail, "port %d is taken by another program and Northrou is not answering on it - change [server] port in %s", port, configPath)
	}

	// Lid switch. A laptop pressed into service as a home server that still
	// suspends (or loops retrying a masked suspend) on lid close will drop
	// streams and interrupt scans; surfaced here since this is the check an
	// already-installed, already-running server hits, unlike the one at
	// `northrou install` time.
	if w := service.LidSwitchWarning(); w != "" {
		add("lid switch", checkWarn, "%s", w)
	}

	// ffmpeg. Locate only - doctor diagnoses, it does not kick off downloads.
	ffm := ffmpeg.NewManager(cfg.Server.DataDir, cfg.Transcode.PreferSystemFFmpeg)
	if paths, ok := ffm.Locate(); ok {
		if v, err := ffm.Version(ctx); err == nil {
			add("ffmpeg", checkPass, "%s (%s)", v, paths.FFmpeg)
		} else {
			add("ffmpeg", checkWarn, "found at %s but not runnable: %v", paths.FFmpeg, err)
		}
	} else {
		add("ffmpeg", checkWarn, "not downloaded yet - the server fetches it automatically on first run")
	}

	// Tesseract (optional, PGS subtitle OCR).
	if tess := subtitles.DetectTesseract(filepath.Join(cfg.Server.DataDir, "bin")); tess != "" {
		add("tesseract", checkPass, "%s", tess)
	} else {
		add("tesseract", checkWarn, "not found - PGS (BluRay image) subtitles won't be converted to text; SRT/ASS still work")
	}

	// TMDB.
	if cfg.TMDB.APIKey != "" {
		add("tmdb", checkPass, "API key set")
	} else {
		add("tmdb", checkWarn, "no API key - scans will find files but not posters/descriptions")
	}

	// Remote access.
	if !cfg.Remote.Enabled {
		add("remote", checkWarn, "off - this server is reachable on the home network only")
		return out
	}
	if cfg.Remote.ConnectionCode == "" {
		add("remote", checkFail, "enabled but no connection code - run 'northrou setup'")
		return out
	}
	if err := coordinatorReachable(ctx); err != nil {
		add("remote", checkFail, "coordinator unreachable (%v) - remote devices can't find this server; check the box's internet access", err)
	} else {
		add("remote", checkPass, "on - coordinator reachable, connection code set")
	}
	return out
}

// coordinatorReachable checks that the official coordinator answers at all.
// Any HTTP response counts: this diagnoses the box's network path, not the
// coordinator's API surface.
func coordinatorReachable(ctx context.Context) error {
	url := strings.TrimRight(config.DefaultCoordinationURL, "/") + "/"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}
