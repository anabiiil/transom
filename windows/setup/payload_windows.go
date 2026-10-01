//go:build windows && !uninstaller

package main

import "embed"

//go:embed all:payload
var payload embed.FS

const buildMode = "install"

func payloadFile(name string) ([]byte, error) {
	return payload.ReadFile("payload/" + name)
}
