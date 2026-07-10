package cli

import (
	"fmt"

	"github.com/rhymeswithlimo/northrou/backend/internal/buildinfo"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println(buildinfo.String())
			return nil
		},
	}
}
