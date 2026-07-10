package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"path/filepath"

	"github.com/rhymeswithlimo/northrou/backend/internal/app"
	"github.com/rhymeswithlimo/northrou/backend/internal/mediainfo"
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
				a.Scanner.SetProber(mediainfo.New(paths.FFprobe))
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
			if err := a.Scanner.Scan(ctx, movieDirs, showDirs); err != nil {
				return err
			}
			p := a.Scanner.Progress()
			fmt.Printf("Done. processed=%d matched=%d unmatched=%d\n", p.Processed, p.Matched, p.Unmatched)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asTV, "tv", false,
		"treat the given paths as TV episodes (default: detect movies vs episodes by filename)")
	return cmd
}
