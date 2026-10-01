//go:build !windows

package ui

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
)

func openBrowser(url string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", url).Run()
	}
	return exec.Command("xdg-open", url).Run()
}

func revealPath(ctx context.Context, path string) error {
	if runtime.GOOS == "darwin" {
		return exec.CommandContext(ctx, "/usr/bin/open", "-R", path).Run()
	}
	return exec.CommandContext(ctx, "xdg-open", filepath.Dir(path)).Run()
}
