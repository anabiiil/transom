//go:build windows

package scan

import (
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

type knownFolderGUID struct {
	Data1        uint32
	Data2, Data3 uint16
	Data4        [8]byte
}

var knownFolderIDs = map[string]knownFolderGUID{
	"Profile":   {0x5e6c858f, 0x0e22, 0x4760, [8]byte{0x9a, 0xfe, 0xea, 0x33, 0x17, 0xb6, 0x71, 0x73}},
	"Desktop":   {0xb4bfcc3a, 0xdb2c, 0x424c, [8]byte{0xb0, 0x29, 0x7f, 0xe9, 0x9a, 0x87, 0xc6, 0x41}},
	"Documents": {0xfdd39ad0, 0x238f, 0x46af, [8]byte{0xad, 0xb4, 0x6c, 0x85, 0x48, 0x03, 0x69, 0xc7}},
	"Downloads": {0x374de290, 0x123f, 0x4565, [8]byte{0x91, 0x64, 0x39, 0xc4, 0x92, 0x5e, 0x46, 0x7b}},
}

var (
	getKnownFolderPath   = syscall.NewLazyDLL("shell32.dll").NewProc("SHGetKnownFolderPath")
	folderOle32          = syscall.NewLazyDLL("ole32.dll")
	folderCoInitialize   = folderOle32.NewProc("CoInitializeEx")
	folderCoUninitialize = folderOle32.NewProc("CoUninitialize")
	folderCoTaskMemFree  = folderOle32.NewProc("CoTaskMemFree")
)

func nativeKnownFolder(name string) string {
	id, ok := knownFolderIDs[name]
	if !ok {
		return ""
	}
	var result *uint16
	hr, _, _ := getKnownFolderPath.Call(uintptr(unsafe.Pointer(&id)), 0, 0, uintptr(unsafe.Pointer(&result)))
	if int32(hr) < 0 || result == nil {
		return ""
	}
	defer folderCoTaskMemFree.Call(uintptr(unsafe.Pointer(result)))
	// Windows paths cannot exceed 32767 UTF-16 units. Read only up to
	// the native NUL terminator rather than copying an arbitrary block.
	var units []uint16
	for i := uintptr(0); i < 32767; i++ {
		unit := *(*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(result)) + i*2))
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	path := syscall.UTF16ToString(units)
	if !filepath.IsAbs(path) || filepath.Dir(filepath.Clean(path)) == filepath.Clean(path) {
		return ""
	}
	return filepath.Clean(path)
}

// WindowsUserFolders follows the current user's actual Known Folder
// locations, including folder redirection and OneDrive. Walkers still skip
// reparse points and cloud-only placeholders; resolving a folder hydrates no
// file contents. The injected-home fallback isolates tests and other users.
func WindowsUserFolders(home string) map[string]string {
	folders := map[string]string{}
	for _, name := range []string{"Desktop", "Documents", "Downloads"} {
		folders[name] = filepath.Join(home, name)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := folderCoInitialize.Call(0, 2)
	if int32(hr) >= 0 {
		defer folderCoUninitialize.Call()
	} else if uint32(hr) != 0x80010106 { // RPC_E_CHANGED_MODE: already initialized
		return folders
	}
	profile := nativeKnownFolder("Profile")
	if profile == "" || !strings.EqualFold(filepath.Clean(home), profile) {
		return folders
	}
	for name := range folders {
		if path := nativeKnownFolder(name); path != "" && !strings.EqualFold(path, profile) {
			folders[name] = path
		}
	}
	return folders
}
