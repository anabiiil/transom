package scan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsCacheAllowlistPreservesProfileData(t *testing.T) {
	home := fakeHome(t)
	local := filepath.Join(home, "AppData", "Local")
	roaming := filepath.Join(home, "AppData", "Roaming")
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", roaming)
	chrome := filepath.Join(local, "Google", "Chrome", "User Data")
	for _, profile := range []string{"Default", "Profile 2", "System Profile", "Profile ../../escape", "Personal Data"} {
		// Invalid names with traversal components must never be generated
		// by directory enumeration; the helper is still tested directly.
		if strings.Contains(profile, "/") {
			continue
		}
		write(t, filepath.Join(chrome, profile, "Cache", "data"), []byte("cache"))
		write(t, filepath.Join(chrome, profile, "Cookies"), []byte("personal"))
		write(t, filepath.Join(chrome, profile, "Service Worker", "CacheStorage", "data"), []byte("offline data"))
	}
	write(t, filepath.Join(roaming, "Code", "User", "settings.json"), []byte("settings"))
	write(t, filepath.Join(roaming, "Code", "Cache", "data"), []byte("cache"))
	paths := WindowsPaths(home, "user-caches")
	allowed := map[string]bool{}
	for _, p := range paths {
		allowed[p] = true
		if p == chrome || p == filepath.Join(chrome, "Default") || strings.Contains(p, "Cookies") || strings.Contains(p, "Service Worker") || strings.Contains(p, "System Profile") || strings.Contains(p, "Personal Data") || strings.Contains(p, "settings.json") {
			t.Fatalf("profile data included in cache allowlist: %s", p)
		}
	}
	for _, p := range []string{filepath.Join(chrome, "Default", "Cache"), filepath.Join(chrome, "Profile 2", "Code Cache"), filepath.Join(roaming, "Code", "Cache")} {
		if !allowed[p] {
			t.Errorf("missing known cache %s", p)
		}
	}
	for _, invalid := range []string{"Profile ", "Profile ../../escape", "Profile X", "System Profile", "Default.evil"} {
		if browserProfile(invalid) {
			t.Errorf("accepted invalid Chromium profile %q", invalid)
		}
	}
}

func TestWindowsPackageCachesExcludeInstalledPackages(t *testing.T) {
	home := fakeHome(t)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("GOCACHE", home) // arbitrary overrides must never widen Windows cleanup
	var haveNuGet bool
	for _, c := range windowsPackageCaches(home) {
		if c.path == home || strings.Contains(c.path, "Library") || strings.Contains(c.path, ".nuget") || strings.Contains(c.path, filepath.Join("go", "pkg", "mod")) {
			t.Fatalf("unsafe or macOS package path: %+v", c)
		}
		if c.path == filepath.Join(home, "AppData", "Local", "NuGet", "v3-cache") {
			haveNuGet = true
		}
	}
	if !haveNuGet {
		t.Fatal("NuGet HTTP cache missing")
	}
}

func TestWindowsTempNeverUsesSystemOverride(t *testing.T) {
	home := fakeHome(t)
	local := filepath.Join(home, "AppData", "Local")
	t.Setenv("LOCALAPPDATA", local)
	systemTemp := realTemp(t)
	t.Setenv("TEMP", systemTemp)
	t.Setenv("TMP", systemTemp)
	write(t, filepath.Join(systemTemp, "other-user.tmp"), []byte("keep"))
	userFile := filepath.Join(local, "Temp", "user.tmp")
	write(t, userFile, []byte("temporary"))
	paths := WindowsPaths(home, "temp-files")
	if len(paths) != 1 || paths[0] != userFile {
		t.Fatalf("temp allowlist widened outside user folder: %v", paths)
	}
}

func TestWindowsLogsOnlyCrashDumpsAndKnownLogFolders(t *testing.T) {
	home := fakeHome(t)
	local := filepath.Join(home, "AppData", "Local")
	t.Setenv("LOCALAPPDATA", local)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	dump := filepath.Join(local, "CrashDumps", "app.exe.1234.dmp")
	write(t, dump, []byte("diagnostics"))
	write(t, filepath.Join(local, "CrashDumps", "personal.txt"), []byte("keep"))
	found := false
	for _, p := range WindowsPaths(home, "user-logs") {
		if p == dump {
			found = true
		}
		if filepath.Base(p) == "personal.txt" || filepath.Base(p) == "CrashDumps" {
			t.Fatalf("log allowlist widened: %s", p)
		}
	}
	if !found {
		t.Fatal("known crash dump not found")
	}
}

func TestWindowsVisualStudioCachePreservesSettings(t *testing.T) {
	home := fakeHome(t)
	local := filepath.Join(home, "AppData", "Local")
	t.Setenv("LOCALAPPDATA", local)
	root := filepath.Join(local, "Microsoft", "VisualStudio")
	for _, version := range []string{"17.0_ab123", "14.0", "Settings", "17.0_weird.name"} {
		write(t, filepath.Join(root, version, "ComponentModelCache", "data"), []byte("cache"))
	}
	paths := WindowsPaths(home, "visual-studio")
	if len(paths) != 2 {
		t.Fatalf("Visual Studio cache selection: %v", paths)
	}
	for _, p := range paths {
		if filepath.Base(p) != "ComponentModelCache" || filepath.Base(filepath.Dir(p)) == "Settings" || strings.Contains(filepath.Base(filepath.Dir(p)), "weird.name") {
			t.Fatalf("Visual Studio settings included: %s", p)
		}
	}
}

func TestWindowsRegistryUsesSupportedCategories(t *testing.T) {
	ids := map[string]Category{}
	for _, d := range platformRegistry("windows") {
		ids[d.ID] = d.Category
	}
	for _, id := range []string{"xcode", "mail-downloads", "simulators", "homebrew"} {
		if _, ok := ids[id]; ok {
			t.Errorf("macOS category exposed on Windows: %s", id)
		}
	}
	if ids["trash"].Name != "Recycle Bin" || ids["visual-studio"].Risk != RiskReview || !strings.Contains(ids["app-leftovers"].Description, "Store/MSIX") || !strings.Contains(ids["app-leftovers"].Description, "Win32") {
		t.Fatalf("Windows categories have misleading metadata: %v", ids)
	}
	if cmds := platformCommands("windows"); len(cmds) != 1 || len(cmds["docker"]) == 0 {
		t.Fatalf("unsupported Windows cleanup commands: %v", cmds)
	}
}

func TestWindowsProtectedTreesAreCaseInsensitive(t *testing.T) {
	for _, name := range []string{"AppData", "APPDATA", "$Recycle.Bin", "WINDOWS", "Program Files", "System Volume Information"} {
		if !windowsProtectedTree("windows", name) || windowsProtectedTree("darwin", name) {
			t.Errorf("wrong platform pruning for %q", name)
		}
	}
	for _, name := range []string{"Projects", "Windows-App", "my-programdata"} {
		if windowsProtectedTree("windows", name) {
			t.Errorf("user project pruned: %q", name)
		}
	}
}

func TestWalkDoesNotFollowLinkedProjectRoot(t *testing.T) {
	root := realTemp(t)
	outside := realTemp(t)
	write(t, filepath.Join(outside, "private", "data.bin"), []byte("keep"))
	link := filepath.Join(root, "linked-project")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	visited := 0
	walkTree(context.Background(), []string{link}, 4, make(chan struct{}, 1), nil, func(string, map[string]bool, string, os.FileInfo, int) bool {
		visited++
		return true
	})
	if visited != 0 {
		t.Fatalf("linked root escaped into another tree (%d entries)", visited)
	}
}
