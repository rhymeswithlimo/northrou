package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"path/filepath"

	"github.com/rhymeswithlimo/northrou/backend/internal/app"
	"github.com/rhymeswithlimo/northrou/backend/internal/mediainfo"
	"github.com/rhymeswithlimo/northrou/backend/internal/scanner"
	"github.com/rhymeswithlimo/northrou/backend/internal/subtitles"
	"github.com/spf13/cobra"
)

func newScanCmd() *cobra.Command {
	var asTV bool
	cmd := &cobra.Command{
		Use:   "scan [path...]",
		Short: "Scan a folder or drive now and exit",
		Long: "Run a one-off library scan and print a summary. Point it at one or " +
			"more folders or drives to scan those, e.g. `northrou scan /media/movies` " +
			"or `northrou scan D:\\media`. Movies and TV episodes are told apart by " +
			"filename; pass --tv to force everything under the given paths to be " +
			"treated as TV episodes (useful for shows with messy names). With no " +
			"path, the movie_dirs and show_dirs from config are scanned instead. " +
			"Best run while the daemon is stopped.",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.New(flagConfigPath)
			if err != nil {
				return err
			}
			defer a.Close()

			// scan opens its own database directly; it never talks to a
			// running server. If something is already answering on this
			// config's port, it's almost certainly the installed service,
			// and if this process resolved a different config/data_dir than
			// that service uses (e.g. run as a different user, so a
			// different $HOME), this scan silently updates an unrelated
			// database instead of the one actually serving the library.
			if alreadyServing(a.Cfg.Server.Port) {
				notice("Northrou is already running as a service on port %d.\n"+
					"This command opens its own copy of the database directly and does "+
					"not talk to that running server. It resolved:\n"+
					"  config:   %s\n"+
					"  data_dir: %s\n"+
					"If the running service uses a different config (e.g. it runs as "+
					"root via 'sudo systemctl status northrou' -> ExecStart), this scan "+
					"will not affect the library it actually serves. Pass --config to "+
					"match the service, e.g.:\n"+
					"  sudo northrou scan --config /root/.config/northrou/config.toml",
					a.Cfg.Server.Port, flagConfigPath, a.Cfg.Server.DataDir)
			}

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Decide what to scan: explicit paths win over configured dirs.
			var movieDirs, showDirs []string
			if len(args) > 0 {
				for _, p := range args {
					if _, err := os.Stat(p); err != nil {
						return fmt.Errorf("cannot scan %q: %w", p, err)
					}
				}
				if asTV {
					showDirs = args
				} else {
					movieDirs = args
				}
			} else {
				if asTV {
					return fmt.Errorf("--tv has no effect without a path; give a folder or drive to scan")
				}
				movieDirs = a.Cfg.Media.MovieDirs
				showDirs = a.Cfg.Media.ShowDirs
				if len(movieDirs) == 0 && len(showDirs) == 0 {
					return fmt.Errorf("nothing to scan: pass a folder or drive, or set movie_dirs/show_dirs in config")
				}
			}

			// Ensure ffprobe is available so technical metadata is captured,
			// and wire the subtitle pipeline for extraction/OCR.
			if paths, err := a.FFmpeg.EnsureInstalled(ctx); err == nil {
				a.Scanner.SetProber(mediainfo.New(paths.FFprobe, mediainfo.WithDeepDolbyVision(a.Cfg.Transcode.ProbeDolbyVision)))
				tess := subtitles.DetectTesseract(filepath.Join(a.Cfg.Server.DataDir, "bin"))
				ex := subtitles.New(a.DB, paths.FFmpeg, tess, a.Cfg.Server.DataDir)
				ex.Start(ctx)
				a.Scanner.SetSubtitleExtractor(ex)
			} else {
				fmt.Fprintln(os.Stderr, "warning: ffprobe unavailable, technical metadata will be skipped")
			}

			if !a.TMDB.Enabled() {
				fmt.Fprintln(os.Stderr, "warning: no TMDB API key set, files will be flagged as unmatched")
			}

			fmt.Println("Scanning…")
			progressDone := make(chan struct{})
			var progressWG sync.WaitGroup
			progressWG.Go(func() {
				printScanProgress(a.Scanner, progressDone)
			})
			scanErr := a.Scanner.Scan(ctx, movieDirs, showDirs)
			close(progressDone)
			progressWG.Wait() // let the last redraw land before printing below
			if isTerminal(os.Stdout) {
				fmt.Println() // move off the in-place progress line
			}
			if scanErr != nil {
				return scanErr
			}

			p := a.Scanner.Progress()
			fmt.Printf("Done. processed=%d matched=%d unmatched=%d\n", p.Processed, p.Matched, p.Unmatched)
			if untouched := p.Processed - p.Matched - p.Unmatched; untouched > 0 {
				fmt.Printf("(%d file(s) not newly matched: already scanned and unchanged, "+
					"or skipped as an unreadable file/duplicate - see logs for detail)\n", untouched)
			}

			unmatched, err := a.DB.ListUnmatched(ctx)
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not read unmatched-file reasons: %v\n", err)
			} else if len(unmatched) > 0 {
				fmt.Printf("\n%d file(s) could not be matched:\n", len(unmatched))
				for _, u := range unmatched {
					fmt.Printf("  %s\n    %s\n", u.Path, u.Reason)
				}
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asTV, "tv", false,
		"treat the given paths as TV episodes (default: detect movies vs episodes by filename)")
	return cmd
}

// printScanProgress polls sc.Progress() until done is closed, so a long scan
// (real libraries with ffprobe + TMDB lookups per file can run tens of
// minutes) doesn't leave the operator staring at a silent terminal. On a real
// terminal it redraws one line in place; piped/redirected output instead gets
// occasional plain lines, since a redrawn line is unreadable noise there.
func printScanProgress(sc *scanner.Scanner, done <-chan struct{}) {
	tty := isTerminal(os.Stdout)
	interval := 10 * time.Second
	if tty {
		interval = 500 * time.Millisecond
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			if line, ok := formatScanProgress(sc.Progress(), time.Now()); ok {
				if tty {
					fmt.Printf("\r\033[K%s", line)
				} else {
					fmt.Println(line)
				}
			}
		}
	}
}

// formatScanProgress renders one progress-line snapshot, or ok=false when
// there's nothing worth printing yet (total not known). Pulled out of
// printScanProgress as a pure function of (p, now) so the ETA math and
// formatting are unit-testable without racing a real ticker/scanner.
func formatScanProgress(p scanner.Progress, now time.Time) (line string, ok bool) {
	if p.Total == 0 {
		return "", false
	}
	pct := 100 * p.Processed / p.Total
	line = fmt.Sprintf("Scanning… %d/%d (%d%%) matched=%d unmatched=%d",
		p.Processed, p.Total, pct, p.Matched, p.Unmatched)
	// ETA needs at least one completed file to have a rate to project from;
	// per-file cost varies a lot (ffprobe + TMDB vs. an already-scanned
	// skip), so this is a rough estimate, not a promise.
	if p.Processed > 0 && p.Processed < p.Total {
		perFile := now.Sub(p.StartedAt) / time.Duration(p.Processed)
		eta := perFile * time.Duration(p.Total-p.Processed)
		line += fmt.Sprintf(" - ETA %s", eta.Round(time.Second))
	}
	if p.CurrentFile != "" {
		line += " - " + filepath.Base(p.CurrentFile)
	}
	return line, true
}
