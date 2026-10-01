//go:build !windows

package scan

import "path/filepath"

// WindowsUserFolders returns user-folder roots; on other platforms the
// existing home-relative paths are preserved.
func WindowsUserFolders(home string) map[string]string {
	return map[string]string{
		"Desktop":   filepath.Join(home, "Desktop"),
		"Documents": filepath.Join(home, "Documents"),
		"Downloads": filepath.Join(home, "Downloads"),
	}
}
