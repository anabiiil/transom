//go:build !unix

// Package proc holds small process helpers shared by the commands.
package proc

import "os/exec"

// Detach is a no-op off unix.
func Detach(cmd *exec.Cmd) {}
