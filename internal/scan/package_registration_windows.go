//go:build windows

package scan

import (
	"syscall"
	"unsafe"
)

var getPackagesByFamily = syscall.NewLazyDLL("kernel32.dll").NewProc("GetPackagesByPackageFamily")

func windowsPackageCount(family string) (uint32, error) {
	if err := getPackagesByFamily.Find(); err != nil {
		return 0, err
	}
	name, err := syscall.UTF16PtrFromString(family)
	if err != nil {
		return 0, err
	}
	var count, bufferLength uint32
	// The sizing probe needs no buffers when a family has zero packages.
	// Only ERROR_SUCCESS with count == 0 proves the family absent. An
	// installed family normally returns ERROR_INSUFFICIENT_BUFFER; every
	// error, including that expected result, conservatively keeps its data.
	code, _, _ := getPackagesByFamily.Call(uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&count)), 0, uintptr(unsafe.Pointer(&bufferLength)), 0)
	if code != 0 {
		return count, syscall.Errno(uint32(code))
	}
	return count, nil
}
