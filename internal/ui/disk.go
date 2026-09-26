package ui

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DiskInfo is /api/disk's data.
type DiskInfo struct {
	Total  int64  `json:"total"`
	Free   int64  `json:"free"`
	Used   int64  `json:"used"`
	Volume string `json:"volume"`
}

var (
	volumeOnce sync.Once
	volumeName = "Macintosh HD"
)

// diskInfo reports the volume holding $HOME. Free is f_bavail (what's
// available to the user), close to Finder's "available" minus purgeable
// space, which only Finder/APFS can see.
func diskInfo() (DiskInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return DiskInfo{}, err
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(home, &st); err != nil {
		return DiskInfo{}, err
	}
	bs := int64(st.Bsize)
	total := int64(st.Blocks) * bs
	free := int64(st.Bavail) * bs
	volumeOnce.Do(func() {
		if name := readVolumeName(); name != "" {
			volumeName = name
		}
	})
	return DiskInfo{Total: total, Free: free, Used: total - free, Volume: volumeName}, nil
}

// readVolumeName asks diskutil for the boot volume's name ("" on failure).
func readVolumeName() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, err := exec.CommandContext(ctx, "/usr/sbin/diskutil", "info", "-plist", "/").Output()
	if err != nil {
		return ""
	}
	c := exec.CommandContext(ctx, "/usr/bin/plutil", "-extract", "VolumeName", "raw", "-o", "-", "-")
	c.Stdin = bytes.NewReader(info)
	out, err := c.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
