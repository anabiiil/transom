package safety

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupPreservesApplicationsAndStateAcrossPlatforms(t *testing.T) {
	home := t.TempDir()
	app := filepath.Join(home, "Library", "Caches", "unknown-cache", "Editor.APP")
	appFile := filepath.Join(app, "Contents", "MacOS", "Electron")
	portable := filepath.Join(home, "Tools", "portable")
	portableFile := filepath.Join(portable, "resources", "app", "node_modules", "dep", "index.js")
	state := filepath.Join(home, "Library", "Application Support", "Code", "User", "settings.json")
	extension := filepath.Join(home, ".vscode", "extensions", "theme", "package.json")
	portableState := filepath.Join(home, "Downloads", "code-portable-data", "user-data", "Backups", "unsaved.txt")
	for _, p := range []string{appFile, portableFile, filepath.Join(portable, "Code.exe"), state, extension, portableState} {
		write(t, p)
	}
	for _, goos := range []string{"darwin", "windows"} {
		for _, path := range []string{app, appFile, filepath.Dir(app), portable, portableFile, filepath.Dir(portable), state, extension, portableState} {
			if err := CheckCleanup(context.Background(), path, home, goos); err == nil {
				t.Errorf("%s allowed protected cleanup %s", goos, path)
			}
		}
	}
	cache := filepath.Join(home, "Library", "Caches", "com.vendor.app")
	write(t, filepath.Join(cache, "data.bin"))
	if err := CheckCleanup(context.Background(), cache, home, "darwin"); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsDisposableCachesKeepEditorSettings(t *testing.T) {
	home := t.TempDir()
	appdata := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("APPDATA", appdata)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	profile := filepath.Join(appdata, "Code")
	for _, leaf := range []string{"User/settings.json", "User/workspaceStorage/history", "Backups/unsaved.txt", "extensions/theme/package.json"} {
		path := filepath.Join(profile, filepath.FromSlash(leaf))
		write(t, path)
		if err := CheckCleanup(context.Background(), path, home, "windows"); err == nil {
			t.Errorf("editor state allowed: %s", path)
		}
	}
	for _, leaf := range []string{"Cache", "Code Cache", "GPUCache"} {
		path := filepath.Join(profile, leaf)
		write(t, filepath.Join(path, "data.bin"))
		if err := CheckCleanup(context.Background(), path, home, "windows"); err != nil {
			t.Errorf("disposable cache refused: %s: %v", path, err)
		}
	}
}

func TestCustomEditorDataInsideCachesAndTempIsPreserved(t *testing.T) {
	for _, goos := range []string{"darwin", "windows"} {
		for _, marker := range []string{"User/settings.json", "User/keybindings.json", "User/globalStorage/state.vscdb", "User/workspaceStorage/workspace/state.vscdb"} {
			t.Run(goos+"/"+marker, func(t *testing.T) {
				home := t.TempDir()
				parent := filepath.Join(home, "Library", "Caches", "custom-cache")
				if goos == "windows" {
					parent = filepath.Join(home, "AppData", "Local", "Temp", "custom-cache")
				}
				profile := filepath.Join(parent, "arbitrary-name")
				file := filepath.Join(profile, filepath.FromSlash(marker))
				backup := filepath.Join(profile, "Backups", "unsaved.txt")
				write(t, file)
				write(t, backup)
				for _, path := range []string{parent, profile, filepath.Join(profile, "User"), file, backup} {
					if err := CheckCleanup(context.Background(), path, home, goos); err == nil {
						t.Errorf("custom editor state allowed: %s", path)
					}
				}
			})
		}
	}
}

func TestWrappedMacApplicationInsideCacheIsPreserved(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, "Library", "Caches", "update-staging")
	app := filepath.Join(cache, "Mobile Editor.app")
	write(t, filepath.Join(app, "Wrapper", "Editor.app", "Info.plist"))
	for _, path := range []string{cache, app, filepath.Join(app, "Wrapper", "Editor.app", "Info.plist")} {
		if err := CheckCleanup(context.Background(), path, home, "darwin"); err == nil {
			t.Errorf("wrapped application allowed for cleanup: %s", path)
		}
	}
}

func TestSymlinkedEditorRootsAndApplicationAncestorsStayProtected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows native guard separately rejects junctions")
	}
	home := t.TempDir()
	relocated := t.TempDir()
	write(t, filepath.Join(relocated, "User", "settings.json"))
	profile := filepath.Join(home, "Library", "Application Support", "Code")
	if err := os.MkdirAll(filepath.Dir(profile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(relocated, profile); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(profile, "User", "settings.json"), filepath.Join(relocated, "User", "settings.json")} {
		if err := CheckCleanup(context.Background(), p, home, "darwin"); err == nil {
			t.Errorf("relocated editor state allowed: %s", p)
		}
	}
	app := filepath.Join(home, "Downloads", "Editor.app")
	write(t, filepath.Join(app, "Contents", "Resources", "app", "node_modules", "dep", "index.js"))
	alias := filepath.Join(home, "cache-link")
	if err := os.Symlink(filepath.Join(app, "Contents", "Resources"), alias); err != nil {
		t.Fatal(err)
	}
	if err := CheckCleanup(context.Background(), filepath.Join(alias, "app", "node_modules"), home, "darwin"); err == nil {
		t.Fatal("linked parent inside an application allowed")
	}
}

func TestCleanupPreservesOnCancellationAndInspectionError(t *testing.T) {
	home := t.TempDir()
	cache := filepath.Join(home, "cache")
	write(t, filepath.Join(cache, "file"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := CheckCleanup(ctx, cache, home, "darwin"); err == nil {
		t.Fatal("cancelled safety inspection allowed deletion")
	}
	if runtime.GOOS != "windows" {
		unreadable := filepath.Join(cache, "unreadable")
		write(t, filepath.Join(unreadable, "data"))
		if err := os.Chmod(unreadable, 0); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(unreadable, 0o755)
		if _, err := os.ReadDir(unreadable); err != nil {
			if err := CheckCleanup(context.Background(), cache, home, "darwin"); err == nil {
				t.Fatal("unverifiable subtree allowed deletion")
			}
		}
	}
}
