package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"transom/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print Transom's version",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("transom", version.Number)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
