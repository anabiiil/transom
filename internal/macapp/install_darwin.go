//go:build darwin

package macapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Install extracts the embedded Transom.app bundle into dest (typically
// /Applications), replacing any existing Transom.app there atomically:
// the bundle is extracted into a temp directory next to dest first, any
// previous Transom.app at the target is removed, then the extraction is
// renamed into place as a single filesystem operation. The extracted
// bundle's com.apple.quarantine xattr (if any) is stripped so a fresh
// install doesn't trigger Gatekeeper's "downloaded from the internet"
// prompt on first launch.
//
// Returns an error if this build has no embedded app (i.e.
// macapp/build.sh never ran) — callers should check Available() first
// to print a friendlier message instead of surfacing this error raw.
func Install(dest string) error {
	if !Available() {
		return fmt.Errorf("macapp: no embedded Transom.app bundle in this build (run macapp/build.sh first)")
	}
	f, err := bundleFS.Open(tarballPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("macapp: creating %s: %w", dest, err)
	}
	target := filepath.Join(dest, "Transom.app")

	tmp, err := os.MkdirTemp(dest, ".transom-app-*")
	if err != nil {
		return fmt.Errorf("macapp: creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmp)

	if err := extractTarGz(f, tmp); err != nil {
		return fmt.Errorf("macapp: extracting bundle: %w", err)
	}
	extracted := filepath.Join(tmp, "Transom.app")
	if info, err := os.Stat(extracted); err != nil || !info.IsDir() {
		return fmt.Errorf("macapp: bundle did not contain Transom.app")
	}

	removeQuarantine(extracted)

	removeAppBundle(target)
	if err := os.Rename(extracted, target); err != nil {
		return fmt.Errorf("macapp: installing to %s: %w", target, err)
	}
	return nil
}

// Remove deletes an installed copy of Transom.app from dest (typically
// /Applications).
func Remove(dest string) error {
	removeAppBundle(filepath.Join(dest, "Transom.app"))
	return nil
}

// removeAppBundle deletes path, but only when it's literally named
// Transom.app — a defensive check against ever recursing into the wrong
// directory.
func removeAppBundle(path string) {
	if filepath.Base(path) != "Transom.app" {
		return
	}
	os.RemoveAll(path)
}

// removeQuarantine strips com.apple.quarantine recursively from the
// extracted bundle. Best-effort: a missing xattr binary, an unquarantined
// tree (the common case — this bundle was built and embedded locally,
// never downloaded), or any other failure here must never fail the
// install.
func removeQuarantine(path string) {
	_ = exec.Command("xattr", "-dr", "com.apple.quarantine", path).Run()
}
