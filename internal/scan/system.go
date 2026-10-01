package scan

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// appleCacheNames are Apple-owned folders in ~/Library/Caches without a
// com.apple. prefix. Apple daemons keep state in several of them
// (CloudKit, GeoServices...), so they're left alone like com.apple.*.
var appleCacheNames = map[string]bool{
	"CloudKit": true, "GeoServices": true, "PassKit": true, "GameKit": true,
	"Animoji": true, "TrickPlay": true, "FamilyCircle": true, "LSMImageCache": true,
	"SharedImageCache": true, "ARFileCache": true, "Metadata": true,
	"CrashReporter": true, "AddressBook": true, "Safari": true,
}

// entryItems turns sized entries into items with a label relative to base.
func entryItems(entries []sized, label func(s sized) string) []Item {
	items := make([]Item, 0, len(entries))
	for _, s := range entries {
		items = append(items, Item{
			Path:    s.path,
			Label:   label(s),
			Size:    s.size,
			ModTime: rfc3339(s.newest),
			Kind:    kindOf(s.info),
		})
	}
	return items
}

func baseLabel(s sized) string { return filepath.Base(s.path) }

func scanUserCaches(ctx context.Context, e *Env) ([]Item, error) {
	if runtime.GOOS == "windows" {
		return windowsItems(ctx, e, "user-caches"), nil
	}
	dir := filepath.Join(e.Home, "Library", "Caches")
	// Folders other categories report (package managers, Homebrew) are
	// skipped so the same bytes aren't listed twice.
	owned := map[string]bool{"Homebrew": true}
	for _, pc := range packageCaches(e.Home) {
		if filepath.Dir(pc.path) == dir {
			owned[filepath.Base(pc.path)] = true
		}
	}
	var paths []string
	for _, p := range children(dir) {
		name := filepath.Base(p)
		if owned[name] || appleCacheNames[name] || strings.HasPrefix(name, "com.apple.") {
			continue
		}
		paths = append(paths, p)
	}
	return entryItems(e.sizePaths(ctx, paths), baseLabel), nil
}

func scanUserLogs(ctx context.Context, e *Env) ([]Item, error) {
	if runtime.GOOS == "windows" {
		return windowsItems(ctx, e, "user-logs"), nil
	}
	dir := filepath.Join(e.Home, "Library", "Logs")
	return entryItems(e.sizePaths(ctx, children(dir)), baseLabel), nil
}

func scanTempFiles(ctx context.Context, e *Env) ([]Item, error) {
	dir := os.TempDir()
	if runtime.GOOS == "windows" {
		// Never sweep a system-wide TEMP override or another user's folder.
		dir = windowsTempDir(e.Home)
	}
	cutoff := time.Now().Add(-24 * time.Hour)
	var paths []string
	for _, p := range children(dir) {
		name := filepath.Base(p)
		if strings.HasPrefix(name, "com.apple.") {
			continue
		}
		fi, err := os.Lstat(p)
		if err != nil || !(fi.Mode().IsRegular() || fi.IsDir()) {
			continue // sockets, pipes, symlinks: live plumbing, tiny anyway
		}
		paths = append(paths, p)
	}
	var old []sized
	for _, s := range e.sizePaths(ctx, paths) {
		// newest covers everything inside a folder: a temp dir some
		// process still writes into doesn't count as old.
		st := statPath(s.path, s.info)
		if s.newest.Before(cutoff) && st.changed.Before(cutoff) && st.birth.Before(cutoff) {
			old = append(old, s)
		}
	}
	return entryItems(old, baseLabel), nil
}

func scanTrash(ctx context.Context, e *Env) ([]Item, error) {
	if runtime.GOOS == "windows" {
		return scanWindowsRecycleBin(ctx, e)
	}
	dir := filepath.Join(e.Home, ".Trash")
	items := entryItems(e.sizePaths(ctx, children(dir)), baseLabel)
	for i := range items {
		items[i].Note = "Cleaning deletes this permanently"
	}
	return items, nil
}

func scanMailDownloads(ctx context.Context, e *Env) ([]Item, error) {
	dir := filepath.Join(e.Home, "Library", "Containers", "com.apple.mail", "Data", "Library", "Mail Downloads")
	return entryItems(e.sizePaths(ctx, children(dir)), baseLabel), nil
}

func scanOldDownloads(ctx context.Context, e *Env) ([]Item, error) {
	dir := WindowsUserFolders(e.Home)["Downloads"]
	cutoff := time.Now().AddDate(0, 0, -90)
	var paths []string
	for _, p := range children(dir) {
		if strings.HasPrefix(filepath.Base(p), ".") {
			continue
		}
		paths = append(paths, p)
	}
	var old []sized
	for _, s := range e.sizePaths(ctx, paths) {
		// A download's mtime can be the server's (old) Last-Modified;
		// ctime/birth time record when it actually landed here. Use
		// the newest of all so nothing recent is called old.
		st := statPath(s.path, s.info)
		newest := s.newest
		for _, t := range []time.Time{st.changed, st.birth} {
			if t.After(newest) {
				newest = t
			}
		}
		if newest.Before(cutoff) {
			s.newest = newest
			old = append(old, s)
		}
	}
	items := entryItems(old, baseLabel)
	for i := range items {
		items[i].Note = "Not touched in over 90 days"
	}
	return items, nil
}
