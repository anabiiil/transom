package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"transom/internal/clean"
	"transom/internal/scan"
)

var (
	cleanCategories []string
	cleanSafeOnly   bool
	cleanDelete     bool
	cleanDryRun     bool
	cleanYes        bool
	cleanOpts       scan.Options
)

var cleanCmd = &cobra.Command{
	Use:   "clean",
	Short: "Scan, then move what was found to the Trash",
	Long: `Scans, shows what would be removed, asks for confirmation, then moves
the items to the Trash (or deletes them permanently with --delete).

Categories of user files (large-files, duplicates) are only included
when named explicitly with --category. Items already in the Trash are
always deleted permanently.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		ids := splitIDs(cleanCategories)
		if err := absRoots(&cleanOpts); err != nil {
			return err
		}
		res, _, err := scanWithProgress(ctx, ids, cleanOpts)
		if err != nil {
			return err
		}
		sel := selectItems(res, len(ids) > 0, cleanSafeOnly)
		if len(sel.ids) == 0 {
			fmt.Println("Nothing to clean.")
			return nil
		}
		printPlan(sel)

		mode := clean.ModeTrash
		verb := "Move %d items (%s) to the Trash?"
		if cleanDelete {
			mode = clean.ModeDelete
			verb = "Permanently delete %d items (%s)?"
		}
		if !cleanDryRun && !cleanYes {
			fmt.Printf("\n"+verb+" [y/N] ", len(sel.ids), humanBytes(sel.total))
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if a := strings.ToLower(strings.TrimSpace(line)); a != "y" && a != "yes" {
				fmt.Println("Cancelled; nothing was changed.")
				return nil
			}
		}

		out, err := clean.Run(ctx, res, clean.Request{Items: sel.ids, Mode: mode, DryRun: cleanDryRun})
		if err != nil {
			return err
		}
		fmt.Println()
		if out.DryRun {
			fmt.Printf("Dry run: would free %s (%d items). Nothing was changed.\n", humanBytes(out.Freed), out.Removed)
		} else {
			where := "moved to the Trash"
			if mode == clean.ModeDelete {
				where = "deleted"
			}
			fmt.Printf("Freed %s: %d items %s.\n", humanBytes(out.Freed), out.Removed, where)
		}
		for _, f := range out.Failed {
			fmt.Fprintf(os.Stderr, "  failed: %s: %s\n", f.Path, f.Error)
		}
		return nil
	},
}

type selection struct {
	ids   []string
	total int64
	cats  []catPlan
}

type catPlan struct {
	name  string
	count int
	size  int64
}

// selectItems picks what clean acts on: every item of the scanned
// categories, minus caution categories unless they were named
// explicitly, minus non-safe items with --safe-only.
func selectItems(res *scan.Result, explicit, safeOnly bool) selection {
	var sel selection
	for _, c := range res.Categories {
		if c.Risk == scan.RiskCaution && !explicit {
			continue
		}
		cp := catPlan{name: c.Name}
		for _, it := range c.Items {
			if safeOnly && it.Risk != scan.RiskSafe {
				continue
			}
			if !explicit && it.Risk == scan.RiskCaution {
				continue
			}
			sel.ids = append(sel.ids, it.ID)
			sel.total += it.Size
			cp.count++
			cp.size += it.Size
		}
		if cp.count > 0 {
			sel.cats = append(sel.cats, cp)
		}
	}
	return sel
}

func printPlan(sel selection) {
	fmt.Println("Will clean:")
	for _, c := range sel.cats {
		fmt.Printf("  %-26s %5d items  %10s\n", c.name, c.count, humanBytes(c.size))
	}
	fmt.Printf("  %-26s %5d items  %10s\n", "Total", len(sel.ids), humanBytes(sel.total))
}

func init() {
	cleanCmd.Flags().StringSliceVarP(&cleanCategories, "category", "c", nil, "only these categories (comma-separated ids)")
	cleanCmd.Flags().BoolVar(&cleanSafeOnly, "safe-only", false, "only items that are safe to remove (caches, logs, build products)")
	cleanCmd.Flags().BoolVar(&cleanDelete, "delete", false, "delete permanently instead of moving to the Trash")
	cleanCmd.Flags().BoolVar(&cleanDryRun, "dry-run", false, "show what would be freed; change nothing")
	cleanCmd.Flags().BoolVarP(&cleanYes, "yes", "y", false, "don't ask for confirmation")
	addScanOptionFlags(cleanCmd, &cleanOpts)
	rootCmd.AddCommand(cleanCmd)
}
