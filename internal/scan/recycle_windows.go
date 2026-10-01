//go:build windows

package scan

import (
	"context"
	"fmt"
	"syscall"
	"unsafe"
)

var queryRecycleBin = syscall.NewLazyDLL("shell32.dll").NewProc("SHQueryRecycleBinW")

func scanWindowsRecycleBin(ctx context.Context, e *Env) ([]Item, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// SHQUERYRBINFO is a 4-byte size followed by two aligned int64 values
	// on both Windows architectures. Explicit padding preserves its ABI.
	info := struct {
		Size         uint32
		_            uint32
		Bytes, Items int64
	}{Size: 24}
	hr, _, _ := queryRecycleBin.Call(0, uintptr(unsafe.Pointer(&info)))
	if int32(hr) < 0 {
		return nil, fmt.Errorf("query Recycle Bin: HRESULT 0x%08x", uint32(hr))
	}
	e.Prog.AddScanned(info.Items)
	if info.Items <= 0 {
		return nil, nil
	}
	return []Item{{Path: WindowsRecycleBinPath, Label: "Recycle Bin", Size: info.Bytes,
		Kind: KindCommand, Risk: RiskReview,
		Note: "Cleaning empties the Recycle Bin permanently on all drives"}}, nil
}
