//go:build windows

package winapp

import (
	"encoding/json"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	getWindowPlacement = user32.NewProc("GetWindowPlacement")
	setWindowPlacement = user32.NewProc("SetWindowPlacement")
	setWindowLongPtr   = user32.NewProc("SetWindowLongPtrW")
	callWindowProc     = user32.NewProc("CallWindowProcW")
	getSystemMetrics   = user32.NewProc("GetSystemMetrics")
)

type point struct{ X, Y int32 }
type rectangle struct{ Left, Top, Right, Bottom int32 }
type placement struct {
	Length, Flags, ShowCommand uint32
	MinPosition, MaxPosition   point
	NormalPosition             rectangle
}

// rememberWindow preserves the user's last position and dimensions, like
// the Mac shell's frame autosave. Invalid/off-screen saved positions are
// ignored so removing a monitor cannot strand the window.
func rememberWindow(handle unsafe.Pointer, path string) {
	hwnd := uintptr(handle)
	if data, err := os.ReadFile(path); err == nil {
		var saved placement
		if json.Unmarshal(data, &saved) == nil && visiblePlacement(saved) {
			saved.Length = uint32(unsafe.Sizeof(saved))
			saved.Flags = 0
			if saved.ShowCommand != 3 { // preserve maximized, restore minimized
				saved.ShowCommand = 1
			}
			setWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&saved)))
		}
	}
	save := func() {
		var current placement
		current.Length = uint32(unsafe.Sizeof(current))
		ok, _, _ := getWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&current)))
		if ok == 0 || !visiblePlacement(current) {
			return
		}
		data, err := json.Marshal(current)
		if err == nil {
			// This file is non-critical UI state. A failed write must never
			// interrupt cleaning, scanning, or closing the window.
			_ = os.WriteFile(path, data, 0o600)
		}
	}
	var original uintptr
	callback := windows.NewCallback(func(window, message, wparam, lparam uintptr) uintptr {
		if message == 0x0010 || message == 0x0232 { // WM_CLOSE / WM_EXITSIZEMOVE
			save()
		}
		result, _, _ := callWindowProc.Call(original, window, message, wparam, lparam)
		return result
	})
	// GWLP_WNDPROC is -4. uintptr's complement expresses it without an
	// overflowing constant on either Windows architecture.
	original, _, _ = setWindowLongPtr.Call(hwnd, ^uintptr(3), callback)
}

func visiblePlacement(saved placement) bool {
	r := saved.NormalPosition
	width, height := r.Right-r.Left, r.Bottom-r.Top
	if width < 900 || height < 600 || width > 100000 || height > 100000 {
		return false
	}
	x, _, _ := getSystemMetrics.Call(76) // SM_XVIRTUALSCREEN
	y, _, _ := getSystemMetrics.Call(77) // SM_YVIRTUALSCREEN
	w, _, _ := getSystemMetrics.Call(78) // SM_CXVIRTUALSCREEN
	h, _, _ := getSystemMetrics.Call(79) // SM_CYVIRTUALSCREEN
	left, top := int32(x), int32(y)
	right, bottom := left+int32(w), top+int32(h)
	// Keep the title bar and a useful part of the window reachable.
	return r.Left < right-100 && r.Right > left+100 && r.Top >= top && r.Top < bottom-100
}
