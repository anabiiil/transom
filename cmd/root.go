// Package cmd defines the transom command-line interface. Each command
// registers itself with rootCmd from its own file's init().
package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"transom/internal/version"
	"transom/internal/winapp"
)

// guiBuild is set by the Windows desktop build. The companion console
// executable keeps ordinary command-line error output instead of dialogs.
var guiBuild = "false"

var rootCmd = &cobra.Command{
	Use:   "transom",
	Short: "Find and safely reclaim disk space on macOS and Windows",
	Long: `Transom — a careful disk cleaner for macOS and Windows.

It finds caches, logs, developer junk (Xcode, package managers,
Homebrew, Docker), stale project dependencies, large and duplicate
files, and leftovers of uninstalled apps. Scanning never changes
anything; cleaning moves items to the Trash unless you ask for
permanent deletion.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS == "windows" {
			return uiCmd.RunE(cmd, args)
		}
		return cmd.Help()
	},
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       version.Number,
	Args:          cobra.NoArgs,
}

// Execute runs the CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		if runtime.GOOS == "windows" && guiBuild == "true" {
			winapp.ShowError(err)
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
		}
		os.Exit(1)
	}
}
