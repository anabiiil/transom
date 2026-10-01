//go:build windows

package proc

import (
	"os/exec"
	"syscall"
)

const (
	createNewProcessGroup = 0x00000200
	createNoWindow        = 0x08000000
)

// Detach lets a browser-mode panel outlive the console that started it.
// CREATE_NO_WINDOW also keeps the console CLI from flashing a new terminal
// when it is launched as the background panel server.
func Detach(cmd *exec.Cmd) {
	HideWindow(cmd)
	cmd.SysProcAttr.CreationFlags |= createNewProcessGroup
}

// HideWindow keeps short-lived tools (PowerShell, package managers, Docker)
// from displaying console windows when called by the desktop application.
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
