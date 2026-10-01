package scan

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"transom/internal/safety"
)

// WindowsRecycleBinPath identifies the native Recycle Bin operation. It is
// display text, never an executable or a filesystem cleanup path.
const WindowsRecycleBinPath = "Windows Recycle Bin"

func windowsAppData(home, name, fallback string) string {
	if p := os.Getenv(name); p != "" && filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(home, "AppData", fallback)
}

func windowsTempDir(home string) string {
	return filepath.Join(windowsAppData(home, "LOCALAPPDATA", "Local"), "Temp")
}

// WindowsPaths is the exact allowlist shared by scanning and the cleanup
// guard. It never returns an app profile, AppData container, installed package
// folder, operating-system folder or another user's temporary directory.
func WindowsPaths(home, category string) []string {
	return windowsPaths(context.Background(), home, category)
}

func windowsPaths(ctx context.Context, home, category string) []string {
	local := windowsAppData(home, "LOCALAPPDATA", "Local")
	roaming := windowsAppData(home, "APPDATA", "Roaming")
	var paths []string
	switch category {
	case "temp-files":
		paths = children(windowsTempDir(home))
	case "package-caches":
		for _, c := range windowsPackageCaches(home) {
			paths = append(paths, c.path)
		}
	case "user-caches":
		// Profile directories are enumerated one level at a time, avoiding
		// glob expansion through junctions. Only Chromium's disposable
		// Cache, Code Cache and GPUCache are included: never cookies,
		// databases, extensions, sessions or Service Worker offline data.
		for _, browser := range []string{"Google/Chrome", "Microsoft/Edge", "BraveSoftware/Brave-Browser", "Chromium"} {
			root := filepath.Join(local, filepath.FromSlash(browser), "User Data")
			for _, profile := range children(root) {
				if !browserProfile(filepath.Base(profile)) || !plainDir(profile) {
					continue
				}
				for _, cache := range []string{"Cache", "Code Cache", "GPUCache"} {
					paths = append(paths, filepath.Join(profile, cache))
				}
			}
		}
		for _, profile := range children(filepath.Join(local, "Mozilla", "Firefox", "Profiles")) {
			if plainDir(profile) {
				paths = append(paths, filepath.Join(profile, "cache2"))
			}
		}
		for _, app := range []string{"Code", "Code - Insiders", "discord", "DiscordCanary", "Slack"} {
			for _, cache := range []string{"Cache", "Code Cache", "GPUCache"} {
				paths = append(paths, filepath.Join(roaming, app, cache))
			}
		}
	case "user-logs":
		for _, app := range []string{"Code", "Code - Insiders", "discord", "DiscordCanary"} {
			paths = append(paths, filepath.Join(roaming, app, "logs"))
		}
		for _, p := range children(filepath.Join(local, "CrashDumps")) {
			if strings.EqualFold(filepath.Ext(p), ".dmp") {
				paths = append(paths, p)
			}
		}
	case "visual-studio":
		for _, version := range children(filepath.Join(local, "Microsoft", "VisualStudio")) {
			if visualStudioVersion.MatchString(filepath.Base(version)) && plainDir(version) {
				paths = append(paths, filepath.Join(version, "ComponentModelCache"))
			}
		}
	case "app-leftovers":
		// Query each family on every call, including cleanup preflight.
		// A package reinstalled after scanning immediately loses its
		// place in the cleanup allowlist.
		paths = windowsPackageLeftovers(home, windowsPackageCount)
	}
	if category == "user-caches" || category == "temp-files" {
		// A known cache location can still contain an update or portable
		// application. Never hand such a parent directory to cleanup.
		var safe []string
		for _, p := range paths {
			if ctx.Err() != nil {
				break
			}
			if safety.CheckCleanup(ctx, p, home, "windows") == nil {
				safe = append(safe, p)
			}
		}
		paths = safe
	}
	return paths
}

var visualStudioVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+(?:_[A-Za-z0-9]+)?$`)

func browserProfile(name string) bool {
	if name == "Default" {
		return true
	}
	if !strings.HasPrefix(name, "Profile ") || len(name) == len("Profile ") {
		return false
	}
	for _, c := range strings.TrimPrefix(name, "Profile ") {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func plainDir(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.IsDir() && !isReparse(fi) && safeReadPath(path)
}

func windowsPackageCaches(home string) []pkgCache {
	local := windowsAppData(home, "LOCALAPPDATA", "Local")
	roaming := windowsAppData(home, "APPDATA", "Roaming")
	return []pkgCache{
		{"npm", filepath.Join(local, "npm-cache", "_cacache")},
		{"npm", filepath.Join(roaming, "npm-cache", "_cacache")},
		{"Yarn", filepath.Join(local, "Yarn", "Cache")},
		{"Yarn", filepath.Join(home, ".yarn", "berry", "cache")},
		{"pnpm store", filepath.Join(local, "pnpm", "store")},
		{"pnpm", filepath.Join(local, "pnpm-cache")},
		{"Composer", filepath.Join(local, "Composer")},
		{"pip", filepath.Join(local, "pip", "Cache")},
		{"Go build cache", filepath.Join(local, "go-build")},
		{"Gradle", filepath.Join(home, ".gradle", "caches")},
		{"NuGet HTTP cache", filepath.Join(local, "NuGet", "v3-cache")},
		{"Bun", filepath.Join(home, ".bun", "install", "cache")},
	}
}

func windowsItems(ctx context.Context, e *Env, category string) []Item {
	paths := windowsPaths(ctx, e.Home, category)
	return entryItems(e.sizePaths(ctx, paths), func(s sized) string {
		for _, root := range []string{windowsAppData(e.Home, "LOCALAPPDATA", "Local"), windowsAppData(e.Home, "APPDATA", "Roaming")} {
			if rel, err := filepath.Rel(root, s.path); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return rel
			}
		}
		return filepath.Base(s.path)
	})
}
