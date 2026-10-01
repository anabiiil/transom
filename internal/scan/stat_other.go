//go:build !darwin && !windows

package scan

import (
	"io/fs"
	"time"
)

type statInfo struct {
	alloc     int64
	dev, ino  uint64
	nlink     uint64
	dataless  bool
	changed   time.Time
	birth     time.Time
	haveInode bool
}

func statOf(fi fs.FileInfo) statInfo {
	return statInfo{alloc: fi.Size(), changed: fi.ModTime(), birth: fi.ModTime()}
}
