//go:build !windows

package scan

import "errors"

func windowsPackageCount(string) (uint32, error) {
	return 0, errors.New("Windows package registration is unavailable")
}
