//go:build !darwin

package macapp

// Install is a no-op outside macOS — there is no Transom.app to place.
func Install(dest string) error { return nil }

// Remove is a no-op outside macOS.
func Remove(dest string) error { return nil }
