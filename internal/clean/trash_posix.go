//go:build !windows

package clean

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// moveToTrash moves p into ~/.Trash, renaming on a name clash
// ("name 2026-09-26 14.03.07.ext"). An item on another volume (EXDEV)
// goes into that volume's own trash, /Volumes/X/.Trashes/<uid>, as
// Finder does. If that isn't possible (or the rename is refused),
// Finder is asked to trash it.
func moveToTrash(p, home string) error {
	trash := filepath.Join(resolveDir(home), ".Trash")
	if err := os.MkdirAll(trash, 0o700); err != nil {
		return err
	}
	err := os.Rename(p, trashName(trash, filepath.Base(p), time.Now()))
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EXDEV) {
		if vt, verr := volumeTrashDir(p); verr == nil {
			if rerr := os.Rename(p, trashName(vt, filepath.Base(p), time.Now())); rerr == nil {
				return nil
			}
		}
	}
	if errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		if ferr := finderTrash(p); ferr != nil {
			return fmt.Errorf("move to Trash: %v; Finder: %v", err, ferr)
		}
		return nil
	}
	return err
}

// finderScript trashes the POSIX path given as its first argument. The
// path is passed through argv, never interpolated into the script.
var finderScript = []string{
	"-e", "on run argv",
	"-e", "set f to POSIX file (item 1 of argv)",
	"-e", "tell application \"Finder\" to delete (f as alias)",
	"-e", "end run",
}

func finderTrash(p string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	args := append(append([]string{}, finderScript...), p)
	c := exec.CommandContext(ctx, "/usr/bin/osascript", args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	c.Stdout = nil
	if err := c.Run(); err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
