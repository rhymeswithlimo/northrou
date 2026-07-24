package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/rhymeswithlimo/northrou/backend/internal/app"
	"github.com/spf13/cobra"
)

// newBackfillKeywordsCmd re-pulls TMDB keyword tags for titles that were matched
// before keyword ingestion existed. Keywords power thematic recommendations and
// similarity; newly scanned titles get them for free, but the existing library
// has none until this one-off runs. It only fetches titles missing keywords, so
// it is safe to re-run and resumes where it left off if interrupted.
func newBackfillKeywordsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "backfill-keywords",
		Short: "Fetch TMDB keyword tags for existing library titles",
		Long: "Populate keyword tags for movies and shows that were matched before " +
			"keyword ingestion was added. Keywords are the thematic signal behind " +
			"recommendations and 'similar titles'. Uses TMDB's lightweight keyword " +
			"endpoint (one small request per title). Only titles that have no " +
			"keywords are fetched, so it is safe to re-run and resumes if stopped. " +
			"Best run while the daemon is stopped.",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := app.New(flagConfigPath)
			if err != nil {
				return err
			}
			defer a.Close()

			if !a.TMDB.Enabled() {
				return fmt.Errorf("no TMDB API key set; run `northrou tmdb-key` first")
			}
			if alreadyServing(a.Cfg.Server.Port) {
				notice("A northrou service is already running on port %d. This command "+
					"opens its own copy of the database directly. If it resolved a "+
					"different config than the running service, it will write keywords "+
					"into a database the service never reads. Pass --config to match.",
					a.Cfg.Server.Port)
			}

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			movies, err := a.DB.MoviesMissingKeywords(ctx)
			if err != nil {
				return fmt.Errorf("list movies: %w", err)
			}
			shows, err := a.DB.ShowsMissingKeywords(ctx)
			if err != nil {
				return fmt.Errorf("list shows: %w", err)
			}
			total := len(movies) + len(shows)
			if total == 0 {
				notice("Every title already has keywords. Nothing to do.")
				return nil
			}
			fmt.Printf("Backfilling keywords for %d titles (%d movies, %d shows)…\n",
				total, len(movies), len(shows))

			var done, tagged, failed int
			report := func() {
				if isTerminal(os.Stdout) {
					fmt.Printf("\r  %d/%d processed, %d tagged, %d failed", done, total, tagged, failed)
				}
			}

			for _, m := range movies {
				if ctx.Err() != nil {
					break
				}
				names, err := a.TMDB.MovieKeywords(ctx, m.TMDBID)
				if err != nil {
					failed++
				} else {
					if len(names) > 0 {
						if err := a.DB.SetMovieKeywords(ctx, m.ID, names); err != nil {
							failed++
						} else {
							tagged++
						}
					}
				}
				done++
				report()
			}
			for _, s := range shows {
				if ctx.Err() != nil {
					break
				}
				names, err := a.TMDB.TVKeywords(ctx, s.TMDBID)
				if err != nil {
					failed++
				} else {
					if len(names) > 0 {
						if err := a.DB.SetShowKeywords(ctx, s.ID, names); err != nil {
							failed++
						} else {
							tagged++
						}
					}
				}
				done++
				report()
			}
			if isTerminal(os.Stdout) {
				fmt.Println()
			}

			// A running daemon is a separate process and memoizes the library
			// (features + content vectors) until its next scan or restart, so it
			// will keep serving keyword-less vectors after this writes them. We
			// can't reach its in-memory cache from here; tell the user to restart.
			if errors.Is(ctx.Err(), context.Canceled) {
				notice("Interrupted after %d/%d. Re-run to finish the rest.", done, total)
				return nil
			}
			fmt.Printf("Done: %d tagged, %d had no keywords, %d failed.\n",
				tagged, done-tagged-failed, failed)
			if alreadyServing(a.Cfg.Server.Port) {
				notice("A northrou service is running. Restart it (`northrou restart`) so " +
					"it reloads the library and the new keywords take effect - it caches " +
					"the catalog until then.")
			}
			return nil
		},
	}
}
