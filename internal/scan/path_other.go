//go:build !windows

package scan

import "io/fs"

func isReparse(fi fs.FileInfo) bool { return fi.Mode()&fs.ModeSymlink != 0 }

// Unix sizing counts a symlink's own bytes without following its target.
func skipSizingEntry(fs.FileInfo) bool { return false }

// safeReadPath preserves the existing Unix handling: walkers do not follow
// entries that are symlinks, while a selected root may have a linked ancestor.
func safeReadPath(string) bool { return true }

func statPath(_ string, fi fs.FileInfo) statInfo { return statOf(fi) }

// WindowsPathSafe is used only by Windows cleanup guards.
func WindowsPathSafe(string) bool { return false }
