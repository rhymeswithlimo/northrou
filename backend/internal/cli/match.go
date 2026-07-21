package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rhymeswithlimo/northrou/backend/internal/app"
	"github.com/rhymeswithlimo/northrou/backend/internal/mediainfo"
	"github.com/rhymeswithlimo/northrou/backend/internal/model"
	"github.com/rhymeswithlimo/northrou/backend/internal/subtitles"
	"github.com/spf13/cobra"
)

// newMatchCmd is the operator escape hatch for the naming long tail: force a
// file that would not auto-match (or matched wrong) to a specific TMDB id.
func newMatchCmd() *cobra.Command {
	var tmdbID int64
	var season, episode int
	var asTV bool
	cmd := &cobra.Command{
		Use:   "match <file> --tmdb-id <id> [--tv --season N --episode N]",
		Short: "Force a file to a specific TMDB title",
		Long: "Manually link a media file to a TMDB movie or episode, bypassing " +
			"filename parsing. Use it for files that land in the unmatched list or " +
			"matched to the wrong title. For a movie: `northrou match Movie.mkv " +
			"--tmdb-id 27205`. For an episode add --tv with --season/--episode.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("cannot read %q: %w", path, err)
			}
			if tmdbID == 0 {
				return fmt.Errorf("--tmdb-id is required")
			}
			kind := model.KindMovie
			if asTV {
				kind = model.KindEpisode
				if season == 0 || episode == 0 {
					return fmt.Errorf("--season and --episode are required with --tv")
				}
			}

			a, err := app.New(flagConfigPath)
			if err != nil {
				return err
			}
			defer a.Close()
			if !a.TMDB.Enabled() {
				return fmt.Errorf("no TMDB API key configured; set one first")
			}

			ctx := context.Background()
			if paths, err := a.FFmpeg.EnsureInstalled(ctx); err == nil {
				a.Scanner.SetProber(mediainfo.New(paths.FFprobe, mediainfo.WithDeepDolbyVision(a.Cfg.Transcode.ProbeDolbyVision)))
				tess := subtitles.DetectTesseract(filepath.Join(a.Cfg.Server.DataDir, "bin"))
				ex := subtitles.New(a.DB, paths.FFmpeg, tess, a.Cfg.Server.DataDir)
				ex.Start(ctx)
				a.Scanner.SetSubtitleExtractor(ex)
			}

			if err := a.Scanner.ForceMatch(ctx, path, kind, tmdbID, season, episode); err != nil {
				return err
			}
			fmt.Printf("Matched %s -> TMDB %d.\n", filepath.Base(path), tmdbID)
			return nil
		},
	}
	cmd.Flags().Int64Var(&tmdbID, "tmdb-id", 0, "TMDB id of the movie or show to link")
	cmd.Flags().BoolVar(&asTV, "tv", false, "treat the file as a TV episode")
	cmd.Flags().IntVar(&season, "season", 0, "season number (with --tv)")
	cmd.Flags().IntVar(&episode, "episode", 0, "episode number (with --tv)")
	return cmd
}
