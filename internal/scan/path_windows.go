//go:build windows

package scan

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func isReparse(fi fs.FileInfo) bool {
	if fi.Mode()&fs.ModeSymlink != 0 {
		return true
	}
	st, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	return ok && st.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0
}

func skipSizingEntry(fi fs.FileInfo) bool { return isReparse(fi) }

// WindowsPathSafe accepts drive and conventional UNC paths and rejects
// junctions and every other reparse point, including ancestors. A missing
// final leaf is allowed so callers can report a disappeared item; missing
// parents fail. Device namespaces and alternate data streams are excluded.
func WindowsPathSafe(path string) bool {
	path = filepath.Clean(path)
	volume := filepath.VolumeName(path)
	drive := len(volume) == 2 && volume[1] == ':'
	unc := strings.HasPrefix(volume, `\\`) && !strings.HasPrefix(volume, `\\?\`) && !strings.HasPrefix(volume, `\\.\`) && !strings.Contains(volume, ":")
	if !filepath.IsAbs(path) || (!drive && !unc) || strings.Contains(path[len(volume):], ":") {
		return false
	}
	for p := path; ; p = filepath.Dir(p) {
		fi, err := os.Lstat(p)
		if err != nil {
			if p != path || !os.IsNotExist(err) {
				return false
			}
		} else if isReparse(fi) {
			return false
		}
		parent := filepath.Dir(p)
		if parent == p {
			return true
		}
	}
}

func safeReadPath(path string) bool { return WindowsPathSafe(path) }
