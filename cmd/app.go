package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"transom/internal/macapp"
)

var appInstallDest string

var appCmd = &cobra.Command{
	Use:   "app",
	Short: "Manage the native Transom desktop app",
}

var appInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install Transom.app into Applications",
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS == "windows" {
			fmt.Println("Transom.exe is portable on Windows. Copy it to your preferred folder and double-click it; no installation or administrator access is required.")
			return nil
		}
		if runtime.GOOS != "darwin" {
			fmt.Println("transom app is macOS-only.")
			return nil
		}
		if !macapp.Available() {
			fmt.Println("this build doesn't include the Mac app — build it with `bash macapp/build.sh` then rebuild transom")
			return nil
		}
		if err := macapp.Install(appInstallDest); err != nil {
			return err
		}
		fmt.Println("Transom.app installed at", filepath.Join(appInstallDest, "Transom.app"))
		return nil
	},
}

var appOpenCmd = &cobra.Command{
	Use:   "open",
	Short: "Open the native Transom desktop app",
	RunE: func(cmd *cobra.Command, args []string) error {
		if runtime.GOOS == "windows" {
			return uiCmd.RunE(cmd, args)
		}
		if runtime.GOOS != "darwin" {
			fmt.Println("transom app is macOS-only.")
			return nil
		}
		c := exec.Command("open", "-a", "Transom")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("opening Transom.app: %w (installed? try `transom app install`)", err)
		}
		return nil
	},
}

func init() {
	appInstallCmd.Flags().StringVar(&appInstallDest, "dest", "/Applications", "directory to install Transom.app into")
	appCmd.AddCommand(appInstallCmd)
	appCmd.AddCommand(appOpenCmd)
	rootCmd.AddCommand(appCmd)
}
