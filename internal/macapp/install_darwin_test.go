//go:build darwin

package macapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestChooseDestPicksFirstWritable(t *testing.T) {
	base := t.TempDir()
	primary := filepath.Join(base, "primary")   // not writable (fake check)
	fallback := filepath.Join(base, "fallback") // writable

	writable := func(dir string) bool { return dir == fallback }

	got, err := chooseDest([]string{primary, fallback}, writable)
	if err != nil {
		t.Fatalf("chooseDest: %v", err)
	}
	if got != fallback {
		t.Fatalf("chooseDest = %q, want %q", got, fallback)
	}
}

func TestChooseDestPrefersFirstCandidate(t *testing.T) {
	base := t.TempDir()
	primary := filepath.Join(base, "primary")
	fallback := filepath.Join(base, "fallback")

	writable := func(dir string) bool { return true }

	got, err := chooseDest([]string{primary, fallback}, writable)
	if err != nil {
		t.Fatalf("chooseDest: %v", err)
	}
	if got != primary {
		t.Fatalf("chooseDest = %q, want %q (should prefer the first candidate)", got, primary)
	}
}

func TestChooseDestErrorsWhenNoneWritable(t *testing.T) {
	base := t.TempDir()
	candidates := []string{filepath.Join(base, "a"), filepath.Join(base, "b")}

	_, err := chooseDest(candidates, func(string) bool { return false })
	if err == nil {
		t.Fatal("expected an error when no candidate is writable, got nil")
	}
}

func TestWritableDirCreatesAndProbes(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "dir")

	if !writableDir(dir) {
		t.Fatalf("writableDir(%s) = false, want true", dir)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("writableDir should have created %s", dir)
	}
	// The probe file must not be left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("writableDir left files behind: %v", entries)
	}
}

func TestWritableDirFalseForUnwritableParent(t *testing.T) {
	base := t.TempDir()
	// A file (not a directory) can never be MkdirAll'd into.
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	dir := filepath.Join(blocker, "child")

	if writableDir(dir) {
		t.Fatalf("writableDir(%s) = true, want false (parent is a file)", dir)
	}
}

func TestDestCandidatesStartsWithApplications(t *testing.T) {
	cands := destCandidates()
	if len(cands) == 0 || cands[0] != "/Applications" {
		t.Fatalf("destCandidates()[0] = %v, want /Applications first", cands)
	}
}

func writeTestInfoPlist(t *testing.T, appPath, version string) {
	t.Helper()
	contents := filepath.Join(appPath, "Contents")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleShortVersionString</key>
	<string>` + version + `</string>
	<key>CFBundleVersion</key>
	<string>` + version + `</string>
</dict>
</plist>`
	if err := os.WriteFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestInstalledVersionReadsPlist(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "Transom.app")
	writeTestInfoPlist(t, app, "0.1.1")

	got := installedVersion(app)
	if got != "0.1.1" {
		t.Fatalf("installedVersion = %q, want %q", got, "0.1.1")
	}
}

func TestInstalledVersionEmptyWhenMissing(t *testing.T) {
	dir := t.TempDir()
	app := filepath.Join(dir, "NotThere.app")

	if got := installedVersion(app); got != "" {
		t.Fatalf("installedVersion(missing) = %q, want empty string", got)
	}
}
