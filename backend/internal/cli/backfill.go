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

// newBackfillMetadataCmd re-pulls the richer TMDB metadata that powers
// recommendations - keyword tags, production companies (studio rows), and TV
// creators - for titles that were matched before those fields were ingested.
// Newly scanned titles get them for free; this one-off fills in the existing
// library. It fetches full details (one request per title, which returns all of
// it at once) only for titles still missing companies, so it is safe to re-run
// and resumes if interrupted. Best run while the daemon is stopped.
func newBackfillMetadataCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "backfill-metadata",
		Aliases: []string{"backfill-keywords"}, // prior name; keeps old muscle memory working
		Short:   "Fetch keyword/studio/creator metadata for existing library titles",
		Long: "Populate keyword tags, production companies, and TV creators for " +
			"movies and shows matched before those fields were ingested. They drive " +
			"thematic recommendations, 'similar titles', and studio/creator browse " +
			"rows. One TMDB request per title; only titles still missing this data " +
			"are fetched, so it is safe to re-run and resumes if stopped. Best run " +
			"while the daemon is stopped; restart it afterwards so it reloads.",
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
					"different config than the running service, it will write into a "+
					"database the service never reads. Pass --config to match.",
					a.Cfg.Server.Port)
			}

			ctx, stop := signal.NotifyContext(context.Background(),
				os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Companies are the field every pre-upgrade title lacks, so they drive
			// the work list; each full fetch also (re)writes keywords and creators.
			movies, err := a.DB.MoviesMissingCompanies(ctx)
			if err != nil {
				return fmt.Errorf("list movies: %w", err)
			}
			shows, err := a.DB.ShowsMissingCompanies(ctx)
			if err != nil {
				return fmt.Errorf("list shows: %w", err)
			}
			total := len(movies) + len(shows)
			if total == 0 {
				notice("Every title already has this metadata. Nothing to do.")
				return nil
			}
			fmt.Printf("Backfilling metadata for %d titles (%d movies, %d shows)…\n",
				total, len(movies), len(shows))

			var done, updated, failed int
			report := func() {
				if isTerminal(os.Stdout) {
					fmt.Printf("\r  %d/%d processed, %d updated, %d failed", done, total, updated, failed)
				}
			}

			for _, m := range movies {
				if ctx.Err() != nil {
					break
				}
				d, err := a.TMDB.MovieDetails(ctx, m.TMDBID)
				if err != nil {
					failed++
				} else {
					_ = a.DB.SetMovieKeywords(ctx, m.ID, d.KeywordNames())
					if err := a.DB.SetMovieCompanies(ctx, m.ID, d.CompanyNames()); err != nil {
						failed++
					} else {
						updated++
					}
				}
				done++
				report()
			}
			for _, s := range shows {
				if ctx.Err() != nil {
					break
				}
				d, err := a.TMDB.TVDetails(ctx, s.TMDBID)
				if err != nil {
					failed++
				} else {
					_ = a.DB.SetShowKeywords(ctx, s.ID, d.KeywordNames())
					_ = a.DB.SetShowCreators(ctx, s.ID, d.CreatorNames())
					if err := a.DB.SetShowCompanies(ctx, s.ID, d.CompanyNames()); err != nil {
						failed++
					} else {
						updated++
					}
				}
				done++
				report()
			}
			if isTerminal(os.Stdout) {
				fmt.Println()
			}

			// A running daemon is a separate process and memoizes the library until
			// its next scan or restart, so it keeps serving the old data after this
			// writes the new. We can't reach its cache; tell the user to restart.
			if errors.Is(ctx.Err(), context.Canceled) {
				notice("Interrupted after %d/%d. Re-run to finish the rest.", done, total)
				return nil
			}
			fmt.Printf("Done: %d updated, %d failed.\n", updated, failed)
			if alreadyServing(a.Cfg.Server.Port) {
				notice("A northrou service is running. Restart it (`northrou restart`) so " +
					"it reloads the library and the new metadata takes effect - it caches " +
					"the catalog until then.")
			}
			return nil
		},
	}
}
