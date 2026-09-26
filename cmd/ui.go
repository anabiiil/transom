package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"transom/internal/proc"
	"transom/internal/ui"
)

var (
	uiWindowHost bool
	uiDetached   bool
	uiForeground bool
)

// detachedIdleExit is how long a background panel lives without any
// request from its page.
const detachedIdleExit = time.Hour

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open the Transom control panel in your browser",
	Long: `Starts Transom's control panel on 127.0.0.1 and opens it in your
default browser. The panel runs in the background so the terminal stays
free; use --foreground to keep it attached (Ctrl-C stops it).`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if uiWindowHost {
			// Transom.app runs us as a child and owns our lifetime via
			// stdin; stdout carries only the URL line.
			return ui.RunHost(ctx, os.Stdin, os.Stdout)
		}
		if uiDetached {
			return ui.Run(ctx, detachedIdleExit, nil)
		}
		if !uiForeground {
			if exe, err := os.Executable(); err == nil {
				c := exec.Command(exe, "ui", "--detached")
				proc.Detach(c)
				if err := c.Start(); err == nil {
					_ = c.Process.Release()
					fmt.Println("Control panel opening in your browser — this terminal is free.")
					return nil
				}
			}
		}
		return ui.Run(ctx, 0, os.Stdout)
	},
}

func init() {
	uiCmd.Flags().BoolVar(&uiWindowHost, "window-host", false, "serve the panel for Transom.app: print TRANSOM_UI_URL=… and run until stdin closes")
	uiCmd.Flags().BoolVar(&uiDetached, "detached", false, "internal: already detached from the terminal")
	_ = uiCmd.Flags().MarkHidden("detached")
	uiCmd.Flags().BoolVar(&uiForeground, "foreground", false, "stay attached to the terminal")
	rootCmd.AddCommand(uiCmd)
}
