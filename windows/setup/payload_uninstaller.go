//go:build windows && uninstaller

package main

import "errors"

const buildMode = "uninstall"

func payloadFile(string) ([]byte, error) {
	return nil, errors.New("the uninstaller does not contain installation files")
}
