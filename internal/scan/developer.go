package scan

import (
	"context"
	"os"
	"path/filepath"
)

func scanXcode(ctx context.Context, e *Env) ([]Item, error) {
	dev := filepath.Join(e.Home, "Library", "Developer")
	sections := []struct {
		dir, label, risk, note string
	}{
		{filepath.Join(dev, "Xcode", "DerivedData"), "DerivedData", RiskSafe, "Rebuilt on the next build"},
		{filepath.Join(dev, "Xcode", "iOS DeviceSupport"), "iOS DeviceSupport", RiskSafe, "Re-copied when the device is next connected"},
		{filepath.Join(dev, "Xcode", "watchOS DeviceSupport"), "watchOS DeviceSupport", RiskSafe, "Re-copied when the device is next connected"},
		{filepath.Join(dev, "Xcode", "tvOS DeviceSupport"), "tvOS DeviceSupport", RiskSafe, "Re-copied when the device is next connected"},
		{filepath.Join(dev, "Xcode", "visionOS DeviceSupport"), "visionOS DeviceSupport", RiskSafe, "Re-copied when the device is next connected"},
		{filepath.Join(dev, "CoreSimulator", "Caches"), "Simulator caches", RiskSafe, ""},
		{filepath.Join(dev, "Xcode", "Archives"), "Archives", RiskReview, "App archives: needed to symbolicate crash reports of shipped builds"},
	}
	var items []Item
	for _, s := range sections {
		for _, it := range entryItems(e.sizePaths(ctx, children(s.dir)), baseLabel) {
			it.Label = s.label + ": " + it.Label
			it.Risk = s.risk
			it.Note = s.note
			items = append(items, it)
		}
	}
	return items, nil
}

type pkgCache struct{ label, path string }

// packageCaches lists the download caches of package managers. They're
// removed as a whole; each tool recreates its cache on demand. The Go
// module cache (~/go/pkg/mod) is deliberately absent: it's read-only on
// disk and needs `go clean -modcache`.
func packageCaches(home string) []pkgCache {
	lc := filepath.Join(home, "Library", "Caches")
	caches := []pkgCache{
		{"npm", filepath.Join(home, ".npm", "_cacache")},
		{"Yarn", filepath.Join(lc, "Yarn")},
		{"Yarn", filepath.Join(home, ".yarn", "berry", "cache")},
		{"Yarn", filepath.Join(home, ".cache", "yarn")},
		{"pnpm store", filepath.Join(home, "Library", "pnpm", "store")},
		{"pnpm store", filepath.Join(home, ".local", "share", "pnpm", "store")},
		{"pnpm store", filepath.Join(home, ".pnpm-store")},
		{"pnpm", filepath.Join(lc, "pnpm")},
		{"Composer", filepath.Join(lc, "composer")},
		{"Composer", filepath.Join(home, ".composer", "cache")},
		{"Composer", filepath.Join(home, ".cache", "composer")},
		{"pip", filepath.Join(lc, "pip")},
		{"pip", filepath.Join(home, ".cache", "pip")},
		{"Go build cache", filepath.Join(lc, "go-build")},
		{"Gradle", filepath.Join(home, ".gradle", "caches")},
		{"CocoaPods", filepath.Join(lc, "CocoaPods")},
		{"Bun", filepath.Join(home, ".bun", "install", "cache")},
	}
	if gc := os.Getenv("GOCACHE"); gc != "" && gc != "off" {
		caches = append(caches, pkgCache{"Go build cache", gc})
	}
	return caches
}

func scanPackageCaches(ctx context.Context, e *Env) ([]Item, error) {
	caches := packageCaches(e.Home)
	label := map[string]string{}
	var paths []string
	for _, c := range caches {
		if fi, err := os.Lstat(c.path); err != nil || !fi.IsDir() {
			continue
		}
		if _, dup := label[c.path]; dup {
			continue
		}
		label[c.path] = c.label
		paths = append(paths, c.path)
	}
	items := entryItems(e.sizePaths(ctx, paths), func(s sized) string { return label[s.path] })
	for i := range items {
		items[i].Note = "Re-downloaded when needed"
	}
	return items, nil
}
