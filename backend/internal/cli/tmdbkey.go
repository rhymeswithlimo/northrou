package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newTMDBKeyCmd manages the TMDB API key after setup - the counterpart to the
// wizard's optional key step, for the operator who skipped it or wants to
// change or remove it. Like `cc` and `devices`, it drives the running daemon
// (so the change takes effect without a restart) and falls back to editing
// config.toml directly when nothing is running.
func newTMDBKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tmdb-key",
		Short: "Show, set, or remove this server's TMDB API key",
		Long: "The TMDB API key fetches posters, descriptions, cast, and artwork " +
			"for your library. It stays on this server. With no subcommand, report " +
			"whether a key is set.\n\n" +
			"On a box where Northrou runs as a system service (as root), run it " +
			"with sudo so it can read/write that config.",
		RunE: func(cmd *cobra.Command, args []string) error {
			set, err := tmdbKeyIsSet(cmd.Context())
			if err != nil {
				return err
			}
			if set {
				notice("A TMDB API key is set.\nReplace it with 'northrou tmdb-key set <key>', or remove it with 'northrou tmdb-key clear'.")
			} else {
				notice("No TMDB API key is set. Add one with 'northrou tmdb-key set <key>'.")
			}
			return nil
		},
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "set <key>",
			Short: "Set (or replace) the TMDB API key",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				key := strings.TrimSpace(args[0])
				if key == "" {
					return fmt.Errorf("the key is empty")
				}
				restarted, err := setTMDBKey(cmd.Context(), key)
				if err != nil {
					return err
				}
				msg := "TMDB API key saved. Run a scan to fill in missing artwork: northrou scan"
				if !restarted {
					msg = "TMDB API key saved to config. It takes effect when the server next starts."
				}
				notice("%s", msg)
				return nil
			},
		},
		&cobra.Command{
			Use:     "clear",
			Aliases: []string{"remove", "unset"},
			Short:   "Remove the TMDB API key",
			RunE: func(cmd *cobra.Command, args []string) error {
				if _, err := setTMDBKey(cmd.Context(), ""); err != nil {
					return err
				}
				notice("TMDB API key removed. New scans won't fetch posters or descriptions until you add one again.")
				return nil
			},
		},
	)
	return cmd
}

// tmdbKeyIsSet reports whether a key is configured, via the running daemon when
// up (which never echoes the key, only has_tmdb_key) or the config file.
func tmdbKeyIsSet(ctx context.Context) (bool, error) {
	cfg, err := loadConfigWithHint("tmdb-key")
	if err != nil {
		return false, err
	}
	if alreadyServing(cfg.Server.Port) {
		c, err := newLocalAPI(ctx, cfg.Server.Port)
		if err != nil {
			return false, err
		}
		var out struct {
			HasTMDBKey bool `json:"has_tmdb_key"`
		}
		if err := c.get(ctx, "/api/admin/config", &out); err != nil {
			return false, err
		}
		return out.HasTMDBKey, nil
	}
	return cfg.TMDB.APIKey != "", nil
}

// setTMDBKey sets the key to key (empty clears it). It PATCHes the running
// daemon when up, so the change is live; otherwise it writes config.toml
// directly. restarted reports whether the change is already in effect (daemon
// updated) versus pending the next start (file edited).
func setTMDBKey(ctx context.Context, key string) (live bool, err error) {
	cfg, err := loadConfigWithHint("tmdb-key")
	if err != nil {
		return false, err
	}
	if alreadyServing(cfg.Server.Port) {
		c, cerr := newLocalAPI(ctx, cfg.Server.Port)
		if cerr != nil {
			return false, cerr
		}
		body, _ := json.Marshal(map[string]string{"tmdb_api_key": key})
		if err := c.patch(ctx, "/api/admin/config", body, nil); err != nil {
			return false, err
		}
		return true, nil
	}
	cfg.TMDB.APIKey = key
	if err := cfg.Save(flagConfigPath); err != nil {
		return false, err
	}
	return false, nil
}
