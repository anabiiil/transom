//go:build windows

package scan

import (
	"io/fs"
	"syscall"
	"time"
	"unsafe"
)

type statInfo struct {
	alloc          int64
	dev, ino       uint64
	nlink          uint64
	dataless       bool
	changed, birth time.Time
	haveInode      bool
}

func statOf(fi fs.FileInfo) statInfo {
	st := statInfo{alloc: fi.Size(), changed: fi.ModTime(), birth: fi.ModTime()}
	if fi.IsDir() {
		st.alloc = 0
	}
	if data, ok := fi.Sys().(*syscall.Win32FileAttributeData); ok {
		st.birth = time.Unix(0, data.CreationTime.Nanoseconds())
		// Offline placeholders must never be read and hydrated by hashing.
		st.dataless = data.FileAttributes&(0x00001000|0x00400000|0x00040000) != 0
	}
	return st
}

var fileInformationEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetFileInformationByHandleEx")

func statPath(path string, fi fs.FileInfo) statInfo {
	st := statOf(fi)
	if fi.IsDir() || isReparse(fi) {
		return st
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return st
	}
	// Handles are opened for metadata only, with sharing enabled; this
	// does not read content or interfere with another application's files.
	h, err := syscall.CreateFile(p, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err == nil {
		var info syscall.ByHandleFileInformation
		if syscall.GetFileInformationByHandle(h, &info) == nil {
			st.dev = uint64(info.VolumeSerialNumber)
			st.ino = uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)
			st.nlink = uint64(info.NumberOfLinks)
			st.haveInode = true
		}
		var basic struct {
			CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
			FileAttributes                                          uint32
			_                                                       uint32
		}
		if ok, _, _ := fileInformationEx.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&basic)), 40); ok != 0 && basic.ChangeTime != 0 {
			changed := syscall.Filetime{LowDateTime: uint32(basic.ChangeTime), HighDateTime: uint32(uint64(basic.ChangeTime) >> 32)}
			st.changed = time.Unix(0, changed.Nanoseconds())
		}
		var standard struct {
			AllocationSize, EndOfFile int64
			NumberOfLinks             uint32
			DeletePending, Directory  byte
			_                         [2]byte
		}
		if ok, _, _ := fileInformationEx.Call(uintptr(h), 1, uintptr(unsafe.Pointer(&standard)), 24); ok != 0 {
			st.alloc = standard.AllocationSize
		}
		syscall.CloseHandle(h)
	}
	return st
}
