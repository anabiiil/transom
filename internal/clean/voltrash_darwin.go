//go:build darwin

package clean

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// volumeTrashDir returns the per-user trash of the volume holding p —
// /Volumes/X/.Trashes/<uid>, where Finder itself puts items trashed on
// that volume (they show up in the Dock's Trash). It isn't created: if
// it doesn't exist the caller falls back to asking Finder.
func volumeTrashDir(p string) (string, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(filepath.Dir(p), &st); err != nil {
		return "", err
	}
	b := make([]byte, 0, len(st.Mntonname))
	for _, c := range st.Mntonname {
		b = append(b, byte(c))
	}
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	mount := string(b)
	if mount == "" || mount == "/" {
		return "", errors.New("not on a separate volume")
	}
	dir := filepath.Join(mount, ".Trashes", strconv.Itoa(os.Getuid()))
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", errors.New(dir + " is not a directory")
	}
	return dir, nil
}
