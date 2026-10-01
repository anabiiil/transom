//go:build windows

package clean

import (
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var (
	shell32                     = syscall.NewLazyDLL("shell32.dll")
	ole32                       = syscall.NewLazyDLL("ole32.dll")
	coInitializeEx              = ole32.NewProc("CoInitializeEx")
	coUninitialize              = ole32.NewProc("CoUninitialize")
	coCreateInstance            = ole32.NewProc("CoCreateInstance")
	shCreateItemFromParsingName = shell32.NewProc("SHCreateItemFromParsingName")
	shEmptyRecycleBin           = shell32.NewProc("SHEmptyRecycleBinW")
)

// Native Shell interfaces keep original filenames and locations available to
// Explorer's Restore action. No shell command or manual $Recycle.Bin writes.
// COM calls use the SDK's IFileOperation vtable order.
type shellGUID struct {
	Data1        uint32
	Data2, Data3 uint16
	Data4        [8]byte
}

var (
	clsidFileOperation = shellGUID{0x3ad05575, 0x8857, 0x4850, [8]byte{0x92, 0x77, 0x11, 0xb8, 0x5b, 0xdb, 0x8e, 0x09}}
	iidFileOperation   = shellGUID{0x947aab5f, 0x0a5c, 0x4c13, [8]byte{0xb4, 0xd6, 0x4b, 0xf7, 0x83, 0x6f, 0xc9, 0xf8}}
	iidShellItem       = shellGUID{0x43826d1e, 0xe718, 0x42ee, [8]byte{0xbc, 0x55, 0xa1, 0xe2, 0x61, 0xc3, 0x7b, 0xfe}}
	iidProgressSink    = shellGUID{0x04b0f1a7, 0x9490, 0x44bc, [8]byte{0x96, 0xe1, 0x42, 0x96, 0xa3, 0x12, 0x52, 0xe2}}
	iidUnknown         = shellGUID{0, 0, 0, [8]byte{0xc0, 0, 0, 0, 0, 0, 0, 0x46}}
)

const (
	fofSilent              = 0x0004
	fofNoConfirmation      = 0x0010
	fofAllowUndo           = 0x0040
	fofNoErrorUI           = 0x0400
	fofNoConnectedElements = 0x2000
	fofxRecycleOnDelete    = 0x00080000
	fofxEarlyFailure       = 0x00100000
	fofxAddUndoRecord      = 0x20000000
	tsfDeleteRecycle       = 0x80
	hresultAbort           = 0x80004004
	hresultNoInterface     = 0x80004002
)

func hresultError(action string, hr uintptr) error {
	if int32(uint32(hr)) < 0 {
		return fmt.Errorf("%s failed (Windows HRESULT 0x%08X)", action, uint32(hr))
	}
	return nil
}

type comObject struct{ vtable *[23]uintptr }

func comCall(object *comObject, slot int, args ...uintptr) uintptr {
	argv := append([]uintptr{uintptr(unsafe.Pointer(object))}, args...)
	hr, _, _ := syscall.SyscallN(object.vtable[slot], argv...)
	runtime.KeepAlive(object)
	return hr
}

// A progress sink refuses a delete if Shell proposes permanent removal.
// PostDeleteItem is checked as PerformOperations can succeed while skipping
// an individual item. The sink stays alive until all COM references end.
type recycleSink struct {
	vtable    *[19]uintptr
	refs      int32
	rejected  bool
	completed bool
	result    uint32
}

func sinkQuery(this *recycleSink, iid *shellGUID, out **recycleSink) uintptr {
	if out == nil {
		return 0x80004003
	}
	*out = nil
	if iid == nil {
		return hresultNoInterface
	}
	if *iid != iidUnknown && *iid != iidProgressSink {
		return hresultNoInterface
	}
	*out = this
	atomic.AddInt32(&this.refs, 1)
	return 0
}
func sinkAddRef(this *recycleSink) uintptr  { return uintptr(atomic.AddInt32(&this.refs, 1)) }
func sinkRelease(this *recycleSink) uintptr { return uintptr(atomic.AddInt32(&this.refs, -1)) }
func sinkPreDelete(this *recycleSink, flags uintptr, _ *comObject) uintptr {
	if flags&tsfDeleteRecycle == 0 {
		this.rejected = true
		return hresultAbort
	}
	return 0
}
func sinkPostDelete(this *recycleSink, _ uintptr, _ *comObject, result uintptr, _ *comObject) uintptr {
	this.completed = true
	this.result = uint32(result)
	return 0
}

var recycleSinkVtable = [19]uintptr{
	syscall.NewCallback(sinkQuery),
	syscall.NewCallback(sinkAddRef),
	syscall.NewCallback(sinkRelease),
	syscall.NewCallback(func(uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(sinkPreDelete),
	syscall.NewCallback(sinkPostDelete),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr, uintptr, uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr) uintptr { return 0 }),
	syscall.NewCallback(func(uintptr) uintptr { return 0 }),
}

func moveToTrash(p, _ string) error {
	if err := validateWindowsPath(p); err != nil {
		return err
	}
	if volumeRoot(p) {
		return errors.New("refusing to recycle a drive/share root")
	}
	sink := &recycleSink{vtable: &recycleSinkVtable, refs: 1}
	defer func() { runtime.KeepAlive(sink) }()
	// IFileOperation requires a single-threaded COM apartment.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := coInitializeEx.Call(0, 0x2|0x4) // APARTMENTTHREADED | DISABLE_OLE1DDE
	if err := hresultError("initialize Windows Shell", hr); err != nil {
		return err
	}
	defer coUninitialize.Call()

	var operation *comObject
	hr, _, _ = coCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOperation)), 0, 1, uintptr(unsafe.Pointer(&iidFileOperation)), uintptr(unsafe.Pointer(&operation)))
	if err := hresultError("create Recycle Bin operation", hr); err != nil {
		return err
	}
	if operation == nil {
		return errors.New("Windows Shell returned no file operation")
	}
	defer comCall(operation, 2)
	flags := uintptr(fofSilent | fofNoConfirmation | fofAllowUndo | fofNoErrorUI | fofNoConnectedElements | fofxRecycleOnDelete | fofxEarlyFailure | fofxAddUndoRecord)
	if err := hresultError("configure recoverable deletion", comCall(operation, 5, flags)); err != nil {
		return err
	}

	name, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return err
	}
	var item *comObject
	hr, _, _ = shCreateItemFromParsingName.Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&iidShellItem)), uintptr(unsafe.Pointer(&item)))
	if err := hresultError("resolve Recycle Bin item", hr); err != nil {
		return err
	}
	if item == nil {
		return errors.New("Windows Shell returned no file item")
	}
	defer comCall(item, 2)
	if err := hresultError("queue Recycle Bin item", comCall(operation, 18, uintptr(unsafe.Pointer(item)), uintptr(unsafe.Pointer(sink)))); err != nil {
		return err
	}
	hr = comCall(operation, 21)
	if sink.rejected {
		return errors.New("this item cannot be recycled; permanent deletion was refused")
	}
	if err := hresultError("move to Recycle Bin", hr); err != nil {
		return err
	}
	var aborted int32
	if err := hresultError("check Recycle Bin operation", comCall(operation, 22, uintptr(unsafe.Pointer(&aborted)))); err != nil {
		return err
	}
	if aborted != 0 {
		return errors.New("Recycle Bin operation was canceled")
	}
	if !sink.completed {
		return errors.New("Windows did not confirm that the item was recycled")
	}
	return hresultError("recycle item", uintptr(sink.result))
}

func emptyRecycleBin() error {
	// All drives, no progress/sound/dialog. Caller requires explicit ModeDelete.
	hr, _, _ := shEmptyRecycleBin.Call(0, 0, 0x1|0x2|0x4)
	return hresultError("empty Recycle Bin", hr)
}
