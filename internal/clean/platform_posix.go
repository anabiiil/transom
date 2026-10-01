//go:build !windows

package clean

import (
	"context"
	"path/filepath"

	"transom/internal/scan"
)

func pathKey(p string) string                  { return filepath.Clean(p) }
func pathsEqual(p, q string) bool              { return pathKey(p) == pathKey(q) }
func platformGuard(g Guard, _ scan.Item) Guard { return g }
func permanentRemoval(mode string, it scan.Item) bool {
	return mode == ModeDelete || it.Category == "trash"
}
func platformCommand(context.Context, scan.Item, string, bool) (bool, error) { return false, nil }

func platformCovered(path, parent string, _ bool) bool {
	return pathsEqual(path, parent) || within(path, parent)
}
