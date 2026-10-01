package scan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"transom/internal/safety"
)

// visitFunc is called for every entry the walker meets (concurrently —
// it must be safe for that). siblings are the names in the same
// directory. For directories, the return value says whether to descend.
type visitFunc func(dir string, siblings map[string]bool, name string, fi fs.FileInfo, depth int) bool

// walkTree walks roots in parallel (bounded by sem) up to maxDepth
// levels below each root, never following symlinks and skipping
// unreadable directories silently.
func walkTree(ctx context.Context, roots []string, maxDepth int, sem chan struct{}, prog *Progress, visit visitFunc) {
	var wg sync.WaitGroup
	home, _ := os.UserHomeDir()
	for _, r := range roots {
		if isBundle(filepath.Base(r)) || safety.ProtectedLocation(r, home, runtime.GOOS) {
			continue
		}
		if fi, err := os.Lstat(r); err != nil || !fi.IsDir() || isReparse(fi) || !safeReadPath(r) {
			continue
		}
		walkDir(ctx, r, 1, maxDepth, sem, prog, visit, &wg)
	}
	wg.Wait()
}

func walkDir(ctx context.Context, dir string, depth, maxDepth int, sem chan struct{}, prog *Progress, visit visitFunc, wg *sync.WaitGroup) {
	if ctx.Err() != nil || depth > maxDepth || safety.IsApplicationDir(dir) {
		return
	}
	entries, err := readDirUnsorted(dir)
	if err != nil {
		return
	}
	prog.AddScanned(int64(len(entries)))
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		names[e.Name()] = true
	}
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil || isReparse(fi) {
			continue
		}
		if !visit(dir, names, e.Name(), fi, depth) || !fi.IsDir() {
			continue
		}
		child := filepath.Join(dir, e.Name())
		select {
		case sem <- struct{}{}:
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				walkDir(ctx, child, depth+1, maxDepth, sem, prog, visit, wg)
			}()
		default:
			walkDir(ctx, child, depth+1, maxDepth, sem, prog, visit, wg)
		}
	}
}

// bundleExts are package directories that look like folders but are
// documents or apps: never look inside them (deleting a file inside a
// Photos library corrupts it).
var bundleExts = map[string]bool{
	".app": true, ".photoslibrary": true, ".photolibrary": true, ".aplibrary": true,
	".musiclibrary": true, ".tvlibrary": true, ".imovielibrary": true, ".fcpbundle": true,
	".logicx": true, ".band": true, ".bundle": true, ".framework": true, ".xcarchive": true,
	".sparsebundle": true, ".vmwarevm": true, ".pvm": true, ".utm": true, ".lrdata": true,
	".lrcat-data": true, ".kext": true, ".plugin": true,
}

func isBundle(name string) bool { return bundleExts[strings.ToLower(filepath.Ext(name))] }

// depKind reports whether name (a directory inside dir, whose entry
// names are siblings) is a dependency folder, and of which ecosystem.
// Only the unambiguous cases count: a folder called vendor or target is
// deps only next to the manifest that produces it AND carrying the
// marker that tool writes into it. (Laravel's public/vendor and
// lang/vendor hold published assets, not Composer packages — they have
// no vendor/autoload.php.)
func depKind(dir string, siblings map[string]bool, name string) (string, bool) {
	has := func(rel ...string) bool {
		_, err := os.Lstat(filepath.Join(append([]string{dir, name}, rel...)...))
		return err == nil
	}
	switch name {
	case "node_modules":
		return "node", siblings["package.json"]
	case "vendor":
		if siblings["composer.json"] && has("autoload.php") {
			return "composer", true
		}
		if siblings["go.mod"] && has("modules.txt") {
			return "go", true
		}
	case "target":
		if siblings["Cargo.toml"] && (has("CACHEDIR.TAG") || has("debug") || has("release")) {
			return "cargo", true
		}
		if siblings["pom.xml"] && (has("classes") || has("maven-status")) {
			return "maven", true
		}
	case ".venv":
		if has("pyvenv.cfg") {
			return "python", true
		}
	}
	return "", false
}

type depDir struct {
	path, project, kind string
	depth               int
}

// treeIndex is what a walk of the home tree yields, shared by the
// stale-deps and large-files scanners when their roots coincide.
type treeIndex struct {
	deps  []depDir
	large []sized // regular files at or over the large threshold
}

const (
	walkMaxDepth = 12 // how deep the home walk goes
	depsMaxDepth = 8  // dependency dirs deeper than this aren't reported
)

// homeIndex walks roots once per scan (memoized by roots) collecting
// dependency dirs and large files. Pruned: ~/Library, ~/.Trash, hidden
// directories, the Go module cache, package bundles, and the insides of
// dependency dirs.
func (e *Env) homeIndex(ctx context.Context, roots []string) *treeIndex {
	key := strings.Join(roots, "\x00")
	e.idxMu.Lock()
	defer e.idxMu.Unlock()
	if e.idx == nil {
		e.idx = map[string]*treeIndex{}
	}
	if idx, ok := e.idx[key]; ok {
		return idx
	}
	skip := map[string]bool{
		filepath.Join(e.Home, "Library"):                                  true,
		filepath.Join(e.Home, ".Trash"):                                   true,
		filepath.Join(e.Home, "go", "pkg"):                                true,
		filepath.Join(e.Home, "Applications"):                             true,
		filepath.Join(e.Home, "Pictures", "Photos Library.photoslibrary"): true,
	}
	if runtime.GOOS == "windows" {
		for _, p := range []string{filepath.Join(e.Home, "AppData"), windowsAppData(e.Home, "LOCALAPPDATA", "Local"), windowsAppData(e.Home, "APPDATA", "Roaming")} {
			skip[p] = true
		}
	}
	minLarge := int64(e.Opts.LargeMinMB) * 1000 * 1000
	idx := &treeIndex{}
	var mu sync.Mutex
	walkTree(ctx, roots, walkMaxDepth, e.sem, e.Prog, func(dir string, sib map[string]bool, name string, fi fs.FileInfo, depth int) bool {
		p := filepath.Join(dir, name)
		if fi.IsDir() {
			if kind, ok := depKind(dir, sib, name); ok {
				if depth <= depsMaxDepth {
					mu.Lock()
					idx.deps = append(idx.deps, depDir{path: p, project: dir, kind: kind, depth: depth})
					mu.Unlock()
				}
				return false
			}
			if name == "node_modules" || strings.HasPrefix(name, ".") || skip[p] || isBundle(name) || windowsProtectedTree(runtime.GOOS, name) {
				return false
			}
			return true
		}
		if !fi.Mode().IsRegular() {
			return false
		}
		st := statPath(p, fi)
		if st.alloc >= minLarge && !st.dataless {
			mu.Lock()
			idx.large = append(idx.large, sized{path: p, info: fi, size: st.alloc, newest: fi.ModTime()})
			mu.Unlock()
		}
		return false
	})
	if ctx.Err() == nil {
		e.idx[key] = idx
	}
	return idx
}

// tildify shows paths under home as ~/...
func tildify(home, p string) string {
	if p == home {
		return "~"
	}
	if rel, err := filepath.Rel(home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
		return "~" + string(filepath.Separator) + rel
	}
	return p
}

func windowsProtectedTree(goos, name string) bool {
	if goos != "windows" {
		return false
	}
	switch strings.ToLower(name) {
	case "appdata", "$recycle.bin", "system volume information", "windows", "program files", "program files (x86)", "programdata":
		return true
	}
	return false
}
