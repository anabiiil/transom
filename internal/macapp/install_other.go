//go:build !darwin

package macapp

import "fmt"

// Install is a no-op outside macOS — there is no Transom.app to place.
func Install(dest string) error { return nil }

// Remove is a no-op outside macOS.
func Remove(dest string) error { return nil }

// EnsureInstalled is unsupported outside macOS. transom ui only calls
// it from its darwin-only branch, so this is never reached in practice.
func EnsureInstalled(notify func(string)) (path string, changed bool, err error) {
	return "", false, fmt.Errorf("macapp: EnsureInstalled is darwin-only")
}

// Installed always reports nothing outside macOS.
func Installed() (path string, ok bool) { return "", false }
