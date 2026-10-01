//go:build windows

package clean

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"transom/internal/scan"
)

var protectedHomeChildren = []string{
	"Desktop", "Documents", "Downloads", "Pictures", "Videos", "Movies", "Music",
	"AppData", "Application Data", "Local Settings", "Contacts", "Favorites",
	"Links", "Saved Games", "Searches", "OneDrive", "Public", ".Trash",
}

func pathKey(p string) string     { return strings.ToLower(filepath.Clean(p)) }
func pathsEqual(p, q string) bool { return pathKey(p) == pathKey(q) }

// within uses the platform's volume and separator rules, so C:\\Users\\a
// cannot include C:\\Users\\ab or a different drive/share.
func within(p, root string) bool {
	rel, err := filepath.Rel(pathKey(root), pathKey(p))
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// insideAllowedRoot additionally compares directory identities. Case-folding
// alone could mistake a distinct sibling for Home in a directory configured
// with Windows' optional case-sensitive mode (for example a WSL workspace).
func insideAllowedRoot(p, root string) bool {
	if !within(p, root) {
		return false
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		return false
	}
	for parent := filepath.Dir(p); pathsEqual(parent, root) || within(parent, root); parent = filepath.Dir(parent) {
		info, err := os.Stat(parent)
		if err != nil {
			return false
		}
		if os.SameFile(info, rootInfo) {
			return true
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	return false
}

func sameAllowedRoot(p, root string) bool {
	if !pathsEqual(p, root) {
		return false
	}
	left, lerr := os.Lstat(p)
	right, rerr := os.Lstat(root)
	if lerr == nil && rerr == nil {
		return os.SameFile(left, right)
	}
	// Missing scanned leaves are allowed through to Run's disappearance
	// report, but their parents must still be the same actual directory.
	if os.IsNotExist(lerr) && os.IsNotExist(rerr) {
		leftParent, lerr := os.Stat(filepath.Dir(p))
		rightParent, rerr := os.Stat(filepath.Dir(root))
		return lerr == nil && rerr == nil && os.SameFile(leftParent, rightParent)
	}
	return false
}

func volumeRoot(p string) bool {
	p = filepath.Clean(p)
	v := filepath.VolumeName(p)
	return v == "" || p == v || pathsEqual(p, v+string(filepath.Separator)) || filepath.Dir(p) == p
}

// validateWindowsPath excludes Win32 aliases, streams, device namespaces,
// and wildcard interpretation before either the filesystem or Shell sees p.
func validateWindowsPath(p string) error {
	if p == "" || !filepath.IsAbs(p) {
		return fmt.Errorf("not an absolute path: %s", p)
	}
	p = strings.ReplaceAll(p, "/", `\`)
	if strings.HasPrefix(p, `\\?\`) || strings.HasPrefix(p, `\\.\`) || strings.HasPrefix(p, `\??\`) {
		return errors.New("device and extended namespace paths are not allowed")
	}
	v := filepath.VolumeName(p)
	if v == "" {
		return errors.New("path must name a drive or UNC share")
	}
	parts := strings.Split(strings.TrimPrefix(p, v), `\`)
	if strings.HasPrefix(v, `\\`) {
		volumeParts := strings.Split(strings.TrimPrefix(v, `\\`), `\`)
		if len(volumeParts) != 2 || volumeParts[0] == "" || volumeParts[1] == "" {
			return errors.New("invalid UNC server/share")
		}
		parts = append(volumeParts, parts...)
	} else if len(v) != 2 || v[1] != ':' || !((v[0] >= 'A' && v[0] <= 'Z') || (v[0] >= 'a' && v[0] <= 'z')) {
		return errors.New("invalid drive path")
	}
	for _, part := range parts {
		if part == ".." {
			return errors.New("path contains '..'")
		}
		if part == "." {
			return errors.New("path contains a dot element")
		}
		if strings.ContainsAny(part, ":*?\x00\"<>|") || strings.IndexFunc(part, func(r rune) bool { return r < 32 }) >= 0 {
			return errors.New("streams, wildcards and invalid path characters are not allowed")
		}
		if strings.TrimRight(part, " .") != part {
			return errors.New("trailing dots and spaces are not allowed")
		}
		name := strings.ToUpper(strings.TrimRight(strings.SplitN(part, ".", 2)[0], " "))
		if name == "CON" || name == "PRN" || name == "AUX" || name == "NUL" ||
			name == "COM¹" || name == "COM²" || name == "COM³" || name == "LPT¹" || name == "LPT²" || name == "LPT³" ||
			len(name) == 4 && (strings.HasPrefix(name, "COM") || strings.HasPrefix(name, "LPT")) && name[3] >= '0' && name[3] <= '9' {
			return errors.New("reserved device names are not allowed")
		}
	}
	return nil
}

func resolveDir(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(r)
	}
	return p
}

func windowsSystemDirs(root string) []string {
	v := filepath.VolumeName(root)
	var dirs []string
	for _, name := range []string{"Windows", "Program Files", "Program Files (x86)", "ProgramData", "Recovery", "$Recycle.Bin", "System Volume Information", "PerfLogs", "Boot", "MSOCache", "Config.Msi", "Documents and Settings"} {
		dirs = append(dirs, filepath.Join(v+`\`, name))
	}
	for _, key := range []string{"SystemRoot", "WINDIR", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "ProgramData"} {
		if p := os.Getenv(key); p != "" && filepath.IsAbs(p) {
			dirs = append(dirs, resolveDir(p))
		}
	}
	return dirs
}

func windowsAppDataContainers(home string) []string {
	appData := filepath.Join(home, "AppData")
	dirs := []string{appData, filepath.Join(appData, "Local"), filepath.Join(appData, "Roaming"), filepath.Join(appData, "LocalLow")}
	for _, key := range []string{"LOCALAPPDATA", "APPDATA"} {
		if p := os.Getenv(key); validateWindowsPath(p) == nil {
			dirs = append(dirs, resolveDir(p))
		}
	}
	return dirs
}

func unsafeProjectRoot(root string) error {
	if err := validateWindowsPath(root); err != nil {
		return err
	}
	root = filepath.Clean(root)
	if volumeRoot(root) {
		return fmt.Errorf("project root too broad: %s", root)
	}
	for _, d := range windowsSystemDirs(root) {
		if pathsEqual(root, d) || within(root, d) {
			return fmt.Errorf("project root is inside a system folder: %s", root)
		}
	}
	users := filepath.Join(filepath.VolumeName(root)+`\`, "Users")
	if pathsEqual(root, users) || pathsEqual(filepath.Dir(root), users) {
		return fmt.Errorf("project root is a whole users or user folder: %s", root)
	}
	for _, key := range []string{"LOCALAPPDATA", "APPDATA"} {
		if p := os.Getenv(key); validateWindowsPath(p) == nil {
			container := resolveDir(p)
			if pathsEqual(root, container) || within(root, container) || within(container, root) {
				return fmt.Errorf("project root overlaps an AppData container: %s", root)
			}
		}
	}
	// AppData remains application state even when selected as a project root.
	for _, part := range strings.Split(root, `\`) {
		if strings.EqualFold(part, "AppData") {
			return fmt.Errorf("project root is inside AppData: %s", root)
		}
	}
	return nil
}

func checkProjectRoot(resolved string, roots []string) error {
	if !DepDirNames[strings.ToLower(filepath.Base(resolved))] {
		return fmt.Errorf("outside the home folder and not a dependency folder: %s", resolved)
	}
	for _, r := range roots {
		if validateWindowsPath(r) != nil || !scan.WindowsPathSafe(r) {
			continue
		}
		root := resolveDir(r)
		if unsafeProjectRoot(root) == nil && insideAllowedRoot(resolved, root) {
			return nil
		}
	}
	return fmt.Errorf("not inside a safe project root: %s", resolved)
}

func DefaultGuard() (Guard, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Guard{}, err
	}
	g := Guard{Home: home}
	for _, root := range scan.WindowsUserFolders(home) {
		g.UserRoots = append(g.UserRoots, root)
	}
	// The system-wide Windows temp folder is deliberately excluded.
	tmp := resolveDir(os.TempDir())
	if validateWindowsPath(tmp) == nil && !volumeRoot(tmp) && scan.WindowsPathSafe(tmp) {
		blocked := false
		for _, root := range windowsSystemDirs(tmp) {
			if pathsEqual(tmp, root) || within(tmp, root) {
				blocked = true
				break
			}
		}
		if !blocked {
			g.TempRoots = []string{tmp}
		}
	}
	return g, nil
}

// Check rejects reparse points (including junctions) and protects Windows,
// profile, AppData and volume containers. AppData removal is restricted to
// the scanner's approved caches and verified orphan package data. Application
// state never passes merely because it is beneath the user's home.
func (g Guard) checkPath(p string) (string, error) {
	if err := validateWindowsPath(p); err != nil {
		return "", err
	}
	clean := filepath.Clean(p)
	if volumeRoot(clean) {
		return "", errors.New("refusing to touch a drive or share root")
	}
	if g.Home == "" {
		return "", errors.New("home directory unknown")
	}
	if validateWindowsPath(g.Home) != nil || volumeRoot(g.Home) || !scan.WindowsPathSafe(g.Home) {
		return "", errors.New("home directory is unsafe")
	}
	if !scan.WindowsPathSafe(clean) {
		return "", errors.New("cannot remove reparse points or paths through junctions")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return "", fmt.Errorf("cannot resolve %s: %w", filepath.Dir(clean), err)
	}
	resolved := filepath.Join(parent, filepath.Base(clean))
	if err := validateWindowsPath(resolved); err != nil {
		return "", err
	}
	home := resolveDir(g.Home)
	if pathsEqual(resolved, home) || within(home, resolved) {
		return "", fmt.Errorf("refusing to touch the home folder or its parents: %s", p)
	}
	for _, system := range windowsSystemDirs(resolved) {
		if pathsEqual(resolved, system) || within(resolved, system) || within(system, resolved) {
			return "", fmt.Errorf("refusing to touch a system folder: %s", p)
		}
	}
	if pathsEqual(filepath.Dir(resolved), home) {
		for _, name := range protectedHomeChildren {
			if strings.EqualFold(filepath.Base(resolved), name) {
				return "", fmt.Errorf("refusing to touch ~/%s itself", name)
			}
		}
	}
	containers := windowsAppDataContainers(home)
	insideAppData := false
	for _, root := range containers {
		if pathsEqual(resolved, root) || within(root, resolved) {
			return "", fmt.Errorf("refusing to touch an AppData container or its parents: %s", p)
		}
		if within(resolved, root) {
			insideAppData = true
		}
	}
	allowed := insideAllowedRoot(resolved, home) && !insideAppData
	for _, t := range g.TempRoots {
		if validateWindowsPath(t) != nil || volumeRoot(t) || !scan.WindowsPathSafe(t) {
			continue
		}
		root := resolveDir(t)
		if pathsEqual(resolved, root) || within(root, resolved) {
			return "", fmt.Errorf("refusing to touch a temp root or its parents: %s", p)
		}
		if insideAllowedRoot(resolved, root) {
			allowed = true
		}
	}
	cacheAllowed := false
	for _, r := range g.CacheRoots {
		if validateWindowsPath(r) != nil || volumeRoot(r) || !scan.WindowsPathSafe(r) {
			continue
		}
		root := resolveDir(r)
		broad := false
		for _, container := range containers {
			if pathsEqual(root, container) || within(container, root) {
				broad = true
				break
			}
		}
		if !broad && (sameAllowedRoot(resolved, root) || insideAllowedRoot(resolved, root)) {
			allowed = true
			cacheAllowed = true
		}
	}
	for _, r := range g.UserRoots {
		if validateWindowsPath(r) != nil || volumeRoot(r) {
			continue
		}
		root := resolveDir(r)
		if pathsEqual(resolved, root) || within(root, resolved) {
			return "", fmt.Errorf("refusing to touch a user folder or its parents: %s", p)
		}
		if scan.WindowsPathSafe(root) && insideAllowedRoot(resolved, root) && !insideAppData {
			allowed = true
		}
	}
	if g.RequireCacheRoot && !cacheAllowed {
		return "", fmt.Errorf("item is not in the current cleanup allowlist; rescan and try again: %s", p)
	}
	if insideAppData && !allowed {
		return "", fmt.Errorf("AppData path is not an approved cleanup location: %s", p)
	}
	if !allowed {
		if len(g.ProjectRoots) == 0 {
			return "", fmt.Errorf("outside the home folder: %s", p)
		}
		if err := checkProjectRoot(resolved, g.ProjectRoots); err != nil {
			return "", err
		}
	}
	return resolved, nil
}
