package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rhymeswithlimo/northrou/backend/internal/config"
	"github.com/rhymeswithlimo/northrou/backend/internal/logging"
	"github.com/spf13/cobra"
)

func newLogsCmd() *cobra.Command {
	var follow bool
	var lines int
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show the server's recent log output",
		Long: "Print the tail of the server's log file (data_dir/logs/northrou.log), " +
			"which the daemon writes regardless of how it was started. Use -f to " +
			"keep following new lines, like tail -f.\n\n" +
			"It reads the data directory named by the config file this command can " +
			"see, so on a box where Northrou runs as a system service (as root), " +
			"run it with sudo.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(flagConfigPath)
			if err != nil {
				return fmt.Errorf("couldn't read config at %s: %w\n"+
					"(if Northrou runs as a system service, try: sudo northrou logs)",
					flagConfigPath, err)
			}
			path := logging.Path(cfg.Server.DataDir)
			tail, err := logging.Tail(path, lines)
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("no log file yet at %s - has the server run since this version was installed?", path)
			}
			if err != nil {
				return err
			}
			if _, err := os.Stdout.Write(tail); err != nil {
				return err
			}
			if !follow {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			return followFile(cmd.Context(), f, path, os.Stdout)
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new log lines as they arrive")
	cmd.Flags().IntVarP(&lines, "lines", "n", 200, "number of trailing lines to show")
	return cmd
}

// followFile keeps copying new bytes from the log to out, reopening when the
// file is rotated out from under us (its size shrinks or the inode goes away).
func followFile(ctx context.Context, f *os.File, path string, out io.Writer) error {
	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(500 * time.Millisecond):
		}
		info, err := os.Stat(path)
		if err != nil {
			continue // mid-rotation; retry
		}
		if info.Size() < offset {
			// Rotated: start over at the top of the fresh file.
			nf, err := os.Open(path)
			if err != nil {
				continue
			}
			f.Close()
			f = nf
			offset = 0
		}
		n, err := io.Copy(out, f)
		if err != nil {
			return err
		}
		offset += n
	}
}
