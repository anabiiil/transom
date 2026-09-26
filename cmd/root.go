// Package cmd defines the transom command-line interface. Each command
// registers itself with rootCmd from its own file's init().
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"transom/internal/version"
)

var rootCmd = &cobra.Command{
	Use:   "transom",
	Short: "Find and safely reclaim disk space on your Mac",
	Long: `Transom — a careful disk cleaner for macOS.

It finds caches, logs, developer junk (Xcode, package managers,
Homebrew, Docker), stale project dependencies, large and duplicate
files, and leftovers of uninstalled apps. Scanning never changes
anything; cleaning moves items to the Trash unless you ask for
permanent deletion.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       version.Number,
}

// Execute runs the CLI.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
