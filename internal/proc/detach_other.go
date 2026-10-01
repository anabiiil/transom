//go:build !unix && !windows

// Package proc holds small process helpers shared by the commands.
package proc

import "os/exec"

// Detach is a no-op on platforms without a native process helper.
func Detach(cmd *exec.Cmd) {}
