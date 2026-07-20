package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rhymeswithlimo/northrou/backend/internal/buildinfo"
	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/service"
	"github.com/rhymeswithlimo/northrou/backend/internal/tui"
	"github.com/rhymeswithlimo/northrou/backend/internal/update"
	"github.com/spf13/cobra"
)

// errNotYet marks commands whose full behavior arrives in a later build phase.
// The commands exist now so the CLI surface is stable.
var errNotYet = errors.New("not implemented yet in this build")

func newInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install Northrou as a system service and start it",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := service.Install(flagConfigPath); err != nil {
				return needsRoot(cmd, err)
			}
			fmt.Println("Northrou installed and started as a system service.")
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
			fmt.Println("Northrou service removed.")
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
			// No --addr means we are the box: the config we would edit is the
			// config the daemon reads, so folder editing is safe to offer.
			local := addr == ""
			base := addr
			if local {
				cfg, _, err := config.LoadOrInit(flagConfigPath)
				if err != nil {
					return err
				}
				port := cfg.Server.Port
				base = fmt.Sprintf("http://localhost:%d", port)
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
				fmt.Printf("Northrou is up to date (%s).\n", buildinfo.Version)
				return nil
			}
			fmt.Printf("Update available: %s → %s\n\n%s\n\n", buildinfo.Version, latest.Version, latest.Notes)
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
			fmt.Printf("Updated to %s. Restart the service to run the new version:\n  sudo northrou uninstall && sudo northrou install\n", latest.Version)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "apply the update without confirmation")
	cmd.Flags().BoolVar(&checkOnly, "check", false, "only check; do not install")
	return cmd
}
