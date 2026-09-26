//go:build darwin

package macapp

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"transom/internal/version"
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

// EnsureInstalled makes sure a copy of Transom.app matching this
// build's version (internal/version.Number) is installed at
// /Applications (or ~/Applications, if /Applications isn't writable —
// Homebrew installs put `transom` on PATH but can't write into
// /Applications, and not every /Applications is group-writable),
// installing or updating it as needed. notify, when non-nil, is called
// once with a human-readable progress message right before an install
// or update begins; it's not called when the installed app already
// matches this build's version.
//
// Callers should check Available() first — EnsureInstalled errors if
// this build has no embedded app at all (macapp/build.sh never ran).
func EnsureInstalled(notify func(string)) (path string, changed bool, err error) {
	if !Available() {
		return "", false, fmt.Errorf("macapp: no embedded Transom.app bundle in this build (run macapp/build.sh first)")
	}
	dest, err := chooseDest(destCandidates(), writableDir)
	if err != nil {
		return "", false, err
	}
	target := filepath.Join(dest, "Transom.app")

	switch current := installedVersion(target); {
	case current == version.Number:
		return target, false, nil
	case current == "":
		if notify != nil {
			notify(fmt.Sprintf("Installing Transom.app into %s…", dest))
		}
	default:
		if notify != nil {
			notify(fmt.Sprintf("Updating Transom.app to %s…", version.Number))
		}
		// A running copy holds its own Contents/Resources/transom open;
		// ask it to quit first so it doesn't keep running stale code
		// after Install below replaces the bundle out from under it.
		// Install itself replaces atomically either way — this is only
		// about not leaving a live process on the old version.
		quitRunningApp()
	}

	if err := Install(dest); err != nil {
		return "", false, err
	}
	return target, true, nil
}

// Installed reports whether a copy of Transom.app already exists at any
// of the usual install locations, regardless of version. It's used when
// this build has no embedded bundle to install (a dev build without
// macapp/build.sh having run): if a previously installed app is there,
// `transom ui` opens it rather than falling back to the browser.
func Installed() (path string, ok bool) {
	for _, dest := range destCandidates() {
		target := filepath.Join(dest, "Transom.app")
		if info, err := os.Stat(target); err == nil && info.IsDir() {
			return target, true
		}
	}
	return "", false
}

// destCandidates lists install directories in preference order:
// /Applications, then the per-user ~/Applications.
func destCandidates() []string {
	dests := []string{"/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dests = append(dests, filepath.Join(home, "Applications"))
	}
	return dests
}

// chooseDest returns the first candidate writable reports as usable.
// writable is injected so tests can probe temp directories instead of
// the real /Applications.
func chooseDest(candidates []string, writable func(string) bool) (string, error) {
	for _, c := range candidates {
		if writable(c) {
			return c, nil
		}
	}
	return "", fmt.Errorf("macapp: no writable Applications directory found (tried %v)", candidates)
}

// writableDir reports whether dir exists (or can be created) and can be
// written to: it creates dir if needed, then confirms with a throwaway
// probe file.
func writableDir(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	probe := filepath.Join(dir, fmt.Sprintf(".transom-write-test-%d", os.Getpid()))
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o644)
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(probe)
	return true
}

// installedVersion reads CFBundleShortVersionString from appPath's
// Info.plist, returning "" if appPath doesn't exist or the plist can't
// be read — either way, the caller treats that as "needs installing".
func installedVersion(appPath string) string {
	plist := filepath.Join(appPath, "Contents", "Info.plist")
	out, err := exec.Command("plutil", "-extract", "CFBundleShortVersionString", "raw", "-o", "-", plist).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// transomAppBundleID is Transom.app's CFBundleIdentifier (see
// macapp/Info.plist), used to ask a running copy to quit by identity
// rather than by name or path.
const transomAppBundleID = "dev.transom.app"

// quitRunningApp asks a running Transom.app to quit and waits up to 5s
// for it to actually exit. Best-effort: if nothing is running, or it
// doesn't quit in time, the caller proceeds regardless.
func quitRunningApp() {
	if !appRunning() {
		return
	}
	_ = exec.Command("osascript", "-e", fmt.Sprintf(`quit app id %q`, transomAppBundleID)).Run()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !appRunning() {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// appRunning reports whether Transom.app's process is currently running.
func appRunning() bool {
	return exec.Command("pgrep", "-f", "Transom.app/Contents/MacOS/Transom").Run() == nil
}
