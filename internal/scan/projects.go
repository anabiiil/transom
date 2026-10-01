package scan

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Last-touched detection. Directory mtimes and tool-written files are
// useless here: git refreshes .git on every status/fetch, `npm install`
// rewrites package-lock.json, Finder writes .DS_Store, dev tools rewrite
// .env — any of them made every project look fresh. What counts is the
// newest mtime of the project's own (non-hidden) files a few levels
// deep, and of git activity that means work (HEAD moves, branch refs).

const (
	touchMaxDepth = 4     // levels below the project walked for source files
	touchMaxFiles = 20000 // give up (treat as "unknown", i.e. not stale) beyond this
)

// lockFiles are rewritten by installs, not by working on the project.
var lockFiles = map[string]bool{
	"package-lock.json": true, "npm-shrinkwrap.json": true, "yarn.lock": true,
	"pnpm-lock.yaml": true, "bun.lockb": true, "bun.lock": true, "composer.lock": true,
	"Cargo.lock": true, "Gemfile.lock": true, "poetry.lock": true, "Pipfile.lock": true,
	"uv.lock": true, "go.sum": true,
}

// generatedDirs hold build output, caches and runtime state, not edits.
var generatedDirs = map[string]bool{
	"node_modules": true, "vendor": true, "target": true, "storage": true,
	"dist": true, "build": true, "out": true, "coverage": true, "tmp": true,
	"temp": true, "logs": true, "log": true, "cache": true, "__pycache__": true,
	"bower_components": true, "Pods": true, "DerivedData": true,
}

// projectLastTouched estimates when the project was last worked on. It
// returns the zero time when that can't be told (too many files,
// unreadable), which callers treat as "not stale".
func projectLastTouched(project string) time.Time {
	var newest time.Time
	bump := func(t time.Time) {
		if t.After(newest) {
			newest = t
		}
	}
	files := 0
	var walk func(dir string, depth int) bool
	walk = func(dir string, depth int) bool {
		entries, err := readDirUnsorted(dir)
		if err != nil {
			return depth > 1 // an unreadable subfolder is skipped; an unreadable project is unknown
		}
		for _, e := range entries {
			name := e.Name()
			if strings.HasPrefix(name, ".") || lockFiles[name] {
				continue
			}
			fi, err := e.Info()
			if err != nil || isReparse(fi) {
				continue
			}
			switch {
			case fi.IsDir():
				if depth < touchMaxDepth && !generatedDirs[name] && !isBundle(name) {
					if !walk(filepath.Join(dir, name), depth+1) {
						return false
					}
				}
			case fi.Mode().IsRegular():
				if files++; files > touchMaxFiles {
					return false
				}
				bump(fi.ModTime())
			}
		}
		return true
	}
	if !walk(project, 1) {
		return time.Time{}
	}

	// Git: HEAD moving (commit, checkout, pull) and local branch refs.
	// Not .git itself, index or FETCH_HEAD: status/fetch touch those.
	git := filepath.Join(project, ".git")
	for _, p := range []string{"HEAD", filepath.Join("logs", "HEAD"), "ORIG_HEAD"} {
		if fi, err := os.Lstat(filepath.Join(git, p)); err == nil && !isReparse(fi) && safeReadPath(filepath.Join(git, p)) {
			bump(fi.ModTime())
		}
	}
	_ = filepath.WalkDir(filepath.Join(git, "refs", "heads"), func(p string, d fs.DirEntry, err error) error {
		if err == nil {
			fi, statErr := d.Info()
			if statErr != nil || isReparse(fi) || !safeReadPath(p) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				bump(fi.ModTime())
			}
		}
		return nil
	})

	if newest.IsZero() {
		if fi, err := os.Lstat(project); err == nil {
			newest = fi.ModTime()
		}
	}
	return newest
}

func scanStaleDeps(ctx context.Context, e *Env) ([]Item, error) {
	idx := e.homeIndex(ctx, e.Opts.Roots)
	cutoff := time.Now().AddDate(0, 0, -e.Opts.StaleDays)
	touched := map[string]time.Time{}
	var paths []string
	for _, d := range idx.deps {
		t, ok := touched[d.project+"\x00"]
		if !ok {
			t = projectLastTouched(d.project)
			touched[d.project+"\x00"] = t
		}
		if t.IsZero() || !t.Before(cutoff) {
			continue
		}
		touched[d.path] = t
		paths = append(paths, d.path)
	}
	var items []Item
	for _, s := range e.sizePaths(ctx, paths) {
		t := touched[s.path]
		days := int(time.Since(t).Hours() / 24)
		items = append(items, Item{
			Path:    s.path,
			Label:   tildify(e.Home, s.path),
			Size:    s.size,
			ModTime: rfc3339(t),
			Kind:    KindDir,
			Note:    fmt.Sprintf("Project untouched for %d days; reinstall to restore", days),
		})
	}
	return items, nil
}

func scanLargeFiles(ctx context.Context, e *Env) ([]Item, error) {
	roots := []string{e.Home}
	if runtime.GOOS == "windows" {
		folders := WindowsUserFolders(e.Home)
		for _, name := range []string{"Desktop", "Documents", "Downloads"} {
			path := folders[name]
			rel, err := filepath.Rel(e.Home, path)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				roots = append(roots, path)
			}
		}
	}
	idx := e.homeIndex(ctx, roots)
	items := make([]Item, 0, len(idx.large))
	for _, s := range idx.large {
		items = append(items, Item{
			Path:    s.path,
			Label:   filepath.Base(s.path),
			Size:    s.size,
			ModTime: rfc3339(s.newest),
			Kind:    KindFile,
			Note:    "In " + tildify(e.Home, filepath.Dir(s.path)),
		})
	}
	return items, nil
}
