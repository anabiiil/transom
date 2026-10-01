//go:build !darwin && !windows && !linux

package ui

import "errors"

func diskInfo() (DiskInfo, error) {
	return DiskInfo{}, errors.New("disk usage unavailable on this platform")
}
