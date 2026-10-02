// Package safety protects application installations and editor state from
// filesystem cleanup, independently of scan categories and saved scan results.
package safety

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const inspectionLimit = 100000

var editorNames = []string{"Code", "Code - Insiders", "Code - OSS", "VSCodium", "Cursor", "Windsurf"}

func inside(path, root string) bool {
	path, root = strings.ToLower(filepath.Clean(path)), strings.ToLower(filepath.Clean(root))
	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

type editorRoot struct {
	path        string
	allowCaches bool
}

func editorRoots(home, goos string) []editorRoot {
	var roots []editorRoot
	add := func(path string, allowCaches bool) {
		roots = append(roots, editorRoot{path, allowCaches})
		if real, err := filepath.EvalSymlinks(path); err == nil && real != path {
			roots = append(roots, editorRoot{real, allowCaches})
		}
	}
	for _, name := range []string{".vscode", ".vscode-insiders", ".vscode-oss", ".vscode-server", ".vscode-server-insiders", ".cursor", ".windsurf"} {
		add(filepath.Join(home, name), false)
	}
	for _, name := range editorNames {
		add(filepath.Join(home, "Library", "Application Support", name), false)
		if goos == "windows" {
			roaming := filepath.Join(home, "AppData", "Roaming")
			if p := os.Getenv("APPDATA"); filepath.IsAbs(p) {
				roaming = p
			}
			add(filepath.Join(roaming, name), true)
		}
	}
	return roots
}

// protection holds what protectedLocation compares against, resolved once:
// CheckCleanup consults it for every entry of a folder it walks, and
// resolving symlinks per entry made large cache folders take minutes.
type protection struct {
	profiles []editorRoot
	appRoots []string // installation folders, plus their symlink targets
}

func newProtection(home, goos string) protection {
	p := protection{profiles: editorRoots(home, goos)}
	addApps := func(root string) {
		p.appRoots = append(p.appRoots, root)
		if real, err := filepath.EvalSymlinks(root); err == nil {
			p.appRoots = append(p.appRoots, real)
		}
	}
	addApps(filepath.Join(home, "Applications"))
	if goos == "windows" {
		local := filepath.Join(home, "AppData", "Local")
		if p := os.Getenv("LOCALAPPDATA"); filepath.IsAbs(p) {
			local = p
		}
		addApps(filepath.Join(local, "Programs"))
	}
	return p
}

func protectedLocation(path string, prot protection) bool {
	profiles := prot.profiles
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		name := strings.ToLower(part)
		if name == ".vscode" || strings.HasPrefix(name, ".vscode-") || name == ".cursor" || name == ".windsurf" ||
			name == "code-portable-data" || name == "code-insiders-portable-data" ||
			strings.HasSuffix(name, ".shipit") || name == "com.microsoft.vscode" || strings.HasPrefix(name, "com.microsoft.vscode.") ||
			name == "com.microsoft.vscodeinsiders" || strings.HasPrefix(name, "com.microsoft.vscodeinsiders.") ||
			name == "com.microsoft.vscodeexploration" || strings.HasPrefix(name, "com.microsoft.vscodeexploration.") {
			return true
		}
	}
	for _, root := range profiles {
		if !inside(path, root.path) {
			continue
		}
		// Windows' existing exact cache allowlist may keep cleaning these
		// disposable directories; never the profile, User, Backups or extensions.
		if root.allowCaches {
			allowed := false
			for _, cache := range []string{"Cache", "Code Cache", "GPUCache", "logs"} {
				if inside(path, filepath.Join(root.path, cache)) {
					allowed = true
					break
				}
			}
			if allowed {
				continue
			}
		}
		return true
	}
	for _, root := range prot.appRoots {
		if inside(path, root) {
			return true
		}
	}
	return false
}

func directory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func regular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// IsApplicationDir recognizes real macOS bundles and portable desktop editors.
// A reverse-DNS cache such as com.vendor.app is not itself an app bundle.
func IsApplicationDir(path string) bool {
	if strings.EqualFold(filepath.Ext(path), ".app") &&
		(directory(filepath.Join(path, "Contents")) || directory(filepath.Join(path, "Wrapper")) || regular(filepath.Join(path, "Info.plist"))) {
		return true
	}
	for _, name := range []string{"Code.exe", "Code - Insiders.exe", "VSCodium.exe", "Cursor.exe", "Windsurf.exe"} {
		if regular(filepath.Join(path, name)) {
			return true
		}
	}
	// Other Electron applications can use a renamed executable. Their
	// packaged application is still identifiable without reading user files.
	if regular(filepath.Join(path, "resources", "app", "product.json")) || regular(filepath.Join(path, "resources", "app.asar")) {
		return true
	}
	return false
}

// IsEditorDataDir recognizes VS Code's user-data layout even when the user
// chooses an arbitrary --user-data-dir, including inside cache/temp folders.
func IsEditorDataDir(path string) bool {
	user := filepath.Join(path, "User")
	if !directory(user) {
		return false
	}
	return regular(filepath.Join(user, "settings.json")) ||
		regular(filepath.Join(user, "keybindings.json")) ||
		regular(filepath.Join(user, "globalStorage", "state.vscdb")) ||
		directory(filepath.Join(user, "workspaceStorage"))
}

// markerNames are the lower-cased child names IsApplicationDir and
// IsEditorDataDir look for.
var markerNames = map[string]bool{
	"contents": true, "wrapper": true, "info.plist": true, "resources": true, "user": true,
	"code.exe": true, "code - insiders.exe": true, "vscodium.exe": true, "cursor.exe": true, "windsurf.exe": true,
}

// mayHoldApplication is a cheap pre-check for CheckCleanup's walk: one
// directory listing instead of IsApplicationDir's and IsEditorDataDir's
// individual probes. It only rules a folder out when none of their marker
// names is present; anything unreadable gets the full checks.
func mayHoldApplication(path string) bool {
	if strings.EqualFold(filepath.Ext(path), ".app") {
		return true
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return true
	}
	for _, e := range entries {
		if markerNames[strings.ToLower(e.Name())] {
			return true
		}
	}
	return false
}

func disposableProfileCache(path, root string, profiles []editorRoot) bool {
	for _, profile := range profiles {
		if !profile.allowCaches || !strings.EqualFold(filepath.Clean(root), filepath.Clean(profile.path)) {
			continue
		}
		for _, cache := range []string{"Cache", "Code Cache", "GPUCache", "logs"} {
			if inside(path, filepath.Join(root, cache)) {
				return true
			}
		}
	}
	return false
}

// ProtectedLocation is a cheap check for an installation/profile itself or
// anything inside it, including a configured project root inside an app.
func ProtectedLocation(path, home, goos string) bool {
	return protectedFrom(path, newProtection(home, goos))
}

func protectedFrom(path string, prot protection) bool {
	profiles := prot.profiles
	if protectedLocation(path, prot) {
		return true
	}
	for ancestor := filepath.Clean(path); ; ancestor = filepath.Dir(ancestor) {
		if IsApplicationDir(ancestor) {
			return true
		}
		if IsEditorDataDir(ancestor) && !disposableProfileCache(path, ancestor, profiles) {
			return true
		}
		if parent := filepath.Dir(ancestor); parent == ancestor {
			break
		}
	}
	return false
}

// CheckCleanup refuses both an application and a parent containing one.
// It inspects without following child symlinks. Errors, cancellation and a
// bounded inspection preserve the candidate instead of assuming it is safe.
func CheckCleanup(ctx context.Context, path, home, goos string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	prot := newProtection(home, goos)
	if protectedFrom(path, prot) {
		return fmt.Errorf("refusing to remove an application or editor data: %s", path)
	}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(parent, filepath.Base(path))
	}
	if protectedFrom(path, prot) {
		return fmt.Errorf("refusing to remove an application or editor data: %s", path)
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil // callers separately report disappeared scan items
	}
	if err != nil {
		return fmt.Errorf("cannot verify cleanup safety: %w", err)
	}
	if !info.IsDir() {
		return nil
	}
	visited := 0
	return filepath.WalkDir(path, func(p string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("cannot verify cleanup safety: %w", walkErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		visited++
		if visited > inspectionLimit {
			return fmt.Errorf("cannot safely inspect this large folder; preserving %s", path)
		}
		if protectedLocation(p, prot) || entry.IsDir() && mayHoldApplication(p) && (IsApplicationDir(p) || IsEditorDataDir(p)) {
			return fmt.Errorf("folder contains an application or editor data; preserving %s", path)
		}
		return nil
	})
}
