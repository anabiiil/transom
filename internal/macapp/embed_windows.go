//go:build windows

package macapp

import "embed"

// Windows never ships the unrelated macOS application bundle.
//go:embed bundle/.gitkeep
var bundleFS embed.FS

const tarballPath = "bundle/Transom.app.tar.gz"

func Available() bool { return false }
