//go:build unix

// Package proc holds small process helpers shared by the commands.
package proc

import (
	"os/exec"
	"syscall"
)

// Detach puts cmd in its own session so it outlives the terminal that
// started it (closing the terminal doesn't SIGHUP it).
func Detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
}
