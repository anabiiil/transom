package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"transom/internal/scan"
)

var (
	scanCategories []string
	scanJSON       bool
	scanVerbose    bool
	scanOpts       scan.Options
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Report reclaimable space (read-only)",
	Long: `Scans for reclaimable space and prints a report per category.
Scanning is read-only: nothing is moved or deleted.

Categories: user-caches, user-logs, temp-files, trash, mail-downloads,
xcode, simulators, package-caches, homebrew, docker, stale-deps,
large-files, old-downloads, duplicates, app-leftovers.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := absRoots(&scanOpts); err != nil {
			return err
		}
		res, took, err := scanWithProgress(ctx, splitIDs(scanCategories), scanOpts)
		if err != nil {
			return err
		}
		if scanJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(res)
		}
		printReport(res, scanVerbose)
		fmt.Printf("\nScanned in %.1fs. Nothing was changed; run `transom clean` to reclaim space.\n", took.Seconds())
		return nil
	},
}

// printReport prints one line per category (and its biggest items when
// verbose), then the total.
func printReport(res *scan.Result, verbose bool) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "CATEGORY\tITEMS\t      SIZE\tRISK")
	for _, c := range res.Categories {
		fmt.Fprintf(tw, "%s\t%5d\t%10s\t%s\n", c.Name, c.Count, humanBytes(c.TotalSize), c.Risk)
		if !verbose {
			continue
		}
		for i, it := range c.Items {
			if i == 10 {
				fmt.Fprintf(tw, "    … and %d more\t\t\t\n", len(c.Items)-10)
				break
			}
			fmt.Fprintf(tw, "    %s\t\t%10s\t\n", truncate(it.Label, 60), humanBytes(it.Size))
		}
	}
	fmt.Fprintf(tw, "Total (each path counted once)\t\t%10s\t\n", humanBytes(res.TotalSize))
	tw.Flush()
}

// absRoots makes --root values absolute (the guard ignores relative roots).
func absRoots(opts *scan.Options) error {
	for i, r := range opts.Roots {
		if r == "~" || strings.HasPrefix(r, "~/") || (runtime.GOOS == "windows" && strings.HasPrefix(r, `~\`)) {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			r = filepath.Join(home, strings.TrimLeft(strings.TrimPrefix(r, "~"), `/\`))
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			return err
		}
		opts.Roots[i] = abs
	}
	return nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// addScanOptionFlags wires the scan options onto a command.
func addScanOptionFlags(c *cobra.Command, opts *scan.Options) {
	c.Flags().IntVar(&opts.StaleDays, "stale-days", 60, "stale-deps: projects untouched for this many days")
	c.Flags().IntVar(&opts.LargeMinMB, "large-min-mb", 500, "large-files: minimum size in MB")
	c.Flags().StringSliceVar(&opts.Roots, "root", nil, "stale-deps: project roots to search (default: the projectRoots setting, else your home folder)")
}

func init() {
	scanCmd.Flags().StringSliceVarP(&scanCategories, "category", "c", nil, "only these categories (comma-separated ids)")
	scanCmd.Flags().BoolVar(&scanJSON, "json", false, "print the full scan result as JSON")
	scanCmd.Flags().BoolVarP(&scanVerbose, "verbose", "v", false, "list the biggest items of each category")
	addScanOptionFlags(scanCmd, &scanOpts)
	rootCmd.AddCommand(scanCmd)
}
