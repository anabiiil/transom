package scan

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestUserCacheScanPreservesVSCodeUpdateCopies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("macOS cache locations")
	}
	home := fakeHome(t)
	caches := filepath.Join(home, "Library", "Caches")
	for _, name := range []string{
		"com.microsoft.VSCode", "com.microsoft.VSCode.ShipIt",
		"COM.MICROSOFT.VSCODEINSIDERS.SHIPIT", "com.microsoft.VSCodeExploration",
		"com.vendor.editor.ShipIt",
	} {
		write(t, filepath.Join(caches, name, "update", "Visual Studio Code.app", "Contents", "MacOS", "Electron"), []byte("installed editor"))
	}
	disposable := filepath.Join(caches, "com.vendor.thumbnail-cache")
	write(t, filepath.Join(disposable, "cache.bin"), []byte("rebuildable cache"))
	items, err := scanUserCaches(context.Background(), &Env{Home: home, Prog: NewProgress(), sem: make(chan struct{}, parallelism)})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Path != disposable {
		t.Fatalf("editor or updater was offered for cleanup: %+v", items)
	}
}

func TestUserCacheScanPreservesEmbeddedApplication(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("macOS cache locations")
	}
	home := fakeHome(t)
	caches := filepath.Join(home, "Library", "Caches")
	installed := filepath.Join(caches, "unexpected-cache-name")
	write(t, filepath.Join(installed, "staging", "Editor.app", "Contents", "MacOS", "Editor"), []byte("application"))
	disposable := filepath.Join(caches, "com.vendor.cache")
	write(t, filepath.Join(disposable, "cache.bin"), []byte("cache"))
	items, err := scanUserCaches(context.Background(), &Env{Home: home, Prog: NewProgress(), sem: make(chan struct{}, parallelism)})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Path != disposable {
		t.Fatalf("application-bearing cache was offered for cleanup: %+v", items)
	}
}

func TestTempScanPreservesProgramsRegardlessOfAge(t *testing.T) {
	home := fakeHome(t)
	tmp := realTemp(t)
	write(t, filepath.Join(tmp, "update", "Visual Studio Code.app", "Contents", "MacOS", "Electron"), []byte("Mac editor"))
	write(t, filepath.Join(tmp, "portable-editor", "Code.exe"), []byte("Windows editor"))
	write(t, filepath.Join(tmp, "portable-editor", "resources", "app", "package.json"), []byte(`{"name":"code"}`))
	disposable := filepath.Join(tmp, "old-cache")
	write(t, filepath.Join(disposable, "cache.bin"), []byte("temporary cache"))
	// A future cutoff makes fixture birth/ctime older too. Aging only mtime
	// would not exercise the scan's production age checks.
	items, err := scanTempFilesIn(context.Background(), &Env{Home: home, Prog: NewProgress(), sem: make(chan struct{}, parallelism)}, tmp, time.Now().Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Path != disposable {
		t.Fatalf("application-bearing temporary directory was offered for cleanup: %+v", items)
	}
}

func TestWindowsCacheSelectionPreservesVSCodeInstallAndProfile(t *testing.T) {
	home := fakeHome(t)
	local := filepath.Join(home, "AppData", "Local")
	roaming := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", roaming)
	for _, app := range []string{"Code", "Code - Insiders"} {
		root := filepath.Join(roaming, app)
		for _, rel := range []string{"User/settings.json", "User/globalStorage/state.vscdb", "User/workspaceStorage/history", "Backups/unsaved.txt", "extensions/theme/package.json"} {
			write(t, filepath.Join(root, filepath.FromSlash(rel)), []byte("keep"))
		}
		write(t, filepath.Join(root, "Cache", "cache.bin"), []byte("cache"))
	}
	write(t, filepath.Join(local, "Programs", "Microsoft VS Code", "Code.exe"), []byte("installed editor"))
	write(t, filepath.Join(home, ".vscode", "extensions", "extension", "package.json"), []byte("extension"))
	protected := []string{filepath.Join(local, "Programs", "Microsoft VS Code"), filepath.Join(home, ".vscode")}
	for _, app := range []string{"Code", "Code - Insiders"} {
		root := filepath.Join(roaming, app)
		protected = append(protected, root, filepath.Join(root, "User"), filepath.Join(root, "Backups"), filepath.Join(root, "extensions"))
	}
	var foundCodeCache bool
	for _, p := range WindowsPaths(home, "user-caches") {
		for _, app := range []string{"Code", "Code - Insiders"} {
			root := filepath.Join(roaming, app)
			if p == filepath.Join(root, "Cache") {
				foundCodeCache = true
			}
		}
		for _, root := range protected {
			if p == root || (filepath.Base(root) != "Code" && filepath.Base(root) != "Code - Insiders" && strings.HasPrefix(p, root+string(filepath.Separator))) {
				t.Fatalf("editor installation or profile included in cache allowlist: %s", p)
			}
		}
	}
	if !foundCodeCache {
		t.Fatal("known disposable editor cache missing")
	}
}

func TestWindowsCacheSelectionPreservesPortableEditorInsideCache(t *testing.T) {
	home := fakeHome(t)
	local := filepath.Join(home, "AppData", "Local")
	roaming := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", roaming)
	profile := filepath.Join(local, "Google", "Chrome", "User Data", "Default")
	applicationCache := filepath.Join(profile, "Cache")
	write(t, filepath.Join(applicationCache, "portable", "Code.exe"), []byte("editor"))
	write(t, filepath.Join(applicationCache, "portable", "resources", "app", "package.json"), []byte(`{"name":"code"}`))
	disposable := filepath.Join(profile, "GPUCache")
	write(t, filepath.Join(disposable, "cache.bin"), []byte("cache"))
	var foundDisposable bool
	for _, p := range WindowsPaths(home, "user-caches") {
		if p == applicationCache {
			t.Fatalf("portable editor inside cache was offered for cleanup: %s", p)
		}
		if p == disposable {
			foundDisposable = true
		}
	}
	if !foundDisposable {
		t.Fatal("ordinary browser cache was excluded")
	}
}
