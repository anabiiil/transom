//go:build !windows

package clean

import (
	"context"
	"path/filepath"

	"transom/internal/scan"
)

func pathKey(p string) string     { return filepath.Clean(p) }
func pathsEqual(p, q string) bool { return pathKey(p) == pathKey(q) }
func platformGuard(g Guard, it scan.Item) Guard {
	if it.Category == "user-caches" {
		g.CacheRoots = []string{filepath.Join(g.Home, "Library", "Caches")}
		g.RequireCacheRoot = true
	}
	return g
}

func platformValidate(ctx context.Context, home, path string, it scan.Item) error {
	if it.Category == "app-leftovers" {
		return scan.ValidateAppLeftover(ctx, home, path)
	}
	return nil
}
func permanentRemoval(mode string, it scan.Item) bool {
	return mode == ModeDelete || it.Category == "trash"
}
func platformCommand(context.Context, scan.Item, string, bool) (bool, error) { return false, nil }

func platformCovered(path, parent string, _ bool) bool {
	return pathsEqual(path, parent) || within(path, parent)
}
