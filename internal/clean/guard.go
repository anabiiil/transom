//go:build !windows

// Package clean removes items of a scan result — by item id only, after
// every path passes the guard in this file.
package clean

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// protectedHomeChildren may never be removed themselves (their contents
// can be). The contract's list, plus .Trash, which trash mode moves into.
var protectedHomeChildren = []string{
	"Desktop", "Documents", "Downloads", "Library", "Pictures",
	"Movies", "Music", "Applications", "Public", ".Trash",
}

// systemDirs may never be, or contain, a project root.
var systemDirs = []string{
	"/System", "/Library", "/Applications", "/usr", "/bin", "/sbin", "/etc",
	"/private", "/var", "/tmp", "/opt", "/cores", "/dev", "/Network", "/Users",
}

// unsafeProjectRoot reports why root (clean, symlinks resolved) can't
// serve as a project root: "/", "/Volumes", a volume's own root
// ("/Volumes/X"), and system locations (or anything inside them) are
// refused. Paths under /Users are fine only below a user's folder
// (/Users/x/...), i.e. not "/Users" or "/Users/x" themselves.
func unsafeProjectRoot(root string) error {
	if root == "" || !filepath.IsAbs(root) {
		return fmt.Errorf("project root must be absolute: %q", root)
	}
	if root == "/" || root == "/Volumes" || filepath.Dir(root) == "/Volumes" {
		return fmt.Errorf("project root too broad: %s", root)
	}
	for _, d := range systemDirs {
		if strings.EqualFold(root, d) {
			return fmt.Errorf("project root is a system folder: %s", root)
		}
		if d == "/Users" {
			if strings.EqualFold(filepath.Dir(root), d) {
				return fmt.Errorf("project root is a whole user folder: %s", root)
			}
			continue
		}
		if within(strings.ToLower(root), strings.ToLower(d)) {
			return fmt.Errorf("project root inside a system folder: %s", root)
		}
	}
	return nil
}

// checkProjectRoot allows resolved (clean, parent symlinks resolved)
// only if its base name is a dependency folder name and it lies
// strictly inside one of the (safe) roots. It's the one rule that lets
// anything outside $HOME and the temp roots be removed.
func checkProjectRoot(resolved string, roots []string) error {
	if !DepDirNames[filepath.Base(resolved)] {
		return fmt.Errorf("outside the home folder and not a dependency folder: %s", resolved)
	}
	for _, r := range roots {
		if r == "" || !filepath.IsAbs(r) {
			continue
		}
		for _, part := range strings.Split(r, string(filepath.Separator)) {
			if part == ".." {
				return fmt.Errorf("project root contains '..': %s", r)
			}
		}
		root := resolveDir(r)
		if err := unsafeProjectRoot(root); err != nil {
			continue
		}
		if within(resolved, root) {
			return nil
		}
	}
	return fmt.Errorf("not inside a project root: %s", resolved)
}

// DefaultGuard is the guard for the current user: $HOME plus the
// per-user temp directory ($TMPDIR, i.e. /private/var/folders/xx/yyy/T,
// and its parent /private/var/folders/xx/yyy).
func DefaultGuard() (Guard, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Guard{}, err
	}
	g := Guard{Home: home}
	tmp := filepath.Clean(os.TempDir())
	if rt, err := filepath.EvalSymlinks(tmp); err == nil {
		tmp = rt
	}
	g.TempRoots = append(g.TempRoots, tmp)
	if filepath.Base(tmp) == "T" && strings.HasPrefix(tmp, "/private/var/folders/") {
		g.TempRoots = append(g.TempRoots, filepath.Dir(tmp))
	}
	return g, nil
}

// resolveDir cleans and resolves symlinks of a directory, falling back
// to the cleaned path when it can't be resolved.
func resolveDir(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// within reports whether p is strictly inside root (both clean, absolute).
func within(p, root string) bool {
	if root == "/" {
		return p != "/"
	}
	return strings.HasPrefix(p, root+"/")
}

// Check returns the path to operate on if p may be removed, or why not.
//
// Rules (docs/CONTRACT.md, safety principle 4): p must be absolute and
// contain no ".." element; its parent is resolved with EvalSymlinks (the
// leaf is not — a symlink is removed, never its target); the result
// must lie strictly inside $HOME or an allowed temp root; it must not
// be "/", $HOME, one of $HOME's standard folders, a temp root, or an
// ancestor of $HOME. Outside $HOME and the temp roots, only a
// dependency folder strictly inside one of g.ProjectRoots passes.
func (g Guard) checkPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("empty path")
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("not an absolute path: %s", p)
	}
	for _, part := range strings.Split(p, string(filepath.Separator)) {
		if part == ".." {
			return "", fmt.Errorf("path contains '..': %s", p)
		}
	}
	clean := filepath.Clean(p)
	if clean == "/" {
		return "", errors.New("refusing to touch /")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return "", fmt.Errorf("cannot resolve %s: %w", filepath.Dir(clean), err)
	}
	resolved := filepath.Join(parent, filepath.Base(clean))
	if resolved == "/" || filepath.Dir(resolved) == resolved {
		return "", errors.New("refusing to touch /")
	}

	if g.Home == "" {
		return "", errors.New("home directory unknown")
	}
	home := resolveDir(g.Home)
	if home == "/" {
		return "", errors.New("home directory is /")
	}
	// APFS is case-insensitive by default: compare protected names
	// without case so ~/documents can't slip past.
	if strings.EqualFold(resolved, home) || within(home, resolved) || strings.EqualFold(home, resolved) {
		return "", fmt.Errorf("refusing to touch the home folder or its parents: %s", p)
	}
	if strings.EqualFold(filepath.Dir(resolved), home) {
		base := filepath.Base(resolved)
		for _, name := range protectedHomeChildren {
			if strings.EqualFold(base, name) {
				return "", fmt.Errorf("refusing to touch ~/%s itself", name)
			}
		}
	}
	// ~/Library/Caches, ~/Library/Preferences, …: their contents are
	// fair game, the folders themselves never are.
	if strings.EqualFold(filepath.Dir(resolved), filepath.Join(home, "Library")) {
		return "", fmt.Errorf("refusing to touch ~/Library/%s itself", filepath.Base(resolved))
	}

	allowed := within(resolved, home)
	for _, t := range g.TempRoots {
		if t == "" {
			continue
		}
		root := resolveDir(t)
		if root == "/" {
			continue
		}
		if strings.EqualFold(resolved, root) || within(root, resolved) {
			return "", fmt.Errorf("refusing to touch a temp root or its parents: %s", p)
		}
		if within(resolved, root) {
			allowed = true
		}
	}
	if !allowed {
		if len(g.ProjectRoots) == 0 {
			return "", fmt.Errorf("outside the home folder: %s", p)
		}
		if err := checkProjectRoot(resolved, g.ProjectRoots); err != nil {
			return "", err
		}
	}
	if g.RequireCacheRoot {
		allowed := false
		for _, root := range g.CacheRoots {
			if root != "" && filepath.IsAbs(root) && within(resolved, resolveDir(root)) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("path is not in this category's cleanup locations: %s", p)
		}
	}
	return resolved, nil
}
