//go:build !darwin && !windows

package clean

import "errors"

func volumeTrashDir(p string) (string, error) { return "", errors.New("unsupported") }
