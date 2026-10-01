//go:build windows

package clean

import (
	"context"
	"errors"
	"os"

	"transom/internal/scan"
)

func platformGuard(g Guard, it scan.Item) Guard {
	g.CacheRoots = scan.WindowsPaths(g.Home, it.Category)
	g.RequireCacheRoot = it.Category == "app-leftovers" || it.Category == "user-caches"
	return g
}

func platformValidate(context.Context, string, string, scan.Item) error { return nil }

// Windows never converts a trash-mode file request to permanent deletion.
func permanentRemoval(mode string, _ scan.Item) bool { return mode == ModeDelete }

func platformCommand(ctx context.Context, it scan.Item, mode string, dryRun bool) (bool, error) {
	if it.Category != "trash" {
		return false, nil
	}
	if it.Kind != scan.KindCommand || it.Path != scan.WindowsRecycleBinPath || it.ID != scan.ItemID("trash", scan.WindowsRecycleBinPath) {
		return true, errors.New("invalid Recycle Bin scan item; rescan and try again")
	}
	if mode != ModeDelete {
		return true, errors.New("emptying the Recycle Bin requires explicit permanent-delete mode")
	}
	if err := ctx.Err(); err != nil {
		return true, err
	}
	if dryRun {
		return true, nil
	}
	return true, emptyRecycleBin()
}

func platformCovered(path, parent string, dryRun bool) bool {
	if !pathsEqual(path, parent) && !within(path, parent) {
		return false
	}
	if dryRun {
		return sameAllowedRoot(path, parent) || insideAllowedRoot(path, parent)
	}
	// A case-sensitive sibling can share the same folded path prefix. If it
	// still exists after deleting parent, it was not removed by that cleanup.
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}
