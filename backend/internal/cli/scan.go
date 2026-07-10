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
	return &cobra.Command{
		Use:   "scan",
		Short: "Scan configured media folders now and exit",
		Long: "Run a one-off library scan against the configured movie and TV " +
			"folders, then print a summary. Best run while the daemon is stopped.",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.New(flagConfigPath)
			if err != nil {
				return err
			}
			defer a.Close()

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

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
			if err := a.Scanner.Scan(ctx, a.Cfg.Media.MovieDirs, a.Cfg.Media.ShowDirs); err != nil {
				return err
			}
			p := a.Scanner.Progress()
			fmt.Printf("Done. processed=%d matched=%d unmatched=%d\n", p.Processed, p.Matched, p.Unmatched)
			return nil
		},
	}
}
