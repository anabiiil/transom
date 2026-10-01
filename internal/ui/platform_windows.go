//go:build windows

package ui

import (
	"context"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var shell = syscall.NewLazyDLL("shell32.dll")
var ole = syscall.NewLazyDLL("ole32.dll")

func openBrowser(url string) error {
	u, err := syscall.UTF16PtrFromString(url)
	if err != nil {
		return err
	}
	verb, _ := syscall.UTF16PtrFromString("open")
	r, _, _ := shell.NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(u)), 0, 0, 1)
	if r <= 32 {
		return fmt.Errorf("open browser: Windows error %d", r)
	}
	return nil
}

// Select the exact scanned item in Explorer without constructing shell text.
func revealPath(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := ole.NewProc("CoInitializeEx").Call(0, 2)
	if int32(hr) < 0 {
		return fmt.Errorf("initialize Explorer: HRESULT 0x%08x", uint32(hr))
	}
	defer ole.NewProc("CoUninitialize").Call()
	var pidl uintptr
	hr, _, _ = shell.NewProc("SHParseDisplayName").Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&pidl)), 0, 0)
	if int32(hr) < 0 {
		return fmt.Errorf("locate item: HRESULT 0x%08x", uint32(hr))
	}
	defer ole.NewProc("CoTaskMemFree").Call(pidl)
	hr, _, _ = shell.NewProc("SHOpenFolderAndSelectItems").Call(pidl, 0, 0, 0)
	if int32(hr) < 0 {
		return fmt.Errorf("select item in Explorer: HRESULT 0x%08x", uint32(hr))
	}
	return nil
}
