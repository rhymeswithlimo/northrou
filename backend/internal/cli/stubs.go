package cli

import (
	"fmt"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/buildinfo"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/service"
	"github.com/rhymeswithlimo/northrou/backend/internal/tui"
	"github.com/rhymeswithlimo/northrou/backend/internal/update"
	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install Northrou as a system service and start it",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := service.Install(flagConfigPath); err != nil {
				return needsRoot(cmd, err)
			}
			notice("Northrou installed and started as a system service.")
			if warning := service.LidSwitchWarning(); warning != "" {
				notice("warning: %s", warning)
			}
			return nil
		},
	}
}

func newUninstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and uninstall the Northrou system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := service.Uninstall(flagConfigPath); err != nil {
				return needsRoot(cmd, err)
			}
			notice("Northrou service removed.")
			return nil
		},
	}
}

func newAdminCmd() *cobra.Command {
	var addr string
	cmd := &cobra.Command{
		Use:   "admin",
		Short: "Launch the admin dashboard (TUI)",
		Long: "Open the Northrou admin dashboard in your terminal. Connects to " +
			"the running server's local API and shows active streams, hardware " +
			"acceleration, capacity, and scan/library status.\n\n" +
			"The Library tab is also where this server's media folders are set. " +
			"They live here rather than in the apps because the paths describe " +
			"this machine's own disks. With --addr the dashboard is read-only: " +
			"another server's folders are edited from that server.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// No --addr means we are the box: the config we would edit is
			// meant to be the config the daemon reads, so folder editing is
			// offered - but that's only true if this process resolved the
			// same config file the daemon did, which depends on running as
			// the same user (config paths are per-user $HOME/XDG dirs). A
			// mismatch here silently edits a file the daemon never reads,
			// so flag it whenever a server already answers on the port:
			// that's the daemon, and it's worth a beat to confirm this is
			// its config before changing folders "successfully" into the
			// void.
			local := addr == ""
			base := addr
			if local {
				cfg, _, err := config.LoadOrInit(flagConfigPath)
				if err != nil {
					return err
				}
				port := cfg.Server.Port
				base = fmt.Sprintf("http://localhost:%d", port)
				if alreadyServing(port) {
					notice("A northrou service is already running on port %d. Library "+
						"changes made here are written to:\n  %s\n"+
						"If the running service was installed for a different user (e.g. "+
						"it runs as root), this is NOT its config file and folder changes "+
						"here won't reach it - the service will keep reporting no folders "+
						"configured. Match it with:  sudo northrou admin", port, flagConfigPath)
				}
			}
			return tui.Run(base, flagConfigPath, local)
		},
	}
	cmd.Flags().StringVar(&addr, "addr", "", "server base URL (default from config, e.g. http://localhost:8674)")
	return cmd
}

func newUpdateCmd() *cobra.Command {
	var yes, checkOnly bool
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and apply updates from GitHub releases",
		RunE: func(cmd *cobra.Command, args []string) error {
			u := update.New(update.DefaultRepo, buildinfo.Version)
			latest, err := u.Latest(cmd.Context())
			if err != nil {
				return fmt.Errorf("check for updates: %w", err)
			}
			if !u.HasUpdate(latest) {
				notice("Northrou is up to date (%s).", buildinfo.Version)
				return nil
			}
			notice("Update available: %s → %s", buildinfo.Version, latest.Version)
			fmt.Printf("\n%s\n\n", latest.Notes)
			if checkOnly {
				return nil
			}
			if !yes {
				fmt.Print("Install this update now? [y/N] ")
				var resp string
				_, _ = fmt.Scanln(&resp)
				if !strings.EqualFold(strings.TrimSpace(resp), "y") {
					fmt.Println("Aborted.")
					return nil
				}
			}
			fmt.Println("Downloading and installing…")
			if err := u.Apply(cmd.Context(), latest); err != nil {
				return needsRoot(cmd, err)
			}
			// The new binary is in place; the running service is still the old
			// one until it restarts. Do that here rather than asking the
			// operator to.
			if st, err := service.GetStatus(flagConfigPath); err == nil && st == service.StatusRunning {
				if err := service.Restart(flagConfigPath); err != nil {
					notice("Updated to %s, but the running service could not be restarted (%v).\nRestart it to run the new version:  sudo northrou restart", latest.Version, err)
					return nil
				}
				notice("Updated to %s and restarted the service.", latest.Version)
				return nil
			}
			notice("Updated to %s.", latest.Version)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "apply the update without confirmation")
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only check; do not install")
	return cmd
}
