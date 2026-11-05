package cmd

import (
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		RunE: func(cmd *cobra.Command, args []string) error {
			out := newPrinter(cmd.OutOrStdout())
			out.Println(cmd.Root().Version)
			return out.Err()
		},
	}
}
