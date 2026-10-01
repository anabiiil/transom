//go:build !windows

package proc

import "os/exec"

// HideWindow is only needed for Windows console processes.
func HideWindow(cmd *exec.Cmd) {}
