//go:build darwin

package scan

import (
	"io/fs"
	"syscall"
	"time"
)

// sfDataless is SF_DATALESS from <sys/stat.h>: the file's content lives
// in the cloud (iCloud Drive "optimized storage"). Reading it would
// trigger a download, so content-reading scanners skip such files.
const sfDataless = 0x40000000

type statInfo struct {
	alloc     int64 // allocated bytes on disk
	dev, ino  uint64
	nlink     uint64
	dataless  bool
	changed   time.Time // ctime
	birth     time.Time
	haveInode bool
}

func statOf(fi fs.FileInfo) statInfo {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return statInfo{alloc: fi.Size(), changed: fi.ModTime(), birth: fi.ModTime()}
	}
	return statInfo{
		alloc:     st.Blocks * 512,
		dev:       uint64(st.Dev),
		ino:       st.Ino,
		nlink:     uint64(st.Nlink),
		dataless:  st.Flags&sfDataless != 0,
		changed:   time.Unix(st.Ctimespec.Unix()),
		birth:     time.Unix(st.Birthtimespec.Unix()),
		haveInode: true,
	}
}
