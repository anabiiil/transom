package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"transom/internal/macapp"
	"transom/internal/proc"
	"transom/internal/ui"
	"transom/internal/winapp"
)

var (
	uiWindowHost bool
	uiDetached   bool
	uiForeground bool
	uiBrowser    bool
)

// detachedIdleExit is how long a background panel lives without any
// request from its page.
const detachedIdleExit = time.Hour

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Open Transom's control panel",
	Long: `On macOS, opens Transom.app — installing it into /Applications (or
updating it, if a different version is already there) first if needed —
around the control panel. Use --browser for the old behavior instead:
starts the panel's server on 127.0.0.1 and opens it in your default
browser, detached from the terminal so it stays free (use --foreground
to keep it attached instead). On Windows, opens the same control panel
in a native desktop window; --browser uses your default browser instead.`,
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
		if runtime.GOOS == "windows" && !uiBrowser {
			return ui.RunDesktop(ctx, winapp.Run)
		}
		if runtime.GOOS == "darwin" && !uiBrowser {
			opened, err := openNativeApp()
			if err != nil {
				return err
			}
			if opened {
				return nil
			}
			fmt.Println("Transom.app isn't available in this build — opening in your browser instead.")
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

// openNativeApp is transom ui's default macOS behavior: make sure
// Transom.app is installed and current, then open it in place of running
// a server in this process. It reports (false, nil) — not an error —
// when there's nothing to open: no embedded bundle in this build (a dev
// build that hasn't run macapp/build.sh) and no previously installed
// copy either, so the caller can fall back to browser mode.
func openNativeApp() (bool, error) {
	var target string
	if macapp.Available() {
		path, _, err := macapp.EnsureInstalled(func(msg string) { fmt.Println(msg) })
		if err != nil {
			return false, err
		}
		target = path
	} else if path, ok := macapp.Installed(); ok {
		target = path
	} else {
		return false, nil
	}
	if err := exec.Command("open", "-a", target).Run(); err != nil {
		return false, fmt.Errorf("opening %s: %w (try `transom ui --browser`)", target, err)
	}
	fmt.Println("Opened Transom.")
	return true, nil
}

func init() {
	uiCmd.Flags().BoolVar(&uiWindowHost, "window-host", false, "serve the panel for Transom.app: print TRANSOM_UI_URL=… and run until stdin closes")
	uiCmd.Flags().BoolVar(&uiDetached, "detached", false, "internal: already detached from the terminal")
	_ = uiCmd.Flags().MarkHidden("detached")
	uiCmd.Flags().BoolVar(&uiForeground, "foreground", false, "stay attached to the terminal (browser mode only)")
	uiCmd.Flags().BoolVar(&uiBrowser, "browser", false, "start the panel's server and open it in your default browser instead of the native app")
	rootCmd.AddCommand(uiCmd)
}
