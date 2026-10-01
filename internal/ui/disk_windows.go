//go:build windows

package ui

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var diskKernel = syscall.NewLazyDLL("kernel32.dll")

func diskInfo() (DiskInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return DiskInfo{}, err
	}
	p, err := syscall.UTF16PtrFromString(home)
	if err != nil {
		return DiskInfo{}, err
	}
	var available, total, free uint64
	ok, _, callErr := diskKernel.NewProc("GetDiskFreeSpaceExW").Call(uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&available)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if ok == 0 {
		return DiskInfo{}, fmt.Errorf("read disk space: %w", callErr)
	}
	root := make([]uint16, 32768)
	ok, _, callErr = diskKernel.NewProc("GetVolumePathNameW").Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&root[0])), uintptr(len(root)))
	if ok == 0 {
		return DiskInfo{}, fmt.Errorf("read volume path: %w", callErr)
	}
	name := make([]uint16, 261)
	ok, _, _ = diskKernel.NewProc("GetVolumeInformationW").Call(uintptr(unsafe.Pointer(&root[0])),
		uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)), 0, 0, 0, 0, 0)
	volume := syscall.UTF16ToString(root)
	if ok != 0 && name[0] != 0 {
		volume = syscall.UTF16ToString(name) + " (" + volume + ")"
	}
	return DiskInfo{Total: int64(total), Free: int64(available), Used: int64(total - available), Volume: volume}, nil
}
