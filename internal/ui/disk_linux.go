//go:build linux

package ui

import (
	"os"
	"syscall"
)

func diskInfo() (DiskInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return DiskInfo{}, err
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(home, &st); err != nil {
		return DiskInfo{}, err
	}
	total, free := int64(st.Blocks)*int64(st.Bsize), int64(st.Bavail)*int64(st.Bsize)
	return DiskInfo{Total: total, Free: free, Used: total - free, Volume: "Home volume"}, nil
}
