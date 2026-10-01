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

func protectedLocation(path, home, goos string, profiles []editorRoot) bool {
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
	if insideResolved(path, filepath.Join(home, "Applications")) {
		return true
	}
	if goos == "windows" {
		local := filepath.Join(home, "AppData", "Local")
		if p := os.Getenv("LOCALAPPDATA"); filepath.IsAbs(p) {
			local = p
		}
		if insideResolved(path, filepath.Join(local, "Programs")) {
			return true
		}
	}
	return false
}

func insideResolved(path, root string) bool {
	if inside(path, root) {
		return true
	}
	real, err := filepath.EvalSymlinks(root)
	return err == nil && inside(path, real)
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

// ProtectedLocation is a cheap check for an installation/profile itself or
// anything inside it, including a configured project root inside an app.
func ProtectedLocation(path, home, goos string) bool {
	if protectedLocation(path, home, goos, editorRoots(home, goos)) {
		return true
	}
	for ancestor := filepath.Clean(path); ; ancestor = filepath.Dir(ancestor) {
		if IsApplicationDir(ancestor) {
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
	if ProtectedLocation(path, home, goos) {
		return fmt.Errorf("refusing to remove an application or editor data: %s", path)
	}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(parent, filepath.Base(path))
	}
	if ProtectedLocation(path, home, goos) {
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
	profiles := editorRoots(home, goos)
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
		if protectedLocation(p, home, goos, profiles) || entry.IsDir() && IsApplicationDir(p) {
			return fmt.Errorf("folder contains an application or editor data; preserving %s", path)
		}
		return nil
	})
}
