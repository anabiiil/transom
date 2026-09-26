//go:build !darwin

package clean

import "errors"

func volumeTrashDir(p string) (string, error) { return "", errors.New("unsupported") }
